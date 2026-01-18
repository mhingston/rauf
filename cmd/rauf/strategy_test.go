package main

import (
	"testing"
)

func TestShouldRunStep_Comprehensive(t *testing.T) {
	tests := []struct {
		name     string
		step     strategyStep
		result   iterationResult
		expected bool
	}{
		{
			name:     "no condition",
			step:     strategyStep{Mode: "build"},
			result:   iterationResult{Stalled: false},
			expected: true,
		},
		{
			name:     "if stalled (true)",
			step:     strategyStep{Mode: "plan", If: "stalled"},
			result:   iterationResult{Stalled: true},
			expected: true,
		},
		{
			name:     "if stalled (false)",
			step:     strategyStep{Mode: "plan", If: "stalled"},
			result:   iterationResult{Stalled: false},
			expected: false,
		},
		{
			name:     "if verify_pass (true)",
			step:     strategyStep{Mode: "build", If: "verify_pass"},
			result:   iterationResult{VerifyStatus: "pass"},
			expected: true,
		},
		{
			name:     "if verify_pass (false)",
			step:     strategyStep{Mode: "build", If: "verify_pass"},
			result:   iterationResult{VerifyStatus: "fail"},
			expected: false,
		},
		{
			name:     "if verify_fail (true)",
			step:     strategyStep{Mode: "build", If: "verify_fail"},
			result:   iterationResult{VerifyStatus: "fail"},
			expected: true,
		},
		{
			name:     "if verify_fail (false - pass)",
			step:     strategyStep{Mode: "build", If: "verify_fail"},
			result:   iterationResult{VerifyStatus: "pass"},
			expected: false,
		},
		{
			name:     "if verify_fail (false - skipped)",
			step:     strategyStep{Mode: "build", If: "verify_fail"},
			result:   iterationResult{VerifyStatus: "skipped"},
			expected: false,
		},
		{
			name:     "unknown condition",
			step:     strategyStep{Mode: "build", If: "unknown_condition"},
			result:   iterationResult{},
			expected: true, // Defaults to true? Let's check logic.
			// Logic: switch step.If { case "stalled": ... default: return true }
			// So unknown condition returns true.
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRunStep(tc.step, tc.result)
			if got != tc.expected {
				t.Errorf("shouldRunStep() = %v, want %v", got, tc.expected)
			}
		})
	}
}
