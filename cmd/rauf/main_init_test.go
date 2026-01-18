package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunInit(t *testing.T) {
	// Helper to setup clean dir
	setup := func(t *testing.T) (string, func()) {
		dir := t.TempDir()
		cwd, _ := os.Getwd()
		os.Chdir(dir)
		return dir, func() { os.Chdir(cwd) }
	}

	t.Run("basic create", func(t *testing.T) {
		_, cleanup := setup(t)
		defer cleanup()

		if err := runInit(false, false); err != nil {
			t.Fatalf("runInit failed: %v", err)
		}

		if _, err := os.Stat("rauf.yaml"); err != nil {
			t.Error("rauf.yaml not created")
		}
		if _, err := os.Stat("specs/README.md"); err != nil {
			t.Error("specs/README.md not created")
		}

		// Check gitignore
		content, _ := os.ReadFile(".gitignore")
		if !strings.Contains(string(content), "logs/") {
			t.Error(".gitignore missing logs/")
		}
	})

	t.Run("skip existing without force", func(t *testing.T) {
		_, cleanup := setup(t)
		defer cleanup()

		os.WriteFile("rauf.yaml", []byte("existing"), 0o644)
		if err := runInit(false, false); err != nil {
			t.Fatalf("runInit failed: %v", err)
		}

		content, _ := os.ReadFile("rauf.yaml")
		if string(content) != "existing" {
			t.Error("expected skip existing file")
		}
	})

	t.Run("overwrite with force", func(t *testing.T) {
		_, cleanup := setup(t)
		defer cleanup()

		os.WriteFile("rauf.yaml", []byte("existing"), 0o644)
		if err := runInit(true, false); err != nil {
			t.Fatalf("runInit failed: %v", err)
		}

		content, _ := os.ReadFile("rauf.yaml")
		if string(content) == "existing" {
			t.Error("expected overwrite existing file")
		}
	})

	t.Run("gitignore update", func(t *testing.T) {
		_, cleanup := setup(t)
		defer cleanup()

		os.WriteFile(".gitignore", []byte("existing\n"), 0o644)
		if err := runInit(false, false); err != nil {
			t.Fatal(err)
		}

		content, _ := os.ReadFile(".gitignore")
		if !strings.Contains(string(content), "existing\nlogs/") {
			t.Errorf("expected append to gitignore, got %q", string(content))
		}
	})
}
