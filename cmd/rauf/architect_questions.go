package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

const maxArchitectQuestions = 3

func runArchitectQuestions(ctx context.Context, runner runtimeExec, promptContent *string, output string, harness, harnessArgs, model string, yoloEnabled bool, logFile *os.File, retryCfg retryConfig) (string, bool) {
	reader := bufio.NewReader(os.Stdin)
	totalAsked := 0
	updatedOutput := output
	for {
		questions := extractQuestions(updatedOutput)
		if len(questions) == 0 || totalAsked >= maxArchitectQuestions {
			break
		}
		answers := []string{}
		for _, q := range questions {
			if totalAsked >= maxArchitectQuestions {
				break
			}
			totalAsked++
			fmt.Printf("Architect question: %s\n> ", q)
			text, _ := reader.ReadString('\n')
			text = strings.TrimSpace(text)
			if text == "" {
				text = "(no answer provided)"
			}
			answers = append(answers, fmt.Sprintf("Q: %s\nA: %s", q, text))
		}
		if len(answers) == 0 {
			break
		}
		*promptContent = *promptContent + "\n\n# Architect Answers\n\n" + strings.Join(answers, "\n\n")
		nextOutput, err := runHarness(ctx, *promptContent, harness, harnessArgs, model, yoloEnabled, logFile, retryCfg, runner)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Architect follow-up failed:", err)
			return output, false
		}
		updatedOutput = nextOutput
	}
	return updatedOutput, totalAsked > 0
}

func extractQuestions(output string) []string {
	lines := strings.Split(output, "\n")
	questions := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "RAUF_QUESTION:") {
			question := strings.TrimSpace(strings.TrimPrefix(line, "RAUF_QUESTION:"))
			if question != "" {
				questions = append(questions, question)
			}
		}
	}
	return questions
}
