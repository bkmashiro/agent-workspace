package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func AddTrigger(root, name string, trigger Trigger) error {
	if err := validateName(name); err != nil {
		return err
	}
	trigger, err := normalizeTrigger(trigger)
	if err != nil {
		return err
	}
	manifest, err := loadManifest(root)
	if err != nil {
		return err
	}
	if manifest.Triggers == nil {
		manifest.Triggers = make(map[string]Trigger)
	}
	manifest.Triggers[name] = trigger
	return saveManifest(root, manifest)
}

func TriggerCatalog(root string) (map[string]Trigger, error) {
	catalog := make(map[string]Trigger)
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
			return nil, err
		}
		var pkg PackageManifest
		if err := yaml.Unmarshal(data, &pkg); err != nil {
			return nil, fmt.Errorf("parse package %s: %w", entry.Name(), err)
		}
		for name, trigger := range pkg.Triggers {
			trigger, err = normalizeTrigger(trigger)
			if err != nil {
				return nil, fmt.Errorf("package %s trigger %s: %w", pkg.Name, name, err)
			}
			trigger.Name = pkg.Name + ":" + name
			trigger.Source = "package:" + pkg.Name
			if !strings.Contains(trigger.Run, ":") {
				trigger.Run = pkg.Name + ":" + trigger.Run
			}
			catalog[trigger.Name] = trigger
		}
	}

	manifest, err := loadManifest(root)
	if err != nil {
		return nil, err
	}
	for name, trigger := range manifest.Triggers {
		trigger, err = normalizeTrigger(trigger)
		if err != nil {
			return nil, fmt.Errorf("workspace trigger %s: %w", name, err)
		}
		trigger.Name = name
		trigger.Source = "workspace"
		catalog[name] = trigger
	}
	return catalog, nil
}

func SortedTriggers(catalog map[string]Trigger) []Trigger {
	names := make([]string, 0, len(catalog))
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]Trigger, 0, len(names))
	for _, name := range names {
		result = append(result, catalog[name])
	}
	return result
}

func MatchTriggers(root, command string) ([]Trigger, error) {
	catalog, err := TriggerCatalog(root)
	if err != nil {
		return nil, err
	}
	normalized := strings.Join(strings.Fields(command), " ")
	var matched []Trigger
	for _, trigger := range catalog {
		ok, err := wildcardMatch(trigger.Match, normalized)
		if err != nil {
			return nil, fmt.Errorf("trigger %s: %w", trigger.Name, err)
		}
		if ok {
			matched = append(matched, trigger)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Name < matched[j].Name })
	return matched, nil
}

type TriggerFireResult struct {
	Matched  int `json:"matched"`
	Deferred int `json:"deferred"`
	ExitCode int `json:"exit_code"`
}

func FireTriggers(ctx context.Context, root, observedCommand, session string, stdout, stderr io.Writer) TriggerFireResult {
	matched, err := MatchTriggers(root, observedCommand)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return TriggerFireResult{ExitCode: 2}
	}
	fireResult := TriggerFireResult{Matched: len(matched)}
	for _, trigger := range matched {
		if trigger.Delivery == "defer" && strings.TrimSpace(session) == "" {
			fmt.Fprintf(stderr, "trigger %s requires a session for deferred delivery\n", trigger.Name)
			fireResult.ExitCode = 2
		}
	}
	if fireResult.ExitCode != 0 {
		return fireResult
	}
	for _, trigger := range matched {
		var commandOutput, commandErrors bytes.Buffer
		result, runErr := Run(ctx, root, trigger.Run, nil, &commandOutput, &commandErrors)
		exitCode := result.ExitCode
		if runErr != nil {
			exitCode = 2
			fmt.Fprintln(&commandErrors, runErr)
		}
		if trigger.Delivery == "defer" {
			event := InboxEvent{
				Source:      "trigger:" + trigger.Name,
				Trigger:     trigger.Name,
				Command:     trigger.Run,
				Fingerprint: result.TestedState,
				ExitCode:    exitCode,
				Stdout:      commandOutput.String(),
				Stderr:      commandErrors.String(),
			}
			if _, err := EnqueueEvent(root, session, event); err != nil {
				fmt.Fprintf(stderr, "defer trigger %s: %v\n", trigger.Name, err)
				fireResult.ExitCode = 2
				continue
			}
			fireResult.Deferred++
			fmt.Fprintf(stdout, "Deferred trigger %s result for session %s\n", trigger.Name, session)
			continue
		}
		if commandOutput.Len() > 0 {
			_, _ = io.Copy(stdout, &commandOutput)
		}
		if commandErrors.Len() > 0 {
			_, _ = io.Copy(stderr, &commandErrors)
		}
		if exitCode != 0 && fireResult.ExitCode == 0 {
			fireResult.ExitCode = exitCode
		}
	}
	return fireResult
}

func normalizeTrigger(trigger Trigger) (Trigger, error) {
	if trigger.Delivery == "" {
		trigger.Delivery = "defer"
	}
	if err := validateTrigger(trigger); err != nil {
		return Trigger{}, err
	}
	return trigger, nil
}

func validateTrigger(trigger Trigger) error {
	if strings.TrimSpace(trigger.Match) == "" {
		return errors.New("trigger match is required")
	}
	if strings.TrimSpace(trigger.Run) == "" {
		return errors.New("trigger run command is required")
	}
	if trigger.Delivery != "defer" && trigger.Delivery != "wake" {
		return fmt.Errorf("unsupported trigger delivery %q", trigger.Delivery)
	}
	_, err := wildcardRegex(trigger.Match)
	return err
}

func wildcardMatch(pattern, value string) (bool, error) {
	expression, err := wildcardRegex(strings.Join(strings.Fields(pattern), " "))
	if err != nil {
		return false, err
	}
	return expression.MatchString(value), nil
}

func wildcardRegex(pattern string) (*regexp.Regexp, error) {
	var expression strings.Builder
	expression.WriteString("^")
	for _, character := range pattern {
		switch character {
		case '*':
			expression.WriteString(".*")
		case '?':
			expression.WriteString(".")
		default:
			expression.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	expression.WriteString("$")
	return regexp.Compile(expression.String())
}
