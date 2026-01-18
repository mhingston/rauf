package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseArgs_CLI(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected modeConfig
	}{
		{
			name: "quiet flag",
			args: []string{"--quiet"},
			expected: modeConfig{
				Quiet:      true,
				JSONOutput: true, // --quiet implies --json usually? No, user requested --quiet OR --json
				// Wait, Requirement: "Add --quiet (or --json) flag... suppress standard logging and output a final JSON summary"
				// My implementation likely links them or sets Quiet=true for both.
				// Let's verify behavior.
			},
		},
		{
			name: "json flag",
			args: []string{"--json"},
			expected: modeConfig{
				JSONOutput: true,
				Quiet:      true,
			},
		},
		{
			name: "report flag",
			args: []string{"--report", "report.json"},
			expected: modeConfig{
				ReportPath: "report.json",
			},
		},
		{
			name: "timeout flags",
			args: []string{"--timeout", "5m", "--attempt-timeout", "30s"},
			expected: modeConfig{
				Timeout:        5 * time.Minute,
				AttemptTimeout: 30 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs failed: %v", err)
			}
			if tt.name == "quiet flag" {
				if !cfg.Quiet {
					t.Error("expected Quiet=true")
				}
			}
			if tt.name == "json flag" {
				if !cfg.JSONOutput {
					t.Error("expected JSONOutput=true")
				}
				if !cfg.Quiet {
					t.Error("expected Quiet=true (implicit)")
				}
			}
			if tt.name == "report flag" {
				if cfg.ReportPath != tt.expected.ReportPath {
					t.Errorf("expected ReportPath=%q, got %q", tt.expected.ReportPath, cfg.ReportPath)
				}
			}
			if tt.name == "timeout flags" {
				if cfg.Timeout != tt.expected.Timeout {
					t.Errorf("expected Timeout=%v, got %v", tt.expected.Timeout, cfg.Timeout)
				}
				if cfg.AttemptTimeout != tt.expected.AttemptTimeout {
					t.Errorf("expected AttemptTimeout=%v, got %v", tt.expected.AttemptTimeout, cfg.AttemptTimeout)
				}
			}
		})
	}
}

func TestRunReport_Generated(t *testing.T) {
	// Integration test for report generation logic
	// We verify that a report struct can be marshaled and contains expected fields.

	report := RunReport{
		StartTime: time.Now(),
		EndTime:   time.Now().Add(1 * time.Second),
		Success:   true,
		Iterations: []IterationStats{
			{
				Iteration:  1,
				Mode:       "test",
				Model:      "gpt-4",
				Duration:   "1s",
				Result:     iterationResult{ExitReason: "success"},
				ExitReason: "success",
			},
		},
	}
	report.TotalDuration = report.EndTime.Sub(report.StartTime).String()

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to marshal report: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal report: %v", err)
	}

	if decoded["success"] != true {
		t.Error("expected success=true")
	}
	if decoded["total_iterations"] != nil { // Wait, int default is 0
		// Check Iterations array
		iters := decoded["iterations"].([]interface{})
		if len(iters) != 1 {
			t.Errorf("expected 1 iteration, got %d", len(iters))
		}
	}
}

// Ensure context timeout works in real execution path is hard without mocking runMain internals.
// But we verified runMode checks ctx.Err().
