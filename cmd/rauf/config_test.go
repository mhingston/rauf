package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigBytes_Basic(t *testing.T) {
	yaml := `
harness: npm test
no_push: true
log_dir: mylogs
runtime: docker
docker_image: node:latest
`
	var cfg runtimeConfig
	err := parseConfigBytes([]byte(yaml), &cfg)
	if err != nil {
		t.Fatalf("parseConfigBytes failed: %v", err)
	}
	if cfg.Harness != "npm test" || !cfg.NoPush || cfg.LogDir != "mylogs" || cfg.Runtime != "docker" || cfg.DockerImage != "node:latest" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestParseConfigBytes_Escalation(t *testing.T) {
	yaml := `
model_escalation:
  enabled: true
  min_strong_iterations: 5
  max_escalations: 3
  trigger:
    consecutive_verify_fails: 1
`
	var cfg runtimeConfig
	cfg.ModelEscalation = defaultEscalationConfig()
	err := parseConfigBytes([]byte(yaml), &cfg)
	if err != nil {
		t.Fatalf("parseConfigBytes failed: %v", err)
	}
	if !cfg.ModelEscalation.Enabled || cfg.ModelEscalation.CooldownIters != 5 || cfg.ModelEscalation.MaxEscalations != 3 || cfg.ModelEscalation.ConsecutiveVerifyFails != 1 {
		t.Errorf("unexpected escalation config: %+v", cfg.ModelEscalation)
	}
}

func TestParseConfigBytes_Recovery(t *testing.T) {
	yaml := `
recovery:
  consecutive_verify_fails: 3
  no_progress_iters: 4
  guardrail_failures: 5
`
	var cfg runtimeConfig
	// Note: In real usage, loadConfig sets defaults first.
	// Here we test pure parsing to ensure YAML keys map to the struct.

	err := parseConfigBytes([]byte(yaml), &cfg)
	if err != nil {
		t.Fatalf("parseConfigBytes failed: %v", err)
	}

	if cfg.Recovery.ConsecutiveVerifyFails != 3 {
		t.Errorf("got %d, want 3", cfg.Recovery.ConsecutiveVerifyFails)
	}
	if cfg.Recovery.NoProgressIters != 4 {
		t.Errorf("got %d, want 4", cfg.Recovery.NoProgressIters)
	}
	if cfg.Recovery.GuardrailFailures != 5 {
		t.Errorf("got %d, want 5", cfg.Recovery.GuardrailFailures)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rauf.yaml")
	content := "harness: go test ./..."
	os.WriteFile(path, []byte(content), 0o644)

	cfg, ok, err := loadConfig(path)
	if err != nil || !ok {
		t.Fatalf("loadConfig failed: %v, %v", err, ok)
	}
	if cfg.Harness != "go test ./..." {
		t.Errorf("got %q, want 'go test ./...'", cfg.Harness)
	}

	// Missing file
	_, ok, err = loadConfig(filepath.Join(dir, "nonexistent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected ok=false for missing file")
	}
}

func TestParseConfigBytes_Extra(t *testing.T) {
	yaml := `
harness: test-harness
harness_args: --arg=1
no_push: true
log_dir: /logs
runtime: docker
docker_image: ubuntu:latest
docker_args: -v /tmp:/tmp
docker_container: rauf-container
max_files_changed: 10
max_commits_per_iteration: 5
forbidden_paths: .env
no_progress_iterations: 15
on_verify_fail: soft_reset
verify_missing_policy: strict
allow_verify_fallback: true
require_verify_on_change: true
require_verify_for_plan_update: true
retry_on_failure: true
retry_max_attempts: 5
retry_backoff_base: 500ms
retry_backoff_max: 5s
retry_jitter: true
retry_match: error,fail
plan_lint_policy: strict
model_default: gpt-3.5
model_strong: gpt-4
model_flag: --model
model_override: true
model_escalation:
  enabled: true
  cooldown_iters: 5
  max_escalations: 2
  consecutive_verify_fails: 3
  no_progress_iters: 4
  guardrail_failures: 2
recovery:
  consecutive_verify_fails: 5
  no_progress_iters: 10
  guardrail_failures: 5
strategy:
  - mode: plan
    iterations: 3
    until: verify_pass
    if: verify_fail
`
	var cfg runtimeConfig
	err := parseConfigBytes([]byte(yaml), &cfg)
	if err != nil {
		t.Errorf("parseConfigBytes error: %v", err)
	}

	if cfg.Harness != "test-harness" {
		t.Errorf("Harness: got %q", cfg.Harness)
	}
	if !cfg.NoPush {
		t.Errorf("NoPush: got %v", cfg.NoPush)
	}
	if cfg.MaxFilesChanged != 10 {
		t.Errorf("MaxFilesChanged: got %d", cfg.MaxFilesChanged)
	}
	if len(cfg.Strategy) != 1 {
		t.Errorf("Strategy len: got %d", len(cfg.Strategy))
	}
	if cfg.Strategy[0].Mode != "plan" {
		t.Errorf("Strategy mode: got %q", cfg.Strategy[0].Mode)
	}

	// Test multiline rejection warning logic (by ensuring no crash/error).
	yamlMultiline := `
key: |
  multiline
  value
`
	err = parseConfigBytes([]byte(yamlMultiline), &cfg)
	if err != nil {
		t.Errorf("parseConfigBytes multiline error: %v", err)
	}
}
