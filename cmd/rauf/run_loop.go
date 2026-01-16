package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
)

type iterationResult struct {
	VerifyStatus string
	VerifyOutput string
	Stalled      bool
	HeadBefore   string
	HeadAfter    string
}

const completionSentinel = "RAUF_COMPLETE"

func runStrategy(cfg modeConfig, fileCfg runtimeConfig, runner runtimeExec, state raufState, gitAvailable bool, branch, planPath, defaultModel string, yoloEnabled bool, harness, harnessArgs string, noPush bool, logDir string, retryEnabled bool, retryMaxAttempts int, retryBackoffBase, retryBackoffMax time.Duration, retryJitter bool, retryMatch []string) {
	lastResult := iterationResult{}
	for _, step := range fileCfg.Strategy {
		if !shouldRunStep(step, lastResult) {
			continue
		}
		modeCfg := cfg
		modeCfg.mode = step.Mode
		modeCfg.promptFile = promptForMode(step.Mode)
		maxIterations := step.Iterations
		if maxIterations <= 0 {
			maxIterations = 1
		}
		modeCfg.maxIterations = 1
		model := defaultModel
		if step.Model != "" {
			model = step.Model
		}
		for i := 0; i < maxIterations; i++ {
			result := runMode(modeCfg, fileCfg, runner, state, gitAvailable, branch, planPath, model, yoloEnabled, harness, harnessArgs, noPush, logDir, retryEnabled, retryMaxAttempts, retryBackoffBase, retryBackoffMax, retryJitter, retryMatch)
			lastResult = result
			if !shouldContinueUntil(step, result) {
				break
			}
		}
	}
}

