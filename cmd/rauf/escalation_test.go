package main

import (
	"strings"
	"testing"
)

func TestApplyModelChoice_AddsFlag(t *testing.T) {
	result := applyModelChoice("--verbose", "--model", "opus", false)
	expected := "--verbose --model opus"
	if result != expected {
		t.Errorf("applyModelChoice(...) = %q, want %q", result, expected)
	}
}

func TestApplyModelChoice_EmptyArgs(t *testing.T) {
	result := applyModelChoice("", "--model", "opus", false)
	expected := "--model opus"
	if result != expected {
		t.Errorf("applyModelChoice(...) = %q, want %q", result, expected)
	}
}

func TestApplyModelChoice_RespectsExisting(t *testing.T) {
	result := applyModelChoice("--model sonnet --verbose", "--model", "opus", false)
	// Should not add opus since --model already exists
	expected := "--model sonnet --verbose"
	if result != expected {
		t.Errorf("applyModelChoice(...) = %q, want %q", result, expected)
	}
}

func TestApplyModelChoice_OverrideExisting(t *testing.T) {
	result := applyModelChoice("--model sonnet --verbose", "--model", "opus", true)
	// Should replace sonnet with opus
	expected := "sonnet --verbose --model opus" // Note: fields order might change based on impl, filtering removes "sonnet" if it was "flag value"
	// Wait, my impl removes `flag` and `value`.
	// Parts: ["--model", "sonnet", "--verbose"]
	// Filtered: ["--verbose"]
	// Appended: ["--verbose", "--model", "opus"]
	expected = "--verbose --model opus"
	if result != expected {
		t.Errorf("applyModelChoice(...) = %q, want %q", result, expected)
	}
}

func TestApplyModelChoice_OverrideExisting_EqualsStyle(t *testing.T) {
	result := applyModelChoice("--model=sonnet --verbose", "--model", "opus", true)
	// Parts: ["--model=sonnet", "--verbose"]
	// Filtered: ["--verbose"]
	// Appended: ["--verbose", "--model", "opus"]
	expected := "--verbose --model opus"
	if result != expected {
		t.Errorf("applyModelChoice(...) = %q, want %q", result, expected)
	}
}

func TestApplyModelChoice_EmptyModel(t *testing.T) {
	result := applyModelChoice("--verbose", "--model", "", false)
	// Should not add anything when model is empty
	expected := "--verbose"
	if result != expected {
		t.Errorf("applyModelChoice(...) = %q, want %q", result, expected)
	}
}

func TestApplyModelChoice_EmptyFlag(t *testing.T) {
	result := applyModelChoice("--verbose", "", "opus", false)
	// Should not add anything when flag is empty
	expected := "--verbose"
	if result != expected {
		t.Errorf("applyModelChoice(...) = %q, want %q", result, expected)
	}
}

func TestContainsModelFlag(t *testing.T) {
	tests := []struct {
		args     string
		flag     string
		expected bool
	}{
		{"--model sonnet --verbose", "--model", true},
		{"--verbose", "--model", false},
		{"--model=sonnet --verbose", "--model", true},
		{"", "--model", false},
		{"--model-other foo", "--model", false}, // Different flag entirely
	}

	for _, tt := range tests {
		result := containsModelFlag(tt.args, tt.flag)
		if result != tt.expected {
			t.Errorf("containsModelFlag(%q, %q) = %v, want %v", tt.args, tt.flag, result, tt.expected)
		}
	}
}

func TestShouldEscalateModel_Disabled(t *testing.T) {
	state := raufState{ConsecutiveVerifyFails: 10}
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{Enabled: false},
		ModelStrong:     "opus",
	}

	shouldEscalate, _, _ := shouldEscalateModel(state, cfg)
	if shouldEscalate {
		t.Error("should not escalate when disabled")
	}
}

func TestShouldEscalateModel_NoStrongModel(t *testing.T) {
	state := raufState{ConsecutiveVerifyFails: 10}
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:                true,
			ConsecutiveVerifyFails: 2,
		},
		ModelStrong: "", // No strong model configured
	}

	shouldEscalate, _, _ := shouldEscalateModel(state, cfg)
	if shouldEscalate {
		t.Error("should not escalate when no strong model configured")
	}
}

func TestShouldEscalateModel_ConsecutiveVerifyFails(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:                true,
			ConsecutiveVerifyFails: 2,
			MaxEscalations:         5,
		},
		ModelStrong: "opus",
	}

	// Below threshold - should not escalate
	state := raufState{ConsecutiveVerifyFails: 1}
	shouldEscalate, _, _ := shouldEscalateModel(state, cfg)
	if shouldEscalate {
		t.Error("should not escalate below threshold")
	}

	// At threshold - should escalate
	state.ConsecutiveVerifyFails = 2
	shouldEscalate, reason, _ := shouldEscalateModel(state, cfg)
	if !shouldEscalate {
		t.Error("should escalate at threshold")
	}
	if reason != "consecutive_verify_fails" {
		t.Errorf("reason = %q, want consecutive_verify_fails", reason)
	}
}

