package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	baseArchitectQuestions   = 3
	bonusQuestionsPerFailure = 1
)

// maxArchitectQuestionsForState returns the dynamic limit based on backpressure state.
// Allows extra questions when the model is dealing with prior failures.
func maxArchitectQuestionsForState(state raufState) int {
	max := baseArchitectQuestions
	if state.PriorGuardrailStatus == "fail" {
		max += bonusQuestionsPerFailure
	}
	if state.LastVerificationStatus == "fail" {
		max += bonusQuestionsPerFailure
	}
	return max
}

func runArchitectQuestions(ctx context.Context, runner runtimeExec, promptContent *string, output string, state raufState, harness, harnessArgs string, logFile *os.File, retryCfg retryConfig) (string, bool) {
	reader := bufio.NewReader(os.Stdin)
	totalAsked := 0
	updatedOutput := output
	maxQuestions := maxArchitectQuestionsForState(state)
	for {
		// Check for context cancellation at the start of each loop iteration
		select {
		case <-ctx.Done():
			return updatedOutput, totalAsked > 0
		default:
		}

		questions := extractQuestions(updatedOutput)
		if len(questions) == 0 || totalAsked >= maxQuestions {
			break
		}
		answers := []string{}
		for _, q := range questions {
			if totalAsked >= maxQuestions {
				break
			}
			totalAsked++
			fmt.Printf("Architect question: %s\n> ", q)

			// Use a goroutine to read input so we can also check for context cancellation
			inputChan := make(chan string, 1)
			go func() {
				text, _ := reader.ReadString('\n')
				inputChan <- strings.TrimSpace(text)
			}()

			var text string
			select {
			case <-ctx.Done():
				return updatedOutput, totalAsked > 0
			case text = <-inputChan:
				// Got input from user
			case <-time.After(5 * time.Minute):
				// Timeout after 5 minutes of no input
				text = "(no answer provided - timeout)"
			}

			if text == "" {
				text = "(no answer provided)"
			}
			answers = append(answers, fmt.Sprintf("Q: %s\nA: %s", q, text))
		}
		if len(answers) == 0 {
			break
		}
		*promptContent = *promptContent + "\n\n# Architect Answers\n\n" + strings.Join(answers, "\n\n")
		nextResult, err := runHarness(ctx, *promptContent, harness, harnessArgs, logFile, retryCfg, runner)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Architect follow-up failed:", err)
			return output, false
		}
		updatedOutput = nextResult.Output
	}
	return updatedOutput, totalAsked > 0
}

func extractQuestions(output string) []string {
	questions := []string{}
	var fence fenceState
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence.processLine(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "RAUF_QUESTION:") {
			question := strings.TrimSpace(strings.TrimPrefix(trimmed, "RAUF_QUESTION:"))
			if question != "" {
				questions = append(questions, question)
			}
		}
	}
	return questions
}
