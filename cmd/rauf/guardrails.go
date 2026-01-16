package main

import (
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
