package main

import (
	"testing"
)

func TestExtractHypothesis_Basic(t *testing.T) {
	output := `## Backpressure Response
HYPOTHESIS: The test fails because the mock wasn't set up correctly.
DIFFERENT_THIS_TIME: I will verify the mock configuration first.
`
	hyp, diff := extractHypothesis(output)
	if hyp != "The test fails because the mock wasn't set up correctly." {
		t.Errorf("hypothesis = %q, want mock message", hyp)
	}
	if diff != "I will verify the mock configuration first." {
		t.Errorf("different = %q, want verify message", diff)
	}
}

func TestExtractHypothesis_ShortAlias(t *testing.T) {
	output := `HYPOTHESIS: Database connection timeout.
DIFFERENT: Use retry logic.`
	hyp, diff := extractHypothesis(output)
	if hyp != "Database connection timeout." {
		t.Errorf("hypothesis = %q", hyp)
	}
	if diff != "Use retry logic." {
		t.Errorf("different = %q", diff)
	}
}

func TestExtractHypothesis_Empty(t *testing.T) {
	output := `Just some regular output without hypothesis markers.`
	hyp, diff := extractHypothesis(output)
	if hyp != "" || diff != "" {
		t.Errorf("expected empty, got hyp=%q diff=%q", hyp, diff)
	}
}

func TestExtractHypothesis_InsideCodeFence(t *testing.T) {
	output := "```\nHYPOTHESIS: should be ignored\nDIFFERENT: also ignored\n```"
	hyp, diff := extractHypothesis(output)
	if hyp != "" || diff != "" {
		t.Errorf("should ignore inside fence, got hyp=%q diff=%q", hyp, diff)
	}
}

func TestHasRequiredHypothesis(t *testing.T) {
	t.Run("complete", func(t *testing.T) {
		output := "HYPOTHESIS: reason\nDIFFERENT_THIS_TIME: action"
		if !hasRequiredHypothesis(output) {
			t.Error("expected true")
		}
	})

	t.Run("missing different", func(t *testing.T) {
		output := "HYPOTHESIS: reason"
		if hasRequiredHypothesis(output) {
			t.Error("expected false when DIFFERENT missing")
		}
	})

	t.Run("missing hypothesis", func(t *testing.T) {
		output := "DIFFERENT_THIS_TIME: action"
		if hasRequiredHypothesis(output) {
			t.Error("expected false when HYPOTHESIS missing")
		}
	})
}

func TestExtractTypedQuestions_Untyped(t *testing.T) {
	output := `RAUF_QUESTION: What is the expected format?`
	questions := extractTypedQuestions(output)
	if len(questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(questions))
	}
	if questions[0].Type != "" {
		t.Errorf("expected empty type, got %q", questions[0].Type)
	}
	if questions[0].Question != "What is the expected format?" {
		t.Errorf("question = %q", questions[0].Question)
	}
}

func TestExtractTypedQuestions_Typed(t *testing.T) {
	output := `RAUF_QUESTION:CLARIFY: Is this a breaking change?
RAUF_QUESTION:DECISION: Should we use option A or B?
RAUF_QUESTION:ASSUMPTION: I assume the API supports versioning?`

	questions := extractTypedQuestions(output)
	if len(questions) != 3 {
		t.Fatalf("expected 3 questions, got %d", len(questions))
	}

	expected := []struct {
		typ      string
		question string
	}{
		{"CLARIFY", "Is this a breaking change?"},
		{"DECISION", "Should we use option A or B?"},
		{"ASSUMPTION", "I assume the API supports versioning?"},
	}

	for i, exp := range expected {
		if questions[i].Type != exp.typ {
			t.Errorf("question %d type = %q, want %q", i, questions[i].Type, exp.typ)
		}
		if questions[i].Question != exp.question {
			t.Errorf("question %d = %q, want %q", i, questions[i].Question, exp.question)
		}
	}
}

func TestExtractTypedQuestions_InsideFence(t *testing.T) {
	output := "```\nRAUF_QUESTION: ignored\n```\nRAUF_QUESTION: This one counts"
	questions := extractTypedQuestions(output)
	if len(questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(questions))
	}
	if questions[0].Question != "This one counts" {
		t.Errorf("question = %q", questions[0].Question)
	}
}

func TestFormatTypedQuestionForDisplay(t *testing.T) {
	t.Run("with type", func(t *testing.T) {
		q := TypedQuestion{Type: "CLARIFY", Question: "Is this correct?"}
		result := formatTypedQuestionForDisplay(q)
		if result != "[CLARIFY] Is this correct?" {
			t.Errorf("result = %q", result)
		}
	})

	t.Run("without type", func(t *testing.T) {
		q := TypedQuestion{Type: "", Question: "Plain question"}
		result := formatTypedQuestionForDisplay(q)
		if result != "Plain question" {
			t.Errorf("result = %q", result)
		}
	})
}

func TestExtractTypedQuestions_Sticky(t *testing.T) {
	output := `
Some noise
RAUF_QUESTION:ASSUMPTION:STICKY: Is this production?
RAUF_QUESTION:ASSUMPTION:GLOBAL: Is network available?
RAUF_QUESTION:ASSUMPTION: Local assumption
`
	questions := extractTypedQuestions(output)
	if len(questions) != 3 {
		t.Fatalf("got %d questions, want 3", len(questions))
	}

	if questions[0].StickyScope != "sticky" {
		t.Errorf("first question scope = %q, want 'sticky'", questions[0].StickyScope)
	}
	if questions[0].Question != "Is this production?" {
		t.Errorf("got %q, want 'Is this production?'", questions[0].Question)
	}

	if questions[1].StickyScope != "global" {
		t.Errorf("second question scope = %q, want 'global'", questions[1].StickyScope)
	}
	if questions[1].Question != "Is network available?" {
		t.Errorf("got %q, want 'Is network available?'", questions[1].Question)
	}

	if questions[2].StickyScope != "" {
		t.Error("third question should provide empty StickyScope")
	}
}

func TestFormatTypedQuestionForDisplay_Sticky(t *testing.T) {
	q := TypedQuestion{Type: "ASSUMPTION", Question: "Foo?", StickyScope: "global"}
	display := formatTypedQuestionForDisplay(q)
	expected := "[ASSUMPTION:GLOBAL] Foo?"
	if display != expected {
		t.Errorf("got %q, want %q", display, expected)
	}
}
