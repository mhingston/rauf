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
