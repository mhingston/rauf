package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPromptContent(t *testing.T) {
	dir := t.TempDir()
	promptFile := dir + "/prompt.md"
	os.WriteFile(promptFile, []byte("Mode: {{.Mode}}, Task: {{.ActiveTask}}"), 0o644)

	data := promptData{
		Mode:       "plan",
		ActiveTask: "My Task {{injection}}",
	}

	rendered, hash, err := buildPromptContent(promptFile, data)
	if err != nil {
		t.Fatalf("buildPromptContent failed: %v", err)
	}

	if !strings.Contains(rendered, "Mode: plan") {
		t.Errorf("expected Mode: plan, got %q", rendered)
	}
	// Verify escaping occurred
	if !strings.Contains(rendered, "Task: My Task { {injection} }") {
		t.Errorf("expected escaped task, got %q", rendered)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}
}

func TestBuildContextPack(t *testing.T) {
	task := planTask{
		TitleLine:      "## Task 1",
		SpecRefs:       []string{"spec1.md"},
		FilesMentioned: []string{"file1.go"},
	}
	state := raufState{
		LastVerificationOutput:  "error at line 10",
		LastVerificationCommand: "make test",
	}

	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	os.WriteFile("spec1.md", []byte("spec content"), 0o644)
	os.WriteFile("file1.go", []byte("code content"), 0o644)

	got := buildContextPack("PLAN.md", task, []string{"true"}, state, false, "Fix verification!")

	if !strings.Contains(got, "Plan Path: PLAN.md") {
		t.Error("missing plan path")
	}
	if !strings.Contains(got, "Active Task: ## Task 1") {
		t.Error("missing active task title")
	}
	if !strings.Contains(got, "SYSTEM: Fix verification!") {
		t.Error("missing system instruction")
	}
	if !strings.Contains(got, "error at line 10") {
		t.Error("missing verification output")
	}
	// Note: readSpecContexts and readRelevantFiles are called inside
	if !strings.Contains(got, "code content") {
		t.Error("missing file content")
	}
}

func TestReadAgentsCapabilityMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	content := `## Commands
- ls
- cat
## Git
- git add
- git commit
## Other
- hidden
`
	os.WriteFile(path, []byte(content), 0o644)

	got := readAgentsCapabilityMap(path, 100)
	if !strings.Contains(got, "- ls") {
		t.Error("missing Commands section content")
	}
	if !strings.Contains(got, "- git commit") {
		t.Error("missing Git section content")
	}
	if strings.Contains(got, "- hidden") {
		t.Error("included non-whitelisted section")
	}
}

func TestBuildRepoMap(t *testing.T) {
	// Mock gitOutput
	origGitOutput := gitOutput
	defer func() { gitOutput = origGitOutput }()

	t.Run("git unavailable", func(t *testing.T) {
		if buildRepoMap(false) != "" {
			t.Error("expected empty repo map when git unavailable")
		}
	})

	t.Run("git available", func(t *testing.T) {
		gitOutput = func(args ...string) (string, error) {
			return "file1.txt\nfile2.txt", nil
		}
		got := buildRepoMap(true)
		if !strings.Contains(got, "file1.txt") || !strings.Contains(got, "file2.txt") {
			t.Errorf("unexpected repo map: %q", got)
		}
	})
}

func TestBuildSpecIndex(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	os.MkdirAll("specs", 0o755)
	os.WriteFile("specs/one.md", []byte("---\nstatus: stable\n---\n"), 0o644)

	got := buildSpecIndex()
	if !strings.Contains(got, "specs/one.md (status: stable)") {
		t.Errorf("unexpected spec index: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	s := "Hello World"
	if truncateHead(s, 5) != "Hello" {
		t.Errorf("expected Hello, got %q", truncateHead(s, 5))
	}
	if truncateTail(s, 5) != "World" {
		t.Errorf("expected World, got %q", truncateTail(s, 5))
	}

	// UTF-8 boundary test
	s2 := "こんにちは"                // "Hello" in Japanese, 5 characters, 15 bytes
	trunc := truncateHead(s2, 4) // 4 bytes is middle of first character
	if len(trunc) > 4 {
		t.Errorf("expected length <= 4, got %d", len(trunc))
	}
}

func TestIsWithinRoot(t *testing.T) {
	root := "/tmp/rauf"
	tests := []struct {
		target   string
		expected bool
	}{
		{"/tmp/rauf/foo.go", true},
		{"/tmp/rauf/subdir/bar.go", true},
		{"/tmp/rauf/../outside.go", false},
		{"/etc/passwd", false},
	}

	for _, tt := range tests {
		got := isWithinRoot(root, tt.target)
		if got != tt.expected {
			t.Errorf("isWithinRoot(%q, %q) = %v, want %v", root, tt.target, got, tt.expected)
		}
	}
}

func TestBuildRepoMap_Truncate(t *testing.T) {
	// Mock gitOutput
	origGitOutput := gitOutput
	defer func() { gitOutput = origGitOutput }()

	gitOutput = func(args ...string) (string, error) {
		var lines []string
		for i := 0; i < maxRepoMapLines + 10; i++ {
			lines = append(lines, "file")
		}
		return strings.Join(lines, "\n"), nil
	}

	got := buildRepoMap(true)
	lines := strings.Split(got, "\n")
	if len(lines) != maxRepoMapLines {
		t.Errorf("expected %d lines, got %d", maxRepoMapLines, len(lines))
	}
}

func TestBuildContextPack_Full(t *testing.T) {
	task := planTask{
		TitleLine: "## Task 2",
		SpecRefs: []string{"spec.md"},
	}
	state := raufState{
		RecoveryMode:           "verify",
		LastVerificationOutput: "fail",
		LastVerificationCommand: "make test",
		Assumptions: []Assumption{
			{Question: "Q1", StickyScope: "global"},
			{Question: "Q2"},
		},
		ArchivedAssumptions: []ArchivedAssumption{
			{Assumption: Assumption{Question: "Q3"}, ClearedRecoveryMode: "verify", ClearedReason: "fixed"},
			{Assumption: Assumption{Question: "Q4"}, ClearedRecoveryMode: "other", ClearedReason: "ignore"},
		},
	}
	
	// Create dummy spec file
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)
	os.WriteFile("spec.md", []byte("spec"), 0o644)

	got := buildContextPack("PLAN.md", task, []string{"cmd1", "cmd2"}, state, true, "Verify me")

	if !strings.Contains(got, "- cmd1") || !strings.Contains(got, "- cmd2") {
		t.Error("missing multiple verify commands")
	}
	if !strings.Contains(got, "Alert: Recovery mode: verify") && !strings.Contains(got, "Recovery mode: verify") {
		t.Error("missing recovery mode alert")
	}
	if !strings.Contains(got, "Command: make test") {
		t.Error("missing last verify command")
	}
	if !strings.Contains(got, "[GLOBAL] Q1") {
		t.Error("missing sticky assumption")
	}
	if !strings.Contains(got, "- Q2") {
		t.Error("missing normal assumption")
	}
	if !strings.Contains(got, "Q3 (Resolved: fixed)") {
		t.Error("missing resurfaced assumption")
	}
	if strings.Contains(got, "Q4") {
		t.Error("included irrelevant archived assumption")
	}
}
