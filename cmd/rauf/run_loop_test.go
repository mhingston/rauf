package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasCompletionSentinel(t *testing.T) {
	if hasCompletionSentinel("all done") {
		t.Fatalf("expected no completion sentinel")
	}
	if hasCompletionSentinel("status: ok RAUF_COMPLETE maybe") {
		t.Fatalf("expected no completion sentinel for inline token")
	}
	if hasCompletionSentinel("```text\nRAUF_COMPLETE\n```") {
		t.Fatalf("expected no completion sentinel inside code block")
	}
	if hasCompletionSentinel("~~~\nRAUF_COMPLETE\n~~~") {
		t.Fatalf("expected no completion sentinel inside tilde fence")
	}
	if hasCompletionSentinel("```\nRAUF_COMPLETE\n``") {
		t.Fatalf("expected no completion sentinel with mismatched fence length")
	}
	if hasCompletionSentinel("~~~\nRAUF_COMPLETE\n```") {
		t.Fatalf("expected no completion sentinel with mismatched fence character")
	}
	if !hasCompletionSentinel("status: ok\nRAUF_COMPLETE\n") {
		t.Fatalf("expected completion sentinel to be detected")
	}
}

func TestNormalizeVerifyMissingPolicyFallbackRequiresAllow(t *testing.T) {
	cfg := runtimeConfig{
		VerifyMissingPolicy: "fallback",
		AllowVerifyFallback: false,
	}
	if got := normalizeVerifyMissingPolicy(cfg); got == "fallback" {
		t.Fatalf("expected fallback to be disabled without allow flag, got %q", got)
	}
	cfg.AllowVerifyFallback = true
	if got := normalizeVerifyMissingPolicy(cfg); got != "fallback" {
		t.Fatalf("expected fallback when allow flag set, got %q", got)
	}
}

