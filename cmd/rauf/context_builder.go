package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type repoMap struct {
	Tree                 string
	Entrypoints          []string
	Modules              []string
	ConfigLocations      []string
	SideEffectBoundaries []string
}

func buildEnhancedContext(ctx context.Context, task string, gitAvailable bool) (string, string, error) {
	// 1. Repo Map (Cached)
	rmContent := ""
	useCache := gitAvailable && !isGitDirty()
	if useCache {
		var ok bool
		rmContent, ok = loadRepoMapFromCache(gitAvailable)
		if !ok {
			rm, err := generateRepoMap()
			if err != nil {
				return "", "", err
			}
			rmContent = rm.String()
			saveRepoMapToCache(rmContent, gitAvailable)
		}
	} else {
		rm, err := generateRepoMap()
		if err != nil {
			return "", "", err
		}
		rmContent = rm.String()
	}

	// 2. Context Pack (Task-specific, not cached)
	cp, err := generateContextPack(ctx, task, gitAvailable)
	if err != nil {
		return rmContent, "", err
	}
	return rmContent, cp.String(), nil
}

func (rm repoMap) String() string {
	var b strings.Builder
	b.WriteString("# Repo Map\n\n")

	b.WriteString("## Folder Tree (Depth 3)\n")
	b.WriteString(rm.Tree)
	b.WriteString("\n\n")

	if len(rm.Entrypoints) > 0 {
		b.WriteString("## Entrypoints\n")
		for _, e := range rm.Entrypoints {
			b.WriteString(fmt.Sprintf("- %s\n", e))
		}
		b.WriteString("\n")
	}

	if len(rm.Modules) > 0 {
		b.WriteString("## Modules/Packages\n")
		for _, m := range rm.Modules {
			b.WriteString(fmt.Sprintf("- %s\n", m))
		}
		b.WriteString("\n")
	}

	if len(rm.ConfigLocations) > 0 {
		b.WriteString("## Config & Parsing\n")
		for _, c := range rm.ConfigLocations {
			b.WriteString(fmt.Sprintf("- %s\n", c))
		}
		b.WriteString("\n")
	}

	if len(rm.SideEffectBoundaries) > 0 {
		b.WriteString("## Side Effect Boundaries (FS, Net, Proc)\n")
		for _, s := range rm.SideEffectBoundaries {
			b.WriteString(fmt.Sprintf("- %s\n", s))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func generateRepoMap() (repoMap, error) {
	rm := repoMap{}

	// 1. Tree generation (Deterministic and Depth-limited)
	tree, err := buildFolderTree(".", 3)
	if err != nil {
		return rm, err
	}
	rm.Tree = tree

	// 2. Discover files
	files, err := listAllFiles()
	if err != nil {
		return rm, err
	}

	// 3. Side Effect Discovery (Optimization: Use rg instead of O(N) reads)
	sideEffects := make(map[string]bool)
	if _, err := exec.LookPath("rg"); err == nil {
		out, err := exec.Command("rg", "-l", "--fixed-strings", "-e", "os.", "-e", "net/http", "-e", "exec.Command", ".").Output()
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line != "" {
					sideEffects[line] = true
				}
			}
		}
	}

	// 4. Heuristic discovery
	for _, f := range files {
		if f == "" {
			continue
		}
		// Entrypoints
		if isEntrypoint(f) {
			rm.Entrypoints = append(rm.Entrypoints, f)
		}
		// Modules (Top-level dirs)
		dir := strings.Split(f, string(filepath.Separator))[0]
		if dir != "." && isModuleDir(dir) {
			found := false
			for _, m := range rm.Modules {
				if m == dir {
					found = true
					break
				}
			}
			if !found {
				rm.Modules = append(rm.Modules, dir)
			}
		}
		// Config
		if isConfig(f) {
			rm.ConfigLocations = append(rm.ConfigLocations, f)
		}
		// Side Effects
		if sideEffects[f] {
			rm.SideEffectBoundaries = append(rm.SideEffectBoundaries, f)
		}
	}

	// Deterministic ordering
	sort.Strings(rm.Entrypoints)
	sort.Strings(rm.Modules)
	sort.Strings(rm.ConfigLocations)
	sort.Strings(rm.SideEffectBoundaries)

	return rm, nil
}

func buildFolderTree(root string, maxDepth int) (string, error) {
	var b strings.Builder
	var walk func(string, int) error
	walk = func(path string, depth int) error {
		if depth > maxDepth {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil // Skip unreadable
		}
		// Deterministic sort
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				continue
			}
			if entry.IsDir() {
				b.WriteString(fmt.Sprintf("%s%s/\n", strings.Repeat("  ", depth), name))
				if err := walk(filepath.Join(path, name), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := walk(root, 0)
	return b.String(), err
}

type contextHit struct {
	Path       string
	Score      int
	Rationale  string
	LineRanges []string
	Excerpts   string
}

type contextPack struct {
	Summary   string
	Hits      []contextHit
	ZeroState string
}

func (cp contextPack) String() string {
	var b strings.Builder
	b.WriteString("# Context Pack\n\n")

	if cp.ZeroState != "" {
		b.WriteString("## No Relevant Code Found\n")
		b.WriteString(cp.ZeroState)
		b.WriteString("\n")
		return b.String()
	}

	b.WriteString("## Summary\n")
	b.WriteString(cp.Summary)
	b.WriteString("\n\n")

	for _, hit := range cp.Hits {
		b.WriteString(fmt.Sprintf("### %s\n", hit.Path))
		b.WriteString(fmt.Sprintf("**Rationale**: %s (Score: %d)\n\n", hit.Rationale, hit.Score))
		b.WriteString("```")
		// Add language for syntax highlighting if possible
		ext := filepath.Ext(hit.Path)
		if len(ext) > 1 {
			b.WriteString(ext[1:])
		}
		b.WriteString("\n")
		b.WriteString(hit.Excerpts)
		b.WriteString("\n```\n\n")
	}

	return b.String()
}

func generateContextPack(ctx context.Context, task string, gitAvailable bool) (contextPack, error) {
	cp := contextPack{
		Summary: fmt.Sprintf("Targeted context for task: %s", truncateHead(task, 100)),
	}

	keywords := extractKeywords(task)
	if len(keywords) == 0 {
		cp.ZeroState = "No keywords could be extracted from the task description."
		return cp, nil
	}

	hits := make(map[string]*contextHit)

	// 1. Focused searches
	for _, kw := range keywords {
		foundFiles, err := performSearch(ctx, kw, gitAvailable)
		if err != nil {
			return cp, err
		}
		for _, f := range foundFiles {
			if f == "" {
				continue
			}
			if hit, ok := hits[f]; ok {
				hit.Score += 5
				hit.Rationale += fmt.Sprintf("; found by kw: %s", kw)
			} else {
				hits[f] = &contextHit{
					Path:      f,
					Score:     5,
					Rationale: fmt.Sprintf("Direct search hit for kw: %s", kw),
				}
			}
		}
	}

	// 2. Symbol Expansion (LSP-Lite)
	// Snapshot keys to avoid mutation during iteration
	var snapshot []string
	for k := range hits {
		snapshot = append(snapshot, k)
	}
	sort.Strings(snapshot) // Further determinism

	for _, path := range snapshot {
		hit := hits[path]
		symbols := discoverSymbols(hit.Path)
		for _, sym := range symbols {
			callers, err := findCallers(ctx, sym, gitAvailable)
			if err != nil {
				continue
			}
			for _, callerFile := range callers {
				if callerFile == "" {
					continue
				}
				if h, ok := hits[callerFile]; ok {
					h.Score += 3
					h.Rationale += fmt.Sprintf("; call site of %s", sym)
				} else {
					hits[callerFile] = &contextHit{
						Path:      callerFile,
						Score:     3,
						Rationale: fmt.Sprintf("Call site of symbol %s (found in %s)", sym, hit.Path),
					}
				}
			}
		}
	}

	// 3. Assemble and Sort
	var finalHits []contextHit
	for _, h := range hits {
		// Populate excerpts (Targeted: lines around matches)
		excerpt, err := getExcerpts(h.Path, keywords)
		if err == nil && excerpt != "" {
			h.Excerpts = excerpt
			finalHits = append(finalHits, *h)
		}
	}

	// Deterministic Sort: Score (desc), then Path (asc)
	sort.Slice(finalHits, func(i, j int) bool {
		if finalHits[i].Score != finalHits[j].Score {
			return finalHits[i].Score > finalHits[j].Score
		}
		return finalHits[i].Path < finalHits[j].Path
	})

	// Caps
	if len(finalHits) > 12 {
		finalHits = finalHits[:12]
	}
	cp.Hits = finalHits

	if len(cp.Hits) == 0 {
		cp.ZeroState = fmt.Sprintf("Search for keywords [%s] returned no relevant files.", strings.Join(keywords, ", "))
	}

	return cp, nil
}

func extractKeywords(task string) []string {
	// Simple keyword extraction (splitting, stop words)
	stop := map[string]bool{"the": true, "and": true, "for": true, "with": true, "should": true, "would": true}

	// Preserve quoted phrases
	quotedRegex := regexp.MustCompile(`"([^"]+)"`)
	quoted := quotedRegex.FindAllStringSubmatch(task, -1)

	words := strings.Fields(task)
	var kws []string
	seen := make(map[string]bool)

	for _, q := range quoted {
		phrase := strings.TrimSpace(q[1])
		if phrase != "" && !seen[phrase] {
			kws = append(kws, phrase)
			seen[phrase] = true
		}
	}

	for _, w := range words {
		w = strings.ToLower(regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(w, ""))
		if len(w) > 3 && !stop[w] && !seen[w] {
			kws = append(kws, w)
			seen[w] = true
		}
		if len(kws) >= 6 {
			break
		}
	}
	return kws
}

func performSearch(ctx context.Context, kw string, gitAvailable bool) ([]string, error) {
	var raw string
	if _, err := exec.LookPath("rg"); err == nil {
		out, err := exec.CommandContext(ctx, "rg", "-l", "--fixed-strings", "--", kw, ".").Output()
		if err != nil {
			if isSearchNoMatch(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("rg search failed for %q: %w", kw, err)
		}
		raw = string(out)
	} else if gitAvailable {
		out, err := gitOutputRaw("grep", "-l", "--fixed-strings", "-e", kw)
		if err == nil {
			raw = out
		}
		// git grep returns a non-zero status when there are no matches.
		// Treat errors as "no results" since we use fixed strings.
	} else {
		return nil, fmt.Errorf("no search tool available (rg missing and git unavailable)")
	}

	if raw == "" {
		return nil, nil
	}

	var results []string
	for _, f := range strings.Split(strings.TrimSpace(raw), "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			results = append(results, f)
		}
	}
	return results, nil
}

func isSearchNoMatch(err error) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode() == 1
	}
	return false
}

func discoverSymbols(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)

	ext := filepath.Ext(path)
	var symRegex *regexp.Regexp

	switch ext {
	case ".go":
		symRegex = regexp.MustCompile(`func\s+([A-Z][a-zA-Z0-9_]+)`)
	case ".ts", ".js", ".tsx", ".jsx":
		symRegex = regexp.MustCompile(`(?:function|const|let|class)\s+([a-zA-Z0-9_]+)`)
	case ".py":
		symRegex = regexp.MustCompile(`(?:def|class)\s+([a-zA-Z0-9_]+)`)
	default:
		return nil
	}

	matches := symRegex.FindAllStringSubmatch(content, -1)
	var syms []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			syms = append(syms, m[1])
			seen[m[1]] = true
		}
	}
	return syms
}