func TestShouldEscalateModel_NoProgressIters(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:         true,
			NoProgressIters: 3,
			MaxEscalations:  5,
		},
		ModelStrong: "opus",
	}

	state := raufState{NoProgressStreak: 3}
	shouldEscalate, reason, _ := shouldEscalateModel(state, cfg)
	if !shouldEscalate {
		t.Error("should escalate on no-progress streak")
	}
	if reason != "no_progress_iters" {
		t.Errorf("reason = %q, want no_progress_iters", reason)
	}
}

func TestShouldEscalateModel_GuardrailFailures(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:           true,
			GuardrailFailures: 2,
			MaxEscalations:    5,
		},
		ModelStrong: "opus",
	}

	state := raufState{ConsecutiveGuardrailFails: 2}
	shouldEscalate, reason, _ := shouldEscalateModel(state, cfg)
	if !shouldEscalate {
		t.Error("should escalate on guardrail failures")
	}
	if reason != "guardrail_failures" {
		t.Errorf("reason = %q, want guardrail_failures", reason)
	}
}

func TestShouldEscalateModel_MaxEscalationsRespected(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:                true,
			ConsecutiveVerifyFails: 2,
			MaxEscalations:         2,
		},
		ModelStrong: "opus",
	}

	state := raufState{
		ConsecutiveVerifyFails: 10,
		EscalationCount:        2, // Already at max
	}

	shouldEscalate, trigger, suppressed := shouldEscalateModel(state, cfg)
	if shouldEscalate {
		t.Error("should not escalate when at max escalations")
	}
	if trigger != "consecutive_verify_fails" {
		t.Errorf("trigger = %q, want consecutive_verify_fails", trigger)
	}
	if suppressed != "max_escalations_reached" {
		t.Errorf("suppressed = %q, want max_escalations_reached", suppressed)
	}
}

func TestShouldEscalateModel_MinStrongIterationsActive(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:                true,
			ConsecutiveVerifyFails: 2,
			MinStrongIterations:    5,
			MaxEscalations:         5,
		},
		ModelStrong: "opus",
	}

	state := raufState{
		ConsecutiveVerifyFails:       10, // Trigger met
		CurrentModel:                 "opus",
		MinStrongIterationsRemaining: 3, // Still active
	}

	shouldEscalate, trigger, suppressed := shouldEscalateModel(state, cfg)
	if shouldEscalate {
		t.Error("should not escalate (re-trigger) when min strong iterations active")
	}
	if trigger != "consecutive_verify_fails" {
		t.Errorf("trigger = %q, want consecutive_verify_fails", trigger)
	}
	if suppressed != "min_strong_iterations_active" {
		t.Errorf("suppressed = %q, want min_strong_iterations_active", suppressed)
	}
}

func TestShouldEscalateModel_CooldownPreventsToggle(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:                true,
			ConsecutiveVerifyFails: 2,
			MinStrongIterations:    3,
			MaxEscalations:         5,
		},
		ModelStrong: "opus",
	}

	state := raufState{
		ConsecutiveVerifyFails:       0, // Trigger resolved
		CurrentModel:                 "opus",
		MinStrongIterationsRemaining: 2, // Still in cooldown
	}

	// De-escalation check
	shouldDeescalate := shouldDeescalateModel(state, cfg)
	if shouldDeescalate {
		t.Error("should not de-escalate while in cooldown")
	}

	// After cooldown expires
	state.MinStrongIterationsRemaining = 0
	shouldDeescalate = shouldDeescalateModel(state, cfg)
	if !shouldDeescalate {
		t.Error("should de-escalate when cooldown expired")
	}
}

func TestComputeEffectiveModel_DefaultWhenDisabled(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{Enabled: false},
		ModelDefault:    "sonnet",
	}
	state := raufState{}

	model := computeEffectiveModel(state, cfg)
	if model != "sonnet" {
		t.Errorf("computeEffectiveModel() = %q, want 'sonnet'", model)
	}
}

func TestComputeEffectiveModel_ReturnsCurrentModel(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{Enabled: true},
		ModelDefault:    "sonnet",
	}
	state := raufState{CurrentModel: "opus"}

	model := computeEffectiveModel(state, cfg)
	if model != "opus" {
		t.Errorf("computeEffectiveModel() = %q, want 'opus'", model)
	}
}

