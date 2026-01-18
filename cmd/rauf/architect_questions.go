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

// runArchitectQuestions handles interactive Q&A during architect mode.
// It extracts RAUF_QUESTION: lines from output and prompts the user for answers.
//
// Known limitation: Go's stdin reads are blocking and cannot be interrupted.
// If the context is cancelled or timeout occurs while waiting for user input,
// the goroutine reading stdin will remain blocked until the user eventually
// provides input (at which point it will exit cleanly). This is a fundamental
// limitation of Go's I/O model, not a bug. In practice, this only affects
// scenarios where the user cancels during a question prompt.
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

			// Use a goroutine to read input so we can also check for context cancellation.
			// Note: If context is cancelled or timeout occurs before user input, the goroutine
			// will remain blocked on ReadString until input is received (stdin reads cannot be
			// interrupted in Go). The goroutine will exit once any input is eventually provided.
			inputChan := make(chan string, 1)
			go func() {
				text, _ := reader.ReadString('\n')
				select {
				case inputChan <- strings.TrimSpace(text):
				default:
					// Channel not being read (timeout/cancel occurred), discard input
				}
			}()

			var text string
			select {
			case <-ctx.Done():
				// Goroutine may leak until user provides input; this is a known limitation
				// of blocking stdin reads in Go.
				return updatedOutput, totalAsked > 0
			case text = <-inputChan:
				// Got input from user
			case <-time.After(5 * time.Minute):
				// Timeout after 5 minutes of no input.
				// Goroutine will exit once user eventually provides input.
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
