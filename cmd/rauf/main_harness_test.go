package main

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestRunHarness_RetryLogic(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	ctx := context.Background()
	runner := runtimeExec{Runtime: "shell"}

	retryCfg := retryConfig{
		Enabled:     true,
		MaxAttempts: 2,
		Match:       []string{"retry me"},
		BackoffBase: 1 * time.Millisecond,
		Jitter:      false,
	}

	t.Run("success on first try", func(t *testing.T) {
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("echo", "success")
		}
		res, err := runHarness(ctx, "prompt", "test-cmd", "", nil, retryCfg, runner)
		if err != nil {
			t.Fatal(err)
		}
		if res.RetryCount != 0 {
			t.Errorf("got %d retries, want 0", res.RetryCount)
		}
	})

	t.Run("retry then success", func(t *testing.T) {
		calls := 0
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			calls++
			if calls == 1 {
				return exec.CommandContext(ctx, "sh", "-c", "echo 'retry me'; exit 1")
			}
			return exec.CommandContext(ctx, "echo", "success")
		}
		res, err := runHarness(ctx, "prompt", "test-cmd", "", nil, retryCfg, runner)
		if err != nil {
			t.Fatal(err)
		}
		if res.RetryCount != 1 {
			t.Errorf("got %d retries, want 1", res.RetryCount)
		}
		if res.RetryReason != "retry me" {
			t.Errorf("got reason %q, want 'retry me'", res.RetryReason)
		}
	})

	t.Run("max attempts reached", func(t *testing.T) {
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("sh", "-c", "echo 'retry me'; exit 1")
		}
		res, err := runHarness(ctx, "prompt", "test-cmd", "", nil, retryCfg, runner)
		if err == nil {
			t.Error("expected error after max retries")
		}
		if res.RetryCount != 2 {
			t.Errorf("got %d retries, want 2", res.RetryCount)
		}
	})
}

func TestRunHarnessOnce_Timeout(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	runner := runtimeExec{Runtime: "shell"}
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately so the command should fail or return canceled
	cancel()

	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sleep", "1")
	}

	_, err := runHarnessOnce(ctx, "prompt", "test-cmd", "", nil, runner)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}