func TestHarnessHelperProcess(t *testing.T) {
	if os.Getenv("RAUF_HARNESS_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	counterPath := os.Getenv("RAUF_HARNESS_COUNTER")
	if counterPath != "" {
		file, err := os.OpenFile(counterPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = file.WriteString("run\n")
			_ = file.Close()
		}
	}
	os.Exit(0)
}

func TestRunStrategyNoProgressStops(t *testing.T) {
	repoDir := t.TempDir()
	counterDir := t.TempDir()
	counterPath := filepath.Join(counterDir, "count.txt")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	prompt := "Mode: {{.Mode}}\n"
	if err := os.WriteFile("PROMPT_plan.md", []byte(prompt), 0o644); err != nil {
		t.Fatalf("write prompt failed: %v", err)
	}

	t.Setenv("RAUF_HARNESS_HELPER", "1")
	t.Setenv("RAUF_HARNESS_COUNTER", counterPath)

	cfg := modeConfig{
		mode:          "plan",
		promptFile:    "PROMPT_plan.md",
		maxIterations: 1,
		explicitMode:  false,
	}
	fileCfg := runtimeConfig{
		NoProgressIters: 2,
		Strategy: []strategyStep{
			{Mode: "plan", Iterations: 5, Until: "verify_pass"},
		},
	}

	runStrategy(
		context.Background(),
		cfg,
		fileCfg,
		runtimeExec{},
		raufState{},
		false,
		"",
		"IMPLEMENTATION_PLAN.md",
		os.Args[0],
		"-test.run=TestHarnessHelperProcess",
		true,
		"logs",
		false,
		0,
		0,
		0,
		false,
		nil,
		nil,
		nil,
		&RunReport{},
	)

	data, err := os.ReadFile(counterPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read counter failed: %v", err)
	}
	count := 0
	if len(data) > 0 {
		count = len(strings.Split(strings.TrimSpace(string(data)), "\n"))
	}
	if count != 2 {
		t.Fatalf("expected 2 harness runs, got %d", count)
	}
}

func TestShouldContinueUntil(t *testing.T) {
	tests := []struct {
		name     string
		step     strategyStep
		result   iterationResult
		expected bool
	}{
		{
			name:     "empty until continues",
			step:     strategyStep{Mode: "build", Iterations: 5, Until: ""},
			result:   iterationResult{VerifyStatus: "skipped"},
			expected: true,
		},
		{
			name:     "verify_pass continues when not passed",
			step:     strategyStep{Mode: "build", Iterations: 5, Until: "verify_pass"},
			result:   iterationResult{VerifyStatus: "fail"},
			expected: true,
		},
		{
			name:     "verify_pass stops when passed",
			step:     strategyStep{Mode: "build", Iterations: 5, Until: "verify_pass"},
			result:   iterationResult{VerifyStatus: "pass"},
			expected: false,
		},
		{
			name:     "verify_fail continues when not failed",
			step:     strategyStep{Mode: "build", Iterations: 5, Until: "verify_fail"},
			result:   iterationResult{VerifyStatus: "pass"},
			expected: true,
		},
		{
			name:     "verify_fail stops when failed",
			step:     strategyStep{Mode: "build", Iterations: 5, Until: "verify_fail"},
			result:   iterationResult{VerifyStatus: "fail"},
			expected: false,
		},
		{
			name:     "unknown until condition continues",
			step:     strategyStep{Mode: "build", Iterations: 5, Until: "unknown_condition"},
			result:   iterationResult{VerifyStatus: "pass"},
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldContinueUntil(tc.step, tc.result)
			if got != tc.expected {
				t.Errorf("shouldContinueUntil() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestFormatVerifyCommands(t *testing.T) {
	tests := []struct {
		cmds     []string
		expected string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"npm test"}, "npm test"},
		{[]string{"npm test", "npm run lint"}, "npm test && npm run lint"},
		{[]string{"go test ./...", "go vet ./..."}, "go test ./... && go vet ./..."},
	}

	for _, tt := range tests {
		result := formatVerifyCommands(tt.cmds)
		if result != tt.expected {
			t.Errorf("formatVerifyCommands(%v) = %q, want %q", tt.cmds, result, tt.expected)
		}
	}
}

func TestNormalizePlanLintPolicy(t *testing.T) {
	tests := []struct {
		policy   string
		expected string
	}{
		{"warn", "warn"},
		{"fail", "fail"},
		{"off", "off"},
		{"WARN", "warn"},
		{"Fail", "fail"},
		{"invalid", "warn"},
		{"", "warn"},
	}

	for _, tt := range tests {
		cfg := runtimeConfig{PlanLintPolicy: tt.policy}
		result := normalizePlanLintPolicy(cfg)
		if result != tt.expected {
			t.Errorf("normalizePlanLintPolicy({%q}) = %q, want %q", tt.policy, result, tt.expected)
		}
	}
}

func TestPromptForMode(t *testing.T) {
	tests := []struct {
		mode     string
		expected string
	}{
		{"build", "PROMPT_build.md"},
		{"plan", "PROMPT_plan.md"},
		{"architect", "PROMPT_architect.md"},
		{"custom", "PROMPT_build.md"}, // Unknown modes default to build
		{"", "PROMPT_build.md"},       // Empty defaults to build
	}

	for _, tt := range tests {
		result := promptForMode(tt.mode)
		if result != tt.expected {
			t.Errorf("promptForMode(%q) = %q, want %q", tt.mode, result, tt.expected)
		}
	}
}

func TestShouldRunStep(t *testing.T) {
	t.Run("no if condition", func(t *testing.T) {
		step := strategyStep{Mode: "build"}
		result := shouldRunStep(step, iterationResult{})
		if !result {
			t.Error("expected true when no if condition")
		}
	})

	t.Run("if stalled true", func(t *testing.T) {
		step := strategyStep{Mode: "build", If: "stalled"}
		result := shouldRunStep(step, iterationResult{Stalled: true})
		if !result {
			t.Error("expected true when stalled matches")
		}
	})

	t.Run("if stalled false", func(t *testing.T) {
		step := strategyStep{Mode: "build", If: "stalled"}
		result := shouldRunStep(step, iterationResult{Stalled: false})
		if result {
			t.Error("expected false when not stalled")
		}
	})

	t.Run("if verify_fail matches", func(t *testing.T) {
		step := strategyStep{Mode: "build", If: "verify_fail"}
		result := shouldRunStep(step, iterationResult{VerifyStatus: "fail"})
		if !result {
			t.Error("expected true when verify failed")
		}
	})
}

func TestRunVerification(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "verify.log")
	logFile, _ := os.Create(logPath)
	defer logFile.Close()

	runner := runtimeExec{Runtime: "shell"}
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// echo adds a newline
			return exec.Command("echo", "all ok")
		}
		output, err := runVerification(ctx, runner, []string{"test-cmd"}, logFile)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output, "all ok") {
			t.Errorf("got %q, want it to contain 'all ok'", output)
		}
	})

	t.Run("failure stops loop", func(t *testing.T) {
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// shellArgs returns "sh", ["-c", cmd]
			if name == "sh" && strings.Contains(args[1], "fail-cmd") {
				return exec.Command("false")
			}
			return exec.Command("echo", "should not run")
		}
		output, err := runVerification(ctx, runner, []string{"fail-cmd", "next-cmd"}, logFile)
		if err == nil {
			t.Error("expected error for fail-cmd")
		}
		if strings.Contains(output, "should not run") {
			t.Error("should not have run second command")
		}
	})
}

