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
