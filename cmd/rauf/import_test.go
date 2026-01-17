package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunImportSpecfirstSingleFileSlug(t *testing.T) {
	root := t.TempDir()
	chdirTemp(t, root)

	specfirst := filepath.Join(root, ".specfirst")
	state := `{"stage_outputs":{"requirements":{"prompt_hash":"abc","files":["user-auth.md"]}}}`
	if err := os.MkdirAll(filepath.Join(specfirst, "artifacts", "requirements", "abc"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specfirst, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}
	artifactPath := filepath.Join(specfirst, "artifacts", "requirements", "abc", "user-auth.md")
	if err := os.WriteFile(artifactPath, []byte("# User Auth\n\nBody\n"), 0o644); err != nil {
		t.Fatalf("write artifact failed: %v", err)
	}

	cfg := modeConfig{
		importStage: "requirements",
		importDir:   specfirst,
	}
	if err := runImportSpecfirst(cfg); err != nil {
		t.Fatalf("import failed: %v", err)
	}

	specPath := filepath.Join(root, "specs", "user-auth.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec failed: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "artifact: abc") {
		t.Fatalf("expected artifact hash in spec")
	}
}

func TestRunImportSpecfirstMultipleFilesFallbackSlug(t *testing.T) {
	root := t.TempDir()
	chdirTemp(t, root)

	specfirst := filepath.Join(root, ".specfirst")
	state := `{"stage_outputs":{"requirements":{"prompt_hash":"def","files":["one.md","two.md"]}}}`
	if err := os.MkdirAll(filepath.Join(specfirst, "artifacts", "requirements", "def"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specfirst, "state.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write state failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specfirst, "artifacts", "requirements", "def", "one.md"), []byte("# One\n"), 0o644); err != nil {
		t.Fatalf("write artifact failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specfirst, "artifacts", "requirements", "def", "two.md"), []byte("# Two\n"), 0o644); err != nil {
		t.Fatalf("write artifact failed: %v", err)
	}

	cfg := modeConfig{
		importStage: "requirements",
		importDir:   specfirst,
	}
	if err := runImportSpecfirst(cfg); err != nil {
		t.Fatalf("import failed: %v", err)
	}

	specPath := filepath.Join(root, "specs", "requirements.md")
	if _, err := os.Stat(specPath); err != nil {
		t.Fatalf("expected spec at %s", specPath)
	}
}