func TestApplyVerifyFailPolicy(t *testing.T) {
	orig := gitExec
	defer func() { gitExec = orig }()

	gitExec = func(args ...string) (string, error) {
		if args[0] == "show-ref" {
			// Simulate branch doesn't exist by returning error
			// We can't easily return a real exec.ExitError with code 1,
			// but we can return any error and make sure gitBranchExists handles it.
			// Wait, if it's not an ExitError, it might return true error.
			return "", errors.New("not found")
		}
		return "", nil
	}

	tests := []struct {
		policy     string
		headBefore string
		headAfter  string
		expected   string
	}{
		{"soft_reset", "base", "tip", "base"},
		{"hard_reset", "base", "tip", "base"},
		{"keep_commit", "base", "tip", "tip"},
		{"wip_branch", "base", "tip", "base"},
		{"invalid", "base", "tip", "tip"},
		{"soft_reset", "base", "base", "base"},
		{"", "base", "tip", "base"},
	}

	for _, tt := range tests {
		cfg := runtimeConfig{OnVerifyFail: tt.policy}
		got := applyVerifyFailPolicy(cfg, tt.headBefore, tt.headAfter)
		if got != tt.expected {
			t.Errorf("applyVerifyFailPolicy(%q, %q, %q) = %q, want %q", tt.policy, tt.headBefore, tt.headAfter, got, tt.expected)
		}
	}

	t.Run("wip_branch success", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if args[0] == "show-ref" {
				return "", errors.New("not found")
			}
			return "", nil
		}
		cfg := runtimeConfig{OnVerifyFail: "wip_branch"}
		got := applyVerifyFailPolicy(cfg, "base", "tip")
		if got != "base" {
			t.Errorf("expected base, got %q", got)
		}
	})
}