func runMode(cfg modeConfig, fileCfg runtimeConfig, runner runtimeExec, state raufState, gitAvailable bool, branch, planPath, model string, yoloEnabled bool, harness, harnessArgs string, noPush bool, logDir string, retryEnabled bool, retryMaxAttempts int, retryBackoffBase, retryBackoffMax time.Duration, retryJitter bool, retryMatch []string) iterationResult {
	iteration := 0
	noProgress := 0
	maxNoProgress := fileCfg.NoProgressIters
	if maxNoProgress <= 0 {
		maxNoProgress = 2
	}
	logDirName := strings.TrimSpace(logDir)
	if logDirName == "" {
		logDirName = "logs"
	}
	excludeDirs := []string{".git", ".rauf", logDirName}

	lastResult := iterationResult{}

	for {
		if cfg.maxIterations > 0 && iteration >= cfg.maxIterations {
			fmt.Printf("Reached max iterations: %d\n", cfg.maxIterations)
			break
		}
		iterNum := iteration + 1

		if cfg.mode == "build" {
			if hasPlanFile(planPath) && !hasUncheckedTasks(planPath) && state.LastVerificationStatus != "fail" {
				fmt.Println("No unchecked tasks found. Exiting.")
				break
			}
		}

		headBefore := ""
		if gitAvailable {
			var err error
			headBefore, err = gitOutput("rev-parse", "HEAD")
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error: unable to read git HEAD")
				os.Exit(1)
			}
		}

		planHashBefore := ""
		if hasPlanFile(planPath) {
			planHashBefore = fileHash(planPath)
		}

		var task planTask
		var verifyCmds []string
		verifyPolicy := ""
		needVerifyInstruction := ""
		missingVerify := false
		lintPolicy := ""
			if cfg.mode == "build" {
				if active, ok, err := readActiveTask(planPath); err == nil && ok {
					task = active
					verifyCmds = append([]string{}, active.VerifyCmds...)
					lintPolicy = normalizePlanLintPolicy(fileCfg)
					if lintPolicy != "off" {
						issues := lintPlanTask(task)
						if issues.MultipleVerify || issues.MultipleOutcome {
							var warnings []string
							if issues.MultipleVerify {
								warnings = append(warnings, "multiple Verify commands")
							}
							if issues.MultipleOutcome {
								warnings = append(warnings, "multiple Outcome lines")
							}
							fmt.Fprintf(os.Stderr, "Plan lint: %s\n", strings.Join(warnings, "; "))
							if lintPolicy == "fail" {
								os.Exit(1)
							}
						}
					}
				} else if err != nil {
					fmt.Fprintf(os.Stderr, "Plan lint: unable to parse active task: %v\n", err)
				}
			verifyPolicy = normalizeVerifyMissingPolicy(fileCfg)
			if len(verifyCmds) == 0 && (verifyPolicy == "fallback") {
				verifyCmds = readAgentsVerifyFallback("AGENTS.md")
				if len(verifyCmds) > 0 {
					fmt.Println("Using AGENTS.md verify fallback (explicitly enabled).")
				}
			}
			if len(verifyCmds) == 0 {
				if verifyPolicy == "agent_enforced" {
					missingVerify = true
					missingReason := "missing"
					if task.VerifyPlaceholder {
						missingReason = "placeholder (Verify: TBD)"
					}
					needVerifyInstruction = fmt.Sprintf("This task has no valid Verify command (%s). Your only job is to update the plan with a correct Verify command.", missingReason)
				} else {
					missingReason := "missing"
					if task.VerifyPlaceholder {
						missingReason = "placeholder (Verify: TBD)"
					}
					fmt.Fprintf(os.Stderr, "Error: verification command %s. Update the plan before continuing.\n", missingReason)
					os.Exit(1)
				}
			}
		}

		fingerprintBefore := ""
		fingerprintBeforePlanExcluded := ""
		if !gitAvailable {
			fingerprintBefore = workspaceFingerprint(".", excludeDirs, nil)
			if missingVerify && planPath != "" {
				fingerprintBeforePlanExcluded = workspaceFingerprint(".", excludeDirs, []string{planPath})
			}
		}

		contextPack := ""
		if cfg.mode == "build" {
			contextPack = buildContextPack(planPath, task, verifyCmds, state, gitAvailable, needVerifyInstruction)
		}

		repoMap := ""
		specIndex := ""
		planSummary := ""
		if cfg.mode == "architect" {
			repoMap = buildRepoMap(gitAvailable)
		}
		if cfg.mode == "plan" {
			specIndex = buildSpecIndex()
			repoMap = buildRepoMap(gitAvailable)
		}
		if cfg.mode == "build" {
			planSummary = buildPlanSummary(planPath, task)
		}
		capabilityMap := readAgentsCapabilityMap("AGENTS.md", maxCapabilityBytes)
		contextFile := readContextFile(".rauf/context.md", maxContextBytes)

		promptContent, promptHash, err := buildPromptContent(cfg.promptFile, promptData{
			Mode:                    cfg.mode,
			PlanPath:                planPath,
			ActiveTask:              task.TitleLine,
			VerifyCommand:           formatVerifyCommands(verifyCmds),
			CapabilityMap:           capabilityMap,
			ContextFile:             contextFile,
			SpecContext:             "",
			RelevantFiles:           "",
			RepoMap:                 repoMap,
			SpecIndex:               specIndex,
			PlanSummary:             planSummary,
			PriorVerification:       state.LastVerificationOutput,
			PriorVerificationCmd:    state.LastVerificationCommand,
			PriorVerificationStatus: state.LastVerificationStatus,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if contextPack != "" {
			promptContent = contextPack + "\n\n" + promptContent
		}

		logFile, logPath, err := openLogFile(cfg.mode, logDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("Logs:   %s\n", logPath)

		writeLogEntry(logFile, logEntry{
			Type:       "iteration_start",
			Mode:       cfg.mode,
			Model:      model,
			Iteration:  iterNum,
			VerifyCmd:  formatVerifyCommands(verifyCmds),
			PlanHash:   planHashBefore,
			PromptHash: promptHash,
			Branch:     branch,
		})

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		retryCfg := retryConfig{
			Enabled:     retryEnabled,
			MaxAttempts: retryMaxAttempts,
			BackoffBase: retryBackoffBase,
			BackoffMax:  retryBackoffMax,
			Jitter:      retryJitter,
			Match:       retryMatch,
		}

		output, err := runHarness(ctx, promptContent, harness, harnessArgs, model, yoloEnabled, logFile, retryCfg, runner)
		if err != nil {
			stop()
			if closeErr := logFile.Close(); closeErr != nil {
				fmt.Fprintln(os.Stderr, closeErr)
			}
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "Interrupted. Exiting.")
				os.Exit(130)
			}
			fmt.Fprintln(os.Stderr, "Harness run failed:", err)
			os.Exit(1)
		}

		completionSignal := ""
		if hasCompletionSentinel(output) {
			completionSignal = completionSentinel
		}

		prevVerifyStatus := state.LastVerificationStatus
		prevVerifyHash := state.LastVerificationHash
		if cfg.mode == "architect" {
			if updated, ok := runArchitectQuestions(ctx, runner, &promptContent, output, harness, harnessArgs, model, yoloEnabled, logFile, retryCfg); ok {
				output = updated
			}
		}

		verifyStatus := "skipped"
		verifyOutput := ""
		if cfg.mode == "build" && len(verifyCmds) > 0 {
			verifyOutput, err = runVerification(ctx, runner, verifyCmds, logFile)
			if err != nil {
				verifyStatus = "fail"
			} else {
				verifyStatus = "pass"
			}
			verifyOutput = normalizeVerifyOutput(verifyOutput)
			if verifyStatus == "fail" {
				state.LastVerificationOutput = verifyOutput
				state.LastVerificationCommand = formatVerifyCommands(verifyCmds)
				state.LastVerificationStatus = verifyStatus
				state.LastVerificationHash = fileHashFromString(verifyOutput)
			} else {
				state.LastVerificationOutput = ""
				state.LastVerificationCommand = formatVerifyCommands(verifyCmds)
				state.LastVerificationStatus = verifyStatus
				state.LastVerificationHash = ""
			}
			_ = saveState(state)
		}
		stop()

		headAfter := headBefore
		if gitAvailable {
			var err error
			headAfter, err = gitOutput("rev-parse", "HEAD")
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error: unable to read git HEAD")
				os.Exit(1)
			}
		}

		if cfg.mode == "build" && gitAvailable && verifyStatus == "fail" {
			headAfter = applyVerifyFailPolicy(fileCfg, headBefore, headAfter)
		}

		planHashAfter := planHashBefore
		if hasPlanFile(planPath) {
			planHashAfter = fileHash(planPath)
		}

		guardrailOk := true
		guardrailReason := ""
		worktreeChanged := false
		if cfg.mode == "build" {
			if gitAvailable {
				worktreeChanged = headAfter != headBefore || !isCleanWorkingTree() || planHashAfter != planHashBefore
				guardrailOk, guardrailReason = enforceGuardrails(fileCfg, headBefore, headAfter)
				if guardrailOk {
					if missingVerify {
						guardrailOk, guardrailReason = enforceMissingVerifyGuardrail(planPath, headBefore, headAfter, planHashBefore != planHashAfter)
					} else {
						guardrailOk, guardrailReason = enforceVerificationGuardrails(fileCfg, verifyStatus, planHashBefore != planHashAfter, worktreeChanged)
					}
				}
			} else if missingVerify {
				fingerprintAfterPlanExcluded := workspaceFingerprint(".", excludeDirs, []string{planPath})
				guardrailOk, guardrailReason = enforceMissingVerifyNoGit(planHashBefore != planHashAfter, fingerprintBeforePlanExcluded, fingerprintAfterPlanExcluded)
			}
		}

		pushAllowed := verifyStatus != "fail" && guardrailOk
		if gitAvailable && !noPush && pushAllowed {
			if headAfter != headBefore {
				if err := gitPush(branch); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			} else {
				fmt.Println("No new commit to push. Skipping git push.")
			}
		} else if !gitAvailable {
			fmt.Println("Git unavailable; skipping push.")
		} else if !pushAllowed {
			fmt.Println("Skipping git push due to verification/guardrail failure.")
		} else {
			fmt.Println("No-push enabled; skipping git push.")
		}

		stalled := false
		if gitAvailable {
			stalled = isCleanWorkingTree() && headAfter == headBefore && planHashAfter == planHashBefore
		} else {
			fingerprintAfter := workspaceFingerprint(".", excludeDirs, nil)
			stalled = fingerprintAfter == fingerprintBefore && planHashAfter == planHashBefore
		}

		progress := headAfter != headBefore || planHashAfter != planHashBefore
		if verifyStatus != "skipped" && (verifyStatus != prevVerifyStatus || state.LastVerificationHash != prevVerifyHash) {
			progress = true
		}
		exitReason := ""
		if completionSignal != "" && (cfg.mode != "build" || (!missingVerify && verifyStatus != "fail")) {
			exitReason = "agent_complete"
		}
		if !progress {
			noProgress++
			if noProgress >= maxNoProgress {
				if exitReason == "" {
					exitReason = "no_progress"
				}
			}
		} else {
			noProgress = 0
		}

		if cfg.mode == "build" {
			if hasPlanFile(planPath) && !hasUncheckedTasks(planPath) && verifyStatus != "fail" {
				if exitReason == "" {
					exitReason = "no_unchecked_tasks"
				}
			}
		}

		if stalled && progress == false {
			fmt.Println("No changes detected in iteration.")
		}

		writeLogEntry(logFile, logEntry{
			Type:             "iteration_end",
			Mode:             cfg.mode,
			Iteration:        iterNum,
			VerifyCmd:        formatVerifyCommands(verifyCmds),
			VerifyStatus:     verifyStatus,
			VerifyOutput:     verifyOutput,
			PlanHash:         planHashAfter,
			PromptHash:       promptHash,
			Branch:           branch,
			HeadBefore:       headBefore,
			HeadAfter:        headAfter,
			Guardrail:        guardrailReason,
			ExitReason:       exitReason,
			CompletionSignal: completionSignal,
		})

		if closeErr := logFile.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, closeErr)
		}

		if exitReason != "" {
			switch exitReason {
			case "no_progress":
				fmt.Printf("No progress after %d iterations. Exiting.\n", maxNoProgress)
			case "no_unchecked_tasks":
				fmt.Println("No unchecked tasks remaining. Exiting.")
			case "agent_complete":
				fmt.Println("Agent requested completion. Exiting.")
			}
			break
		}

		iteration++
		lastResult = iterationResult{
			VerifyStatus: verifyStatus,
			VerifyOutput: verifyOutput,
			Stalled:      stalled,
			HeadBefore:   headBefore,
			HeadAfter:    headAfter,
		}
		fmt.Printf("\n\n======================== LOOP %d ========================\n\n", iteration)
	}

	return lastResult
}

