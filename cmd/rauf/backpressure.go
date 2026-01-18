package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// highSignalPatterns are pre-compiled patterns for extracting important error lines.
// Compiled at package init time for performance.
var highSignalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bFAIL\b`),
	regexp.MustCompile(`(?i)\bFAILED\b`),
	regexp.MustCompile(`(?i)\bERROR\b`),
	regexp.MustCompile(`(?i)\bpanic\b`),
	regexp.MustCompile(`(?i)\bundefined\b`),
	regexp.MustCompile(`(?i)no such file`),
	regexp.MustCompile(`\w+\.\w+:\d+`), // file:line pattern (e.g., foo.go:42)
	regexp.MustCompile(`^---\s*(FAIL|PASS):`),
	regexp.MustCompile(`(?i)^=== (RUN|FAIL)`),
	regexp.MustCompile(`(?i)expected.*got`),
	regexp.MustCompile(`(?i)assertion failed`),
}

// formatGuardrailBackpressure converts a guardrail reason code into an actionable instruction.
func formatGuardrailBackpressure(reason string) string {
	if reason == "" {
		return ""
	}

	switch {
	case strings.HasPrefix(reason, "forbidden_path:"):
		path := strings.TrimPrefix(reason, "forbidden_path:")
		return "You attempted to modify forbidden directory: " + path + ". Choose an alternative file/approach."
	case reason == "max_files_changed":
		return "Reduce scope: modify fewer files. Prefer smaller, focused patches."
	case reason == "max_commits_exceeded":
		return "Squash work into fewer commits. Complete one task at a time."
	case reason == "verify_required_for_change":
		return "You must run Verify successfully before changing files. Define or fix verification first."
	case reason == "plan_update_without_verify":
		return "Plan changed but verification didn't pass. Fix verification before modifying the plan."
	case reason == "missing_verify_plan_not_updated":
		return "Verification is missing. Update the plan to add a valid Verify command."
	case reason == "missing_verify_non_plan_change":
		return "Verification is missing. You may only update the plan until Verify is defined."
	default:
		return "Guardrail violation: " + reason
	}
}

// hasBackpressureResponse checks if the model output contains the required response header.
// Returns true if the output contains "## Backpressure Response" outside of code fences.
func hasBackpressureResponse(output string) bool {
	return scanLinesOutsideFence(output, func(trimmed string) bool {
		return strings.HasPrefix(trimmed, "## Backpressure Response")
	})
}

// summarizeVerifyOutput extracts high-signal error lines from verification output.
func summarizeVerifyOutput(output string, maxLines int) []string {
	if output == "" || maxLines <= 0 {
		return nil
	}

	lines := strings.Split(output, "\n")
	result := []string{}

	for _, line := range lines {
		if len(result) >= maxLines {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, pattern := range highSignalPatterns {
			if pattern.MatchString(line) {
				result = append(result, trimmed)
				break
			}
		}
	}

	return result
}

// generatePlanDiff creates a truncated diff excerpt when plan hash changes.
// Returns the diff content and a source indicator ("working-tree" or "staged").
func generatePlanDiff(planPath string, gitAvailable bool, maxLines int) string {
	if !gitAvailable || planPath == "" {
		return "Plan file was modified (git diff unavailable)."
	}

	source := "working-tree"
	output, err := exec.Command("git", "diff", "--", planPath).Output()
	if err != nil || len(output) == 0 {
		// Try staged diff
		output, err = exec.Command("git", "diff", "--cached", "--", planPath).Output()
		if err != nil {
			return "Plan file was modified (git diff failed)."
		}
		source = "staged"
	}

	if len(output) == 0 {
		return "Plan file was modified (diff empty)."
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "... (truncated)")
	}

	return fmt.Sprintf("[source: %s]\n%s", source, strings.Join(lines, "\n"))
}

// buildBackpressurePack assembles the complete Backpressure Pack section for injection.
func buildBackpressurePack(state raufState, gitAvailable bool) string {
	var b strings.Builder

	// Check if there's any backpressure to report
	hasGuardrail := state.PriorGuardrailStatus == "fail" && state.PriorGuardrailReason != ""
	hasVerifyFail := state.LastVerificationStatus == "fail" && state.LastVerificationOutput != ""
	hasExitReason := state.PriorExitReason != "" && state.PriorExitReason != "completion_contract_satisfied"
	hasPlanDrift := state.PlanHashBefore != "" && state.PlanHashAfter != "" && state.PlanHashBefore != state.PlanHashAfter
	hasRetry := state.PriorRetryCount > 0

	if !hasGuardrail && !hasVerifyFail && !hasExitReason && !hasPlanDrift && !hasRetry {
		return ""
	}

	b.WriteString("## Backpressure Pack (from previous iteration)\n\n")
	b.WriteString("**IMPORTANT: Address these issues FIRST before any new work.**\n\n")

	// Priority ordering
	b.WriteString("**Priority:**\n")
	b.WriteString("1. Resolve Guardrail Failures\n")
	b.WriteString("2. Fix Verification Failures\n")
	b.WriteString("3. Address Plan Changes\n")
	b.WriteString("4. Address stalling/retry issues if present (often caused by excessive output or repeated tool usage)\n\n")

	// Guardrail failure
	if hasGuardrail {
		b.WriteString("### Guardrail Failure\n\n")
		b.WriteString("- Status: **BLOCKED**\n")
		b.WriteString("- Reason: `")
		b.WriteString(state.PriorGuardrailReason)
		b.WriteString("`\n")
		b.WriteString("- Action Required: ")
		b.WriteString(formatGuardrailBackpressure(state.PriorGuardrailReason))
		b.WriteString("\n\n")
	}

	// Verification failure
	if hasVerifyFail {
		b.WriteString("### Verification Failure\n\n")
		b.WriteString("- Verify Command: `")
		b.WriteString(state.LastVerificationCommand)
		b.WriteString("`\n")
		b.WriteString("- Status: **FAIL**\n")
		if state.ConsecutiveVerifyFails >= 2 {
			b.WriteString("- Consecutive Failures: ")
			b.WriteString(fmt.Sprintf("%d\n", state.ConsecutiveVerifyFails))
			b.WriteString("- **HYPOTHESIS REQUIRED**: Before attempting another fix, you MUST:\n")
			b.WriteString("  1. State your diagnosis of why the previous fix failed\n")
			b.WriteString("  2. Explain what you will do differently this time\n")
			b.WriteString("  3. Only then proceed with the fix\n\n")
		} else {
			b.WriteString("- Action Required: Fix these errors before any new work.\n\n")
		}

		keyErrors := summarizeVerifyOutput(state.LastVerificationOutput, 30)
		if len(keyErrors) > 0 {
			b.WriteString("**Key Errors:**\n\n```\n")
			for _, line := range keyErrors {
				b.WriteString(line)
				b.WriteString("\n")
			}
			b.WriteString("```\n\n")
		}
	}

	// Exit reason from previous iteration
	if hasExitReason {
		b.WriteString("### Prior Exit Reason\n\n")
		b.WriteString("- Reason: `")
		b.WriteString(state.PriorExitReason)
		b.WriteString("`\n")
		switch state.PriorExitReason {
		case "no_progress":
			b.WriteString("- Action Required: Make meaningful progress. Consider:\n")
			b.WriteString("  - Reducing scope to a smaller change\n")
			b.WriteString("  - Re-running verification with additional diagnostics\n")
			b.WriteString("  - Asking a clarifying question via RAUF_QUESTION\n")
			b.WriteString("  - Abandoning the current approach and trying a different strategy\n\n")
		case "no_unchecked_tasks":
			b.WriteString("- Note: All tasks complete. Emit RAUF_COMPLETE if done.\n\n")
		default:
			b.WriteString("\n")
		}
	}

	// Plan drift
	if hasPlanDrift {
		b.WriteString("### Plan Changes Detected\n\n")
		b.WriteString("- Plan was modified in the previous iteration.\n")
		b.WriteString("- Action Required: Keep plan edits minimal and justify them explicitly.\n")
		if state.PlanDiffSummary != "" {
			b.WriteString("\n**Diff excerpt:**\n\n```diff\n")
			b.WriteString(state.PlanDiffSummary)
			b.WriteString("\n```\n")
		}
		b.WriteString("\n")
	}

	// Harness retries
	if hasRetry {
		b.WriteString("### Harness Retries\n\n")
		b.WriteString(fmt.Sprintf("- Retries: %d\n", state.PriorRetryCount))
		if state.PriorRetryReason != "" {
			b.WriteString("- Matched: `" + state.PriorRetryReason + "`\n")
		}
		b.WriteString("- Note: The harness experienced transient failures (e.g., rate limits).\n")
		b.WriteString("- Action: Keep responses concise, avoid large file dumps, reduce tool calls per iteration.\n\n")
	}

	b.WriteString("---\n\n")
	return b.String()
}
