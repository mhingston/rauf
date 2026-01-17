package main

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

type planTask struct {
	TitleLine         string
	VerifyCmds        []string
	VerifyPlaceholder bool
	SpecRefs          []string
	TaskBlock         []string
	FilesMentioned    []string
}

type planLintResult struct {
	MultipleVerify  bool
	MultipleOutcome bool
}

func readActiveTask(planPath string) (planTask, bool, error) {
	file, err := os.Open(planPath)
	if err != nil {
		return planTask{}, false, err
	}
	defer file.Close()

	taskLine := regexp.MustCompile(`^\s*[-*]\s+\[\s\]\s+(.+)$`)
	verifyLine := regexp.MustCompile(`^\s*[-*]\s+Verify:\s*(.*)$`)
	specLine := regexp.MustCompile(`^\s*[-*]\s+Spec:\s*(.+)$`)

	var task planTask
	found := false
	collecting := false
	inVerifyBlock := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if match := taskLine.FindStringSubmatch(line); match != nil {
			if found {
				break
			}
			found = true
			collecting = true
			task.TitleLine = strings.TrimSpace(match[1])
			task.TaskBlock = append(task.TaskBlock, line)
			continue
		}
		if !collecting {
			continue
		}
		task.TaskBlock = append(task.TaskBlock, line)
		if inVerifyBlock {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") {
				inVerifyBlock = false
				continue
			}
			cmd := strings.TrimSpace(strings.Trim(trimmed, "`"))
			if cmd == "" {
				continue
			}
			if isVerifyPlaceholder(cmd) {
				task.VerifyPlaceholder = true
				continue
			}
			task.VerifyCmds = append(task.VerifyCmds, cmd)
			continue
		}
		if match := verifyLine.FindStringSubmatch(line); match != nil {
			raw := strings.TrimSpace(match[1])
			raw = strings.Trim(raw, "`")
			if raw == "" {
				inVerifyBlock = true
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(raw), "```") {
				inVerifyBlock = true
				continue
			}
			if isVerifyPlaceholder(raw) {
				task.VerifyPlaceholder = true
				continue
			}
			task.VerifyCmds = append(task.VerifyCmds, raw)
		}
		if match := specLine.FindStringSubmatch(line); match != nil {
			ref := strings.TrimSpace(match[1])
			if ref != "" {
				if path, ok := splitSpecPath(ref); ok {
					task.SpecRefs = append(task.SpecRefs, path)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return planTask{}, false, err
	}
	if !found {
		return planTask{}, false, nil
	}

	task.FilesMentioned = extractFileMentions(task.TaskBlock)
	return task, true, nil
}

func lintPlanTask(task planTask) planLintResult {
	return planLintResult{
		MultipleVerify:  len(task.VerifyCmds) > 1,
		MultipleOutcome: countOutcomeLines(task.TaskBlock) > 1,
	}
}

func countOutcomeLines(lines []string) int {
	outcomeLine := regexp.MustCompile(`^\s*[-*]?\s*Outcome:\s*\S+`)
	count := 0
	for _, line := range lines {
		if outcomeLine.MatchString(line) {
			count++
		}
	}
	return count
}

func isVerifyPlaceholder(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(value, "tbd") {
		return false
	}
	if len(value) == 3 {
		return true
	}
	switch value[3] {
	case ' ', ':', '-':
		return true
	default:
		return false
	}
}

func splitSpecPath(value string) (string, bool) {
	parts := strings.SplitN(value, "#", 2)
	path := strings.TrimSpace(parts[0])
	if path == "" {
		return "", false
	}
	return path, true
}

func extractFileMentions(lines []string) []string {
	seen := make(map[string]struct{})
	paths := []string{}
	candidate := regexp.MustCompile(`[A-Za-z0-9_./-]+\.[A-Za-z0-9]+`)
	for _, line := range lines {
		for _, match := range candidate.FindAllString(line, -1) {
			path := strings.Trim(match, "`"+"\""+"'")
			if path == "" {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			absPath, ok := resolveRepoPath(path)
			if !ok {
				continue
			}
			if _, err := os.Stat(absPath); err == nil {
				seen[path] = struct{}{}
				paths = append(paths, repoRelativePath(absPath))
			}
		}
	}
	return paths
}

func readAgentsVerifyFallback(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	candidates := []string{
		"Tests (fast):",
		"Tests (full):",
		"Typecheck/build:",
	}
	for _, prefix := range candidates {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			cmd := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if cmd == "" || strings.Contains(cmd, "[") {
				continue
			}
			return []string{cmd}
		}
	}
	return nil
}