func promptForMode(mode string) string {
	switch strings.ToLower(mode) {
	case "architect":
		return "PROMPT_architect.md"
	case "plan":
		return "PROMPT_plan.md"
	default:
		return "PROMPT_build.md"
	}
}

func hasCompletionSentinel(output string) bool {
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
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
		if trimmed == completionSentinel {
			return true
		}
	}
	return false
}

func runVerification(ctx context.Context, runner runtimeExec, cmds []string, logFile *os.File) (string, error) {
	var combined strings.Builder
	for _, cmd := range cmds {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		fmt.Printf("Running verification: %s\n", cmd)
		output, err := runner.runShell(ctx, cmd, io.MultiWriter(os.Stdout, logFile), io.MultiWriter(os.Stderr, logFile))
		if output != "" {
			combined.WriteString("## Command: ")
			combined.WriteString(cmd)
			combined.WriteString("\n")
			combined.WriteString(output)
			combined.WriteString("\n")
		}
		if err != nil {
			return combined.String(), err
		}
	}
	return combined.String(), nil
}

func normalizeVerifyMissingPolicy(cfg runtimeConfig) string {
	policy := strings.ToLower(strings.TrimSpace(cfg.VerifyMissingPolicy))
	if policy == "" {
		if cfg.AllowVerifyFallback {
			return "fallback"
		}
		return "strict"
	}
	switch policy {
	case "strict", "agent_enforced", "fallback":
		return policy
	default:
		return "strict"
	}
}

