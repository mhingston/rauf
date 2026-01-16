package main

import (
	"strings"
)

type strategyStep struct {
	Mode       string
	Model      string
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
		return true
	}
}

func shouldContinueUntil(step strategyStep, result iterationResult) bool {
	if step.Until == "" {
		return false
	}
	switch strings.ToLower(step.Until) {
	case "verify_pass":
		return result.VerifyStatus != "pass"
	case "verify_fail":
		return result.VerifyStatus != "fail"
	default:
		return false
	}
}
