package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWatchCISucceedsAfterRunsReachTerminalState(t *testing.T) {
	fakeBin := t.TempDir()
	state := filepath.Join(t.TempDir(), "calls")
	writeExecutable(t, filepath.Join(fakeBin, "gh"), `#!/bin/sh
set -eu
if [ "$1 $2" = "run list" ]; then
  count=0
  [ ! -f "$FAKE_STATE" ] || count=$(cat "$FAKE_STATE")
  count=$((count + 1))
  printf '%s' "$count" > "$FAKE_STATE"
  if [ "$count" -ge 2 ]; then
    printf '11\tCI\tcompleted\tsuccess\thttps://example/run/11\n'
  fi
  exit 0
fi
exit 2
`)

	result := runScript(t, "watch-ci.sh", fakeBin, state,
		"--repo", "owner/repo", "--sha", "abc123", "--timeout", "3", "--poll", "0")
	if result.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", result.exitCode, result.stderr, result.stdout)
	}
	if !strings.Contains(result.stdout, "GITHUB_CI PASSED") || !strings.Contains(result.stdout, "commit=abc123") {
		t.Fatalf("stdout=%s", result.stdout)
	}
}

func TestWatchCIUsesNewestRunPerWorkflow(t *testing.T) {
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "gh"), `#!/bin/sh
set -eu
if [ "$1 $2" = "run list" ]; then
  printf '22\tCI\tcompleted\tsuccess\thttps://example/run/22\n'
  printf '21\tCI\tcompleted\tfailure\thttps://example/run/21\n'
  exit 0
fi
exit 2
`)
	result := runScript(t, "watch-ci.sh", fakeBin, "",
		"--repo", "owner/repo", "--sha", "rerun123", "--timeout", "3", "--poll", "0")
	if result.exitCode != 0 || !strings.Contains(result.stdout, "GITHUB_CI PASSED") {
		t.Fatalf("exit=%d stderr=%s stdout=%s", result.exitCode, result.stderr, result.stdout)
	}
	if strings.Contains(result.stdout, "run=21") {
		t.Fatalf("superseded workflow attempt leaked into result: %s", result.stdout)
	}
}

func TestWatchCIFailureIncludesBoundedFailedLogs(t *testing.T) {
	fakeBin := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBin, "gh"), `#!/bin/sh
set -eu
if [ "$1 $2" = "run list" ]; then
  printf '12\tCI\tcompleted\tfailure\thttps://example/run/12\n'
  exit 0
fi
if [ "$1 $2" = "run view" ]; then
  i=1
  while [ "$i" -le 220 ]; do
    printf 'log-line-%03d\n' "$i"
    i=$((i + 1))
  done
  exit 0
fi
exit 2
`)

	result := runScript(t, "watch-ci.sh", fakeBin, "",
		"--repo", "owner/repo", "--sha", "bad123", "--timeout", "3", "--poll", "0")
	if result.exitCode != 1 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", result.exitCode, result.stderr, result.stdout)
	}
	if !strings.Contains(result.stdout, "GITHUB_CI FAILED") || !strings.Contains(result.stdout, "log-line-220") {
		t.Fatalf("stdout=%s", result.stdout)
	}
	if strings.Contains(result.stdout, "log-line-001") {
		t.Fatal("failed log output was not bounded to its tail")
	}
}

func TestWatchPRReturnsNewChangesRequestedReview(t *testing.T) {
	fakeBin := t.TempDir()
	state := filepath.Join(t.TempDir(), "calls")
	writeExecutable(t, filepath.Join(fakeBin, "gh"), `#!/bin/sh
set -eu
case "$*" in
  *'--jq .number'*) printf '42\n'; exit 0 ;;
  *'@tsv'*)
    count=0
    [ ! -f "$FAKE_STATE" ] || count=$(cat "$FAKE_STATE")
    count=$((count + 1))
    printf '%s' "$count" > "$FAKE_STATE"
    if [ "$count" -eq 1 ]; then
      printf '42\thttps://example/pr/42\t\t0\t0\n'
    else
      printf '42\thttps://example/pr/42\tCHANGES_REQUESTED\t1\t0\n'
    fi
    exit 0
    ;;
  *'review:'*) printf 'review: alice [CHANGES_REQUESTED] Fix the timeout path\n'; exit 0 ;;
esac
exit 2
`)

	result := runScript(t, "watch-pr.sh", fakeBin, state,
		"--repo", "owner/repo", "--timeout", "3", "--poll", "0")
	if result.exitCode != 1 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", result.exitCode, result.stderr, result.stdout)
	}
	if !strings.Contains(result.stdout, "GITHUB_PR CHANGES_REQUESTED") ||
		!strings.Contains(result.stdout, "Fix the timeout path") {
		t.Fatalf("stdout=%s", result.stdout)
	}
}

type scriptResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runScript(t *testing.T, script, fakeBin, state string, args ...string) scriptResult {
	t.Helper()
	scriptPath, err := filepath.Abs(script)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(scriptPath, args...)
	command.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_STATE="+state)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	result := scriptResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		result.exitCode = exitError.ExitCode()
		return result
	}
	t.Fatalf("run %s: %v", script, err)
	return scriptResult{}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
