package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInitCreatesWorkspaceOutsideGit(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), root, []string{"init"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".agent", "workspace.yaml")); err != nil {
		t.Fatalf("workspace manifest missing: %v", err)
	}
}

func TestCLIAddListAndRun(t *testing.T) {
	root := newWorkspace(t)

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), root, []string{
		"add", "hello", "--description", "Say hello", "--", "printf", "hello",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("add exit=%d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runCLI(context.Background(), root, []string{"list", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list exit=%d stderr=%s", code, stderr.String())
	}
	var listed []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &listed); err != nil {
		t.Fatalf("invalid list JSON: %v\n%s", err, stdout.String())
	}
	if len(listed) != 1 || listed[0].Name != "hello" || listed[0].Description != "Say hello" {
		t.Fatalf("listed=%#v", listed)
	}

	stdout.Reset()
	stderr.Reset()
	code = runCLI(context.Background(), root, []string{"run", "hello"}, &stdout, &stderr)
	if code != 0 || stdout.String() != "hello" {
		t.Fatalf("run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCLIInspectJSONFromNestedDirectory(t *testing.T) {
	root := newWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"fixture","scripts":{"test":"true"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "src")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), nested, []string{"inspect", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("inspect exit=%d stderr=%s", code, stderr.String())
	}
	var result struct {
		Root string `json:"root"`
		Name string `json:"name"`
		Git  bool   `json:"git"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Root != root || result.Name != "fixture" || !result.Git {
		t.Fatalf("inspect=%#v", result)
	}
}

func TestCLITriggerFireAndInboxDrain(t *testing.T) {
	t.Setenv("AW_STATE_HOME", t.TempDir())
	root := newWorkspace(t)
	if code, _, stderr := runForTest(t, root, "add", "ci", "--", "printf failed >&2; exit 9"); code != 0 {
		t.Fatalf("add code=%d stderr=%s", code, stderr)
	}
	if code, _, stderr := runForTest(t, root, "trigger", "add", "after-push", "--match", "git push*", "--run", "ci", "--delivery", "defer"); code != 0 {
		t.Fatalf("trigger add code=%d stderr=%s", code, stderr)
	}
	code, output, stderr := runForTest(t, root, "trigger", "match", "--json", "--", "git", "push", "origin", "main")
	if code != 0 || !strings.Contains(output, `"name": "after-push"`) {
		t.Fatalf("trigger match code=%d stdout=%s stderr=%s", code, output, stderr)
	}
	code, _, stderr = runForTest(t, root, "trigger", "fire", "--session", "test-session", "--", "git", "push", "origin", "main")
	if code != 0 {
		t.Fatalf("trigger fire code=%d stderr=%s", code, stderr)
	}
	code, output, stderr = runForTest(t, root, "inbox", "list", "--session", "test-session", "--json")
	if code != 0 || !strings.Contains(output, `"exit_code": 9`) || !strings.Contains(output, `"trigger": "after-push"`) {
		t.Fatalf("inbox list code=%d stdout=%s stderr=%s", code, output, stderr)
	}
	code, output, stderr = runForTest(t, root, "inbox", "drain", "--session", "test-session", "--json")
	if code != 0 || !strings.Contains(output, `"exit_code": 9`) {
		t.Fatalf("inbox drain code=%d stdout=%s stderr=%s", code, output, stderr)
	}
	code, output, stderr = runForTest(t, root, "inbox", "list", "--session", "test-session", "--json")
	if code != 0 || strings.TrimSpace(output) != "[]" {
		t.Fatalf("inbox not empty: code=%d stdout=%s stderr=%s", code, output, stderr)
	}
}

func TestCLIInstallGitPackage(t *testing.T) {
	root := newWorkspace(t)
	repository := t.TempDir()
	runGitCommand(t, repository, "init")
	runGitCommand(t, repository, "config", "user.name", "Test")
	runGitCommand(t, repository, "config", "user.email", "test@example.com")
	packageDir := filepath.Join(repository, "packages", "demo")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\nversion: 0.1.0\ncommands:\n  ping:\n    run: printf git-pong\n"
	if err := os.WriteFile(filepath.Join(packageDir, "package.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repository, "add", ".")
	runGitCommand(t, repository, "commit", "-m", "package")
	revision := strings.TrimSpace(runGitCommand(t, repository, "rev-parse", "HEAD"))

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), root, []string{
		"install", repository, "--ref", revision, "--subdir", "packages/demo", "--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), revision) {
		t.Fatalf("install output does not include revision: %s", stdout.String())
	}
}

func TestCLIInstallLocalPackage(t *testing.T) {
	root := newWorkspace(t)
	source := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\nversion: 0.1.0\ncommands:\n  ping:\n    run: printf pong\n"
	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), root, []string{"install", source, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"name": "demo"`) {
		t.Fatalf("install output=%s", stdout.String())
	}

	stdout.Reset()
	code = runCLI(context.Background(), root, []string{"run", "demo:ping"}, &stdout, &stderr)
	if code != 0 || stdout.String() != "pong" {
		t.Fatalf("package run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCLIRejectsMalformedAdd(t *testing.T) {
	root := newWorkspace(t)
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), root, []string{"add", "broken"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "usage") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func runForTest(t *testing.T, root string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), root, args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func runGitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func newWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}