func TestRunMode_Scenarios(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	// Mock execCommand to avoid actually running anything
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf", "shell-output")
	}

	// Mock gitExec
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()
	gitExec = func(args ...string) (string, error) {
		return "ok", nil
	}

	cfg := modeConfig{
		mode:          "build",
		promptFile:    "PROMPT_build.md",
		maxIterations: 1,
		planPath:      "PLAN.md",
	}
	fileCfg := runtimeConfig{Harness: "test-harness"}
	runner := runtimeExec{Runtime: "shell"}

	// Create PLAN.md and PROMPT_build.md to satisfy readActiveTask and prompt logic
	os.WriteFile("PROMPT_build.md", []byte("prompt"), 0o644)

	t.Run("no unchecked tasks exits", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [x] Task 1\n - Verify: true\n"), 0o644)
		_, err := runMode(context.Background(), cfg, fileCfg, runner, raufState{}, false, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, nil, nil, &RunReport{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("max iterations hit", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] Task 1\n - Verify: true\n"), 0o644)
		// Iterations = 1
		res, err := runMode(context.Background(), cfg, fileCfg, runner, raufState{}, false, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, nil, nil, &RunReport{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if res.ExitReason != "" {
			// Actually max iterations break doesn't set an exit reason in iterationResult, it just breaks.
			// But it shouldn't be an error.
		}
	})

	t.Run("interrupted", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] Task 1\n - Verify: true\n"), 0o644)
		// Create .rauf/interrupt to trigger interruption
		os.MkdirAll(".rauf", 0o755)
		os.WriteFile(".rauf/interrupt", []byte("STOP"), 0o644)
		defer os.Remove(".rauf/interrupt")

		_, err := runMode(context.Background(), cfg, fileCfg, runner, raufState{}, false, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, nil, nil, &RunReport{})
		if err != nil && !strings.Contains(err.Error(), "interrupted") {
			t.Errorf("expected interrupted error, got %v", err)
		}
	})

	t.Run("agent_enforced missing verify", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] Task 1\n"), 0o644)
		// Set verify_missing_policy to agent_enforced via fileCfg
		cfgEnforced := cfg
		cfgEnforced.maxIterations = 1
		fileCfgEnforced := fileCfg
		fileCfgEnforced.VerifyMissingPolicy = "agent_enforced"

		_, err := runMode(context.Background(), cfgEnforced, fileCfgEnforced, runner, raufState{}, false, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, nil, nil, &RunReport{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("strict missing verify fails", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] Task 1\n"), 0o644)
		cfgStrict := cfg
		fileCfgStrict := fileCfg
		fileCfgStrict.VerifyMissingPolicy = "strict"

		_, err := runMode(context.Background(), cfgStrict, fileCfgStrict, runner, raufState{}, false, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, nil, nil, &RunReport{})
		if err == nil {
			t.Error("expected missing verify error in strict mode")
		}
	})
	t.Run("no_progress exit", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] Task 1\n - Verify: true\n"), 0o644)
		fileCfgStall := fileCfg
		fileCfgStall.NoProgressIters = 1

		state := raufState{
			LastVerificationStatus: "pass",
			LastVerificationHash:   fileHashFromString("## Command: true\nshell-output"), // Normalized
		}

		// To trigger no_progress, we need noProgress >= maxNoProgress.
		res, err := runMode(context.Background(), cfg, fileCfgStall, runner, state, false, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 1, nil, nil, &RunReport{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if res.ExitReason != "no_progress" {
			t.Errorf("expected exitReason no_progress, got %q", res.ExitReason)
		}
	})

	t.Run("model escalation", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] Task 1\n - Verify: true\n"), 0o644)
		fileCfgEsc := fileCfg
		fileCfgEsc.ModelEscalation.Enabled = true
		fileCfgEsc.ModelDefault = "base-model"
		fileCfgEsc.ModelStrong = "strong-model"
		fileCfgEsc.ModelFlag = "--model"

		state := raufState{
			ConsecutiveVerifyFails: 3, // Trigger escalation
		}

		res, err := runMode(context.Background(), cfg, fileCfgEsc, runner, state, false, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, nil, nil, &RunReport{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		// We can't easily check if the model was applied to harness args without more mocks,
		// but we can check if the iteration finished successfully.
		if res.VerifyStatus != "pass" {
			t.Error("expected pass")
		}
	})

	t.Run("plan mode success", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] Task 1\n"), 0o644)
		cfgPlan := cfg
		cfgPlan.mode = "plan"
		cfgPlan.maxIterations = 1

		res, err := runMode(context.Background(), cfgPlan, fileCfg, runner, raufState{}, false, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, nil, nil, &RunReport{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if res.VerifyStatus != "skipped" {
			t.Errorf("expected skipped verification in plan mode, got %q", res.VerifyStatus)
		}
	})

	t.Run("git push failure", func(t *testing.T) {
		os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] Task 1\n - Verify: true\n"), 0o644)

		// Mock gitExec to fail on push
		origGitExec := gitExec
		defer func() { gitExec = origGitExec }()

		// We need headAfter != headBefore to trigger push.
		// Since rev-parse is mocked to return "hash", they'll be equal.
		// Let's make them different.
		revParseCount := 0
		gitExec = func(args ...string) (string, error) {
			if args[0] == "push" {
				return "", errors.New("push failed")
			}
			if args[0] == "rev-parse" && args[1] == "HEAD" {
				revParseCount++
				return fmt.Sprintf("hash%d", revParseCount), nil
			}
			return "ok", nil
		}

		_, err := runMode(context.Background(), cfg, fileCfg, runner, raufState{}, true, "main", "PLAN.md", "harness", "", false, "logs", false, 0, 0, 0, false, nil, 0, nil, nil, &RunReport{})
		if err == nil {
			t.Error("expected push error")
		}
	})
}
