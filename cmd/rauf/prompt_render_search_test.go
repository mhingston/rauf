package main

import (
	"context"
	"errors"
	"testing"
)

func TestSearchRelevantFiles(t *testing.T) {
	origRgSearch := rgSearch
	defer func() { rgSearch = origRgSearch }()

	task := planTask{
		TitleLine:      "Task Title",
		FilesMentioned: []string{"main.go"},
	}

	t.Run("no search terms", func(t *testing.T) {
		taskEmpty := planTask{}
		results := searchRelevantFiles(taskEmpty)
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %v", results)
		}
	})

	t.Run("with results", func(t *testing.T) {
		rgSearch = func(ctx context.Context, term string) ([]string, error) {
			if term == "title" {
				return []string{"file1.go", "file2.go"}, nil
			}
			return nil, nil
		}

		results := searchRelevantFiles(task)
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %v", results)
		}
		if results[0] != "file1.go" {
			t.Errorf("got %q, want file1.go", results[0])
		}
	})

	t.Run("search error", func(t *testing.T) {
		rgSearch = func(ctx context.Context, term string) ([]string, error) {
			return nil, errors.New("rg failed")
		}
		results := searchRelevantFiles(task)
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %v", results)
		}
	})

	t.Run("max results limit", func(t *testing.T) {
		rgSearch = func(ctx context.Context, term string) ([]string, error) {
			return []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, nil
		}
		results := searchRelevantFiles(task)
		if len(results) != 8 {
			t.Errorf("expected 8 results (limit), got %d", len(results))
		}
	})
}

func TestExtractSearchTerms(t *testing.T) {
	task := planTask{
		TitleLine:      "Refactor the login logic in auth.go",
		FilesMentioned: []string{"auth_test.go"},
	}
	terms := extractSearchTerms(task)
	// Title words: Refactor, login, logic, auth.go
	// FilesMentioned words: auth_test.go
	// Filtered: Refactor (maybe?), login, logic, auth.go, auth_test.go
	// Small words like "the", "in" should be filtered out.

	foundLogic := false
	foundLogin := false
	for _, term := range terms {
		if term == "logic" {
			foundLogic = true
		}
		if term == "login" {
			foundLogin = true
		}
	}
	if !foundLogic || !foundLogin {
		t.Errorf("missing expected search terms in %v", terms)
	}
}
