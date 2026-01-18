package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestFormatGuardrailBackpressure(t *testing.T) {
	tests := []struct {
		reason   string
		expected string
	}{
		{"", ""},
		{"forbidden_path:/etc", "You attempted to modify forbidden directory: /etc. Choose an alternative file/approach."},
		{"max_files_changed", "Reduce scope: modify fewer files. Prefer smaller, focused patches."},
		{"max_commits_exceeded", "Squash work into fewer commits. Complete one task at a time."},
		{"verify_required_for_change", "You must run Verify successfully before changing files. Define or fix verification first."},
		{"plan_update_without_verify", "Plan changed but verification didn't pass. Fix verification before modifying the plan."},
		{"missing_verify_plan_not_updated", "Verification is missing. Update the plan to add a valid Verify command."},
		{"missing_verify_non_plan_change", "Verification is missing. You may only update the plan until Verify is defined."},
		{"unknown", "Guardrail violation: unknown"},
	}

	for _, tt := range tests {
		got := formatGuardrailBackpressure(tt.reason)
		if got != tt.expected {
			t.Errorf("formatGuardrailBackpressure(%q) = %q, want %q", tt.reason, got, tt.expected)
		}
	}
}

func TestHasBackpressureResponse(t *testing.T) {
	tests := []struct {
		output   string
		expected bool
	}{
		{"## Backpressure Response\nI understand.", true},
		{"Hello\n## Backpressure Response\nOK", true},
		{"```\n## Backpressure Response\n```", false},
		{"Just text", false},
	}

	for _, tt := range tests {
		got := hasBackpressureResponse(tt.output)
		if got != tt.expected {
			t.Errorf("hasBackpressureResponse(%q) = %v, want %v", tt.output, got, tt.expected)
		}
	}
}

func TestSummarizeVerifyOutput(t *testing.T) {
	output := `=== RUN   TestFoo
--- FAIL: TestFoo (0.00s)
    foo_test.go:42: expected 1 got 2
PASS
`
	got := summarizeVerifyOutput(output, 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(got))
	}
	if !strings.Contains(got[0], "RUN") {
		t.Errorf("unexpected line 0: %q", got[0])
	}
	if !strings.Contains(got[1], "FAIL") {
		t.Errorf("unexpected line 1: %q", got[1])
	}
	if !strings.Contains(got[2], "expected") {
		t.Errorf("unexpected line 2: %q", got[2])
	}
}

