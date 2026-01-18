package main

import (
	"os"
	"testing"
)

func TestListSpecs(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	os.MkdirAll("specs", 0o755)

	t.Run("various specs", func(t *testing.T) {
		os.WriteFile("specs/stable.md", []byte("---\nstatus: stable\n---\n"), 0o644)
		os.WriteFile("specs/draft.md", []byte("---\nstatus: draft\n---\n"), 0o644)
		os.WriteFile("specs/nostatus.md", []byte("---\nfoo: bar\n---\n"), 0o644)
		os.WriteFile("specs/notfrontmatter.md", []byte("hello\n"), 0o644)

		entries, err := listSpecs()
		if err != nil {
			t.Fatalf("listSpecs failed: %v", err)
		}

		foundStable := false
		foundDraft := false
		foundNoStatus := false
		foundNotFrontmatter := false

		for _, entry := range entries {
			if entry == "specs/stable.md (status: stable)" {
				foundStable = true
			}
			if entry == "specs/draft.md (status: draft)" {
				foundDraft = true
			}
			if entry == "specs/nostatus.md (status: unknown)" {
				foundNoStatus = true
			}
			if entry == "specs/notfrontmatter.md (status: unknown)" {
				foundNotFrontmatter = true
			}
		}

		if !foundStable {
			t.Error("missing stable spec")
		}
		if !foundDraft {
			t.Error("missing draft spec")
		}
		if !foundNoStatus {
			t.Error("missing nostatus spec")
		}
		if !foundNotFrontmatter {
			t.Error("missing notfrontmatter spec")
		}
	})
}
