package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractKeywords(t *testing.T) {
	task := `Refactor the "payment gateway" logic in payment.go and fix the "timeout error"`
	kws := extractKeywords(task)

	expected := []string{"payment gateway", "timeout error"}
	for _, exp := range expected {
		found := false
		for _, kw := range kws {
			if kw == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected keyword %q not found in %v", exp, kws)
		}
	}
}

func TestGenerateRepoMap(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	// Create dummy structure
	os.MkdirAll("cmd/rauf", 0755)
	os.WriteFile("cmd/rauf/main.go", []byte("package main\nfunc main(){}"), 0644)
	os.WriteFile("rauf.yaml", []byte("config content"), 0644)
	os.MkdirAll("pkg/auth", 0755)
	os.WriteFile("pkg/auth/auth.go", []byte("package auth\nfunc Login(){}"), 0644)

	// Mock gitOutput to return nothing so it falls back to filepath.Walk
	origGitOutput := gitOutput
	gitOutput = func(args ...string) (string, error) {
		return "", os.ErrNotExist
	}
	defer func() { gitOutput = origGitOutput }()

	rm, err := generateRepoMap()
	if err != nil {
		t.Fatalf("generateRepoMap failed: %v", err)
	}

	content := rm.String()
	if !strings.Contains(content, "cmd/") {
		t.Error("missing cmd/ in tree")
	}
	if !strings.Contains(content, "rauf.yaml") {
		t.Error("missing rauf.yaml in config")
	}
	if !strings.Contains(content, "cmd/rauf/main.go") {
		t.Error("missing main.go in entrypoints")
	}
	if !strings.Contains(content, "pkg") {
		t.Error("missing pkg in modules")
	}
}

func TestGenerateContextPack_ZeroHits(t *testing.T) {
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	origGitExec := gitExec
	gitExec = func(args ...string) (string, error) {
		return "", fmt.Errorf("no matches")
	}
	defer func() { gitExec = origGitExec }()

	cp, err := generateContextPack(context.Background(), "something very unique", true)
	if err != nil {
		t.Fatalf("generateContextPack failed: %v", err)
	}

	if cp.ZeroState == "" {
		t.Error("expected ZeroState for no hits")
	}
	if !strings.Contains(cp.String(), "No Relevant Code Found") {
		t.Error("missing zero hits section in output")
	}
}

func TestGetContextCacheKey(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	os.WriteFile("go.mod", []byte("module test"), 0644)
	key1 := getContextCacheKey(false)

	os.WriteFile("go.mod", []byte("module test2"), 0644)
	key2 := getContextCacheKey(false)

	if key1 == key2 {
		t.Error("expected different keys for different go.mod content")
	}

	os.WriteFile("rauf.yaml", []byte("harness: test"), 0644)
	key3 := getContextCacheKey(false)
	if key2 == key3 {
		t.Error("expected different keys for different rauf.yaml content")
	}
}

func TestGetExcerpts(t *testing.T) {
	content := `line 1
line 2
match here
line 4
line 5
line 6
line 7
line 8
match there
line 10`
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	os.WriteFile(path, []byte(content), 0644)

	got, err := getExcerpts(path, []string{"match"})
	if err != nil {
		t.Fatalf("getExcerpts failed: %v", err)
	}

	if !strings.Contains(got, "3 | match here") {
		t.Errorf("missing match line 3: %q", got)
	}
	if !strings.Contains(got, "9 | match there") {
		t.Errorf("missing match line 9: %q", got)
	}
	if !strings.Contains(got, "---") {
		t.Error("missing separator between windows")
	}
}

func TestDiscoverSymbols(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	content := `package main
func ExportedFunc() {}
func unexportedFunc() {}
`
	os.WriteFile(path, []byte(content), 0644)

	syms := discoverSymbols(path)
	if len(syms) != 1 || syms[0] != "ExportedFunc" {
		t.Errorf("expected [ExportedFunc], got %v", syms)
	}

	// Test Python
	pyPath := filepath.Join(dir, "test.py")
	pyContent := `def my_func(): pass
class MyClass: pass
`
	os.WriteFile(pyPath, []byte(pyContent), 0644)
	syms = discoverSymbols(pyPath)
	if len(syms) != 2 {
		t.Errorf("expected 2 symbols for python, got %v", syms)
	}
}

func TestGenerateContextPack_Hits(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	content := "some code with keyword1 and more keyword2"
	os.WriteFile("hit.go", []byte(content), 0644)

	// Mock gitExec to simulate search hits (called by gitOutputRaw)
	origGitExec := gitExec
	gitExec = func(args ...string) (string, error) {
		if len(args) > 0 && args[0] == "grep" {
			return "hit.go\n", nil
		}
		return "", nil
	}
	defer func() { gitExec = origGitExec }()

	cp, err := generateContextPack(context.Background(), "keyword1 keyword2", true)
	if err != nil {
		t.Fatalf("generateContextPack failed: %v", err)
	}

	if len(cp.Hits) == 0 {
		t.Error("expected hits, got 0")
	}
	if !strings.Contains(cp.String(), "hit.go") {
		t.Error("missing hit.go in output")
	}
}

func TestPerformSearch_ErrorPaths(t *testing.T) {
	// 1. No rg, no git
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	res, err := performSearch(context.Background(), "kw", false)
	if err == nil {
		t.Errorf("expected error when no search tools available, got nil")
	}
	if len(res) != 0 {
		t.Errorf("expected 0 results, got %v", res)
	}
}

func TestFindCallers(t *testing.T) {
	// findCallers just calls performSearch
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	origGitExec := gitExec
	gitExec = func(args ...string) (string, error) {
		return "file1.go\nfile2.go\n", nil
	}
	defer func() { gitExec = origGitExec }()

	callers, err := findCallers(context.Background(), "MySym", true)
	if err != nil {
		t.Fatalf("findCallers failed: %v", err)
	}
	if len(callers) != 2 {
		t.Errorf("expected 2 callers, got %d", len(callers))
	}
}

func TestPerformSearch_Full(t *testing.T) {
	// Test with rg missing but git available
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	origGitExec := gitExec
	gitExec = func(args ...string) (string, error) {
		if len(args) > 0 && args[0] == "grep" {
			return "found.go\n", nil
		}
		return "", nil
	}
	defer func() { gitExec = origGitExec }()

	res, err := performSearch(context.Background(), "kw", true)
	if err != nil {
		t.Fatalf("performSearch failed: %v", err)
	}
	if len(res) != 1 || res[0] != "found.go" {
		t.Errorf("expected [found.go], got %v", res)
	}
}
