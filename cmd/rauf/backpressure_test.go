package main

import (
	"strings"
	"testing"
)

func TestFormatGuardrailBackpressure(t *testing.T) {
	tests := []struct {
		reason   string
		expected string
	}{
		{"", ""},
		{"forbidden_path:specs", "You attempted to modify forbidden directory: specs. Choose an alternative file/approach."},
		{"forbidden_path:/some/long/path", "You attempted to modify forbidden directory: /some/long/path. Choose an alternative file/approach."},
		{"max_files_changed", "Reduce scope: modify fewer files. Prefer smaller, focused patches."},
		{"max_commits_exceeded", "Squash work into fewer commits. Complete one task at a time."},
		{"verify_required_for_change", "You must run Verify successfully before changing files. Define or fix verification first."},
		{"plan_update_without_verify", "Plan changed but verification didn't pass. Fix verification before modifying the plan."},
		{"missing_verify_plan_not_updated", "Verification is missing. Update the plan to add a valid Verify command."},
		{"missing_verify_non_plan_change", "Verification is missing. You may only update the plan until Verify is defined."},
		{"unknown_reason", "Guardrail violation: unknown_reason"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			got := formatGuardrailBackpressure(tt.reason)
			if got != tt.expected {
				t.Errorf("formatGuardrailBackpressure(%q) = %q, want %q", tt.reason, got, tt.expected)
			}
		})
	}
}

func TestSummarizeVerifyOutput(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		result := summarizeVerifyOutput("", 30)
		if len(result) != 0 {
			t.Errorf("expected empty result, got %v", result)
		}
	})

	t.Run("zero max lines", func(t *testing.T) {
		result := summarizeVerifyOutput("FAIL: something", 0)
		if len(result) != 0 {
			t.Errorf("expected empty result, got %v", result)
		}
	})

	t.Run("extracts FAIL lines", func(t *testing.T) {
		output := `=== RUN   TestFoo
--- FAIL: TestFoo (0.00s)
    foo_test.go:42: expected 1, got 2
FAIL
FAIL	github.com/example/foo	0.123s`
		result := summarizeVerifyOutput(output, 30)
		if len(result) < 4 {
			t.Errorf("expected at least 4 lines, got %d: %v", len(result), result)
		}
	})

	t.Run("extracts panic lines", func(t *testing.T) {
		output := `panic: runtime error: index out of range [1] with length 0
goroutine 1 [running]:
main.main()
	/path/to/file.go:123 +0x1a2`
		result := summarizeVerifyOutput(output, 30)
		hasError := false
		for _, line := range result {
			if strings.Contains(line, "panic") || strings.Contains(line, "file.go:123") {
				hasError = true
				break
			}
		}
		if !hasError {
			t.Errorf("expected panic or file:line in result, got %v", result)
		}
	})

	t.Run("respects max lines", func(t *testing.T) {
		var lines []string
		for i := 0; i < 50; i++ {
			lines = append(lines, "FAIL: test line")
		}
		output := strings.Join(lines, "\n")
		result := summarizeVerifyOutput(output, 10)
		if len(result) != 10 {
			t.Errorf("expected 10 lines, got %d", len(result))
		}
	})

	t.Run("extracts ERROR lines case insensitive", func(t *testing.T) {
		output := `Error: something went wrong
error: another issue`
		result := summarizeVerifyOutput(output, 30)
		if len(result) != 2 {
			t.Errorf("expected 2 lines, got %d: %v", len(result), result)
		}
	})
}

func TestBuildBackpressurePack_EmptyState(t *testing.T) {
	state := raufState{}
	result := buildBackpressurePack(state, true)
	if result != "" {
		t.Errorf("expected empty result for empty state, got %q", result)
	}
}

func TestBuildBackpressurePack_GuardrailFailure(t *testing.T) {
	state := raufState{
		PriorGuardrailStatus: "fail",
		PriorGuardrailReason: "forbidden_path:specs",
	}
	result := buildBackpressurePack(state, true)

	if !strings.Contains(result, "## Backpressure Pack") {
		t.Error("expected Backpressure Pack header")
	}
	if !strings.Contains(result, "Guardrail Failure") {
		t.Error("expected Guardrail Failure section")
	}
	if !strings.Contains(result, "forbidden_path:specs") {
		t.Error("expected guardrail reason in output")
	}
	if !strings.Contains(result, "alternative file/approach") {
		t.Error("expected actionable instruction in output")
	}
}

