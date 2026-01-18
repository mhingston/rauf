package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitBranchExists(t *testing.T) {
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("exists", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "hash", nil
		}
		exists, err := gitBranchExists("main")
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Error("expected branch to exist")
		}
	})

	t.Run("not found by error message", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "", errors.New("not found")
		}
		exists, err := gitBranchExists("missing")
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Error("expected branch not to exist")
		}
	})

	t.Run("not found by exit code 1", func(t *testing.T) {
		// Create a real ExitError by running a command that fails
		cmd := exec.Command("false")
		err := cmd.Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal("failed to create real ExitError")
		}

		gitExec = func(args ...string) (string, error) {
			return "", exitErr
		}

		exists, err := gitBranchExists("missing")
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Error("expected branch not to exist")
		}
	})

	t.Run("other error", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "", errors.New("permission denied")
		}
		_, err := gitBranchExists("main")
		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestResolvePlanPath_Config(t *testing.T) {
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("returns config path", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "custom/plan.md", nil
		}
		path := resolvePlanPath("main", true, "fallback.md")
		if path != "custom/plan.md" {
			t.Errorf("got %q, want custom/plan.md", path)
		}
	})

	t.Run("git unavailable", func(t *testing.T) {
		path := resolvePlanPath("main", false, "fallback.md")
		if path != "fallback.md" {
			t.Errorf("got %q, want fallback.md", path)
		}
	})

	t.Run("empty branch", func(t *testing.T) {
		path := resolvePlanPath("", true, "fallback.md")
		if path != "fallback.md" {
			t.Errorf("got %q, want fallback.md", path)
		}
	})

	t.Run("config not found", func(t *testing.T) {
		// Create a real ExitError
		cmd := exec.Command("false")
		err := cmd.Run()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal("failed to create real ExitError")
		}

		gitExec = func(args ...string) (string, error) {
			return "", exitErr
		}
		path := resolvePlanPath("main", true, "fallback.md")
		if path != "fallback.md" {
			t.Errorf("got %q, want fallback.md", path)
		}
	})

	t.Run("config returns empty", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "", nil
		}
		path := resolvePlanPath("main", true, "fallback.md")
		if path != "fallback.md" {
			t.Errorf("got %q, want fallback.md", path)
		}
	})
}

func TestGitCheckout(t *testing.T) {
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("success", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "checkout" {
				return "", nil
			}
			return "", nil
		}
		err := gitCheckout("main")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("error", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "", errors.New("checkout failed")
		}
		err := gitCheckout("nonexistent")
		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestGitConfigUnset(t *testing.T) {
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("success", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "config" && args[1] == "--unset" {
				return "", nil
			}
			return "", nil
		}
		err := gitConfigUnset("branch.test.raufScoped")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("error", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "", errors.New("config unset failed")
		}
		err := gitConfigUnset("nonexistent.key")
		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestRunPlanWork(t *testing.T) {
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("empty name", func(t *testing.T) {
		err := runPlanWork("")
		if err == nil || !strings.Contains(err.Error(), "requires a name") {
			t.Errorf("expected 'requires a name' error, got: %v", err)
		}
	})

	t.Run("whitespace only name", func(t *testing.T) {
		err := runPlanWork("   ")
		if err == nil || !strings.Contains(err.Error(), "requires a name") {
			t.Errorf("expected 'requires a name' error, got: %v", err)
		}
	})

	t.Run("git unavailable", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "branch" {
				return "", errors.New("git not found")
			}
			return "", nil
		}
		err := runPlanWork("test-feature")
		if err == nil || !strings.Contains(err.Error(), "git is required") {
			t.Errorf("expected 'git is required' error, got: %v", err)
		}
	})

	t.Run("git returns empty branch", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "branch" {
				return "", nil
			}
			return "", nil
		}
		err := runPlanWork("test-feature")
		if err == nil || !strings.Contains(err.Error(), "git is required") {
			t.Errorf("expected 'git is required' error, got: %v", err)
		}
	})

	t.Run("slug derivation failure", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "branch" {
				return "main", nil
			}
			return "", nil
		}
		err := runPlanWork("!@#$%^&*()")
		if err == nil || !strings.Contains(err.Error(), "unable to derive branch") {
			t.Errorf("expected 'unable to derive branch' error, got: %v", err)
		}
	})

	t.Run("existing branch checkout success", func(t *testing.T) {
		tmpDir := t.TempDir()
		oldWd, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(oldWd)

		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "branch" && args[1] == "--show-current" {
				return "main", nil
			}
			if len(args) >= 1 && args[0] == "show-ref" {
				return "abc123", nil // branch exists
			}
			if len(args) >= 1 && args[0] == "checkout" {
				return "", nil
			}
			if len(args) >= 1 && args[0] == "config" {
				return "", nil
			}
			return "", nil
		}
		err := runPlanWork("test-feature")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		// Check that plan file exists
		if _, err := os.Stat(filepath.Join(tmpDir, ".rauf", "IMPLEMENTATION_PLAN.md")); os.IsNotExist(err) {
			t.Error("expected plan file to be created")
		}
	})

	t.Run("new branch creation", func(t *testing.T) {
		tmpDir := t.TempDir()
		oldWd, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(oldWd)

		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "branch" && args[1] == "--show-current" {
				return "main", nil
			}
			if len(args) >= 1 && args[0] == "show-ref" {
				return "", errors.New("not found") // branch doesn't exist
			}
			if len(args) >= 2 && args[0] == "checkout" && args[1] == "-b" {
				return "", nil
			}
			if len(args) >= 1 && args[0] == "config" {
				return "", nil
			}
			return "", nil
		}
		err := runPlanWork("new-feature")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("branch exists check error", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "branch" && args[1] == "--show-current" {
				return "main", nil
			}
			if len(args) >= 1 && args[0] == "show-ref" {
				return "", errors.New("git internal error")
			}
			return "", nil
		}
		err := runPlanWork("test-feature")
		if err == nil {
			t.Error("expected error from gitBranchExists")
		}
	})

	t.Run("checkout error triggers no rollback", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "branch" && args[1] == "--show-current" {
				return "main", nil
			}
			if len(args) >= 1 && args[0] == "show-ref" {
				return "abc", nil
			}
			if len(args) >= 1 && args[0] == "checkout" {
				return "", errors.New("checkout failed")
			}
			return "", nil
		}
		err := runPlanWork("test-feature")
		if err == nil || !strings.Contains(err.Error(), "checkout failed") {
			t.Errorf("expected checkout error, got: %v", err)
		}
	})

	t.Run("config set failure with rollback", func(t *testing.T) {
		tmpDir := t.TempDir()
		oldWd, _ := os.Getwd()
		os.Chdir(tmpDir)
		defer os.Chdir(oldWd)

		checkoutCalls := 0
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "branch" && args[1] == "--show-current" {
				return "main", nil
			}
			if len(args) >= 1 && args[0] == "show-ref" {
				return "abc", nil
			}
			if len(args) >= 1 && args[0] == "checkout" {
				checkoutCalls++
				return "", nil
			}
			if len(args) >= 1 && args[0] == "config" {
				return "", errors.New("config failed")
			}
			return "", nil
		}
		err := runPlanWork("test-feature")
		if err == nil || !strings.Contains(err.Error(), "failed to set branch config") {
			t.Errorf("expected config error, got: %v", err)
		}
		// Should have called checkout at least twice (initial + rollback)
		if checkoutCalls < 2 {
			t.Errorf("expected rollback checkout, got %d calls", checkoutCalls)
		}
	})
}
