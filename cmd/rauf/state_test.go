package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStatePath(t *testing.T) {
	expected := filepath.Join(".rauf", "state.json")
	if statePath() != expected {
		t.Errorf("statePath() = %q, want %q", statePath(), expected)
	}
}

func TestStateSummaryPath(t *testing.T) {
	expected := filepath.Join(".rauf", "state.md")
	if stateSummaryPath() != expected {
		t.Errorf("stateSummaryPath() = %q, want %q", stateSummaryPath(), expected)
	}
}

func TestTruncateStateSummary(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		result := truncateStateSummary("hello")
		if result != "hello" {
			t.Errorf("expected unchanged, got %q", result)
		}
	})

	t.Run("long string truncated", func(t *testing.T) {
		// Create a string longer than 4KB
		long := make([]byte, 5*1024)
		for i := range long {
			long[i] = 'a'
		}
		result := truncateStateSummary(string(long))
		if len(result) > 4*1024 {
			t.Errorf("expected max 4KB, got %d bytes", len(result))
		}
	})
}

func TestLoadState_FileNotFound(t *testing.T) {
	// Change to temp dir with no state file
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	state := loadState()
	if state.LastVerificationStatus != "" {
		t.Error("expected empty state when file not found")
	}
}

func TestLoadState_InvalidJSON(t *testing.T) {
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	// Create .rauf directory and invalid state file
	os.MkdirAll(".rauf", 0o755)
	os.WriteFile(filepath.Join(".rauf", "state.json"), []byte("not valid json"), 0o644)

	state := loadState()
	// Should return empty state on parse error
	if state.LastVerificationStatus != "" {
		t.Error("expected empty state on invalid JSON")
	}
}

func TestLoadState_ValidJSON(t *testing.T) {
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	state := raufState{
		LastVerificationStatus: "pass",
		ConsecutiveVerifyFails: 2,
		CurrentModel:           "opus",
	}
	data, _ := json.Marshal(state)
	os.MkdirAll(".rauf", 0o755)
	os.WriteFile(filepath.Join(".rauf", "state.json"), data, 0o644)

	loaded := loadState()
	if loaded.LastVerificationStatus != "pass" {
		t.Errorf("expected 'pass', got %q", loaded.LastVerificationStatus)
	}
	if loaded.ConsecutiveVerifyFails != 2 {
		t.Errorf("expected 2, got %d", loaded.ConsecutiveVerifyFails)
	}
	if loaded.CurrentModel != "opus" {
		t.Errorf("expected 'opus', got %q", loaded.CurrentModel)
	}
}

func TestSaveState(t *testing.T) {
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	state := raufState{
		LastVerificationStatus:  "fail",
		LastVerificationCommand: "npm test",
		ConsecutiveVerifyFails:  1,
		CurrentModel:            "sonnet",
		EscalationCount:         1,
		RecoveryMode:            "verify",
	}

	err := saveState(state)
	if err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	// Check state.json was created
	data, err := os.ReadFile(filepath.Join(".rauf", "state.json"))
	if err != nil {
		t.Fatalf("failed to read state.json: %v", err)
	}

	var loaded raufState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to parse state.json: %v", err)
	}

	if loaded.LastVerificationStatus != "fail" {
		t.Error("state not saved correctly")
	}

	// Check state.md was created
	if _, err := os.Stat(filepath.Join(".rauf", "state.md")); os.IsNotExist(err) {
		t.Error("expected state.md to be created")
	}

	t.Run("write failure", func(t *testing.T) {
		// Override statePath to return an invalid path (directory that doesn't exist)
		origStatePath := statePath
		defer func() { statePath = origStatePath }()
		// Create a file named "file_as_dir"
		os.WriteFile("file_as_dir", []byte("content"), 0o644)
		defer os.Remove("file_as_dir")

		statePath = func() string {
			// Trying to use "file_as_dir" as a directory will fail MkdirAll or CreateTemp
			return filepath.Join("file_as_dir", "state.json")
		}

		if err := saveState(state); err == nil {
			t.Error("expected error when directory does not exist")
		}
	})
}

