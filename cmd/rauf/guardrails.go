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
		if err != nil {
			// Fail-closed: if we can't check commit count, reject to be safe
			return false, "git_error_commit_count"
		}
		if v := strings.TrimSpace(count); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > cfg.MaxCommits {
				return false, "max_commits_exceeded"
			}
		}
	}

	files := []string{}
	gitFilesErr := false
	if headAfter != headBefore {
		names, err := gitOutput("diff", "--name-only", headBefore+".."+headAfter)
		if err != nil {
			gitFilesErr = true
		} else {
			files = append(files, splitLines(names)...)
		}
	} else {
		status, err := gitOutputRaw("status", "--porcelain")
		if err != nil {
			gitFilesErr = true
		} else {
			for _, line := range splitStatusLines(status) {
				// Git porcelain v1 format: "XY PATH" where XY are 2 status chars followed by space
				// Minimum valid: 2 status + 1 space + 1 char path = 4 chars
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
	}

	// Fail-closed: if we can't get the file list and there are guardrails that depend on it
	if gitFilesErr && (cfg.MaxFilesChanged > 0 || len(cfg.ForbiddenPaths) > 0) {
		return false, "git_error_file_list"
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
	files, gitErr := listChangedFiles(headBefore, headAfter)
	if gitErr {
		// Fail-closed: if we can't determine changed files, reject to be safe
		return false, "git_error_file_list"
	}
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

// listChangedFiles returns a list of changed files and whether there was a git error.
// When gitErr is true, callers should fail-closed to avoid bypassing guardrails.
func listChangedFiles(headBefore, headAfter string) (files []string, gitErr bool) {
	files = []string{}
	if headAfter != headBefore {
		names, err := gitOutput("diff", "--name-only", headBefore+".."+headAfter)
		if err != nil {
			return nil, true
		}
		files = append(files, splitLines(names)...)
	} else {
		status, err := gitOutputRaw("status", "--porcelain")
		if err != nil {
			return nil, true
		}
		for _, line := range splitStatusLines(status) {
			// Git porcelain v1 format: "XY PATH" where XY are 2 status chars followed by space
			// Minimum valid: 2 status + 1 space + 1 char path = 4 chars
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
	return files, false
}

func parseStatusPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Check for rename indicator. Git format: "old path" -> "new path" or old -> new
	// Only split on " -> " if it's outside quotes to avoid false positives
	// for filenames containing " -> "
	arrowIdx := findUnquotedArrow(value)
	if arrowIdx >= 0 {
		return unquoteGitPath(strings.TrimSpace(value[arrowIdx+4:]))
	}
	// Git uses C-style quoting for paths with special characters
	return unquoteGitPath(value)
}

// findUnquotedArrow finds " -> " outside of quoted strings.
// Returns the index of the space before "->", or -1 if not found.
func findUnquotedArrow(s string) int {
	inQuote := false
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			// Toggle quote state, handling escaped quotes.
			// A quote is escaped if preceded by an odd number of backslashes.
			if !inQuote {
				inQuote = true
			} else {
				// Count preceding backslashes
				numBackslashes := 0
				for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
					numBackslashes++
				}
				// Odd number of backslashes means the quote is escaped
				if numBackslashes%2 == 1 {
					// Escaped quote, stay in quote
				} else {
					inQuote = false
				}
			}
			continue
		}
		if !inQuote && i+4 <= len(s) && s[i:i+4] == " -> " {
			return i
		}
	}
	return -1
}

// unquoteGitPath handles git's C-style quoting for paths with special characters.
// Git quotes paths that contain special characters (spaces, non-ASCII, etc.)
// using C-style escaping with surrounding double quotes.
func unquoteGitPath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) < 2 || path[0] != '"' || path[len(path)-1] != '"' {
		return path
	}
	// Remove surrounding quotes
	inner := path[1 : len(path)-1]
	// Handle common C-style escape sequences
	var result strings.Builder
	result.Grow(len(inner))
	i := 0
	for i < len(inner) {
		if inner[i] == '\\' && i+1 < len(inner) {
			switch inner[i+1] {
			case '\\':
				result.WriteByte('\\')
				i += 2
			case '"':
				result.WriteByte('"')
				i += 2
			case 'n':
				result.WriteByte('\n')
				i += 2
			case 't':
				result.WriteByte('\t')
				i += 2
			case 'r':
				result.WriteByte('\r')
				i += 2
			default:
				// For octal sequences like \302\240, decode them
				if i+3 < len(inner) && isOctalDigit(inner[i+1]) && isOctalDigit(inner[i+2]) && isOctalDigit(inner[i+3]) {
					val := int(inner[i+1]-'0')*64 + int(inner[i+2]-'0')*8 + int(inner[i+3]-'0')
					// Validate octal value is within byte range (0-255)
					if val > 255 {
						// Invalid octal sequence, preserve as-is
						result.WriteByte(inner[i])
						i++
					} else {
						result.WriteByte(byte(val))
						i += 4
					}
				} else {
					result.WriteByte(inner[i])
					i++
				}
			}
		} else {
			result.WriteByte(inner[i])
			i++
		}
	}
	return result.String()
}

func isOctalDigit(b byte) bool {
	return b >= '0' && b <= '7'
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