func normalizePlanLintPolicy(cfg runtimeConfig) string {
	policy := strings.ToLower(strings.TrimSpace(cfg.PlanLintPolicy))
	if policy == "" {
		return "warn"
	}
	switch policy {
	case "warn", "fail", "off":
		return policy
	default:
		return "warn"
	}
}

func formatVerifyCommands(cmds []string) string {
	trimmed := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		trimmed = append(trimmed, cmd)
	}
	return strings.Join(trimmed, " && ")
}

func applyVerifyFailPolicy(cfg runtimeConfig, headBefore, headAfter string) string {
	policy := strings.ToLower(strings.TrimSpace(cfg.OnVerifyFail))
	if policy == "" {
		policy = "soft_reset"
	}
	if headBefore == "" || headAfter == "" || headBefore == headAfter {
		return headAfter
	}

	switch policy {
	case "soft_reset":
		if err := gitQuiet("reset", "--soft", headBefore); err != nil {
			fmt.Fprintln(os.Stderr, "Verify-fail soft reset failed:", err)
			return headAfter
		}
		fmt.Println("Verification failed; soft reset applied to keep changes staged.")
		return headBefore
	case "hard_reset":
		if err := gitQuiet("reset", "--hard", headBefore); err != nil {
			fmt.Fprintln(os.Stderr, "Verify-fail hard reset failed:", err)
			return headAfter
		}
		fmt.Println("Verification failed; hard reset applied (discarded working changes).")
		return headBefore
	case "wip_branch":
		branchName := fmt.Sprintf("wip/verify-fail-%s", time.Now().Format("20060102-150405"))
		name := branchName
		for i := 0; i < 10; i++ {
			exists, err := gitBranchExists(name)
			if err == nil && !exists {
				break
			}
			name = fmt.Sprintf("%s-%d", branchName, i+1)
		}
		if err := gitQuiet("branch", name, headAfter); err != nil {
			fmt.Fprintln(os.Stderr, "Verify-fail branch creation failed:", err)
			return headAfter
		}
		if err := gitQuiet("reset", "--soft", headBefore); err != nil {
			fmt.Fprintln(os.Stderr, "Verify-fail soft reset failed:", err)
			return headAfter
		}
		fmt.Printf("Verification failed; moved commit to %s and soft reset.\n", name)
		return headBefore
	case "keep_commit", "no_push_only":
		return headAfter
	default:
		return headAfter
	}
}
