package main

import (
	"strings"
	"time"
)

// Hypothesis represents a structured hypothesis entry from model output.
type Hypothesis struct {
	Timestamp       time.Time `json:"timestamp"`
	Iteration       int       `json:"iteration"`
	Hypothesis      string    `json:"hypothesis"`
	DifferentAction string    `json:"different_action"`
	VerifyCommand   string    `json:"verify_command,omitempty"`
}

// TypedQuestion represents a question with an optional type tag.
// TypedQuestion represents a question with an optional type tag.
type TypedQuestion struct {
	Type        string // CLARIFY, DECISION, ASSUMPTION, or empty
	Question    string
	StickyScope string // "sticky", "global", or empty
}

// extractHypothesis parses model output for HYPOTHESIS: and DIFFERENT_THIS_TIME: or DIFFERENT: lines.
// Returns the hypothesis if found, with empty strings if not present.
func extractHypothesis(output string) (hypothesis, differentAction string) {
	var fence fenceState
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence.processLine(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "HYPOTHESIS:") {
			hypothesis = strings.TrimSpace(strings.TrimPrefix(trimmed, "HYPOTHESIS:"))
		}
		if strings.HasPrefix(trimmed, "DIFFERENT_THIS_TIME:") {
			differentAction = strings.TrimSpace(strings.TrimPrefix(trimmed, "DIFFERENT_THIS_TIME:"))
		}
		// Also accept the shorter alias
		if strings.HasPrefix(trimmed, "DIFFERENT:") && differentAction == "" {
			differentAction = strings.TrimSpace(strings.TrimPrefix(trimmed, "DIFFERENT:"))
		}
	}
	return hypothesis, differentAction
}

// hasRequiredHypothesis checks if the model output contains a valid hypothesis
// when one is required (e.g., after consecutive verify failures).
func hasRequiredHypothesis(output string) bool {
	hypothesis, differentAction := extractHypothesis(output)
	return hypothesis != "" && differentAction != ""
}

// extractTypedQuestions parses model output for RAUF_QUESTION lines with optional type tags.
// Supports formats:
//   - RAUF_QUESTION:CLARIFY: question text
//   - RAUF_QUESTION:DECISION: question text
//   - RAUF_QUESTION:ASSUMPTION: question text
//   - RAUF_QUESTION: question text (no type)
func extractTypedQuestions(output string) []TypedQuestion {
	questions := []TypedQuestion{}
	var fence fenceState
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence.processLine(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "RAUF_QUESTION:") {
			rest := strings.TrimPrefix(trimmed, "RAUF_QUESTION:")
			rest = strings.TrimSpace(rest)
			if rest == "" {
				continue
			}

			q := TypedQuestion{}

			// Check for type prefix
			for _, tag := range []string{"CLARIFY:", "DECISION:", "ASSUMPTION:"} {
				if strings.HasPrefix(rest, tag) {
					q.Type = strings.TrimSuffix(tag, ":")
					q.Question = strings.TrimSpace(strings.TrimPrefix(rest, tag))

					if q.Type == "ASSUMPTION" {
						upper := strings.ToUpper(q.Question)
						if strings.HasPrefix(upper, "STICKY:") {
							q.StickyScope = "sticky"
							if idx := strings.Index(q.Question, ":"); idx != -1 {
								q.Question = strings.TrimSpace(q.Question[idx+1:])
							}
						} else if strings.HasPrefix(upper, "GLOBAL:") {
							q.StickyScope = "global"
							if idx := strings.Index(q.Question, ":"); idx != -1 {
								q.Question = strings.TrimSpace(q.Question[idx+1:])
							}
						}
					}
					break
				}
			}

			// No type tag found
			if q.Type == "" {
				q.Question = rest
			}

			if q.Question != "" {
				questions = append(questions, q)
			}
		}
	}
	return questions
}

// formatTypedQuestionForDisplay formats a typed question for console display.
func formatTypedQuestionForDisplay(q TypedQuestion) string {
	if q.Type != "" {
		prefix := "[" + q.Type
		if q.StickyScope != "" {
			prefix += ":" + strings.ToUpper(q.StickyScope)
		}
		prefix += "] "
		return prefix + q.Question
	}
	return q.Question
}
