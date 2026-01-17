package main

import (
	"io"
	"os"
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
		cfg,
		fileCfg,
		runtimeExec{},
		raufState{},
		false,
		"",
		"IMPLEMENTATION_PLAN.md",
		"test",
		false,
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
