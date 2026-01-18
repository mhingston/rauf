package main

import (
	"testing"
)

func TestResolveRepoPathWithRoot(t *testing.T) {
	root := "/tmp/rauf"
	tests := []struct {
		path     string
		expected string
		err      bool
	}{
		{"foo.go", "/tmp/rauf/foo.go", false},
		{"subdir/bar.go", "/tmp/rauf/subdir/bar.go", false},
		{"../outside.go", "", true},
		{"/etc/passwd", "", true},
		{"/tmp/rauf/inside.go", "/tmp/rauf/inside.go", false}, // absolute path inside root
	}

	for _, tt := range tests {
		got, ok := resolveRepoPathWithRoot(tt.path, root)
		if ok == tt.err { // tt.err means "expect failure", so ok should be false
			t.Errorf("resolveRepoPathWithRoot(%q) ok = %v, want %v", tt.path, ok, !tt.err)
		}
		if !tt.err && got != tt.expected {
			t.Errorf("resolveRepoPathWithRoot(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}

func TestRepoRelativePathWithRoot(t *testing.T) {
	root := "/tmp/rauf"
	tests := []struct {
		absPath  string
		expected string
	}{
		{"/tmp/rauf/foo.go", "foo.go"},
		{"/tmp/rauf/subdir/bar.go", "subdir/bar.go"},
		{"/tmp/other/foo.go", "../other/foo.go"}, // Rel returns relative path
		{"/tmp/rauf", "."},
	}

	for _, tt := range tests {
		got := repoRelativePathWithRoot(tt.absPath, root)
		if got != tt.expected {
			t.Errorf("repoRelativePathWithRoot(%q) = %q, want %q", tt.absPath, got, tt.expected)
		}
	}
}
