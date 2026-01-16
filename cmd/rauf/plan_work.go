package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runPlanWork(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("plan-work requires a name")
	}
	branch, err := gitOutput("branch", "--show-current")
	if err != nil || branch == "" {
		return fmt.Errorf("git is required for plan-work")
	}

	slug := slugify(name)
	if slug == "" {
		return fmt.Errorf("unable to derive branch name")
	}

	newBranch := fmt.Sprintf("rauf/%s", slug)
	if branch != newBranch {
		exists, err := gitBranchExists(newBranch)
		if err != nil {
			return err
		}
		if exists {
			if err := gitCheckout(newBranch); err != nil {
				return err
			}
		} else {
			if err := gitCheckoutCreate(newBranch); err != nil {
				return err
			}
		}
	}

	planDir := ".rauf"
	planPath := filepath.Join(planDir, "IMPLEMENTATION_PLAN.md")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		if err := os.WriteFile(planPath, []byte(planTemplate), 0o644); err != nil {
			return err
		}
	}

	if err := gitConfigSet(fmt.Sprintf("branch.%s.raufScoped", newBranch), "true"); err != nil {
		return err
	}
	if err := gitConfigSet(fmt.Sprintf("branch.%s.raufPlanPath", newBranch), planPath); err != nil {
		return err
	}

	fmt.Printf("Switched to %s and prepared %s\n", newBranch, planPath)
	return nil
}

func gitBranchExists(name string) (bool, error) {
	_, err := gitOutput("show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err == nil {
		return true, nil
	}
	return false, nil
}

func gitCheckout(branch string) error {
	_, err := gitOutput("checkout", branch)
	return err
}

func gitCheckoutCreate(branch string) error {
	_, err := gitOutput("checkout", "-b", branch)
	return err
}

func gitConfigSet(key, value string) error {
	_, err := gitOutput("config", key, value)
	return err
}

func resolvePlanPath(branch string, gitAvailable bool, fallback string) string {
	if !gitAvailable || branch == "" {
		return fallback
	}
	path, err := gitOutput("config", "--get", fmt.Sprintf("branch.%s.raufPlanPath", branch))
	if err == nil && path != "" {
		return path
	}
	return fallback
}
