package main

import (
	"fmt"
	"strings"
)

// escalationConfig defines model escalation behavior.
type escalationConfig struct {
	Enabled                bool
	ConsecutiveVerifyFails int // Trigger: escalate after N consecutive verify failures
	NoProgressIters        int // Trigger: escalate after N no-progress iterations
	GuardrailFailures      int // Trigger: escalate after N consecutive guardrail failures
	MinStrongIterations    int // Minimum iterations to stay on strong model / wait before de-escalating
	MaxEscalations         int // Maximum number of escalations per run
}

// defaultEscalationConfig returns the default (disabled) escalation config.
func defaultEscalationConfig() escalationConfig {
	return escalationConfig{
		Enabled:                false,
		ConsecutiveVerifyFails: 2,
		NoProgressIters:        2,
		GuardrailFailures:      2,
		MinStrongIterations:    2,
		MaxEscalations:         2,
	}
}

// shouldEscalateModel determines if we should switch to the stronger model.
// Returns (shouldEscalate, triggerReason, suppressionReason).
func shouldEscalateModel(state raufState, cfg runtimeConfig) (bool, string, string) {
	if !cfg.ModelEscalation.Enabled {
		return false, "", ""
	}
	if cfg.ModelStrong == "" {
		return false, "", ""
	}

	// Check triggers first to see if we WOULD escalate
	triggerReason := ""
	if cfg.ModelEscalation.ConsecutiveVerifyFails > 0 &&
		state.ConsecutiveVerifyFails >= cfg.ModelEscalation.ConsecutiveVerifyFails {
		triggerReason = "consecutive_verify_fails"
	} else if cfg.ModelEscalation.NoProgressIters > 0 &&
		state.NoProgressStreak >= cfg.ModelEscalation.NoProgressIters {
		triggerReason = "no_progress_iters"
	} else if cfg.ModelEscalation.GuardrailFailures > 0 &&
		state.ConsecutiveGuardrailFails >= cfg.ModelEscalation.GuardrailFailures {
		triggerReason = "guardrail_failures"
	}

	if triggerReason == "" {
		return false, "", ""
	}

	// Triggers met, check blockers
	if state.EscalationCount >= cfg.ModelEscalation.MaxEscalations {
		return false, triggerReason, "max_escalations_reached"
	}
	// Already escalated and still in minimum duration
	if state.CurrentModel == cfg.ModelStrong && state.MinStrongIterationsRemaining > 0 {
		return false, triggerReason, "min_strong_iterations_active"
	}

	return true, triggerReason, ""
}

// shouldDeescalateModel determines if we should return to the default model.
func shouldDeescalateModel(state raufState, cfg runtimeConfig) bool {
	if !cfg.ModelEscalation.Enabled {
		return false
	}
	if state.CurrentModel != cfg.ModelStrong {
		return false
	}
	// De-escalate if minimum duration has expired
	return state.MinStrongIterationsRemaining <= 0
}

// computeEffectiveModel returns the model to use for this iteration.
func computeEffectiveModel(state raufState, cfg runtimeConfig) string {
	if !cfg.ModelEscalation.Enabled {
		return cfg.ModelDefault
	}
	if state.CurrentModel != "" {
		return state.CurrentModel
	}
	return cfg.ModelDefault
}

// applyModelChoice injects the model flag into harness args.
// If override is true, replaces existing model flag value.
// If override is false, respects existing value if present.
func applyModelChoice(harnessArgs, modelFlag, modelName string, override bool) string {
	if modelFlag == "" || modelName == "" {
		return harnessArgs
	}

	// Check if model flag already exists in args
	if containsModelFlag(harnessArgs, modelFlag) {
		if !override {
			// Respect existing flag - don't override
			return harnessArgs
		}
		// Override existing flag
		// For simplicity, we can do a regex replacement or simpler string manipulation if we assume standard format.
		// A robust way without regex libraries overkill: split fields, filter out the flag, then append.
		// Or using regex to match `-flag value` or `-flag=value`
		// Given harnessArgs is command line string, regex is safer.
		// Pattern: `\s*--model[=\s]+[^\s]+`
		// Note: modelFlag might contain dashes.
		// Let's use a simpler field reconstruction.
		parts, _ := splitArgs(harnessArgs)
		if parts == nil {
			// Fallback if parsing fails (shouldn't happen with valid args, but safe default)
			parts = strings.Fields(harnessArgs)
		}
		newParts := make([]string, 0, len(parts)+2)
		skipNext := false
		for _, part := range parts {
			if skipNext {
				skipNext = false
				continue
			}
			if part == modelFlag {
				// flag ... value format, skip next if it's the value
				// But we are removing it, so we just don't add it.
				skipNext = true
				continue
			}
			if strings.HasPrefix(part, modelFlag+"=") {
				// flag=value format
				continue
			}
			newParts = append(newParts, part)
		}
		// Append new model
		newParts = append(newParts, modelFlag, modelName)
		return strings.Join(newParts, " ")
	}

	// Append model flag
	if harnessArgs == "" {
		return modelFlag + " " + modelName
	}
	return harnessArgs + " " + modelFlag + " " + modelName
}

