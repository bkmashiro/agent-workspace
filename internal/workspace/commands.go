package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func Init(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	path := filepath.Join(root, ".agent", "workspace.yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("workspace already initialized at %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return saveManifest(root, Manifest{Version: 1, Commands: make(map[string]Command)})
}

func AddCommand(root, name string, command Command) error {
	if err := validateName(name); err != nil {
		return err
	}
	if strings.TrimSpace(command.Run) == "" {
		return errors.New("command cannot be empty")
	}
	manifest, err := loadManifest(root)
	if err != nil {
		return err
	}
	if manifest.Commands == nil {
		manifest.Commands = make(map[string]Command)
	}
	manifest.Commands[name] = command
	return saveManifest(root, manifest)
}

func Catalog(root string) (map[string]Command, error) {
	inspection, err := Inspect(root)
	if err != nil {
		return nil, err
	}
	catalog := inspection.Commands

	packageRoot := filepath.Join(root, ".agent", "packages")
	lock, err := loadLockfile(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(packageRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		packagePath := filepath.Join(packageRoot, entry.Name())
		locked, ok := lock.Packages[entry.Name()]
		if !ok {
			return nil, fmt.Errorf("installed package %q is missing from workspace.lock", entry.Name())
		}
		actualDigest, err := digestDirectory(packagePath)
		if err != nil {
			return nil, fmt.Errorf("verify package %s: %w", entry.Name(), err)
		}
		if actualDigest != locked.Digest {
			return nil, fmt.Errorf("installed package %q failed digest verification: got %s, want %s", entry.Name(), actualDigest, locked.Digest)
		}
		data, err := os.ReadFile(filepath.Join(packagePath, "package.yaml"))
		if err != nil {
			return nil, fmt.Errorf("read package %s: %w", entry.Name(), err)
		}
		var pkg PackageManifest
		if err := yaml.Unmarshal(data, &pkg); err != nil {
			return nil, fmt.Errorf("parse package %s: %w", entry.Name(), err)
		}
		if pkg.Name != entry.Name() {
			return nil, fmt.Errorf("package directory %q does not match manifest name %q", entry.Name(), pkg.Name)
		}
		for name, command := range pkg.Commands {
			command.Source = "package:" + pkg.Name
			catalog[pkg.Name+":"+name] = command
		}
	}

	manifest, err := loadManifest(root)
	if err != nil {
		return nil, err
	}
	for name, command := range manifest.Commands {
		command.Source = "workspace"
		catalog[name] = command
	}
	return catalog, nil
}

func SortedCatalog(catalog map[string]Command) []struct {
	Name string `json:"name"`
	Command
} {
	names := make([]string, 0, len(catalog))
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]struct {
		Name string `json:"name"`
		Command
	}, 0, len(names))
	for _, name := range names {
		result = append(result, struct {
			Name string `json:"name"`
			Command
		}{Name: name, Command: catalog[name]})
	}
	return result
}

func Run(ctx context.Context, root, name string, args []string, stdout, stderr io.Writer) (RunResult, error) {
	catalog, err := Catalog(root)
	if err != nil {
		return RunResult{}, err
	}
	command, ok := catalog[name]
	if !ok {
		return RunResult{}, fmt.Errorf("unknown command %q", name)
	}
	fullCommand := command.Run
	for _, arg := range args {
		fullCommand += " " + shellQuote(arg)
	}

	result := RunResult{Command: name}
	if command.Snapshot == "git" {
		result.TestedState, err = GitFingerprint(ctx, root)
		if err != nil {
			return RunResult{}, fmt.Errorf("capture git snapshot: %w", err)
		}
	} else if command.Snapshot != "" {
		return RunResult{}, fmt.Errorf("unsupported snapshot mode %q", command.Snapshot)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", fullCommand)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-lc", fullCommand)
	}
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "AW_WORKSPACE_ROOT="+root, "AW_COMMAND="+name)
	if strings.HasPrefix(command.Source, "package:") {
		packageName := strings.TrimPrefix(command.Source, "package:")
		cmd.Env = append(cmd.Env, "AW_PACKAGE_DIR="+filepath.Join(root, ".agent", "packages", packageName))
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return RunResult{}, runErr
		}
	}

	if command.Snapshot == "git" {
		result.CurrentState, err = GitFingerprint(ctx, root)
		if err != nil {
			return RunResult{}, fmt.Errorf("capture current git state: %w", err)
		}
		result.Stale = result.TestedState != result.CurrentState
		if result.Stale && result.ExitCode == 0 {
			result.ExitCode = ExitStale
		}
	}
	return result, nil
}

func GitFingerprint(ctx context.Context, root string) (string, error) {
	hash := bytes.NewBuffer(nil)
	commands := [][]string{
		{"rev-parse", "HEAD"},
		{"diff", "--binary", "HEAD", "--"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	}
	var untracked []byte
	for index, args := range commands {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = root
		output, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		if index == 2 {
			untracked = output
		} else {
			hash.Write(output)
			hash.WriteByte(0)
		}
	}
	paths := bytes.Split(untracked, []byte{0})
	sort.Slice(paths, func(i, j int) bool { return bytes.Compare(paths[i], paths[j]) < 0 })
	for _, raw := range paths {
		if len(raw) == 0 {
			continue
		}
		path := string(raw)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}
		hash.WriteString(path)
		hash.WriteByte(0)
		hash.Write(data)
		hash.WriteByte(0)
	}
	return digestBytes(hash.Bytes()), nil
}

func loadManifest(root string) (Manifest, error) {
	path := filepath.Join(root, ".agent", "workspace.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{Version: 1, Commands: make(map[string]Command)}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if manifest.Version != 1 {
		return Manifest{}, fmt.Errorf("unsupported workspace manifest version %d", manifest.Version)
	}
	return manifest, nil
}

func saveManifest(root string, manifest Manifest) error {
	directory := filepath.Join(root, ".agent")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(directory, "workspace.yaml"), data, 0o644)
}

func validateName(name string) error {
	if name == "" || strings.ContainsAny(name, " \\/") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid command or package name %q", name)
	}
	return nil
}

func shellQuote(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return `'` + strings.ReplaceAll(value, `'`, `'"'"'`) + `'`
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
