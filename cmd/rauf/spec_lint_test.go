package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintSpecsMissingCompletionContract(t *testing.T) {
	dir := t.TempDir()
	chdirTemp(t, dir)

	if err := os.MkdirAll("specs", 0o755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	spec := `---
id: test
status: approved
---

# Test
`
	if err := os.WriteFile(filepath.Join("specs", "test.md"), []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	err := lintSpecs()
	if err == nil || !strings.Contains(err.Error(), "missing Completion Contract") {
		t.Fatalf("expected missing completion contract error, got %v", err)
	}
}

func TestLintSpecsMissingVerificationCommands(t *testing.T) {
	dir := t.TempDir()
	chdirTemp(t, dir)

	if err := os.MkdirAll("specs", 0o755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	spec := `---
id: test
status: approved
---

# Test

## 4. Completion Contract
Success condition:
- ok
`
	if err := os.WriteFile(filepath.Join("specs", "test.md"), []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	err := lintSpecs()
	if err == nil || !strings.Contains(err.Error(), "no verification commands") {
		t.Fatalf("expected missing verification commands error, got %v", err)
	}
}

func TestLintSpecsVerificationTBD(t *testing.T) {
	dir := t.TempDir()
	chdirTemp(t, dir)

	if err := os.MkdirAll("specs", 0o755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	spec := `---
id: test
status: approved
---

# Test

## 4. Completion Contract
Success condition:
- ok

Verification commands:
- TBD: add command
`
	if err := os.WriteFile(filepath.Join("specs", "test.md"), []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	err := lintSpecs()
	if err == nil || !strings.Contains(err.Error(), "contains TBD") {
		t.Fatalf("expected TBD error, got %v", err)
	}
}

func TestLintSpecsDraftIgnored(t *testing.T) {
	dir := t.TempDir()
	chdirTemp(t, dir)

	if err := os.MkdirAll("specs", 0o755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	spec := `---
id: test
status: draft
---

# Test
`
	if err := os.WriteFile(filepath.Join("specs", "test.md"), []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	if err := lintSpecs(); err != nil {
		t.Fatalf("expected draft spec to be ignored, got %v", err)
	}
}

func TestLintSpecsIgnoresFencedLists(t *testing.T) {
	dir := t.TempDir()
	chdirTemp(t, dir)

	if err := os.MkdirAll("specs", 0o755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	spec := "---\n" +
		"id: test\n" +
		"status: approved\n" +
		"---\n" +
		"\n" +
		"# Test\n" +
		"\n" +
		"## 4. Completion Contract\n" +
		"Success condition:\n" +
		"- ok\n" +
		"\n" +
		"Verification commands:\n" +
		"- go test ./...\n" +
		"\n" +
		"Artifacts/flags:\n" +
		"```text\n" +
		"- not-an-artifact.txt\n" +
		"```\n"
	if err := os.WriteFile(filepath.Join("specs", "test.md"), []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	if err := lintSpecs(); err != nil {
		t.Fatalf("expected fenced list to be ignored, got %v", err)
	}
}

func TestCheckCompletionArtifacts(t *testing.T) {
	dir := t.TempDir()
	chdirTemp(t, dir)

	if err := os.MkdirAll("specs", 0o755); err != nil {
		t.Fatalf("mkdir specs: %v", err)
	}
	spec := `---
id: test
status: approved
---

# Test

## 4. Completion Contract
Success condition:
- ok

Verification commands:
- go test ./...

Artifacts/flags:
- output.txt
`
	specPath := filepath.Join("specs", "test.md")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	ok, reason, satisfied, verified := checkCompletionArtifacts([]string{specPath})
	if ok || reason == "" {
		t.Fatalf("expected missing artifact failure, got ok=%t reason=%q", ok, reason)
	}
	if len(satisfied) != 0 || len(verified) != 0 {
		t.Fatalf("expected no satisfied specs or verified artifacts, got %v %v", satisfied, verified)
	}

	if err := os.WriteFile("output.txt", []byte("ok"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	ok, reason, satisfied, verified = checkCompletionArtifacts([]string{specPath})
	if !ok || reason != "" {
		t.Fatalf("expected artifacts check to pass, got ok=%t reason=%q", ok, reason)
	}
	if len(satisfied) != 1 || satisfied[0] != specPath {
		t.Fatalf("expected spec to be satisfied, got %v", satisfied)
	}
	if len(verified) != 1 || verified[0] != "output.txt" {
		t.Fatalf("expected verified artifact to be output.txt, got %v", verified)
	}
}
