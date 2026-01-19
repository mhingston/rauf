package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadContextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "context.md")
	if err := os.WriteFile(path, []byte("Hello World"), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	// Normal read
	got := readContextFile(path, 100)
	if got != "Hello World" {
		t.Errorf("readContextFile normal: got %q", got)
	}

	// Truncated read
	got = readContextFile(path, 5)
	if got != "Hello" {
		t.Errorf("readContextFile truncated: got %q", got)
	}

	// Missing file
	got = readContextFile("nonexistent.md", 100)
	if got != "" {
		t.Errorf("readContextFile nonexistent: got %q", got)
	}

	// Empty path
	got = readContextFile("", 100)
	if got != "" {
		t.Errorf("readContextFile empty: got %q", got)
	}
}
