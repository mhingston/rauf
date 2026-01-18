package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"
)

type promptData struct {
	Mode                    string
	PlanPath                string
	ActiveTask              string
	VerifyCommand           string
	CapabilityMap           string
	ContextFile             string
	SpecContext             string
	RelevantFiles           string
	RepoMap                 string
	SpecIndex               string
	PlanSummary             string
	PriorVerification       string
	PriorVerificationCmd    string
	PriorVerificationStatus string
}

const (
	maxSpecBytes       = 40 * 1024
	maxRelevantBytes   = 60 * 1024
	maxFileBytes       = 12 * 1024
	maxRepoMapLines    = 200
	maxVerifyOutput    = 12 * 1024
	maxCapabilityBytes = 4 * 1024
	maxContextBytes    = 8 * 1024
)

func buildPromptContent(promptFile string, data promptData) (string, string, error) {
	content, err := os.ReadFile(promptFile)
	if err != nil {
		return "", "", err
	}

	// Escape template delimiters in user-controlled fields to prevent injection
	data.ActiveTask = escapeTemplateDelimiters(data.ActiveTask)
	data.VerifyCommand = escapeTemplateDelimiters(data.VerifyCommand)
	data.PlanSummary = escapeTemplateDelimiters(data.PlanSummary)
	data.PriorVerification = escapeTemplateDelimiters(data.PriorVerification)
	data.PriorVerificationCmd = escapeTemplateDelimiters(data.PriorVerificationCmd)

	tmpl, err := template.New(filepath.Base(promptFile)).Option("missingkey=zero").Parse(string(content))
	if err != nil {
		return "", "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", "", err
	}
	rendered := buf.String()
	hash := sha256.Sum256([]byte(rendered))
	return rendered, fmt.Sprintf("%x", hash), nil
}

// escapeTemplateDelimiters escapes Go template delimiters to prevent template injection
// from user-controlled content like task names or verification output.
func escapeTemplateDelimiters(s string) string {
	// Replace {{ with a literal representation that won't be interpreted as template
	s = strings.ReplaceAll(s, "{{", "{ {")
	s = strings.ReplaceAll(s, "}}", "} }")
	return s
}

