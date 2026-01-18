package main

import (
	"os"
	"strings"
	"testing"
)

func TestLintSpecs(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	t.Run("no specs dir", func(t *testing.T) {
		err := lintSpecs()
		if err != nil {
			t.Errorf("expected nil error when specs dir missing, got %v", err)
		}
	})

	os.MkdirAll("specs", 0o755)

	t.Run("empty specs dir", func(t *testing.T) {
		err := lintSpecs()
		if err != nil {
			t.Errorf("expected nil error for empty specs dir, got %v", err)
		}
	})

	t.Run("valid spec", func(t *testing.T) {
		content := `---
status: stable
---
## Completion Contract
Verification Commands:
- go test ./...
`
		os.WriteFile("specs/valid.md", []byte(content), 0o644)
		err := lintSpecs()
		if err != nil {
			t.Errorf("expected nil error for valid spec, got %v", err)
		}
	})

	t.Run("invalid spec - missing contract", func(t *testing.T) {
		content := `---
status: stable
---
## Other Section
`
		os.WriteFile("specs/invalid.md", []byte(content), 0o644)
		err := lintSpecs()
		if err == nil {
			t.Error("expected error for missing completion contract")
		}
		if !strings.Contains(err.Error(), "missing Completion Contract section") {
			t.Errorf("unexpected error message: %v", err)
		}
		os.Remove("specs/invalid.md")
	})

	t.Run("invalid spec - TBD in command", func(t *testing.T) {
		content := `---
status: stable
---
## Completion Contract
Verification Commands:
- TBD
`
		os.WriteFile("specs/tbd.md", []byte(content), 0o644)
		err := lintSpecs()
		if err == nil {
			t.Error("expected error for TBD in command")
		}
		if !strings.Contains(err.Error(), "verification command contains TBD") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestCheckCompletionArtifacts(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	content := `---
status: stable
---
## Completion Contract
Artifacts/Flags:
- out.bin
`
	os.WriteFile("spec.md", []byte(content), 0o644)

	t.Run("missing artifact", func(t *testing.T) {
		ok, reason, satisfied, verified := checkCompletionArtifacts([]string{"spec.md"})
		if ok {
			t.Error("expected failure for missing artifact")
		}
		if !strings.Contains(reason, "missing artifacts: out.bin") {
			t.Errorf("unexpected reason: %q", reason)
		}
		if len(satisfied) != 0 {
			t.Errorf("expected 0 satisfied, got %d", len(satisfied))
		}
		if len(verified) != 0 {
			t.Errorf("expected 0 verified, got %d", len(verified))
		}
	})

	t.Run("satisfied artifact", func(t *testing.T) {
		os.WriteFile("out.bin", []byte("data"), 0o644)
		ok, reason, satisfied, verified := checkCompletionArtifacts([]string{"spec.md"})
		if !ok {
			t.Errorf("expected success, got error: %v", reason)
		}
		if len(satisfied) != 1 || satisfied[0] != "spec.md" {
			t.Errorf("unexpected satisfied: %v", satisfied)
		}
		if len(verified) != 1 || verified[0] != "out.bin" {
			t.Errorf("unexpected verified: %v", verified)
		}
	})
}
