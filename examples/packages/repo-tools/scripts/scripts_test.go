package scripts_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummaryHandlesRepositoryWithoutCommits(t *testing.T) {
	root := t.TempDir()
	git := exec.Command("git", "init", "-q")
	git.Dir = root
	if output, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	script, err := filepath.Abs("summary.sh")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(script)
	command.Env = append(os.Environ(), "AW_WORKSPACE_ROOT="+root)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("summary: %v\n%s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "head=unborn") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}