func TestBuildBackpressurePack_VerifyFailure(t *testing.T) {
	state := raufState{
		LastVerificationStatus:  "fail",
		LastVerificationCommand: "make test",
		LastVerificationOutput:  "--- FAIL: TestFoo\nfoo_test.go:42: expected true",
	}
	result := buildBackpressurePack(state, true)

	if !strings.Contains(result, "Verification Failure") {
		t.Error("expected Verification Failure section")
	}
	if !strings.Contains(result, "make test") {
		t.Error("expected verify command in output")
	}
	if !strings.Contains(result, "Key Errors") {
		t.Error("expected Key Errors section")
	}
}

func TestBuildBackpressurePack_PlanDrift(t *testing.T) {
	state := raufState{
		PlanHashBefore:  "abc123",
		PlanHashAfter:   "def456",
		PlanDiffSummary: "+- [ ] New task",
	}
	result := buildBackpressurePack(state, true)

	if !strings.Contains(result, "Plan Changes Detected") {
		t.Error("expected Plan Changes Detected section")
	}
	if !strings.Contains(result, "Diff excerpt") {
		t.Error("expected plan diff excerpt")
	}
}

func TestBuildBackpressurePack_ExitReason(t *testing.T) {
	state := raufState{
		PriorExitReason: "no_progress",
	}
	result := buildBackpressurePack(state, true)

	if !strings.Contains(result, "Prior Exit Reason") {
		t.Error("expected Prior Exit Reason section")
	}
	if !strings.Contains(result, "no_progress") {
		t.Error("expected exit reason in output")
	}
}

func TestBuildBackpressurePack_CompletionExcluded(t *testing.T) {
	state := raufState{
		PriorExitReason: "completion_contract_satisfied",
	}
	result := buildBackpressurePack(state, true)

	// completion_contract_satisfied should not produce backpressure
	if result != "" {
		t.Errorf("expected empty result for completion exit reason, got %q", result)
	}
}

func TestGeneratePlanDiff_NoGit(t *testing.T) {
	result := generatePlanDiff("plan.md", false, 50)
	if result != "Plan file was modified (git diff unavailable)." {
		t.Errorf("expected fallback message, got %q", result)
	}
}

func TestBuildBackpressurePack_RetryBackpressure(t *testing.T) {
	state := raufState{
		PriorRetryCount:  3,
		PriorRetryReason: "rate limit",
	}
	result := buildBackpressurePack(state, true)

	if !strings.Contains(result, "Harness Retries") {
		t.Error("expected Harness Retries section")
	}
	if !strings.Contains(result, "Retries: 3") {
		t.Error("expected retry count in output")
	}
	if !strings.Contains(result, "rate limit") {
		t.Error("expected retry reason in output")
	}
}

func TestMaxArchitectQuestionsForState(t *testing.T) {
	t.Run("base case no failures", func(t *testing.T) {
		state := raufState{}
		max := maxArchitectQuestionsForState(state)
		if max != baseArchitectQuestions {
			t.Errorf("expected %d, got %d", baseArchitectQuestions, max)
		}
	})

	t.Run("guardrail failure adds bonus", func(t *testing.T) {
		state := raufState{PriorGuardrailStatus: "fail"}
		max := maxArchitectQuestionsForState(state)
		expected := baseArchitectQuestions + bonusQuestionsPerFailure
		if max != expected {
			t.Errorf("expected %d, got %d", expected, max)
		}
	})

	t.Run("verify failure adds bonus", func(t *testing.T) {
		state := raufState{LastVerificationStatus: "fail"}
		max := maxArchitectQuestionsForState(state)
		expected := baseArchitectQuestions + bonusQuestionsPerFailure
		if max != expected {
			t.Errorf("expected %d, got %d", expected, max)
		}
	})

	t.Run("both failures add bonuses", func(t *testing.T) {
		state := raufState{
			PriorGuardrailStatus:   "fail",
			LastVerificationStatus: "fail",
		}
		max := maxArchitectQuestionsForState(state)
		expected := baseArchitectQuestions + 2*bonusQuestionsPerFailure
		if max != expected {
			t.Errorf("expected %d, got %d", expected, max)
		}
	})
}

