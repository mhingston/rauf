package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnforceGuardrailsForbiddenPathsBoundary(t *testing.T) {
	t.Run("allows similar prefix", func(t *testing.T) {
		repoDir := t.TempDir()
		head := initGitRepo(t, repoDir)
		chdirTemp(t, repoDir)

		if err := os.MkdirAll(filepath.Join(repoDir, "specs-other"), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "specs-other", "ok.txt"), []byte("ok"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		ok, reason := enforceGuardrails(runtimeConfig{ForbiddenPaths: []string{"specs"}}, head, head)
		if !ok {
			t.Fatalf("expected allowed change, got %s", reason)
		}
	})

	t.Run("blocks exact prefix", func(t *testing.T) {
		repoDir := t.TempDir()
		head := initGitRepo(t, repoDir)
		chdirTemp(t, repoDir)

		if err := os.MkdirAll(filepath.Join(repoDir, "specs"), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repoDir, "specs", "blocked.txt"), []byte("nope"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		ok, reason := enforceGuardrails(runtimeConfig{ForbiddenPaths: []string{"specs"}}, head, head)
		if ok || !strings.HasPrefix(reason, "forbidden_path:") {
			t.Fatalf("expected forbidden path guardrail, got ok=%t reason=%s", ok, reason)
		}
	})
}

func TestEnforceMissingVerifyGuardrailPlanPathRelative(t *testing.T) {
	repoDir := t.TempDir()
	head := initGitRepo(t, repoDir)
	chdirTemp(t, repoDir)

	planPath := "IMPLEMENTATION_PLAN.md"
	if err := os.WriteFile(filepath.Join(repoDir, planPath), []byte("# plan\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "other.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "add plan")
	head = runGit(t, repoDir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(repoDir, planPath), []byte("# plan updated\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	files, gitErr := listChangedFiles(head, head)
	if gitErr {
		t.Fatalf("listChangedFiles returned git error")
	}
	if len(files) != 1 || files[0] != planPath {
		status, _ := gitOutput("status", "--porcelain")
		t.Fatalf("unexpected changed files: %v (status=%q)", files, status)
	}
	ok, reason := enforceMissingVerifyGuardrail(planPath, head, head, true)
	if !ok {
		t.Fatalf("expected plan-only change allowed, got %s", reason)
	}

	if err := os.WriteFile(filepath.Join(repoDir, "other.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	ok, reason = enforceMissingVerifyGuardrail(planPath, head, head, true)
	if ok {
		t.Fatalf("expected non-plan change blocked, got ok=%t reason=%s", ok, reason)
	}
}

func TestGitOutputRawPreservesStatusSpaces(t *testing.T) {
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)
	chdirTemp(t, repoDir)

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	out, err := gitOutputRaw("status", "--porcelain")
	if err != nil {
		t.Fatalf("git status failed: %v", err)
	}
	lines := splitStatusLines(out)
	if len(lines) != 1 {
		t.Fatalf("expected 1 status line, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], " M ") {
		t.Fatalf("expected porcelain line to preserve leading space, got %q", lines[0])
	}
}

func TestEnforceGuardrailsForbiddenPathsRename(t *testing.T) {
	repoDir := t.TempDir()
	head := initGitRepo(t, repoDir)
	chdirTemp(t, repoDir)

	runGit(t, repoDir, "config", "status.renameLimit", "0")

	if err := os.MkdirAll(filepath.Join(repoDir, "allowed"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoDir, "specs"), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "allowed", "old.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "add allowed")
	head = runGit(t, repoDir, "rev-parse", "HEAD")

	if err := os.Rename(filepath.Join(repoDir, "allowed", "old.txt"), filepath.Join(repoDir, "specs", "renamed.txt")); err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	ok, reason := enforceGuardrails(runtimeConfig{ForbiddenPaths: []string{"specs"}}, head, head)
	if ok || !strings.HasPrefix(reason, "forbidden_path:") {
		t.Fatalf("expected forbidden path guardrail on rename, got ok=%t reason=%s", ok, reason)
	}
}

func initGitRepo(t *testing.T, dir string) string {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return runGit(t, dir, "rev-parse", "HEAD")
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func chdirTemp(t *testing.T, dir string) {
	t.Helper()
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
}

func TestUnquoteGitPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"unquoted path", "simple/path.txt", "simple/path.txt"},
		{"quoted path with spaces", `"path with spaces/file.txt"`, "path with spaces/file.txt"},
		{"quoted path with escape", `"path\\with\\backslash"`, `path\with\backslash`},
		{"quoted path with quotes", `"say \"hello\""`, `say "hello"`},
		{"quoted path with newline", `"line1\nline2"`, "line1\nline2"},
		{"quoted path with tab", `"col1\tcol2"`, "col1\tcol2"},
		{"quoted path with octal", `"\302\240"`, "\302\240"},
		{"empty string", "", ""},
		{"just quotes", `""`, ""},
		{"single quote not handled", "'single'", "'single'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := unquoteGitPath(tc.input)
			if result != tc.expected {
				t.Errorf("unquoteGitPath(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestParseStatusPathWithQuotes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple path", "simple.txt", "simple.txt"},
		{"quoted path", `"path with spaces.txt"`, "path with spaces.txt"},
		{"rename with quoted dest", `old.txt -> "new file.txt"`, "new file.txt"},
		{"rename both quoted", `"old file.txt" -> "new file.txt"`, "new file.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseStatusPath(tc.input)
			if result != tc.expected {
				t.Errorf("parseStatusPath(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}
func TestEnforceVerificationGuardrails(t *testing.T) {
	cfg := runtimeConfig{
		RequireVerifyForPlanUpdate: true,
		RequireVerifyOnChange:      true,
	}

	tests := []struct {
		name            string
		verifyStatus    string
		planChanged     bool
		worktreeChanged bool
		expectedOk      bool
		expectedReason  string
	}{
		{"plan update with verify pass", "pass", true, false, true, ""},
		{"plan update without verify pass", "fail", true, false, false, "plan_update_without_verify"},
		{"change with verify pass", "pass", false, true, true, ""},
		{"change with verify skipped", "skipped", false, true, false, "verify_required_for_change"},
		{"nothing changed", "skipped", false, false, true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := enforceVerificationGuardrails(cfg, tt.verifyStatus, tt.planChanged, tt.worktreeChanged)
			if ok != tt.expectedOk || reason != tt.expectedReason {
				t.Errorf("enforceVerificationGuardrails() = (%v, %q), want (%v, %q)", ok, reason, tt.expectedOk, tt.expectedReason)
			}
		})
	}
}

func TestEnforceMissingVerifyNoGit(t *testing.T) {
	tests := []struct {
		name              string
		planChanged       bool
		fingerprintBefore string
		fingerprintAfter  string
		expectedOk        bool
		expectedReason    string
	}{
		{"plan not updated", false, "a", "b", false, "missing_verify_plan_not_updated"},
		{"plan updated only", true, "a", "a", true, ""},
		{"plan and other changed", true, "a", "b", false, "missing_verify_non_plan_change"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := enforceMissingVerifyNoGit(tt.planChanged, tt.fingerprintBefore, tt.fingerprintAfter)
			if ok != tt.expectedOk || reason != tt.expectedReason {
				t.Errorf("enforceMissingVerifyNoGit() = (%v, %q), want (%v, %q)", ok, reason, tt.expectedOk, tt.expectedReason)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	result := splitLines("a\nb\nc")
	if len(result) != 3 {
		t.Errorf("expected 3 lines, got %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("unexpected content: %v", result)
	}
}

func TestEnforceGuardrailsCommitCount(t *testing.T) {
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("within limit", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "rev-list" {
				return "2", nil
			}
			return "", nil
		}
		ok, reason := enforceGuardrails(runtimeConfig{MaxCommits: 5}, "HEAD~5", "HEAD")
		if !ok {
			t.Errorf("expected pass, got reason=%s", reason)
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "rev-list" {
				return "10", nil
			}
			return "", nil
		}
		ok, reason := enforceGuardrails(runtimeConfig{MaxCommits: 5}, "HEAD~10", "HEAD")
		if ok || reason != "max_commits_exceeded" {
			t.Errorf("expected max_commits_exceeded, got ok=%t reason=%s", ok, reason)
		}
	})

	t.Run("git error on commit count", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "rev-list" {
				return "", exec.ErrNotFound
			}
			return "", nil
		}
		ok, reason := enforceGuardrails(runtimeConfig{MaxCommits: 5}, "HEAD~5", "HEAD")
		if ok || reason != "git_error_commit_count" {
			t.Errorf("expected git_error_commit_count, got ok=%t reason=%s", ok, reason)
		}
	})
}

func TestEnforceGuardrailsGitErrorFilelist(t *testing.T) {
	// Test git error on diff path (different heads)
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("git error on diff with max files guardrail", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			// Allow rev-list to pass, but fail on diff
			if len(args) >= 1 && args[0] == "diff" {
				return "", exec.ErrNotFound
			}
			return "", nil
		}
		// Different heads trigger diff path
		ok, reason := enforceGuardrails(runtimeConfig{MaxFilesChanged: 5}, "abc", "def")
		if ok || reason != "git_error_file_list" {
			t.Errorf("expected git_error_file_list, got ok=%t reason=%s", ok, reason)
		}
	})

	t.Run("git error on diff with forbidden paths guardrail", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 1 && args[0] == "diff" {
				return "", exec.ErrNotFound
			}
			return "", nil
		}
		ok, reason := enforceGuardrails(runtimeConfig{ForbiddenPaths: []string{"secret"}}, "abc", "def")
		if ok || reason != "git_error_file_list" {
			t.Errorf("expected git_error_file_list, got ok=%t reason=%s", ok, reason)
		}
	})
}

func TestListChangedFiles(t *testing.T) {
	origGitExec := gitExec
	defer func() { gitExec = origGitExec }()

	t.Run("diff mode with changes", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "diff" && args[1] == "--name-only" {
				return "file1.go\nfile2.go", nil
			}
			return "", nil
		}
		files, gitErr := listChangedFiles("HEAD~1", "HEAD")
		if gitErr {
			t.Error("unexpected git error")
		}
		if len(files) != 2 {
			t.Errorf("expected 2 files, got %d", len(files))
		}
	})

	t.Run("diff mode git error", func(t *testing.T) {
		gitExec = func(args ...string) (string, error) {
			return "", exec.ErrNotFound
		}
		_, gitErr := listChangedFiles("HEAD~1", "HEAD")
		if !gitErr {
			t.Error("expected git error")
		}
	})

	t.Run("status mode integration", func(t *testing.T) {
		// Restore real gitExec for integration test
		gitExec = origGitExec

		// Use real git repo for status mode testing
		repoDir := t.TempDir()
		head := initGitRepo(t, repoDir)
		chdirTemp(t, repoDir)

		// Create a modified file
		if err := os.WriteFile(filepath.Join(repoDir, "newfile.txt"), []byte("test"), 0o644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		// Same head means status mode
		files, gitErr := listChangedFiles(head, head)
		if gitErr {
			t.Error("unexpected git error")
		}
		if len(files) != 1 {
			t.Errorf("expected 1 file, got %d: %v", len(files), files)
		}
	})
}

func TestUnquoteGitPath_OctalSequence(t *testing.T) {
	// \302\240 is non-breaking space (utf-8 bytes)
	// \400 is invalid (> 255)

	t.Run("valid octal", func(t *testing.T) {
		input := "\"\\302\\240\""
		expected := "\u00a0"
		if got := unquoteGitPath(input); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})

	t.Run("invalid octal", func(t *testing.T) {
		input := "\"\\400\""
		// Logic: if val > 255, it keeps the backslash and moves on.
		// So it should result in "\\400"
		expected := "\\400"
		if got := unquoteGitPath(input); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})
}
