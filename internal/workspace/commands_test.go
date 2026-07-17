package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAddCommandPersistsAndCatalogListsIt(t *testing.T) {
	root := t.TempDir()

	err := AddCommand(root, "verify", Command{
		Run:         "printf verified",
		Description: "Run repository verification",
	})
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := catalog["verify"]
	if !ok {
		t.Fatalf("verify missing from catalog: %#v", catalog)
	}
	if got.Description != "Run repository verification" || got.Source != "workspace" {
		t.Fatalf("verify = %#v", got)
	}
}

func TestRunExecutesWorkspaceCommandAtRoot(t *testing.T) {
	root := t.TempDir()
	if err := AddCommand(root, "where", Command{Run: "pwd"}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), root, "where", nil, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit = %d, stderr=%s", result.ExitCode, stderr.String())
	}
	resolved, _ := filepath.EvalSymlinks(root)
	output, _ := filepath.EvalSymlinks(filepath.Clean(string(bytes.TrimSpace(stdout.Bytes()))))
	if output != resolved {
		t.Fatalf("pwd = %q, want %q", output, resolved)
	}
}

func TestRunUnknownCommandFails(t *testing.T) {
	_, err := Run(context.Background(), t.TempDir(), "missing", nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
}

func TestCatalogImportsPackageJSONScripts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"lint":"eslint ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := Catalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if catalog["npm:lint"].Run != "npm run lint" {
		t.Fatalf("npm:lint = %#v", catalog["npm:lint"])
	}
}
