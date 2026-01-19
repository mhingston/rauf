package main

import (
	"os"
	"strings"
	"testing"
)

func TestCheckCompletionArtifacts_Paths(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	// Case 1: Empty specRefs
	ok, reason, sat, ver := checkCompletionArtifacts(nil)
	if !ok || reason != "" || len(sat) != 0 || len(ver) != 0 {
		t.Errorf("Empty specRefs: got %v, %q, %v, %v", ok, reason, sat, ver)
	}

	// Case 2: Invalid spec path (outside root or nonexistent)
	ok, reason, _, _ = checkCompletionArtifacts([]string{"nonexistent.md"})
	if ok || !strings.Contains(reason, "no such file") {
		t.Errorf("Invalid spec path: got %v, %q", ok, reason)
	}

	ok, reason, _, _ = checkCompletionArtifacts([]string{"/etc/passwd"})
	if ok || !strings.Contains(reason, "invalid spec path") {
		t.Errorf("Path outside root: got %v, %q", ok, reason)
	}

	// Case 3: Valid spec, no contract section
	os.WriteFile("test.md", []byte("# Title\nNo contract here."), 0o644)
	ok, reason, sat, ver = checkCompletionArtifacts([]string{"test.md"})
	if !ok || len(sat) != 0 || len(ver) != 0 {
		t.Errorf("No contract: got %v, %q, %v, %v", ok, reason, sat, ver)
	}

	// Case 4: Valid contract, no artifacts
	specContent := "---\nid: test\n---\n## 4. Completion Contract\nSuccess condition:\n- Done"
	os.WriteFile("spec.md", []byte(specContent), 0o644)
	ok, reason, sat, ver = checkCompletionArtifacts([]string{"spec.md"})
	if !ok || len(sat) != 1 || sat[0] != "spec.md" {
		t.Errorf("No artifacts in contract: got %v, %q, %v", ok, reason, sat)
	}

	// Case 5: Missing artifacts
	specWithArt := specContent + "\n\nArtifacts/flags:\n- missing.txt"
	os.WriteFile("spec_art.md", []byte(specWithArt), 0o644)
	ok, reason, sat, ver = checkCompletionArtifacts([]string{"spec_art.md"})
	if ok || !strings.Contains(reason, "missing artifacts: missing.txt") {
		t.Errorf("Missing artifacts: got %v, %q", ok, reason)
	}

	// Case 6: Verified artifacts
	os.WriteFile("found.txt", []byte("data"), 0o644)
	specFound := specContent + "\n\nArtifacts/flags:\n- found.txt"
	os.WriteFile("spec_found.md", []byte(specFound), 0o644)
	ok, reason, sat, ver = checkCompletionArtifacts([]string{"spec_found.md"})
	if !ok || len(ver) != 1 || ver[0] != "found.txt" {
		t.Errorf("Found artifacts: got %v, %q, %v", ok, reason, ver)
	}
}
