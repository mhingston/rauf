package main

import (
	"os"
	"strings"
	"testing"
)

func TestEnsureGitignoreLogs(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	t.Run("creates new file", func(t *testing.T) {
		changed, err := ensureGitignoreLogs(false)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Error("expected changed=true for new file")
		}
		data, _ := os.ReadFile(".gitignore")
		if !strings.Contains(string(data), "logs/") {
			t.Errorf("got %q, want logs/", string(data))
		}
	})

	t.Run("appends to existing", func(t *testing.T) {
		os.WriteFile(".gitignore", []byte("node_modules\n"), 0o644)
		changed, err := ensureGitignoreLogs(false)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Error("expected changed=true for missing entry")
		}
		data, _ := os.ReadFile(".gitignore")
		if !strings.Contains(string(data), "node_modules") || !strings.Contains(string(data), "logs/") {
			t.Errorf("unexpected content: %q", string(data))
		}
	})

	t.Run("does not duplicate", func(t *testing.T) {
		os.WriteFile(".gitignore", []byte("logs/\nother\n"), 0o644)
		changed, err := ensureGitignoreLogs(false)
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			t.Error("expected changed=false for existing entry")
		}
		data, _ := os.ReadFile(".gitignore")
		if strings.Count(string(data), "logs/") != 1 {
			t.Errorf("expected 1 occurrence of logs/, got %d", strings.Count(string(data), "logs/"))
		}
	})

	t.Run("dry run", func(t *testing.T) {
		os.Remove(".gitignore")
		changed, err := ensureGitignoreLogs(true)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Error("expected changed=true for dry run on missing file")
		}
		if _, err := os.Stat(".gitignore"); !os.IsNotExist(err) {
			t.Error("expected file not to be created in dry run")
		}
	})

}