func findCallers(ctx context.Context, sym string, gitAvailable bool) ([]string, error) {
	// Search for symbol usage (crude approximation of callers)
	return performSearch(ctx, sym, gitAvailable)
}

func getExcerpts(path string, keywords []string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	// Find lines containing any keyword
	matchLines := make(map[int]bool)
	for i, line := range lines {
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(line), strings.ToLower(kw)) {
				matchLines[i] = true
				break
			}
		}
	}

	if len(matchLines) == 0 {
		// Fallback to head if no matches (though unlikely for a hit)
		if len(lines) > 50 {
			return strings.Join(lines[:50], "\n") + "\n... (truncated)", nil
		}
		return strings.Join(lines, "\n"), nil
	}

	// Build windowed excerpts
	var b strings.Builder
	var lastLine int = -1

	// Collect match line numbers and sort
	var sortedMatches []int
	for m := range matchLines {
		sortedMatches = append(sortedMatches, m)
	}
	sort.Ints(sortedMatches)

	count := 0
	for _, m := range sortedMatches {
		if count >= 50 {
			b.WriteString("\n... (limit reached)")
			break
		}

		start := m - 3
		if start < 0 {
			start = 0
		}
		if start <= lastLine {
			start = lastLine + 1
		}

		end := m + 3
		if end >= len(lines) {
			end = len(lines) - 1
		}

		if start > lastLine && lastLine != -1 {
			b.WriteString("\n---\n")
		}

		for i := start; i <= end; i++ {
			b.WriteString(fmt.Sprintf("%4d | %s\n", i+1, lines[i]))
			count++
			if count >= 50 {
				break
			}
		}
		lastLine = end
	}

	return b.String(), nil
}

