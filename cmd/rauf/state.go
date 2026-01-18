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
	// Model escalation state
	CurrentModel                 string `json:"current_model,omitempty"`
	EscalationCount              int    `json:"escalation_count,omitempty"`
	MinStrongIterationsRemaining int    `json:"min_strong_iterations_remaining,omitempty"`
	ConsecutiveGuardrailFails    int    `json:"consecutive_guardrail_fails,omitempty"`
	NoProgressStreak             int    `json:"no_progress_streak,omitempty"`
	LastEscalationReason         string `json:"last_escalation_reason,omitempty"`
	RecoveryMode                 string `json:"recovery_mode,omitempty"`
	// Hypothesis tracking
	Hypotheses []Hypothesis `json:"hypotheses,omitempty"`
	// Assumption tracking
	Assumptions         []Assumption         `json:"assumptions,omitempty"`
	ArchivedAssumptions []ArchivedAssumption `json:"archived_assumptions,omitempty"`
}

type Assumption struct {
	Question            string `json:"question"`
	Type                string `json:"type"` // e.g. "ASSUMPTION"
	CreatedIteration    int    `json:"created_iteration"`
	CreatedRecoveryMode string `json:"created_recovery_mode"`
	StickyScope         string `json:"sticky_scope"` // "sticky", "global", or empty
}

type ArchivedAssumption struct {
	Assumption
	ClearedReason       string    `json:"cleared_reason"`
	ClearedIteration    int       `json:"cleared_iteration"`
	ClearedRecoveryMode string    `json:"cleared_recovery_mode"`
	RelatedVerifyHash   string    `json:"related_verify_hash,omitempty"`
	ArchivedAt          time.Time `json:"archived_at"`
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

var statePath = func() string {
	return filepath.Join(".rauf", "state.json")
}

var stateSummaryPath = func() string {
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

	// Model escalation status
	if state.CurrentModel != "" || state.EscalationCount > 0 {
		b.WriteString("\n## Model Escalation\n\n")
		b.WriteString("Current model: ")
		if state.CurrentModel == "" {
			b.WriteString("default")
		} else {
			b.WriteString(state.CurrentModel)
		}
		b.WriteString("\n")
		if state.EscalationCount > 0 {
			b.WriteString(fmt.Sprintf("Escalations: %d\n", state.EscalationCount))
		}
		if state.MinStrongIterationsRemaining > 0 {
			b.WriteString(fmt.Sprintf("Min strong iterations remaining: %d\n", state.MinStrongIterationsRemaining))
		}
		if state.LastEscalationReason != "" {
			b.WriteString("Last escalation reason: ")
			b.WriteString(state.LastEscalationReason)
			b.WriteString("\n")
		}
	}

	// Recovery mode status
	if state.RecoveryMode != "" {
		b.WriteString("\n## Recovery Mode\n\n")
		b.WriteString("Mode: ")
		b.WriteString(state.RecoveryMode)
		b.WriteString("\n")
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

// addAssumption adds a new assumption to the state.
func addAssumption(state raufState, q string, stickyScope string, iteration int, recoveryMode string) raufState {
	// Check for duplicates
	for _, a := range state.Assumptions {
		if a.Question == q {
			// Update stickiness if re-assumed as sticky?
			// If duplicate, and new one is sticky while old isn't, maybe upgrade?
			// For now, simple duplicate check.
			return state
		}
	}
	state.Assumptions = append(state.Assumptions, Assumption{
		Question:            q,
		Type:                "ASSUMPTION",
		CreatedIteration:    iteration,
		CreatedRecoveryMode: recoveryMode,
		StickyScope:         stickyScope,
	})
	return state
}

// archiveAssumptions moves non-sticky assumptions matching the mode to the archive.
func archiveAssumptions(state raufState, modeToClear string, reason string, iteration int, verifyHash string) raufState {
	active := []Assumption{}
	for _, a := range state.Assumptions {
		shouldArchive := a.StickyScope == "" && a.CreatedRecoveryMode == modeToClear

		if shouldArchive {
			state.ArchivedAssumptions = append(state.ArchivedAssumptions, ArchivedAssumption{
				Assumption:          a,
				ClearedReason:       reason,
				ClearedIteration:    iteration,
				ClearedRecoveryMode: modeToClear,
				RelatedVerifyHash:   verifyHash,
				ArchivedAt:          time.Now(),
			})
		} else {
			active = append(active, a)
		}
	}
	state.Assumptions = active
	return state
}
