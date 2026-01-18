package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRunMode_Architect(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	// Mock execCommand
	callCount := 0
	origExec := execCommand
	defer func() { execCommand = origExec }()
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		callCount++
		if callCount == 1 {
			// First run: return question
			return exec.Command("printf", "Cycle 1\nRAUF_QUESTION: string\n")
		}
		// Second run: return completion or just output
		return exec.Command("printf", "Cycle 2\n")
	}

	// Mock gitExec
	origGit := gitExec
	defer func() { gitExec = origGit }()
	gitExec = func(args ...string) (string, error) {
		return "ok", nil
	}

	// Setup inputs
	input := bytes.NewBufferString("My Answer\n")
	var output bytes.Buffer

	cfg := modeConfig{
		mode:          "architect",
		promptFile:    "PROMPT_architect.md",
		maxIterations: 1,
	}
	// Need PROMPT_architect.md
	os.WriteFile("PROMPT_architect.md", []byte("Arch Prompt"), 0o644)

	fileCfg := runtimeConfig{}
	runner := runtimeExec{Runtime: "shell"}

	result, err := runMode(context.Background(), cfg, fileCfg, runner, raufState{}, false, "main", "", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, input, &output, &RunReport{})
	if err != nil {
		t.Fatalf("runMode failed: %v", err)
	}

	// Verify interaction
	outStr := output.String()
	if !strings.Contains(outStr, "Architect question: string") {
		t.Error("expected question in output")
	}

	// Check loop ran twice (initial + follow-up)
	// But runMode loop is outer loop.
	// runArchitectQuestions loops internally?
	// yes, runArchitectQuestions loops until no questions.
	// So execCommand should be called:
	// 1. runHarness (initial) -> returns question
	// 2. runArchitectQuestions -> sees question -> prompts -> reads input -> runHarness (follow-up)
	// 3. follow-up returns "Cycle 2" (no question)
	// 4. runArchitectQuestions breaks.
	// 5. runMode loop finishes iteration 1.

	if callCount < 2 {
		t.Errorf("expected at least 2 harness runs, got %d", callCount)
	}

	if result.ExitReason != "" {
		// Should finish iteration normally
	}
}
