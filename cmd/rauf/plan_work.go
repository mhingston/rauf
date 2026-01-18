package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runPlanWork(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("plan-work requires a name")
	}
	originalBranch, err := gitOutput("branch", "--show-current")
	if err != nil || originalBranch == "" {
		return fmt.Errorf("git is required for plan-work")
	}

	slug := slugify(name)
	if slug == "" {
		return fmt.Errorf("unable to derive branch name")
	}

	newBranch := fmt.Sprintf("rauf/%s", slug)
	branchSwitched := false
	if originalBranch != newBranch {
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
		branchSwitched = true
	}

	// Helper to rollback branch switch on failure
	rollback := func() {
		if branchSwitched {
			_ = gitCheckout(originalBranch)
		}
	}

	planDir := ".rauf"
	planPath := filepath.Join(planDir, "IMPLEMENTATION_PLAN.md")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		rollback()
		return err
	}
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		if err := os.WriteFile(planPath, []byte(planTemplate), 0o644); err != nil {
			rollback()
			return err
		}
	}

	if err := gitConfigSet(fmt.Sprintf("branch.%s.raufScoped", newBranch), "true"); err != nil {
		rollback()
		return fmt.Errorf("failed to set branch config: %w", err)
	}
	if err := gitConfigSet(fmt.Sprintf("branch.%s.raufPlanPath", newBranch), planPath); err != nil {
		// Clean up the first config setting before rollback
		if unsetErr := gitConfigUnset(fmt.Sprintf("branch.%s.raufScoped", newBranch)); unsetErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clean up git config: %v\n", unsetErr)
		}
		rollback()
		return fmt.Errorf("failed to set branch config: %w", err)
	}

	fmt.Printf("Switched to %s and prepared %s\n", newBranch, planPath)
	return nil
}

func gitBranchExists(name string) (bool, error) {
	_, err := gitOutput("show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err == nil {
		return true, nil
	}
	// show-ref exits with code 1 when ref doesn't exist, which is expected
	// For other errors (corrupted repo, permission issues), we should propagate
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
	}
	return false, err
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

func gitConfigUnset(key string) error {
	_, err := gitOutput("config", "--unset", key)
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