func buildContextPack(planPath string, task planTask, verifyCmds []string, state raufState, gitAvailable bool, verifyInstruction string) string {
	var b strings.Builder
	b.WriteString("## Context Pack (auto-generated)\n\n")
	if planPath != "" {
		b.WriteString("Plan Path: ")
		b.WriteString(planPath)
		b.WriteString("\n")
	}
	if task.TitleLine != "" {
		b.WriteString("Active Task: ")
		b.WriteString(task.TitleLine)
		b.WriteString("\n")
	}
	if len(verifyCmds) == 1 {
		b.WriteString("Verify: ")
		b.WriteString(verifyCmds[0])
		b.WriteString("\n")
	} else if len(verifyCmds) > 1 {
		b.WriteString("Verify:\n")
		for _, cmd := range verifyCmds {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(cmd)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	if verifyInstruction != "" {
		b.WriteString("SYSTEM: ")
		b.WriteString(verifyInstruction)
		b.WriteString("\n\n")
	}

	if state.LastVerificationOutput != "" {
		b.WriteString("SYSTEM: Previous verification failed.\n")
		if state.LastVerificationCommand != "" {
			b.WriteString("Command: ")
			b.WriteString(state.LastVerificationCommand)
			b.WriteString("\n")
		}
		b.WriteString("Output (truncated):\n\n```")
		b.WriteString(state.LastVerificationOutput)
		b.WriteString("\n```\n\n")
	}

	specs := readSpecContexts(task.SpecRefs, maxSpecBytes)
	if specs != "" {
		b.WriteString("### Spec Context\n\n")
		b.WriteString(specs)
		b.WriteString("\n")
	}

	files := readRelevantFiles(task, gitAvailable, maxRelevantBytes)
	if files != "" {
		b.WriteString("### Relevant Files\n\n")
		b.WriteString(files)
		b.WriteString("\n")
	}

	return b.String()
}

func readSpecContexts(paths []string, maxBytes int) string {
	seen := make(map[string]struct{})
	var b strings.Builder
	budget := maxBytes
	// Cache the root directory for consistent path resolution
	root, err := os.Getwd()
	if err != nil {
		return ""
	}
	for _, path := range paths {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		absPath, ok := resolveRepoPathWithRoot(path, root)
		if !ok {
			continue
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		seen[path] = struct{}{}
		chunk := string(data)
		chunk = truncateHead(chunk, minInt(maxBytes, budget))
		if chunk == "" {
			continue
		}
		b.WriteString("#### ")
		b.WriteString(repoRelativePathWithRoot(absPath, root))
		b.WriteString("\n\n```")
		b.WriteString(chunk)
		b.WriteString("\n```\n\n")
		budget -= len(chunk)
		if budget <= 0 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func readRelevantFiles(task planTask, gitAvailable bool, maxBytes int) string {
	paths := append([]string{}, task.FilesMentioned...)
	if gitAvailable {
		paths = append(paths, searchRelevantFiles(task)...)
	}
	seen := make(map[string]struct{})
	var b strings.Builder
	budget := maxBytes
	// Cache the root directory for consistent path resolution
	root, err := os.Getwd()
	if err != nil {
		return ""
	}
	for _, path := range paths {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		absPath, ok := resolveRepoPathWithRoot(path, root)
		if !ok {
			continue
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		seen[path] = struct{}{}
		chunk := truncateHead(string(data), minInt(maxFileBytes, budget))
		if chunk == "" {
			continue
		}
		b.WriteString("#### ")
		b.WriteString(repoRelativePathWithRoot(absPath, root))
		b.WriteString("\n\n```")
		b.WriteString(chunk)
		b.WriteString("\n```\n\n")
		budget -= len(chunk)
		if budget <= 0 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func readAgentsCapabilityMap(path string, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	section := ""
	var b strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			continue
		}
		if section != "Commands" && section != "Git" {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(trimmed)
		}
	}
	return truncateHead(strings.TrimSpace(b.String()), maxBytes)
}

func readContextFile(path string, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return truncateHead(strings.TrimSpace(string(data)), maxBytes)
}

func searchRelevantFiles(task planTask) []string {
	if _, err := exec.LookPath("rg"); err != nil {
		return nil
	}
	terms := extractSearchTerms(task)
	if len(terms) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	results := []string{}
	for _, term := range terms {
		if len(results) >= 8 {
			break
		}
		// Use a timeout to prevent hanging on large repos or pathological inputs
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		args := []string{"-l", "-F", "-i", "--max-count", "1", term}
		cmd := exec.CommandContext(ctx, "rg", args...)
		output, err := cmd.Output()
		cancel()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			results = append(results, line)
			if len(results) >= 8 {
				break
			}
		}
	}
	return results
}

func extractSearchTerms(task planTask) []string {
	stop := map[string]struct{}{
		"this": {}, "that": {}, "with": {}, "from": {}, "will": {}, "should": {}, "would": {}, "could": {},
		"when": {}, "then": {}, "them": {}, "they": {}, "into": {}, "your": {}, "task": {}, "plan": {},
		"verify": {}, "spec": {}, "notes": {}, "outcome": {}, "build": {}, "mode": {},
	}
	wordPattern := regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]{3,}`)
	lines := append([]string{task.TitleLine}, task.TaskBlock...)
	seen := make(map[string]struct{})
	terms := []string{}
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "verify:") || strings.Contains(lower, "spec:") {
			continue
		}
		for _, match := range wordPattern.FindAllString(line, -1) {
			word := strings.ToLower(match)
			if _, ok := stop[word]; ok {
				continue
			}
			if _, ok := seen[word]; ok {
				continue
			}
			seen[word] = struct{}{}
			terms = append(terms, word)
			if len(terms) >= 6 {
				return terms
			}
		}
	}
	return terms
}

func truncateHead(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(value) <= max {
		return value
	}
	// Truncate by bytes, then back up to valid UTF-8 boundary
	truncated := value[:max]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

func truncateTail(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(value) <= max {
		return value
	}
	// Truncate by bytes from end, then advance to valid UTF-8 boundary
	start := len(value) - max
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func buildRepoMap(gitAvailable bool) string {
	if !gitAvailable {
		return ""
	}
	files, err := gitOutput("ls-files")
	if err != nil || files == "" {
		return ""
	}
	lines := strings.Split(files, "\n")
	if len(lines) > maxRepoMapLines {
		lines = lines[:maxRepoMapLines]
	}
	return strings.Join(lines, "\n")
}

func buildSpecIndex() string {
	entries, err := listSpecs()
	if err != nil || len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for _, entry := range entries {
		b.WriteString(entry)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func readRecentFiles() []string {
	status, err := gitOutputRaw("status", "--porcelain")
	if err != nil || status == "" {
		return nil
	}
	lines := strings.Split(status, "\n")
	paths := []string{}
	for _, line := range lines {
		// Git porcelain v1 format: "XY PATH" where XY are 2 status chars followed by space
		// Minimum valid: 2 status + 1 space + 1 char path = 4 chars
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		path := parseStatusPath(line[3:])
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func buildPlanSummary(planPath string, task planTask) string {
	if task.TitleLine == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Active task from ")
	b.WriteString(planPath)
	b.WriteString(":\n")
	for _, line := range task.TaskBlock {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func normalizeVerifyOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return truncateTail(output, maxVerifyOutput)
}

func resolveRepoPath(path string) (string, bool) {
	return resolveRepoPathWithRoot(path, "")
}

func resolveRepoPathWithRoot(path string, root string) (string, bool) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", false
		}
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		if !isWithinRoot(root, clean) {
			return "", false
		}
		return clean, true
	}
	abs := filepath.Join(root, clean)
	if !isWithinRoot(root, abs) {
		return "", false
	}
	return abs, true
}

func repoRelativePath(absPath string) string {
	return repoRelativePathWithRoot(absPath, "")
}

func repoRelativePathWithRoot(absPath string, root string) string {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return absPath
		}
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return absPath
	}
	return filepath.Clean(rel)
}

func isWithinRoot(root, target string) bool {
	// Resolve symlinks in root to get canonical path
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// If root can't be resolved, use the original
		resolvedRoot = root
	}

	// Resolve symlinks in target to detect path traversal via symlinks
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		// If target doesn't exist or can't be resolved, check the unresolved path
		// This is safe because the file read will fail anyway if it doesn't exist
		resolvedTarget = target
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
