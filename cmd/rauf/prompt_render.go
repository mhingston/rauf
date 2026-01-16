package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
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
	for _, path := range paths {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		data, err := os.ReadFile(path)
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
		b.WriteString(path)
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
	for _, path := range paths {
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		seen[path] = struct{}{}
		chunk := truncateHead(string(data), minInt(maxFileBytes, budget))
		if chunk == "" {
			continue
		}
		b.WriteString("#### ")
		b.WriteString(path)
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
		args := []string{"-l", "-F", "-i", "--max-count", "1", term}
		cmd := exec.Command("rg", args...)
		output, err := cmd.Output()
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
	return value[:max]
}

func truncateTail(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(value) <= max {
		return value
	}
	return value[len(value)-max:]
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
	status, err := gitOutput("status", "--porcelain")
	if err != nil || status == "" {
		return nil
	}
	lines := strings.Split(status, "\n")
	paths := []string{}
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
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
