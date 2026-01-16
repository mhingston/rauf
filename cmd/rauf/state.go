package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type raufState struct {
	LastVerificationOutput  string `json:"last_verification_output"`
	LastVerificationCommand string `json:"last_verification_command"`
	LastVerificationStatus  string `json:"last_verification_status"`
	LastVerificationHash    string `json:"last_verification_hash"`
}

func loadState() raufState {
	path := statePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return raufState{}
	}
	var state raufState
	if err := json.Unmarshal(data, &state); err != nil {
		return raufState{}
	}
	return state
}

func saveState(state raufState) error {
	path := statePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return writeStateSummary(state)
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

	return os.WriteFile(stateSummaryPath(), []byte(b.String()), 0o644)
}

func truncateStateSummary(value string) string {
	const maxSummaryBytes = 4 * 1024
	if len(value) <= maxSummaryBytes {
		return value
	}
	return value[:maxSummaryBytes]
}
