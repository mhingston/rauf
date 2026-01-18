package main

import (
	"testing"
)

func TestApplyModelChoice_QuotedFlag(t *testing.T) {
	// Case where flag uses quotes: --flag="value with spaces"
	// strings.Fields would fail to keep "value with spaces" together if not careful,
	// but here we are substituting --model.

	harnessArgs := `--flag="value with spaces" --verbose`
	modelFlag := "--model"
	modelName := "opus"
	override := false

	result := applyModelChoice(harnessArgs, modelFlag, modelName, override)
	// Expect: --flag="value with spaces" --verbose --model opus
	// splitArgs should preserve the quoted arg.
	expected := `--flag="value with spaces" --verbose --model opus`
	if result != expected {
		t.Errorf("applyModelChoice(...) = %q, want %q", result, expected)
	}
}