// containsModelFlag checks if the harness args already contain the model flag.
func containsModelFlag(harnessArgs, modelFlag string) bool {
	if modelFlag == "" {
		return false
	}
	// Check for flag as a word boundary
	parts, _ := splitArgs(harnessArgs)
	if parts == nil {
		parts = strings.Fields(harnessArgs)
	}
	for _, part := range parts {
		if part == modelFlag || strings.HasPrefix(part, modelFlag+"=") {
			return true
		}
	}
	return false
}

// updateBackpressureState updates proper failure counters and RecoveryMode.
// This runs regardless of model escalation being enabled.
func updateBackpressureState(state raufState, cfg recoveryConfig, verifyFailed bool, guardrailFailed bool, noProgress bool) raufState {
	// Track consecutive verify failures
	if verifyFailed {
		state.ConsecutiveVerifyFails++
	} else {
		state.ConsecutiveVerifyFails = 0
	}

	// Track consecutive guardrail failures
	if guardrailFailed {
		state.ConsecutiveGuardrailFails++
	} else {
		state.ConsecutiveGuardrailFails = 0
	}

	// Track no-progress streak
	if noProgress {
		state.NoProgressStreak++
	} else {
		state.NoProgressStreak = 0
	}

	// Recovery mode logic
	// Prioritize: Guardrail -> Verify -> NoProgress
	// Use configurable thresholds (defaulting to 2 if 0 for safety/compatibility, though default should handle it)
	limitGuardrail := cfg.GuardrailFailures
	if limitGuardrail <= 0 {
		limitGuardrail = 2
	}
	limitVerify := cfg.ConsecutiveVerifyFails
	if limitVerify <= 0 {
		limitVerify = 2
	}
	limitNoProgress := cfg.NoProgressIters
	if limitNoProgress <= 0 {
		limitNoProgress = 2
	}

	if state.ConsecutiveGuardrailFails >= limitGuardrail {
		state.RecoveryMode = "guardrail"
	} else if state.ConsecutiveVerifyFails >= limitVerify {
		state.RecoveryMode = "verify"
	} else if state.NoProgressStreak >= limitNoProgress {
		state.RecoveryMode = "no_progress"
	} else {
		state.RecoveryMode = ""
	}

	return state
}

type escalationEvent struct {
	Type      string // "escalated", "de_escalated", "suppressed", "none"
	FromModel string
	ToModel   string
	Reason    string
	Cooldown  int
}

// updateModelEscalationState handles model switching logic if enabled.
// It assumes failure counters in state have already been updated by updateBackpressureState.
func updateModelEscalationState(state raufState, cfg runtimeConfig) (raufState, escalationEvent) {
	event := escalationEvent{Type: "none"}
	if !cfg.ModelEscalation.Enabled {
		return state, event
	}

	// Manage minimum duration
	if state.MinStrongIterationsRemaining > 0 {
		state.MinStrongIterationsRemaining--
	}

	// Check for escalation
	// Check for escalation
	shouldEscalate, triggerReason, suppressed := shouldEscalateModel(state, cfg)
	if shouldEscalate {
		if state.CurrentModel != cfg.ModelStrong {
			event.Type = "escalated"
			event.FromModel = state.CurrentModel
			if event.FromModel == "" {
				event.FromModel = cfg.ModelDefault
			}
			event.ToModel = cfg.ModelStrong
			event.Reason = triggerReason
			event.Cooldown = cfg.ModelEscalation.MinStrongIterations

			state.CurrentModel = cfg.ModelStrong
			state.EscalationCount++
			state.MinStrongIterationsRemaining = cfg.ModelEscalation.MinStrongIterations
			state.LastEscalationReason = triggerReason
		}
	} else if suppressed != "" {
		event.Type = "suppressed"
		event.Reason = fmt.Sprintf("trigger=%s, blocker=%s", triggerReason, suppressed)
		event.Cooldown = state.MinStrongIterationsRemaining
		event.FromModel = state.CurrentModel
		if event.FromModel == "" {
			event.FromModel = cfg.ModelDefault
		}
		// Suppressed means we wanted to go to Strong but couldn't
		event.ToModel = cfg.ModelStrong
	} else if shouldDeescalateModel(state, cfg) {
		event.Type = "de_escalated"
		event.FromModel = state.CurrentModel
		event.ToModel = cfg.ModelDefault
		event.Reason = "min_strong_iterations_expired"

		// De-escalate to default model
		state.CurrentModel = cfg.ModelDefault
		state.LastEscalationReason = ""
	}

	return state, event
}
