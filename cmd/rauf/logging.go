package main

import (
	"encoding/json"
	"os"
)

type logEntry struct {
	Type                string   `json:"type"`
	Mode                string   `json:"mode,omitempty"`
	Iteration           int      `json:"iteration,omitempty"`
	VerifyCmd           string   `json:"verify_cmd,omitempty"`
	VerifyStatus        string   `json:"verify_status,omitempty"`
	VerifyOutput        string   `json:"verify_output,omitempty"`
	PlanHash            string   `json:"plan_hash,omitempty"`
	PromptHash          string   `json:"prompt_hash,omitempty"`
	Branch              string   `json:"branch,omitempty"`
	HeadBefore          string   `json:"head_before,omitempty"`
	HeadAfter           string   `json:"head_after,omitempty"`
	Guardrail           string   `json:"guardrail,omitempty"`
	ExitReason          string   `json:"exit_reason,omitempty"`
	CompletionSignal    string   `json:"completion_signal,omitempty"`
	CompletionSpecs     []string `json:"completion_specs,omitempty"`
	CompletionArtifacts []string `json:"completion_artifacts,omitempty"`
}

func writeLogEntry(file *os.File, entry logEntry) {
	if file == nil {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = file.Write(append(data, '\n'))
}