func TestRetryMatchToken(t *testing.T) {
	t.Run("empty match list", func(t *testing.T) {
		token, ok := retryMatchToken("rate limit hit", nil)
		if ok || token != "" {
			t.Errorf("expected no match, got %q", token)
		}
	})

	t.Run("matches token", func(t *testing.T) {
		token, ok := retryMatchToken("Error: rate limit exceeded", []string{"rate limit", "429"})
		if !ok || token != "rate limit" {
			t.Errorf("expected 'rate limit', got %q", token)
		}
	})

	t.Run("wildcard match", func(t *testing.T) {
		token, ok := retryMatchToken("any error", []string{"*"})
		if !ok || token != "*" {
			t.Errorf("expected '*', got %q", token)
		}
	})
}

func TestHasBackpressureResponse(t *testing.T) {
	t.Run("empty output returns false", func(t *testing.T) {
		if hasBackpressureResponse("") {
			t.Error("expected false for empty output")
		}
	})

	t.Run("detects response header", func(t *testing.T) {
		output := `Some text
## Backpressure Response

- [ ] Acknowledged: test failure
- [ ] Action: fixing it`
		if !hasBackpressureResponse(output) {
			t.Error("expected true when header present")
		}
	})

	t.Run("variations of header", func(t *testing.T) {
		outputs := []string{
			"## Backpressure Response\n\n- Content",
			"  ## Backpressure Response",
			"## Backpressure Response ",
		}
		for _, output := range outputs {
			if !hasBackpressureResponse(output) {
				t.Errorf("expected true for output: %q", output)
			}
		}
	})

	t.Run("ignores header inside code fence", func(t *testing.T) {
		output := "```\n## Backpressure Response\n```"
		if hasBackpressureResponse(output) {
			t.Error("expected false when inside code fence")
		}
	})

	t.Run("ignores header inside tilde fence", func(t *testing.T) {
		output := "~~~\n## Backpressure Response\n~~~"
		if hasBackpressureResponse(output) {
			t.Error("expected false when inside tilde fence")
		}
	})

	t.Run("detects header after code fence", func(t *testing.T) {
		output := "```\ncode\n```\n## Backpressure Response\n"
		if !hasBackpressureResponse(output) {
			t.Error("expected true when header is after fence")
		}
	})

	t.Run("false when not present", func(t *testing.T) {
		output := "Some random text\nwithout the header"
		if hasBackpressureResponse(output) {
			t.Error("expected false when header not present")
		}
	})
}

func TestBuildBackpressurePack_HypothesisRequired(t *testing.T) {
	state := raufState{
		LastVerificationStatus:  "fail",
		LastVerificationCommand: "make test",
		LastVerificationOutput:  "FAIL: test error",
		ConsecutiveVerifyFails:  2,
	}
	result := buildBackpressurePack(state, true)

	if !strings.Contains(result, "HYPOTHESIS REQUIRED") {
		t.Error("expected HYPOTHESIS REQUIRED in output")
	}
	if !strings.Contains(result, "Consecutive Failures: 2") {
		t.Error("expected consecutive failure count in output")
	}
	if !strings.Contains(result, "diagnosis") {
		t.Error("expected diagnosis instruction in output")
	}
}

func TestBuildBackpressurePack_NoHypothesisOnFirstFail(t *testing.T) {
	state := raufState{
		LastVerificationStatus:  "fail",
		LastVerificationCommand: "make test",
		LastVerificationOutput:  "FAIL: test error",
		ConsecutiveVerifyFails:  1,
	}
	result := buildBackpressurePack(state, true)

	if strings.Contains(result, "HYPOTHESIS REQUIRED") {
		t.Error("should not require hypothesis on first failure")
	}
	if !strings.Contains(result, "Action Required: Fix these errors") {
		t.Error("expected normal action required message")
	}
}
