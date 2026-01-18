package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceFingerprint(t *testing.T) {
	dir := t.TempDir()

	// Create some files
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)
	os.MkdirAll(filepath.Join(dir, "ignored"), 0o755)
	os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("content1"), 0o644)
	os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("content2"), 0o644)
	os.WriteFile(filepath.Join(dir, "subdir", "file3.txt"), []byte("content3"), 0o644)
	os.WriteFile(filepath.Join(dir, "ignored", "file4.txt"), []byte("content4"), 0o644)

	t.Run("basic", func(t *testing.T) {
		f1 := workspaceFingerprint(dir, nil, nil)
		f2 := workspaceFingerprint(dir, nil, nil)
		if f1 != f2 {
			t.Error("expected consistent fingerprint")
		}
	})

	t.Run("exclude dir", func(t *testing.T) {
		f1 := workspaceFingerprint(dir, nil, nil)
		f2 := workspaceFingerprint(dir, []string{"ignored"}, nil)
		if f1 == f2 {
			t.Error("expected different fingerprint when excluding dir")
		}

		// Ensure excluding an empty string or non-existent dir doesn't crash
		workspaceFingerprint(dir, []string{"", "nonexistent"}, nil)
	})

	t.Run("exclude file", func(t *testing.T) {
		f1 := workspaceFingerprint(dir, nil, nil)
		f2 := workspaceFingerprint(dir, nil, []string{"file1.txt"})
		if f1 == f2 {
			t.Error("expected different fingerprint when excluding file")
		}
	})

	t.Run("content change", func(t *testing.T) {
		f1 := workspaceFingerprint(dir, nil, nil)
		os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("changed"), 0o644)
		f2 := workspaceFingerprint(dir, nil, nil)
		if f1 == f2 {
			t.Error("expected different fingerprint after content change")
		}
	})
}
