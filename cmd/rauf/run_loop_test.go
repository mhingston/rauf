package main

import "testing"

func TestHasCompletionSentinel(t *testing.T) {
	if hasCompletionSentinel("all done") {
		t.Fatalf("expected no completion sentinel")
	}
	if hasCompletionSentinel("status: ok RAUF_COMPLETE maybe") {
		t.Fatalf("expected no completion sentinel for inline token")
	}
	if hasCompletionSentinel("```text\nRAUF_COMPLETE\n```") {
		t.Fatalf("expected no completion sentinel inside code block")
	}
	if hasCompletionSentinel("~~~\nRAUF_COMPLETE\n~~~") {
		t.Fatalf("expected no completion sentinel inside tilde fence")
	}
	if hasCompletionSentinel("```\nRAUF_COMPLETE\n``") {
		t.Fatalf("expected no completion sentinel with mismatched fence length")
	}
	if hasCompletionSentinel("~~~\nRAUF_COMPLETE\n```") {
		t.Fatalf("expected no completion sentinel with mismatched fence character")
	}
	if !hasCompletionSentinel("status: ok\nRAUF_COMPLETE\n") {
		t.Fatalf("expected completion sentinel to be detected")
	}
}
