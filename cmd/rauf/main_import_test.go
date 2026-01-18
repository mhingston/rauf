package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunImportSpecfirst(t *testing.T) {
	// Setup temp workspace
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	// Create state.json
	stateContent := `{
		"stage_outputs": {
			"requirements": {
				"prompt_hash": "hash123",
				"files": ["reqs.md", "diagram.mermaid"]
			}
		}
	}`
	os.WriteFile("state.json", []byte(stateContent), 0o644)

	// Create artifacts directory structure
	// root/artifacts/<stage>/<hash>/<file>
	// Here root is importDir, which we will set to ".".
	artifactDir := filepath.Join("artifacts", "requirements", "hash123")
	os.MkdirAll(artifactDir, 0o755)

	os.WriteFile(filepath.Join(artifactDir, "reqs.md"), []byte("# Requirements\n\nContent here."), 0o644)
	os.WriteFile(filepath.Join(artifactDir, "diagram.mermaid"), []byte("graph TD; A-->B;"), 0o644)

	// Test success
	t.Run("success", func(t *testing.T) {
		cfg := modeConfig{
			importDir:   ".",
			importStage: "requirements",
			importSlug:  "my-slug",
		}
		if err := runImportSpecfirst(cfg); err != nil {
			t.Fatalf("runImportSpecfirst failed: %v", err)
		}

		specPath := "specs/my-slug.md"
		content, err := os.ReadFile(specPath)
		if err != nil {
			t.Fatalf("spec file not created: %v", err)
		}
		sContent := string(content)
		if !strings.Contains(sContent, "# Requirements") {
			t.Error("missing reqs content")
		}
		if !strings.Contains(sContent, "graph TD;") {
			t.Error("missing diagram content")
		}
	})

	// Test missing stage
	t.Run("missing stage", func(t *testing.T) {
		cfg := modeConfig{
			importDir:   ".",
			importStage: "missing-stage",
		}
		if err := runImportSpecfirst(cfg); err == nil {
			t.Error("expected error for missing stage")
		}
	})

	// Test file exists
	t.Run("file exists", func(t *testing.T) {
		// Create file first
		os.MkdirAll("specs", 0o755)
		os.WriteFile("specs/collision.md", []byte("exists"), 0o644)

		cfg := modeConfig{
			importDir:   ".",
			importStage: "requirements",
			importSlug:  "collision",
		}
		if err := runImportSpecfirst(cfg); err == nil {
			t.Error("expected error for existing file")
		}

		// Force overwrite
		cfg.importForce = true
		if err := runImportSpecfirst(cfg); err != nil {
			t.Fatalf("expected success with force, got %v", err)
		}
	})

	// Test traversal escape attempt
	t.Run("traversal escape", func(t *testing.T) {
		// Modify state to include traversal
		stateContent := `{
			"stage_outputs": {
				"bad": {
					"prompt_hash": "hash123",
					"files": ["../escaped.md"]
				}
			}
		}`
		os.WriteFile("state.json", []byte(stateContent), 0o644)

		cfg := modeConfig{
			importDir:   ".",
			importStage: "bad",
			importSlug:  "bad",
		}
		if err := runImportSpecfirst(cfg); err == nil {
			t.Error("expected error for traversal")
		}
	})
}

func TestParseImportArgs(t *testing.T) {
	t.Run("all flags", func(t *testing.T) {
		cfg := modeConfig{}
		args := []string{"--stage", "build", "--slug", "my-feature", "--specfirst-dir", "/custom/dir", "--force"}
		err := parseImportArgs(args, &cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.importStage != "build" {
			t.Errorf("importStage = %q, want 'build'", cfg.importStage)
		}
		if cfg.importSlug != "my-feature" {
			t.Errorf("importSlug = %q, want 'my-feature'", cfg.importSlug)
		}
		if cfg.importDir != "/custom/dir" {
			t.Errorf("importDir = %q, want '/custom/dir'", cfg.importDir)
		}
		if !cfg.importForce {
			t.Error("importForce should be true")
		}
	})

	t.Run("missing stage value", func(t *testing.T) {
		cfg := modeConfig{}
		err := parseImportArgs([]string{"--stage"}, &cfg)
		if err == nil || !strings.Contains(err.Error(), "missing value for --stage") {
			t.Errorf("expected 'missing value' error, got %v", err)
		}
	})

	t.Run("missing slug value", func(t *testing.T) {
		cfg := modeConfig{}
		err := parseImportArgs([]string{"--slug"}, &cfg)
		if err == nil || !strings.Contains(err.Error(), "missing value for --slug") {
			t.Errorf("expected 'missing value' error, got %v", err)
		}
	})

	t.Run("missing specfirst-dir value", func(t *testing.T) {
		cfg := modeConfig{}
		err := parseImportArgs([]string{"--specfirst-dir"}, &cfg)
		if err == nil || !strings.Contains(err.Error(), "missing value for --specfirst-dir") {
			t.Errorf("expected 'missing value' error, got %v", err)
		}
	})

	t.Run("unknown flag", func(t *testing.T) {
		cfg := modeConfig{}
		err := parseImportArgs([]string{"--unknown"}, &cfg)
		if err == nil || !strings.Contains(err.Error(), "unknown import flag") {
			t.Errorf("expected 'unknown flag' error, got %v", err)
		}
	})
}

func TestOpenLogFile(t *testing.T) {
	t.Run("default log dir", func(t *testing.T) {
		dir := t.TempDir()
		cwd, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(cwd)

		file, path, err := openLogFile("run", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer file.Close()

		if !strings.HasPrefix(filepath.Base(path), "run-") {
			t.Errorf("log file should start with 'run-', got %q", path)
		}
		if !strings.HasSuffix(path, ".jsonl") {
			t.Errorf("log file should end with .jsonl, got %q", path)
		}
	})

	t.Run("custom log dir", func(t *testing.T) {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "custom-logs")

		file, path, err := openLogFile("import", logDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer file.Close()

		if !strings.Contains(path, "custom-logs") {
			t.Errorf("expected custom log dir in path, got %q", path)
		}
	})
}
