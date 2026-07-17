package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func InstallPackage(root, source string) (InstalledPackage, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return InstalledPackage{}, err
	}
	data, err := os.ReadFile(filepath.Join(source, "package.yaml"))
	if err != nil {
		return InstalledPackage{}, fmt.Errorf("read package.yaml: %w", err)
	}
	var manifest PackageManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return InstalledPackage{}, fmt.Errorf("parse package.yaml: %w", err)
	}
	if err := validateName(manifest.Name); err != nil {
		return InstalledPackage{}, err
	}
	if manifest.Version == "" {
		return InstalledPackage{}, errors.New("package version is required")
	}
	if len(manifest.Commands) == 0 {
		return InstalledPackage{}, errors.New("package must declare at least one command")
	}
	for name, command := range manifest.Commands {
		if err := validateName(name); err != nil {
			return InstalledPackage{}, err
		}
		if strings.TrimSpace(command.Run) == "" {
			return InstalledPackage{}, fmt.Errorf("package command %q is empty", name)
		}
	}

	digest, err := digestDirectory(source)
	if err != nil {
		return InstalledPackage{}, err
	}
	destination := filepath.Join(root, ".agent", "packages", manifest.Name)
	if _, err := os.Stat(destination); err == nil {
		return InstalledPackage{}, fmt.Errorf("package %q is already installed", manifest.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstalledPackage{}, err
	}
	if err := copyDirectory(source, destination); err != nil {
		os.RemoveAll(destination)
		return InstalledPackage{}, err
	}

	installed := InstalledPackage{
		Name:      manifest.Name,
		Version:   manifest.Version,
		Source:    source,
		Digest:    digest,
		Installed: time.Now().UTC().Truncate(time.Second),
	}
	if err := updateLockfile(root, installed); err != nil {
		os.RemoveAll(destination)
		return InstalledPackage{}, err
	}
	return installed, nil
}

func loadLockfile(root string) (Lockfile, error) {
	path := filepath.Join(root, ".agent", "workspace.lock")
	lock := Lockfile{Version: 1, Packages: make(map[string]InstalledPackage)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return lock, nil
	}
	if err != nil {
		return Lockfile{}, err
	}
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return Lockfile{}, fmt.Errorf("parse lockfile: %w", err)
	}
	if lock.Version != 1 {
		return Lockfile{}, fmt.Errorf("unsupported lockfile version %d", lock.Version)
	}
	if lock.Packages == nil {
		lock.Packages = make(map[string]InstalledPackage)
	}
	return lock, nil
}

func updateLockfile(root string, installed InstalledPackage) error {
	path := filepath.Join(root, ".agent", "workspace.lock")
	lock, err := loadLockfile(root)
	if err != nil {
		return err
	}
	if lock.Packages == nil {
		lock.Packages = make(map[string]InstalledPackage)
	}
	lock.Packages[installed.Name] = installed
	data, err := yaml.Marshal(lock)
	if err != nil {
		return err
	}
	return atomicWrite(path, data, 0o644)
}

func digestDirectory(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package contains unsupported symlink %s", relative)
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, relative := range paths {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return "", err
		}
		io.WriteString(hash, filepath.ToSlash(relative))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func copyDirectory(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package contains unsupported symlink %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
