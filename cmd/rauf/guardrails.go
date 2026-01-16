package main

import (
	"path/filepath"
	"strconv"
	"strings"
)

func enforceGuardrails(cfg runtimeConfig, headBefore, headAfter string) (bool, string) {
	if cfg.MaxCommits > 0 {
		count, err := gitOutput("rev-list", "--count", headBefore+".."+headAfter)
		if err == nil {
			if v := strings.TrimSpace(count); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > cfg.MaxCommits {
					return false, "max_commits_exceeded"
				}
			}
		}
	}

	files := []string{}
	if headAfter != headBefore {
		if names, err := gitOutput("diff", "--name-only", headBefore+".."+headAfter); err == nil {
			files = append(files, splitLines(names)...)
		}
	} else if status, err := gitOutput("status", "--porcelain"); err == nil {
		for _, line := range splitLines(status) {
			if len(line) < 4 {
				continue
			}
			files = append(files, strings.TrimSpace(line[3:]))
		}
	}

	if cfg.MaxFilesChanged > 0 && len(files) > cfg.MaxFilesChanged {
		return false, "max_files_changed"
	}

	if len(cfg.ForbiddenPaths) > 0 {
		for _, file := range files {
			for _, forbidden := range cfg.ForbiddenPaths {
				forbidden = strings.TrimSpace(forbidden)
				if forbidden == "" {
					continue
				}
				if strings.HasPrefix(file, forbidden) {
					return false, "forbidden_path:" + forbidden
				}
			}
		}
	}

	return true, ""
}

func enforceVerificationGuardrails(cfg runtimeConfig, verifyStatus string, planChanged bool, worktreeChanged bool) (bool, string) {
	if cfg.RequireVerifyForPlanUpdate && planChanged && verifyStatus != "pass" {
		return false, "plan_update_without_verify"
	}
	if cfg.RequireVerifyOnChange && worktreeChanged && verifyStatus == "skipped" {
		return false, "verify_required_for_change"
	}
	return true, ""
}

func enforceMissingVerifyGuardrail(planPath, headBefore, headAfter string, planChanged bool) (bool, string) {
	if !planChanged {
		return false, "missing_verify_plan_not_updated"
	}
	planPath = filepath.Clean(planPath)
	files := listChangedFiles(headBefore, headAfter)
	for _, file := range files {
		if filepath.Clean(file) != planPath {
			return false, "missing_verify_non_plan_change"
		}
	}
	return true, ""
}

func enforceMissingVerifyNoGit(planChanged bool, fingerprintBefore, fingerprintAfter string) (bool, string) {
	if !planChanged {
		return false, "missing_verify_plan_not_updated"
	}
	if fingerprintBefore != "" && fingerprintAfter != "" && fingerprintBefore != fingerprintAfter {
		return false, "missing_verify_non_plan_change"
	}
	return true, ""
}

func listChangedFiles(headBefore, headAfter string) []string {
	files := []string{}
	if headAfter != headBefore {
		if names, err := gitOutput("diff", "--name-only", headBefore+".."+headAfter); err == nil {
			files = append(files, splitLines(names)...)
		}
	} else if status, err := gitOutput("status", "--porcelain"); err == nil {
		for _, line := range splitLines(status) {
			if len(line) < 4 {
				continue
			}
			path := parseStatusPath(line[3:])
			if path == "" {
				continue
			}
			files = append(files, path)
		}
	}
	return files
}

func parseStatusPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "->") {
		parts := strings.Split(value, "->")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	return value
}

func splitLines(value string) []string {
	if value == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	out := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
