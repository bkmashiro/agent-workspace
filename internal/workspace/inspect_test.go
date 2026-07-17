package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRootFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "src", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("FindRoot() = %q, want %q", got, root)
	}
}

func TestInspectUsesPnpmWhenLockfileExists(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"vitest"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Commands["pnpm:test"].Run != "pnpm run test" {
		t.Fatalf("pnpm:test = %#v", got.Commands["pnpm:test"])
	}
	if _, exists := got.Commands["npm:test"]; exists {
		t.Fatal("npm:test should not be exposed when pnpm-lock.yaml exists")
	}
}

func TestInspectDetectsGitAndPackageScripts(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	packageJSON := `{"name":"demo","scripts":{"test":"vitest","build":"vite build"}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(packageJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" || !got.Git {
		t.Fatalf("Inspect() = %#v", got)
	}
	if len(got.Detected) != 2 || got.Detected[0] != "git" || got.Detected[1] != "npm" {
		t.Fatalf("Detected = %#v, want [git npm]", got.Detected)
	}
	if got.Commands["npm:test"].Run != "npm run test" {
		t.Fatalf("npm:test = %#v", got.Commands["npm:test"])
	}
}
