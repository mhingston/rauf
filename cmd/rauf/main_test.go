package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseArgsModes(t *testing.T) {
	cfg, err := parseArgs([]string{"--help"})
	if err != nil {
		t.Fatalf("help parse failed: %v", err)
	}
	if cfg.mode != "help" {
		t.Fatalf("expected help mode, got %s", cfg.mode)
	}

	cfg, err = parseArgs([]string{"--version"})
	if err != nil {
		t.Fatalf("version parse failed: %v", err)
	}
	if cfg.mode != "version" {
		t.Fatalf("expected version mode, got %s", cfg.mode)
	}

	cfg, err = parseArgs([]string{"init", "--force", "--dry-run"})
	if err != nil {
		t.Fatalf("init parse failed: %v", err)
	}
	if cfg.mode != "init" || !cfg.forceInit || !cfg.dryRunInit {
		t.Fatalf("expected init flags to be set")
	}

	cfg, err = parseArgs([]string{"plan", "3"})
	if err != nil {
		t.Fatalf("plan parse failed: %v", err)
	}
	if cfg.mode != "plan" || cfg.maxIterations != 3 {
		t.Fatalf("expected plan mode with max 3")
	}

	cfg, err = parseArgs([]string{"5"})
	if err != nil {
		t.Fatalf("numeric parse failed: %v", err)
	}
	if cfg.mode != "build" || cfg.maxIterations != 5 {
		t.Fatalf("expected build mode with max 5")
	}

	cfg, err = parseArgs([]string{"import", "--stage", "requirements", "--slug", "user-auth"})
	if err != nil {
		t.Fatalf("import parse failed: %v", err)
	}
	if cfg.mode != "import" || cfg.importStage != "requirements" || cfg.importSlug != "user-auth" {
		t.Fatalf("expected import with stage and slug")
	}
}

func TestSplitArgs(t *testing.T) {
	args, err := splitArgs("--foo bar --name=\"hello world\" --path='a b'")
	if err != nil {
		t.Fatalf("split args failed: %v", err)
	}
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(args))
	}
	if args[2] != "--name=hello world" {
		t.Fatalf("unexpected quoted arg: %q", args[2])
	}
	if args[3] != "--path=a b" {
		t.Fatalf("unexpected single-quoted arg: %q", args[3])
	}
}

func TestHasUncheckedTasks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMPLEMENTATION_PLAN.md")
	content := "# Plan\n- [ ] T1: Do it\n- [x] T2: Done\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !hasUncheckedTasks(path) {
		t.Fatalf("expected unchecked tasks")
	}
}

func TestRunInitDryRun(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	if err := runInit(false, true); err != nil {
		t.Fatalf("init dry run failed: %v", err)
	}

	if _, err := os.Stat("PROMPT_build.md"); err == nil {
		t.Fatalf("expected no files created during dry run")
	}
}

func TestHasAgentsPlaceholders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	content := "# AGENTS\n\n- Tests: [test command]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !hasAgentsPlaceholders(path) {
		t.Fatalf("expected placeholder detection")
	}
}

func TestSlugify(t *testing.T) {
	value := slugify("User Auth v2")
	if value != "user-auth-v2" {
		t.Fatalf("unexpected slug: %q", value)
	}
}

func TestParseConfigBytes(t *testing.T) {
	cfg := runtimeConfig{Model: make(map[string]string)}
	data := []byte(`
harness: opencode
harness_args: "--foo bar"
no_push: true
yolo: false
log_dir: logs-out
retry_on_failure: true
retry_max_attempts: 4
retry_backoff_base: 1s
retry_backoff_max: 10s
retry_jitter: false
retry_match: "rate limit,429"
model:
  architect: opus
  plan: opus
  build: sonnet
`)
	if err := parseConfigBytes(data, &cfg); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}
	if cfg.Harness != "opencode" || cfg.HarnessArgs != "--foo bar" {
		t.Fatalf("unexpected harness config")
	}
	if cfg.LogDir != "logs-out" {
		t.Fatalf("unexpected log dir: %q", cfg.LogDir)
	}
	if !cfg.NoPush || cfg.Yolo {
		t.Fatalf("unexpected bool config")
	}
	if !cfg.RetryOnFailure || cfg.RetryMaxAttempts != 4 {
		t.Fatalf("unexpected retry config")
	}
	if cfg.RetryBackoffBase != time.Second || cfg.RetryBackoffMax != 10*time.Second {
		t.Fatalf("unexpected retry backoff config")
	}
	if cfg.RetryJitter {
		t.Fatalf("unexpected retry jitter config")
	}
	if len(cfg.RetryMatch) != 2 || cfg.RetryMatch[0] != "rate limit" || cfg.RetryMatch[1] != "429" {
		t.Fatalf("unexpected retry match config")
	}
	if cfg.Model["build"] != "sonnet" {
		t.Fatalf("unexpected model map")
	}
}