func TestBuildBackpressurePack(t *testing.T) {
	t.Run("empty state", func(t *testing.T) {
		got := buildBackpressurePack(raufState{}, false)
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("with guardrail fail", func(t *testing.T) {
		state := raufState{
			PriorGuardrailStatus: "fail",
			PriorGuardrailReason: "max_files_changed",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "Guardrail Failure") {
			t.Error("missing Guardrail Failure section")
		}
		if !strings.Contains(got, "Reduce scope") {
			t.Error("missing guardrail action")
		}
	})

	t.Run("with verify fail", func(t *testing.T) {
		state := raufState{
			LastVerificationStatus:  "fail",
			LastVerificationOutput:  "ERROR: something broke",
			LastVerificationCommand: "make test",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "Verification Failure") {
			t.Error("missing Verification Failure section")
		}
		if !strings.Contains(got, "ERROR: something broke") {
			t.Error("missing error summary")
		}
	})

	t.Run("recovery mode guardrail", func(t *testing.T) {
		state := raufState{
			RecoveryMode:         "guardrail",
			PriorGuardrailStatus: "fail",
			PriorGuardrailReason: "forbidden_path:/etc",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "GUARDRAIL RECOVERY") {
			t.Error("missing GUARDRAIL RECOVERY header")
		}
	})

	t.Run("recovery mode verify", func(t *testing.T) {
		state := raufState{
			RecoveryMode:            "verify",
			LastVerificationStatus:  "fail",
			LastVerificationOutput:  "test failed",
			LastVerificationCommand: "make test",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "VERIFY RECOVERY") {
			t.Error("missing VERIFY RECOVERY header")
		}
	})

	t.Run("recovery mode no_progress", func(t *testing.T) {
		state := raufState{
			RecoveryMode:    "no_progress",
			PriorExitReason: "no_progress",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "NO-PROGRESS RECOVERY") {
			t.Error("missing NO-PROGRESS RECOVERY header")
		}
	})

	t.Run("with plan drift", func(t *testing.T) {
		state := raufState{
			PlanHashBefore:  "abc123",
			PlanHashAfter:   "def456",
			PlanDiffSummary: "+ added task\n- removed task",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "Plan Changes Detected") {
			t.Error("missing Plan Changes section")
		}
		if !strings.Contains(got, "+ added task") {
			t.Error("missing diff excerpt")
		}
	})

	t.Run("with exit reason no_progress", func(t *testing.T) {
		state := raufState{
			PriorExitReason: "no_progress",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "Prior Exit Reason") {
			t.Error("missing Prior Exit Reason section")
		}
		if !strings.Contains(got, "Reducing scope") {
			t.Error("missing no_progress guidance")
		}
	})

	t.Run("with exit reason no_unchecked_tasks", func(t *testing.T) {
		state := raufState{
			PriorExitReason: "no_unchecked_tasks",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "no_unchecked_tasks") {
			t.Error("missing exit reason")
		}
		if !strings.Contains(got, "RAUF_COMPLETE") {
			t.Error("missing completion guidance")
		}
	})

	t.Run("with retry backpressure", func(t *testing.T) {
		state := raufState{
			PriorRetryCount:  3,
			PriorRetryReason: "rate_limit",
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "Harness Retries") {
			t.Error("missing Harness Retries section")
		}
		if !strings.Contains(got, "rate_limit") {
			t.Error("missing retry reason")
		}
	})

	t.Run("consecutive verify fails requiring hypothesis", func(t *testing.T) {
		state := raufState{
			LastVerificationStatus:  "fail",
			LastVerificationOutput:  "test failed",
			LastVerificationCommand: "make test",
			ConsecutiveVerifyFails:  2,
		}
		got := buildBackpressurePack(state, false)
		if !strings.Contains(got, "HYPOTHESIS REQUIRED") {
			t.Error("missing HYPOTHESIS REQUIRED message")
		}
		if !strings.Contains(got, "Consecutive Failures: 2") {
			t.Error("missing consecutive failure count")
		}
	})
}

func TestGeneratePlanDiff(t *testing.T) {
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("git unavailable", func(t *testing.T) {
		got := generatePlanDiff("PLAN.md", false, 10)
		if !strings.Contains(got, "git diff unavailable") {
			t.Errorf("unexpected output: %q", got)
		}
	})

	t.Run("working tree diff", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "diff" && args[1] == "--" {
				return "+ change", nil
			}
			return "", nil
		}
		got := generatePlanDiff("PLAN.md", true, 10)
		if !strings.Contains(got, "[source: working-tree]") {
			t.Errorf("expected working-tree source, got: %q", got)
		}
		if !strings.Contains(got, "+ change") {
			t.Errorf("missing diff content: %q", got)
		}
	})

	t.Run("staged diff fallback", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "diff" && args[1] == "--" {
				return "", nil // empty working tree diff
			}
			if len(args) >= 1 && args[0] == "diff" && args[1] == "--cached" {
				return "+ staged change", nil
			}
			return "", nil
		}
		got := generatePlanDiff("PLAN.md", true, 10)
		if !strings.Contains(got, "[source: staged]") {
			t.Errorf("expected staged source, got: %q", got)
		}
		if !strings.Contains(got, "+ staged change") {
			t.Errorf("missing diff content: %q", got)
		}
	})

	t.Run("diff empty", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "", nil
		}
		got := generatePlanDiff("PLAN.md", true, 10)
		if !strings.Contains(got, "diff empty") {
			t.Errorf("expected diff empty message, got: %q", got)
		}
	})

	t.Run("diff failed", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "diff" && args[1] == "--" {
				return "", exec.ErrNotFound
			}
			if len(args) >= 1 && args[0] == "diff" && args[1] == "--cached" {
				return "", exec.ErrNotFound
			}
			return "", nil
		}
		got := generatePlanDiff("PLAN.md", true, 10)
		if !strings.Contains(got, "git diff failed") {
			t.Errorf("expected diff failed message, got: %q", got)
		}
	})
}