func TestDefaultEscalationConfig(t *testing.T) {
	cfg := defaultEscalationConfig()

	if cfg.Enabled {
		t.Error("default should have Enabled = false")
	}
	if cfg.ConsecutiveVerifyFails != 2 {
		t.Errorf("default ConsecutiveVerifyFails = %d, want 2", cfg.ConsecutiveVerifyFails)
	}
	if cfg.NoProgressIters != 2 {
		t.Errorf("default NoProgressIters = %d, want 2", cfg.NoProgressIters)
	}
	if cfg.GuardrailFailures != 2 {
		t.Errorf("default GuardrailFailures = %d, want 2", cfg.GuardrailFailures)
	}
	if cfg.MinStrongIterations != 2 {
		t.Errorf("default MinStrongIterations = %d, want 2", cfg.MinStrongIterations)
	}
	if cfg.MaxEscalations != 2 {
		t.Errorf("default MaxEscalations = %d, want 2", cfg.MaxEscalations)
	}
}
func TestUpdateEscalationState(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:                true,
			ConsecutiveVerifyFails: 2,
			NoProgressIters:        2,
			GuardrailFailures:      2,
			MinStrongIterations:    2,
			MaxEscalations:         2,
		},
		ModelStrong: "opus",
	}

	t.Run("verify failure counter", func(t *testing.T) {
		state := raufState{ConsecutiveVerifyFails: 0}
		state = updateBackpressureState(state, cfg.Recovery, true, false, false)
		if state.ConsecutiveVerifyFails != 1 {
			t.Errorf("got %d, want 1", state.ConsecutiveVerifyFails)
		}
		// Second failure triggers escalation
		state = updateBackpressureState(state, cfg.Recovery, true, false, false)
		state, _ = updateModelEscalationState(state, cfg)

		if state.CurrentModel != "opus" {
			t.Error("should have escalated to opus")
		}
		if state.EscalationCount != 1 {
			t.Errorf("EscalationCount = %d, want 1", state.EscalationCount)
		}
		if state.MinStrongIterationsRemaining != 2 {
			t.Errorf("Cooldown = %d, want 2", state.MinStrongIterationsRemaining)
		}
	})

	t.Run("success resets failure counters", func(t *testing.T) {
		state := raufState{
			ConsecutiveVerifyFails:    1,
			NoProgressStreak:          1,
			ConsecutiveGuardrailFails: 1,
			RecoveryMode:              "verify",
		}
		// Pass resets everything
		state = updateBackpressureState(state, cfg.Recovery, false, false, false)
		if state.ConsecutiveVerifyFails != 0 || state.NoProgressStreak != 0 || state.ConsecutiveGuardrailFails != 0 {
			t.Errorf("counters not reset: %+v", state)
		}
		if state.RecoveryMode != "" {
			t.Error("RecoveryMode should be cleared")
		}
	})

	t.Run("recovery mode trigger - verify", func(t *testing.T) {
		state := raufState{ConsecutiveVerifyFails: 1}
		state = updateBackpressureState(state, cfg.Recovery, true, false, false) // fails = 2
		if state.RecoveryMode != "verify" {
			t.Errorf("RecoveryMode = %q, want 'verify'", state.RecoveryMode)
		}
	})

	t.Run("recovery mode trigger - no_progress", func(t *testing.T) {
		state := raufState{NoProgressStreak: 1}
		state = updateBackpressureState(state, cfg.Recovery, false, false, true) // streak = 2
		if state.RecoveryMode != "no_progress" {
			t.Errorf("RecoveryMode = %q, want 'no_progress'", state.RecoveryMode)
		}
	})
}

func TestUpdateEscalationState_Suppression(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:                true,
			ConsecutiveVerifyFails: 2,
			MaxEscalations:         1, // Set limit to 1
		},
		ModelStrong: "opus",
	}

	state := raufState{
		ConsecutiveVerifyFails: 2, // Trigger met
		EscalationCount:        1, // But already hit max
		CurrentModel:           "sonnet",
	}

	_, event := updateModelEscalationState(state, cfg)
	if event.Type != "suppressed" {
		t.Errorf("event.Type = %q, want 'suppressed'", event.Type)
	}
	if !strings.Contains(event.Reason, "max_escalations_reached") {
		t.Errorf("event.Reason = %q, want it to contain 'max_escalations_reached'", event.Reason)
	}
}

func TestUpdateEscalationState_Deescalation(t *testing.T) {
	cfg := runtimeConfig{
		ModelEscalation: escalationConfig{
			Enabled:             true,
			MinStrongIterations: 5,
		},
		ModelStrong:  "opus",
		ModelDefault: "sonnet",
	}

	// Case 1: Last iteration of strong mode
	state := raufState{
		CurrentModel:                 "opus",
		MinStrongIterationsRemaining: 1,
	}
	state, event := updateModelEscalationState(state, cfg)
	// Remaining was 1, decremented to 0. Should de-escalate now for NEXT iteration.
	if event.Type != "de_escalated" {
		t.Errorf("event.Type = %q, want 'de_escalated'", event.Type)
	}
	if state.MinStrongIterationsRemaining != 0 {
		t.Errorf("MinStrongIterationsRemaining = %d, want 0", state.MinStrongIterationsRemaining)
	}

	// Case 2: Already de-escalated
	state.CurrentModel = "sonnet" // Reset to default
	state, event = updateModelEscalationState(state, cfg)
	if event.Type != "none" {
		t.Errorf("event.Type = %q, want 'none'", event.Type)
	}
}
