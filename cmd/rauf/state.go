package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	return os.WriteFile(path, data, 0o644)
}

func statePath() string {
	return filepath.Join(".rauf", "state.json")
}
