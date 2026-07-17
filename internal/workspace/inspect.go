package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

var rootMarkers = []string{
	".agent/workspace.yaml",
	".git",
	"package.json",
	"pyproject.toml",
	"Cargo.toml",
	"go.mod",
	"Taskfile.yml",
	"justfile",
}

func FindRoot(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(current)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		current = filepath.Dir(current)
	}
	for {
		for _, marker := range rootMarkers {
			if _, err := os.Stat(filepath.Join(current, marker)); err == nil {
				return current, nil
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no workspace root found from %s", start)
		}
		current = parent
	}
}

func Inspect(root string) (Inspection, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Inspection{}, err
	}
	result := Inspection{Root: root, Name: filepath.Base(root), Commands: map[string]Command{}}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		result.Git = true
		result.Detected = append(result.Detected, "git")
	}

	packagePath := filepath.Join(root, "package.json")
	if data, err := os.ReadFile(packagePath); err == nil {
		var pkg struct {
			Name    string            `json:"name"`
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return Inspection{}, fmt.Errorf("parse package.json: %w", err)
		}
		packageManager := "npm"
		if _, err := os.Stat(filepath.Join(root, "pnpm-lock.yaml")); err == nil {
			packageManager = "pnpm"
		} else if _, err := os.Stat(filepath.Join(root, "yarn.lock")); err == nil {
			packageManager = "yarn"
		}
		result.Detected = append(result.Detected, packageManager)
		if pkg.Name != "" {
			result.Name = pkg.Name
		}
		names := make([]string, 0, len(pkg.Scripts))
		for name := range pkg.Scripts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			result.Commands[packageManager+":"+name] = Command{
				Run:         packageManager + " run " + name,
				Description: "package.json script",
				Source:      "package.json",
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Inspection{}, err
	}

	detectedFiles := []struct {
		path string
		name string
	}{
		{".github/workflows", "github-actions"},
		{"wrangler.toml", "cloudflare"},
		{"wrangler.jsonc", "cloudflare"},
		{".vercel/project.json", "vercel"},
		{"netlify.toml", "netlify"},
		{"pyproject.toml", "python"},
		{"Cargo.toml", "cargo"},
		{"go.mod", "go"},
		{"Taskfile.yml", "task"},
		{"justfile", "just"},
	}
	seen := make(map[string]bool)
	for _, name := range result.Detected {
		seen[name] = true
	}
	for _, candidate := range detectedFiles {
		if _, err := os.Stat(filepath.Join(root, candidate.path)); err == nil && !seen[candidate.name] {
			result.Detected = append(result.Detected, candidate.name)
			seen[candidate.name] = true
		}
	}
	return result, nil
}
