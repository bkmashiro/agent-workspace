package workspace

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallGitPackageFromPinnedRefAndSubdirectory(t *testing.T) {
	root := t.TempDir()
	repository := t.TempDir()
	runGit(t, repository, "init")
	runGit(t, repository, "config", "user.email", "test@example.com")
	runGit(t, repository, "config", "user.name", "Test")
	packageDir := filepath.Join(repository, "packages", "demo")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\nversion: 0.2.0\ncommands:\n  ping:\n    run: printf pinned\n"
	if err := os.WriteFile(filepath.Join(packageDir, "package.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", ".")
	runGit(t, repository, "commit", "-m", "package")
	revision := gitOutput(t, repository, "rev-parse", "HEAD")

	installed, err := InstallGitPackage(root, repository, revision, "packages/demo")
	if err != nil {
		t.Fatal(err)
	}
	if installed.Name != "demo" || installed.Revision != revision || installed.Source != repository {
		t.Fatalf("installed = %#v", installed)
	}
	catalog, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if catalog["demo:ping"].Run != "printf pinned" {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestInstallGitPackageRejectsEscapingSubdirectory(t *testing.T) {
	_, err := InstallGitPackage(t.TempDir(), t.TempDir(), "HEAD", "../outside")
	if err == nil {
		t.Fatal("expected escaping package subdirectory to be rejected")
	}
}

func TestInstallLocalPackageAddsNamespacedCommandsAndLock(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "github")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: github\nversion: 0.1.0\ncommands:\n  ci:\n    run: \"$AW_PACKAGE_DIR/ci.sh\"\n    description: Watch CI for the current commit\n"
	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "ci.sh"), []byte("#!/bin/sh\nprintf ci-ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	installed, err := InstallPackage(root, source)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Name != "github" || installed.Digest == "" {
		t.Fatalf("installed = %#v", installed)
	}

	catalog, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := catalog["github:ci"]
	if !ok || got.Run != "$AW_PACKAGE_DIR/ci.sh" || got.Source != "package:github" {
		t.Fatalf("github:ci = %#v, present=%v", got, ok)
	}

	var output bytes.Buffer
	result, err := Run(context.Background(), root, "github:ci", nil, &output, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || output.String() != "ci-ok" {
		t.Fatalf("package command result=%#v output=%q", result, output.String())
	}

	lockPath := filepath.Join(root, ".agent", "workspace.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lockfile missing: %v", err)
	}
}

func TestCatalogRejectsModifiedInstalledPackage(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\nversion: 0.1.0\ncommands:\n  ping:\n    run: printf pong\n"
	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPackage(root, source); err != nil {
		t.Fatal(err)
	}
	installedManifest := filepath.Join(root, ".agent", "packages", "demo", "package.yaml")
	if err := os.WriteFile(installedManifest, []byte(manifest+"# modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Catalog(root); err == nil {
		t.Fatal("expected modified package digest to be rejected")
	}
}

func TestInstallPackageRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "unsafe")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: unsafe\nversion: 0.1.0\ncommands:\n  run:\n    run: ./script\n"
	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/tmp/outside", filepath.Join(source, "script")); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPackage(root, source); err == nil {
		t.Fatal("expected symlink package to be rejected")
	}
}

func TestSnapshotCommandKeepsSuccessfulResultValidWhenTreeIsUnchanged(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("stable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	if err := AddCommand(root, "check", Command{Run: "test -f tracked.txt", Snapshot: "git"}); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), root, "check", nil, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stale || result.ExitCode != 0 || result.TestedState != result.CurrentState {
		t.Fatalf("result = %#v", result)
	}
}

func TestSnapshotCommandReportsStaleWhenWorkspaceChanges(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")

	command := `printf 'after\n' > tracked.txt`
	if err := AddCommand(root, "mutating-check", Command{Run: command, Snapshot: "git"}); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), root, "mutating-check", nil, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stale || result.ExitCode != ExitStale {
		t.Fatalf("result = %#v, want stale exit %d", result, ExitStale)
	}
	if result.TestedState == "" || result.CurrentState == "" || result.TestedState == result.CurrentState {
		t.Fatalf("invalid state stamps: %#v", result)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
