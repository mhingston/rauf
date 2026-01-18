package main

import (
	"os"
	"testing"
	"time"
)

func TestParseConfigBytes_Comprehensive(t *testing.T) {
	yaml := `
harness: my-harness
harness_args: --args
retry_on_failure: true
retry_max_attempts: 5
retry_backoff_base: 1s
retry_backoff_max: 10s
retry_jitter: true
retry_match:
  - "error 1"
  - "error 2"
strategy:
  - mode: plan
    iterations: 1
    until: verify_pass
  - mode: build
    if: stalled
    until: completion
on_verify_fail: hard_reset
verify_missing_policy: fallback
allow_verify_fallback: true
plan_lint_policy: fail
model_default: gpt-4
model_strong: gpt-4-32k
model_flag: --model
model_escalation:
  enabled: true
  max_escalations: 2
  min_strong_iterations: 3
  guardrail_failures: 2
  no_progress_iters: 4
`
	cfg := &runtimeConfig{}
	err := parseConfigBytes([]byte(yaml), cfg)
	if err != nil {
		t.Fatalf("parseConfigBytes failed: %v", err)
	}

	if cfg.Harness != "my-harness" {
		t.Errorf("got harness %q, want my-harness", cfg.Harness)
	}
	if cfg.RetryMaxAttempts != 5 {
		t.Errorf("got max attempts %d, want 5", cfg.RetryMaxAttempts)
	}
	if cfg.RetryBackoffBase != time.Second {
		t.Errorf("got backoff base %v, want 1s", cfg.RetryBackoffBase)
	}
	if len(cfg.RetryMatch) != 2 {
		t.Errorf("got %d retry matches, want 2", len(cfg.RetryMatch))
	}
	if len(cfg.Strategy) != 2 {
		t.Errorf("got %d strategy steps, want 2", len(cfg.Strategy))
	}
	if cfg.Strategy[0].Mode != "plan" {
		t.Errorf("got step 0 mode %q, want plan", cfg.Strategy[0].Mode)
	}
	if cfg.Strategy[1].If != "stalled" {
		t.Errorf("got step 1 if %q, want stalled", cfg.Strategy[1].If)
	}
	if cfg.OnVerifyFail != "hard_reset" {
		t.Errorf("got on_verify_fail %q, want hard_reset", cfg.OnVerifyFail)
	}
	if cfg.PlanLintPolicy != "fail" {
		t.Errorf("got plan_lint_policy %q, want fail", cfg.PlanLintPolicy)
	}
	if cfg.ModelEscalation.Enabled != true {
		t.Error("expected model escalation to be enabled")
	}
	if cfg.ModelEscalation.GuardrailFailures != 2 {
		t.Errorf("got guardrail failures %d, want 2", cfg.ModelEscalation.GuardrailFailures)
	}
	if cfg.ModelEscalation.MinStrongIterations != 3 {
		t.Errorf("got cooldown iters %d, want 3", cfg.ModelEscalation.MinStrongIterations)
	}
	if cfg.ModelEscalation.NoProgressIters != 4 {
		t.Errorf("got no progress iters %d, want 4", cfg.ModelEscalation.NoProgressIters)
	}
}

func TestParseConfigBytes_EdgeCases(t *testing.T) {
	yaml := `
harness: something
# Comment
  # Indented comment
invalid_line
strategy:
  - mode: build
    iterations: invalid
    until: "quoted value" # comment
forbidden_paths:
  - path1
  - path2
model_escalation:
  enabled: maybe
  guardrail_failures: 5
`
	cfg := &runtimeConfig{}
	err := parseConfigBytes([]byte(yaml), cfg)
	if err != nil {
		t.Fatalf("should not fail for invalid values: %v", err)
	}
	if len(cfg.ForbiddenPaths) != 2 {
		t.Errorf("got %v, want 2 paths", cfg.ForbiddenPaths)
	}
	// iterations: invalid should be ignored, stay at 0
	if cfg.Strategy[0].Iterations != 0 {
		t.Errorf("got iters %d, want 0", cfg.Strategy[0].Iterations)
	}
}

func TestLoadConfig_NotFound(t *testing.T) {
	// Should return default config if file missing
	cfg, found, err := loadConfig("non_existent_rauf.yaml")
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if found {
		t.Error("expected not found")
	}
	if cfg.OnVerifyFail != "soft_reset" {
		t.Error("expected default config")
	}
}

func TestLoadConfig_ReadError(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/unreadable.yaml"
	if err := os.WriteFile(path, []byte(""), 0o200); err != nil {
		t.Fatal(err)
	}
	// Make unreadable
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644) // Cleanup

	_, _, err := loadConfig(path)
	if err == nil {
		t.Error("expected read error")
	}
}