func listAllFiles() ([]string, error) {
	// Fallback to filepath.Walk if git not available, but we prefer git ls-files
	out, err := gitOutput("ls-files")
	if err != nil {
		// Fallback
		var files []string
		err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			files = append(files, path)
			return nil
		})
		return files, err
	}
	return strings.Split(strings.TrimSpace(out), "\n"), nil
}

func isEntrypoint(path string) bool {
	base := filepath.Base(path)
	return base == "main.go" || base == "index.ts" || base == "app.py" || strings.HasPrefix(path, "cmd/")
}

func isModuleDir(dir string) bool {
	stop := map[string]bool{".git": true, ".github": true, "logs": true, "tests": true, "spec": true}
	return !stop[dir]
}

func isConfig(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(base, "config") || strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".json") || base == "go.mod" || base == "package.json"
}

func getContextCacheKey(gitAvailable bool) string {
	head := "no-git"
	if gitAvailable {
		if h, err := gitOutput("rev-parse", "HEAD"); err == nil {
			head = strings.TrimSpace(h)
		}
	}
	// Also add dependency/config hashes for invalidation
	depHash := ""
	for _, f := range []string{"go.mod", "package.json", "requirements.txt", "rauf.yaml"} {
		if h := fileHash(f); h != "" {
			depHash += h
		}
	}
	h := sha256.Sum256([]byte(head + depHash + "v1")) // Tool version v1
	return fmt.Sprintf("%x", h)
}

func loadRepoMapFromCache(gitAvailable bool) (string, bool) {
	key := getContextCacheKey(gitAvailable)
	path := filepath.Join(".rauf", "cache", "context", "repo_map_"+key+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func saveRepoMapToCache(content string, gitAvailable bool) {
	key := getContextCacheKey(gitAvailable)
	dir := filepath.Join(".rauf", "cache", "context")
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "repo_map_"+key+".md")
	os.WriteFile(path, []byte(content), 0644)
}

func isGitDirty() bool {
	out, err := gitOutputRaw("status", "--porcelain")
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) != ""
}
