package main

import (
	"strings"
	"testing"
)

func TestFenceState(t *testing.T) {
	f := &fenceState{}

	// Outside fence
	if f.processLine("some text") {
		t.Error("should not be in fence")
	}

	// Opening fence
	if !f.processLine("```go") {
		t.Error("should start fence")
	}
	if !f.inFence {
		t.Error("inFence should be true")
	}

	// Inside fence
	if !f.processLine("  fmt.Println(1)") {
		t.Error("should be in fence")
	}

	// Closing fence (mismatch length)
	if !f.processLine("``") {
		t.Error("shorter backticks should stay in fence")
	}

	// Closing fence (extra content)
	if !f.processLine("``` suffix") {
		t.Error("fence with suffix should stay in fence")
	}

	// Valid closing fence
	if !f.processLine("```") {
		t.Error("should match closing fence")
	}
	if f.inFence {
		t.Error("should exit fence")
	}
}

func TestFenceState_Tilde(t *testing.T) {
	f := &fenceState{}
	if !f.processLine("~~~") {
		t.Error("should start tilde fence")
	}
	if !f.processLine("some content") {
		t.Error("should be in fence")
	}
	if !f.processLine("~~~") {
		t.Error("should close tilde fence")
	}
}

func TestScanLinesOutsideFence(t *testing.T) {
	output := `
Outside 1
` + "```" + `
Inside
` + "```" + `
Outside 2
`
	count := 0
	scanLinesOutsideFence(output, func(trimmed string) bool {
		if strings.HasPrefix(trimmed, "Outside") {
			count++
		}
		return false
	})
	if count != 2 {
		t.Errorf("expected 2 outside lines, got %d", count)
	}
}
