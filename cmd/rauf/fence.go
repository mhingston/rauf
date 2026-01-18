package main

import "strings"

// fenceState tracks code fence parsing state.
type fenceState struct {
	inFence   bool
	fenceChar byte
	fenceLen  int
}

// processLine updates fence state based on the current line.
// Returns true if the line is inside a code fence (should be skipped for content matching).
func (f *fenceState) processLine(trimmed string) bool {
	if len(trimmed) < 3 {
		return f.inFence
	}

	if !f.inFence {
		// Check for opening fence
		if trimmed[0] == '`' || trimmed[0] == '~' {
			fenceChar := trimmed[0]
			fenceLen := countLeadingChars(trimmed, fenceChar)
			if fenceLen >= 3 {
				// Valid opening fence: at least 3 chars, rest is optional language identifier
				f.inFence = true
				f.fenceChar = fenceChar
				f.fenceLen = fenceLen
				return true
			}
		}
		return false
	}

	// Check for closing fence
	if trimmed[0] == f.fenceChar {
		count := countLeadingChars(trimmed, f.fenceChar)
		// Closing fence must have at least as many chars as opening
		// and must consist ONLY of fence characters (no trailing content)
		if count >= f.fenceLen && count == len(trimmed) {
			f.inFence = false
			f.fenceChar = 0
			f.fenceLen = 0
			return true
		}
	}
	return true
}

// countLeadingChars counts consecutive occurrences of char at the start of s.
func countLeadingChars(s string, char byte) int {
	count := 0
	for count < len(s) && s[count] == char {
		count++
	}
	return count
}

// scanLinesOutsideFence iterates over lines and calls the match function
// for each line that is outside of code fences.
// Returns true if match returns true for any line.
func scanLinesOutsideFence(output string, match func(trimmed string) bool) bool {
	var fence fenceState
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence.processLine(trimmed) {
			continue
		}
		if match(trimmed) {
			return true
		}
	}
	return false
}
