package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type raufState struct {
	LastVerificationOutput  string `json:"last_verification_output"`
	LastVerificationCommand string `json:"last_verification_command"`
	LastVerificationStatus  string `json:"last_verification_status"`
	LastVerificationHash    string `json:"last_verification_hash"`
	PriorGuardrailStatus    string `json:"prior_guardrail_status"`
	PriorGuardrailReason    string `json:"prior_guardrail_reason"`
	PriorExitReason         string `json:"prior_exit_reason"`
	PlanHashBefore          string `json:"plan_hash_before"`
	PlanHashAfter           string `json:"plan_hash_after"`
	PlanDiffSummary         string `json:"plan_diff_summary"`
	PriorRetryCount         int    `json:"prior_retry_count"`
	PriorRetryReason        string `json:"prior_retry_reason"`
	ConsecutiveVerifyFails  int    `json:"consecutive_verify_fails"`
	BackpressureInjected    bool   `json:"backpressure_injected"`
}

func loadState() raufState {
	path := statePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: failed to read state file %s: %v\n", path, err)
		}
		return raufState{}
	}
	var state raufState
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse state file %s: %v (using empty state)\n", path, err)
		return raufState{}
	}
	return state
}

func saveState(state raufState) error {
	path := statePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	// Use atomic write: write to temp file then rename
	// This prevents corruption if two processes write simultaneously
	tempFile, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()

	// Ensure cleanup on any error
	success := false
	defer func() {
		if !success {
			tempFile.Close()
			os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(data); err != nil {
		return err
	}
	if err := tempFile.Sync(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	// Atomic rename
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	success = true

	// Write summary file - errors here are non-fatal since primary state was saved
	if err := writeStateSummary(state); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write state summary: %v\n", err)
	}
	return nil
}

func statePath() string {
	return filepath.Join(".rauf", "state.json")
}

func stateSummaryPath() string {
	return filepath.Join(".rauf", "state.md")
}

func writeStateSummary(state raufState) error {
	var b strings.Builder
	b.WriteString("# rauf state\n\n")
	b.WriteString("Updated: ")
	b.WriteString(time.Now().UTC().Format(time.RFC3339))
	b.WriteString("\n\n")
	b.WriteString("Last verification status: ")
	if state.LastVerificationStatus == "" {
		b.WriteString("unknown")
	} else {
		b.WriteString(state.LastVerificationStatus)
	}
	b.WriteString("\n")
	b.WriteString("Last verification command: ")
	if strings.TrimSpace(state.LastVerificationCommand) == "" {
		b.WriteString("none")
	} else {
		b.WriteString(state.LastVerificationCommand)
	}
	b.WriteString("\n")

	if strings.TrimSpace(state.LastVerificationOutput) != "" {
		b.WriteString("\nLast verification output (truncated):\n\n```text\n")
		b.WriteString(truncateStateSummary(state.LastVerificationOutput))
		b.WriteString("\n```\n")
	}

	// Use atomic write pattern for consistency
	path := stateSummaryPath()
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, ".state-summary-*.md.tmp")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()

	writeSuccess := false
	defer func() {
		if !writeSuccess {
			tempFile.Close()
			os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.WriteString(b.String()); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	writeSuccess = true
	return nil
}

func truncateStateSummary(value string) string {
	const maxSummaryBytes = 4 * 1024
	if len(value) <= maxSummaryBytes {
		return value
	}
	// Truncate by bytes, then back up to valid UTF-8 boundary
	truncated := value[:maxSummaryBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}
