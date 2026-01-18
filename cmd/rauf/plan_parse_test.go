package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountOutcomeLines(t *testing.T) {
	lines := []string{
		"- Outcome: first result",
		"  - Outcome: second result",
		"- Notes: something else",
	}
	if got := countOutcomeLines(lines); got != 2 {
		t.Fatalf("expected 2 outcomes, got %d", got)
	}
}

func TestLintPlanTask(t *testing.T) {
	task := planTask{
		VerifyCmds: []string{"go test ./...", "go vet ./..."},
		TaskBlock:  []string{"- Outcome: one", "- Outcome: two"},
	}
	issues := lintPlanTask(task)
	if !issues.MultipleVerify {
		t.Fatalf("expected multiple verify warning")
	}
	if !issues.MultipleOutcome {
		t.Fatalf("expected multiple outcome warning")
	}
}

func TestReadActiveTask(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	// Create dummy files for extraction
	os.MkdirAll("cmd/rauf", 0o755)
	os.WriteFile("cmd/rauf/main.go", []byte("package main"), 0o644)

	planPath := filepath.Join(dir, "PLAN.md")
	content := `
# Implementation Plan

- [x] Done task
- [ ] Active task (cmd/rauf/main.go)
  - Verify: npm test
  - Spec: spec-A#L10
- [ ] Future task
`

	if err := os.WriteFile(planPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	task, ok, err := readActiveTask(planPath)
	if err != nil {
		t.Fatalf("readActiveTask failed: %v", err)
	}
	if !ok {
		t.Fatal("expected to find active task")
	}
	if task.TitleLine != "Active task (cmd/rauf/main.go)" {
		t.Errorf("got title %q, want 'Active task (cmd/rauf/main.go)'", task.TitleLine)
	}
	if len(task.VerifyCmds) != 1 || task.VerifyCmds[0] != "npm test" {
		t.Errorf("unexpected verify cmds: %v", task.VerifyCmds)
	}

	if len(task.SpecRefs) != 1 || task.SpecRefs[0] != "spec-A" {
		t.Errorf("unexpected spec refs: %v", task.SpecRefs)
	}
	if len(task.FilesMentioned) != 1 || task.FilesMentioned[0] != "cmd/rauf/main.go" {
		t.Errorf("unexpected files mentioned: %v", task.FilesMentioned)
	}
}

func TestExtractFileMentions(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	// Create dummy files
	os.MkdirAll("cmd/rauf", 0o755)
	os.WriteFile("cmd/rauf/main.go", []byte("package main"), 0o644)
	os.WriteFile("plan.md", []byte("plan"), 0o644)
	os.WriteFile("main.go", []byte("main"), 0o644)

	tests := []struct {
		input    []string
		expected []string
	}{
		{[]string{"look at cmd/rauf/main.go"}, []string{"cmd/rauf/main.go"}},
		{[]string{"modify plan.md and main.go"}, []string{"plan.md", "main.go"}},
		{[]string{"nothing here"}, []string{}},
	}

	for _, tt := range tests {
		got := extractFileMentions(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("%q: got %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("%q: got[%d]=%q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestReadActiveTask_CodeBlockVerify(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "PLAN.md")
	content := `
- [ ] Active task
  - Verify:
    ` + "```" + `
    npm test
    npm run lint
    ` + "```" + `
`
	if err := os.WriteFile(planPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	task, ok, err := readActiveTask(planPath)
	if err != nil || !ok {
		t.Fatalf("failed to read task: %v, %v", err, ok)
	}
	if len(task.VerifyCmds) != 2 {
		t.Errorf("expected 2 verify cmds, got %d", len(task.VerifyCmds))
	}
}

func TestReadAgentsVerifyFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	content := `
## Capabilities
Tests (fast): go test ./...
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds := readAgentsVerifyFallback(path)
	if len(cmds) != 1 || cmds[0] != "go test ./..." {
		t.Errorf("unexpected fallback: %v", cmds)
	}
}

func TestSplitSpecPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"spec-A", "spec-A", true},
		{"spec-A#L10", "spec-A", true},
		{"", "", false},
		{"#fragment", "", false},
	}
	for _, tt := range tests {
		path, ok := splitSpecPath(tt.input)
		if ok != tt.ok || path != tt.expected {
			t.Errorf("splitSpecPath(%q) = (%q, %v), want (%q, %v)", tt.input, path, ok, tt.expected, tt.ok)
		}
	}
}
