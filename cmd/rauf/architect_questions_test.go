package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestExtractQuestions(t *testing.T) {
	output := `
Some thoughts.
RAUF_QUESTION: What is the target directory?
RAUF_QUESTION: Should I overwrite existing files?
`
	questions := extractTypedQuestions(output)
	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}
	if questions[0].Question != "What is the target directory?" {
		t.Errorf("got %q", questions[0].Question)
	}
}

func TestMaxArchitectQuestionsForState(t *testing.T) {
	state := raufState{}
	if maxArchitectQuestionsForState(state) != baseArchitectQuestions {
		t.Error("expected base limit")
	}

	state.PriorGuardrailStatus = "fail"
	if maxArchitectQuestionsForState(state) != baseArchitectQuestions+bonusQuestionsPerFailure {
		t.Error("expected bonus for guardrail failure")
	}

	state.LastVerificationStatus = "fail"
	if maxArchitectQuestionsForState(state) != baseArchitectQuestions+2*bonusQuestionsPerFailure {
		t.Error("expected bonus for both failures")
	}
}

func TestRunArchitectQuestions(t *testing.T) {
	ctx := context.Background()
	runner := runtimeExec{Runtime: "shell"}
	prompt := "Original prompt"
	output := "RAUF_QUESTION: How are you?\n"
	state := raufState{}

	// Mock input: "Fine\n"
	input := strings.NewReader("Fine\n")
	var outputWriter strings.Builder

	// We need a harness that won't fail or do real work.
	// Since runArchitectQuestions calls runHarness -> runHarnessOnce -> runner.runShell,
	// if we set harness to "true", it should exit 0.
	harness := "true"
	harnessArgs := ""

	// Create a temp log file
	logFile, _ := os.CreateTemp("", "rauf-test-log")
	defer os.Remove(logFile.Name())
	defer logFile.Close()

	updatedOutput, ok := runArchitectQuestions(ctx, runner, &prompt, output, state, harness, harnessArgs, logFile, retryConfig{}, input, &outputWriter)

	if !ok {
		t.Error("expected ok=true since a question was asked and answered")
	}
	if !strings.Contains(prompt, "Fine") {
		t.Error("prompt should have been updated with the answer")
	}
	if !strings.Contains(outputWriter.String(), "Architect question: How are you?") {
		t.Error("output should contain the question prompt")
	}
	_ = updatedOutput
}
