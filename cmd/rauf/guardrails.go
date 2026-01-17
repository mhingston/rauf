package main

import (
	"os"
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
	} else if status, err := gitOutputRaw("status", "--porcelain"); err == nil {
		for _, line := range splitStatusLines(status) {
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

	if cfg.MaxFilesChanged > 0 && len(files) > cfg.MaxFilesChanged {
		return false, "max_files_changed"
	}

	if len(cfg.ForbiddenPaths) > 0 {
		root, rootErr := os.Getwd()
		for _, file := range files {
			fileClean := filepath.Clean(file)
			fileAbs := fileClean
			if rootErr == nil && !filepath.IsAbs(fileClean) {
				fileAbs = filepath.Join(root, fileClean)
			}
			for _, forbidden := range cfg.ForbiddenPaths {
				forbidden = filepath.Clean(strings.TrimSpace(forbidden))
				if forbidden == "" {
					continue
				}
				if rootErr == nil {
					forbiddenAbs := forbidden
					if !filepath.IsAbs(forbidden) {
						forbiddenAbs = filepath.Join(root, forbidden)
					}
					if fileAbs == forbiddenAbs || strings.HasPrefix(fileAbs, forbiddenAbs+string(filepath.Separator)) {
						return false, "forbidden_path:" + forbidden
					}
				} else if fileClean == forbidden || strings.HasPrefix(fileClean, forbidden+string(filepath.Separator)) {
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
	root, rootErr := os.Getwd()
	planPath = filepath.Clean(planPath)
	if rootErr == nil && !filepath.IsAbs(planPath) {
		planPath = filepath.Join(root, planPath)
	}
	files := listChangedFiles(headBefore, headAfter)
	for _, file := range files {
		path := filepath.Clean(file)
		if rootErr == nil && !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if path != planPath {
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
	} else if status, err := gitOutputRaw("status", "--porcelain"); err == nil {
		for _, line := range splitStatusLines(status) {
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

func splitStatusLines(value string) []string {
	if value == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	out := []string{}
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
