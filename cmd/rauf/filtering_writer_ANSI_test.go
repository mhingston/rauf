package main

import (
	"bytes"
	"testing"
)

func TestFilteringWriterANSI(t *testing.T) {
	var out bytes.Buffer
	fw := newFilteringWriter(&out, "RAUF_QUESTION:")

	input := []byte("\x1b[32mRAUF_QUESTION: How are you?\nKeep this line\n\x1b[31mRAUF_QUESTION: Hidden again\n")
	_, err := fw.Write(input)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	expected := "Keep this line\n"
	if out.String() != expected {
		t.Errorf("Expected %q, got %q", expected, out.String())
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"\x1b[32mGreen\x1b[0m", "Green"},
		{"\x1b[?25hVisible", "Visible"},
		{"Plain", "Plain"},
		{"RAUF_QUESTION: No ANSI", "RAUF_QUESTION: No ANSI"},
	}

	for _, tt := range tests {
		got := stripANSI(tt.input)
		if got != tt.expected {
			t.Errorf("stripANSI(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}