func TestWriteStateSummary_AllFields(t *testing.T) {
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	os.MkdirAll(".rauf", 0o755)

	state := raufState{
		LastVerificationStatus:       "fail",
		LastVerificationCommand:      "npm test",
		LastVerificationOutput:       "Error: test failed",
		CurrentModel:                 "opus",
		EscalationCount:              2,
		MinStrongIterationsRemaining: 1,
		LastEscalationReason:         "consecutive_verify_fails",
		RecoveryMode:                 "verify",
	}

	err := writeStateSummary(state)
	if err != nil {
		t.Fatalf("writeStateSummary failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(".rauf", "state.md"))
	content := string(data)

	checks := []string{
		"Last verification status: fail",
		"Last verification command: npm test",
		"Current model: opus",
		"Escalations: 2",
		"Min strong iterations remaining: 1",
		"Last escalation reason: consecutive_verify_fails",
		"Mode: verify",
	}

	for _, check := range checks {
		if !contains(content, check) {
			t.Errorf("expected %q in state summary", check)
		}
	}
}

func TestWriteStateSummary_DefaultModel(t *testing.T) {
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	os.MkdirAll(".rauf", 0o755)

	state := raufState{
		CurrentModel:    "", // Empty = default
		EscalationCount: 1,  // Triggers model section
	}

	writeStateSummary(state)
	data, _ := os.ReadFile(filepath.Join(".rauf", "state.md"))
	if !contains(string(data), "Current model: default") {
		t.Error("expected 'Current model: default' for empty model")
	}
}

func TestAddAssumption(t *testing.T) {
	state := raufState{}

	// Add first assumption
	state = addAssumption(state, "Question 1", "STICKY", 1, "verify")
	if len(state.Assumptions) != 1 {
		t.Errorf("expected 1 assumption, got %d", len(state.Assumptions))
	}
	if state.Assumptions[0].Question != "Question 1" {
		t.Errorf("expected \"Question 1\", got %q", state.Assumptions[0].Question)
	}
	if state.Assumptions[0].StickyScope != "STICKY" {
		t.Errorf("expected \"STICKY\", got %q", state.Assumptions[0].StickyScope)
	}

	// Add duplicate (should be ignored)
	state = addAssumption(state, "Question 1", "", 2, "verify")
	if len(state.Assumptions) != 1 {
		t.Errorf("expected 1 assumption after duplicate add, got %d", len(state.Assumptions))
	}

	// Add distinct assumption
	state = addAssumption(state, "Question 2", "", 1, "verify")
	if len(state.Assumptions) != 2 {
		t.Errorf("expected 2 assumptions, got %d", len(state.Assumptions))
	}
}

func TestArchiveAssumptions(t *testing.T) {
	state := raufState{
		Assumptions: []Assumption{
			{Question: "Q1", StickyScope: "STICKY", CreatedRecoveryMode: "verify"}, // Sticky: keep
			{Question: "Q2", StickyScope: "", CreatedRecoveryMode: "verify"},       // Non-sticky, matching mode: archive
			{Question: "Q3", StickyScope: "", CreatedRecoveryMode: "other"},        // Non-sticky, wrong mode: keep
		},
	}

	state = archiveAssumptions(state, "verify", "success", 5, "hash123")

	// Check active assumptions
	if len(state.Assumptions) != 2 {
		t.Errorf("expected 2 active assumptions, got %d", len(state.Assumptions))
	}
	if state.Assumptions[0].Question != "Q1" {
		t.Errorf("expected Q1 to remain active (sticky)")
	}
	if state.Assumptions[1].Question != "Q3" {
		t.Errorf("expected Q3 to remain active (wrong mode)")
	}

	// Check archived assumptions
	if len(state.ArchivedAssumptions) != 1 {
		t.Errorf("expected 1 archived assumption, got %d", len(state.ArchivedAssumptions))
	}
	archived := state.ArchivedAssumptions[0]
	if archived.Question != "Q2" {
		t.Errorf("expected Q2 to be archived")
	}
	if archived.ClearedReason != "success" {
		t.Errorf("expected reason \"success\", got %q", archived.ClearedReason)
	}
	if archived.ClearedIteration != 5 {
		t.Errorf("expected iteration 5, got %d", archived.ClearedIteration)
	}
	if archived.RelatedVerifyHash != "hash123" {
		t.Errorf("expected hash \"hash123\", got %q", archived.RelatedVerifyHash)
	}
}

func TestSaveState_SummaryFailure(t *testing.T) {
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	os.MkdirAll(".rauf", 0o755)

	state := raufState{LastVerificationStatus: "pass"}

	// Override stateSummaryPath to invalid path
	origSummaryPath := stateSummaryPath
	defer func() { stateSummaryPath = origSummaryPath }()

	// Create a file blocking directory creation or file usage
	os.WriteFile("block_summary", []byte("locked"), 0o644)

	stateSummaryPath = func() string {
		return filepath.Join("block_summary", "state.md")
	}

	// Should NOT error
	if err := saveState(state); err != nil {
		t.Errorf("expected success (warning only) on summary write fail, got error: %v", err)
	}
}
