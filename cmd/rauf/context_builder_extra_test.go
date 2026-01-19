package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestGenerateContextPack_Extra(t *testing.T) {
	ctx := context.Background()

	// Case 1: No keywords
	cp, err := generateContextPack(ctx, "", false)
	if err != nil {
		t.Errorf("Error: %v", err)
	}
	if cp.ZeroState == "" {
		t.Errorf("Expected zero state for empty task")
	}

	// Case 2: Zero hits
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "")
	defer os.Setenv("PATH", origPath)

	origGitExec := gitExec
	gitExec = func(args ...string) (string, error) {
		return "", fmt.Errorf("no matches")
	}
	defer func() { gitExec = origGitExec }()

	cp, err = generateContextPack(ctx, "nonexistentkeywordthatusuallydoesnotexist", true)
	if err != nil {
		t.Errorf("Error: %v", err)
	}
	if len(cp.Hits) != 0 || !strings.Contains(cp.ZeroState, "returned no relevant files") {
		t.Errorf("Expected zero hits: got %d, zeroState: %q", len(cp.Hits), cp.ZeroState)
	}
}
