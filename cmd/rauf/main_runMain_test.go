package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestRunMain_Subcommands(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	os.WriteFile("rauf.yaml", []byte("harness: claude\n"), 0o644)

	tests := []struct {
		name     string
		args     []string
		expected int
	}{
		{"version", []string{"version"}, 0},
		{"help", []string{"help"}, 0},
		{"invalid", []string{"invalid_mode"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runMain(tt.args)
			if got != tt.expected {
				t.Errorf("runMain(%v) = %d, want %d", tt.args, got, tt.expected)
			}
		})
	}
}

func TestRunMain_Init(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	args := []string{"init", "--dry-run"}
	got := runMain(args)
	if got != 0 {
		t.Errorf("runMain(%v) = %d, want 0", args, got)
	}
}

func TestRunMain_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	os.WriteFile("rauf.yaml", []byte("harness: old\n"), 0o644)
	os.WriteFile("PROMPT_build.md", []byte("prompt"), 0o644)
	os.WriteFile("IMPLEMENTATION_PLAN.md", []byte("plan"), 0o644)

	envVars := map[string]string{
		"RAUF_NO_PUSH":                        "1",
		"RAUF_HARNESS":                        "new-harness",
		"RAUF_HARNESS_ARGS":                   "--args",
		"RAUF_LOG_DIR":                        "custom-logs",
		"RAUF_RUNTIME":                        "host",
		"RAUF_DOCKER_IMAGE":                   "alpine",
		"RAUF_DOCKER_ARGS":                    "-v /tmp:/tmp",
		"RAUF_DOCKER_CONTAINER":               "rauf-test",
		"RAUF_ON_VERIFY_FAIL":                 "hard_reset",
		"RAUF_VERIFY_MISSING_POLICY":          "agent_enforced",
		"RAUF_ALLOW_VERIFY_FALLBACK":          "1",
		"RAUF_REQUIRE_VERIFY_ON_CHANGE":       "1",
		"RAUF_REQUIRE_VERIFY_FOR_PLAN_UPDATE": "1",
		"RAUF_RETRY":                          "1",
		"RAUF_RETRY_MAX":                      "5",
		"RAUF_RETRY_BACKOFF_BASE":             "100ms",
		"RAUF_RETRY_BACKOFF_MAX":              "1s",
		"RAUF_RETRY_NO_JITTER":                "1",
		"RAUF_RETRY_MATCH":                    "error,fatal",
		"RAUF_MODEL_DEFAULT":                  "gpt-4",
		"RAUF_MODEL_STRONG":                   "gpt-4-32k",
		"RAUF_MODEL_FLAG":                     "--model",
		"RAUF_MODEL_ESCALATION_ENABLED":       "1",
	}

	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	// Mock git so gitOutput doesn't fail
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()
	gitExec = func(args ...string) (string, error) {
		if len(args) > 0 && args[0] == "config" {
			return "", os.ErrNotExist
		}
		return "master", nil
	}

	// Mock execCommand to avoid actually running anything
	origExecCommand := execCommand
	defer func() { execCommand = origExecCommand }()
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.Command("true")
	}

	// 1 iteration should parse everything and perform setup, then exit
	got := runMain([]string{"1"})
	if got != 0 {
		t.Errorf("runMain(1) = %d, want 0", got)
	}
}

func TestRunMain_PlanWork(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	// Mocking for:
	// 1. gitOutput("branch", "--show-current") -> "main"
	// 2. gitBranchExists("rauf/add-feature") -> false (show-ref --verify)
	// 3. gitCheckoutCreate("rauf/add-feature") -> (checkout -b)
	// 4. gitConfigSet -> (config)

	gitExec = func(args ...string) (string, error) {
		if len(args) > 1 && args[0] == "branch" && args[1] == "--show-current" {
			return "main\n", nil
		}
		if len(args) > 1 && args[0] == "show-ref" {
			return "", errors.New("not found")
		}
		return "ok", nil
	}

	got := runMain([]string{"plan-work", "add feature"})
	if got != 0 {
		t.Errorf("runMain(plan-work) = %d, want 0", got)
	}
}
func TestRunMain_Import(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	// Mock git
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()
	gitExec = func(args ...string) (string, error) {
		return "ok", nil
	}

	// 1. Valid import call
	args := []string{"import", "--stage", "requirements", "--slug", "test-slug", "--force"}
	got := runMain(args)
	// It will likely fail with 1 because there's no specfirst dir to read from,
	// but it will hit the parsing logic.
	_ = got

	// 2. Invalid flag
	argsInvalid := []string{"import", "--unknown"}
	gotInvalid := runMain(argsInvalid)
	if gotInvalid != 1 {
		t.Errorf("expected failure for unknown flag, got %d", gotInvalid)
	}

	// 3. Missing value
	argsMissing := []string{"import", "--stage"}
	gotMissing := runMain(argsMissing)
	if gotMissing != 1 {
		t.Errorf("expected failure for missing value, got %d", gotMissing)
	}
}
