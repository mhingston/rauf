package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type completionContract struct {
	Found        bool
	VerifyCmds   []string
	Artifacts    []string
	SectionTitle string
}

func lintSpecs() error {
	dir := "specs"
	items, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("spec lint: unable to read specs/: %w", err)
	}
	var issues []string
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".md") {
			continue
		}
		if item.Name() == "_TEMPLATE.md" || item.Name() == "README.md" {
			continue
		}
		path := filepath.Join(dir, item.Name())
		status := readSpecStatus(path)
		if strings.EqualFold(status, "draft") {
			continue
		}
		contract, lintIssues, err := lintSpecCompletionContract(path)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		if !contract.Found {
			issues = append(issues, fmt.Sprintf("%s: missing Completion Contract section", path))
			continue
		}
		if len(lintIssues) > 0 {
			issues = append(issues, fmt.Sprintf("%s: %s", path, strings.Join(lintIssues, "; ")))
		}
	}
	if len(issues) > 0 {
		return fmt.Errorf("spec lint failed:\n- %s", strings.Join(issues, "\n- "))
	}
	return nil
}

func lintSpecCompletionContract(path string) (completionContract, []string, error) {
	contract, err := parseCompletionContract(path)
	if err != nil {
		return completionContract{}, nil, err
	}
	var issues []string
	if !contract.Found {
		return contract, issues, nil
	}
	if len(contract.VerifyCmds) == 0 {
		issues = append(issues, "no verification commands in Completion Contract")
	}
	for _, cmd := range contract.VerifyCmds {
		if containsTBD(cmd) {
			issues = append(issues, "verification command contains TBD")
			break
		}
	}
	return contract, issues, nil
}

func parseCompletionContract(path string) (completionContract, error) {
	file, err := os.Open(path)
	if err != nil {
		return completionContract{}, err
	}
	defer file.Close()

	contract := completionContract{}
	scanner := bufio.NewScanner(file)
	inSection := false
	currentList := ""
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			if inSection {
				break
			}
			if strings.Contains(strings.ToLower(title), "completion contract") {
				inSection = true
				contract.Found = true
				contract.SectionTitle = title
				continue
			}
		}

		if !inSection {
			continue
		}

		if strings.HasPrefix(trimmed, "## ") {
			break
		}

		if trimmed == "" {
			currentList = ""
			continue
		}

		label := strings.ToLower(strings.TrimSuffix(trimmed, ":"))
		switch label {
		case "verification commands":
			currentList = "verify"
			continue
		case "artifacts/flags":
			currentList = "artifacts"
			continue
		case "success condition":
			currentList = ""
			continue
		}

		if len(trimmed) >= 3 {
			if !inFence {
				if trimmed[0] == '`' || trimmed[0] == '~' {
					fenceChar = trimmed[0]
					fenceLen = 1
					for fenceLen < len(trimmed) && trimmed[fenceLen] == fenceChar {
						fenceLen++
					}
					if fenceLen >= 3 {
						inFence = true
						continue
					}
				}
			} else if fenceChar != 0 {
				count := 0
				for count < len(trimmed) && trimmed[count] == fenceChar {
					count++
				}
				if count >= fenceLen {
					inFence = false
					fenceChar = 0
					fenceLen = 0
					continue
				}
			}
		}
		if inFence {
			continue
		}

		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
			entry := strings.TrimSpace(strings.TrimLeft(trimmed, "-*"))
			entry = strings.Trim(entry, "`")
			if entry == "" || currentList == "" {
				continue
			}
			switch currentList {
			case "verify":
				contract.VerifyCmds = append(contract.VerifyCmds, entry)
			case "artifacts":
				contract.Artifacts = append(contract.Artifacts, entry)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return completionContract{}, err
	}
	return contract, nil
}

func containsTBD(value string) bool {
	return strings.Contains(strings.ToLower(value), "tbd")
}

func checkCompletionArtifacts(specRefs []string) (bool, string, []string, []string) {
	if len(specRefs) == 0 {
		return true, "", nil, nil
	}
	var failures []string
	var satisfied []string
	var verified []string
	for _, spec := range specRefs {
		specPath := spec
		if abs, ok := resolveRepoPath(spec); ok {
			specPath = abs
		} else {
			failures = append(failures, fmt.Sprintf("%s: invalid spec path", spec))
			continue
		}
		contract, err := parseCompletionContract(specPath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", spec, err))
			continue
		}
		if !contract.Found {
			continue
		}
		specLabel := repoRelativePath(specPath)
		if len(contract.Artifacts) == 0 {
			satisfied = append(satisfied, specLabel)
			continue
		}
		var candidates []string
		for _, artifact := range contract.Artifacts {
			artifact = strings.TrimSpace(artifact)
			if artifact == "" {
				continue
			}
			if abs, ok := resolveRepoPath(artifact); ok {
				candidates = append(candidates, abs)
			}
		}
		if len(candidates) == 0 {
			satisfied = append(satisfied, specLabel)
			continue
		}
		found := false
		missing := []string{}
		for _, abs := range candidates {
			if _, err := os.Stat(abs); err == nil {
				found = true
				verified = append(verified, repoRelativePath(abs))
				continue
			}
			missing = append(missing, repoRelativePath(abs))
		}
		if found {
			satisfied = append(satisfied, specLabel)
		} else {
			failures = append(failures, fmt.Sprintf("%s missing artifacts: %s", spec, strings.Join(missing, ", ")))
		}
	}
	if len(failures) > 0 {
		return false, strings.Join(failures, "; "), satisfied, verified
	}
	return true, "", satisfied, verified
}
