package main

import "testing"

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
