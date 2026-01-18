package main

import (
	"fmt"
	"os"
	"strings"
)

type strategyStep struct {
	Mode       string
	Iterations int
	Until      string
	If         string
}

func shouldRunStep(step strategyStep, lastResult iterationResult) bool {
	if step.If == "" {
		return true
	}
	switch strings.ToLower(step.If) {
	case "stalled":
		return lastResult.Stalled
	case "verify_fail":
		return lastResult.VerifyStatus == "fail"
	case "verify_pass":
		return lastResult.VerifyStatus == "pass"
	default:
		fmt.Fprintf(os.Stderr, "Warning: unknown strategy 'if' condition %q, defaulting to true\n", step.If)
		return true
	}
}

func shouldContinueUntil(step strategyStep, result iterationResult) bool {
	if step.Until == "" {
		// No "until" condition means continue up to max iterations
		return true
	}
	switch strings.ToLower(step.Until) {
	case "verify_pass":
		// Continue until verification passes
		return result.VerifyStatus != "pass"
	case "verify_fail":
		// Continue until verification fails
		return result.VerifyStatus != "fail"
	default:
		// Unknown condition: warn and continue up to max iterations
		fmt.Fprintf(os.Stderr, "Warning: unknown strategy 'until' condition %q, continuing to max iterations\n", step.Until)
		return true
	}
}
