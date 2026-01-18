package main

// defaultRecoveryConfig returns the default recovery thresholds (backward compatible).
func defaultRecoveryConfig() recoveryConfig {
	return recoveryConfig{
		ConsecutiveVerifyFails: 2,
		NoProgressIters:        2,
		GuardrailFailures:      2,
	}
}
