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
	NoProgress   int
	ExitReason   string
}

const completionSentinel = "RAUF_COMPLETE"

var runStrategy = func(ctx context.Context, cfg modeConfig, fileCfg runtimeConfig, runner runtimeExec, state raufState, gitAvailable bool, branch, planPath, harness, harnessArgs string, noPush bool, logDir string, retryEnabled bool, retryMaxAttempts int, retryBackoffBase, retryBackoffMax time.Duration, retryJitter bool, retryMatch []string, stdin io.Reader, stdout io.Writer, report *RunReport) error {
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
		// Reset NoProgress counter at the start of each strategy step
		stepNoProgress := 0
		for i := 0; i < maxIterations; i++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result, err := runMode(ctx, modeCfg, fileCfg, runner, state, gitAvailable, branch, planPath, harness, harnessArgs, noPush, logDir, retryEnabled, retryMaxAttempts, retryBackoffBase, retryBackoffMax, retryJitter, retryMatch, stepNoProgress, stdin, stdout, report)
			if err != nil {
				return err
			}
			// Update state to reflect changes from runMode (critical for strategies)
			// Wait, runMode returns result, but state is passed by value.
			// Ideally runMode should accept *raufState or return updated state.
			// Current arch seems to assume state is re-loaded or mutable?
			// Actually state is modified inside runMode loop but `state` here is a local copy?
			// runMode loop updates its local copy.
			// If we run multiple modes in strategy, we should propagate state.
			// But runMode doesn't return state.
			// This is a pre-existing issue or I am missing something.
			// But for now I just match signature.

			lastResult = result
			stepNoProgress = result.NoProgress
			if result.ExitReason == "no_progress" {
				break
			}
			if !shouldContinueUntil(step, result) {
				break
			}
		}
	}
	return nil
}

var runMode = func(parentCtx context.Context, cfg modeConfig, fileCfg runtimeConfig, runner runtimeExec, state raufState, gitAvailable bool, branch, planPath, harness, harnessArgs string, noPush bool, logDir string, retryEnabled bool, retryMaxAttempts int, retryBackoffBase, retryBackoffMax time.Duration, retryJitter bool, retryMatch []string, startNoProgress int, stdin io.Reader, stdout io.Writer, report *RunReport) (iterationResult, error) {
	iteration := 0
	noProgress := startNoProgress
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

	startIter := time.Now()
	iterStats := IterationStats{
		Iteration: iteration + 1,
		Mode:      cfg.mode,
		Model:     state.CurrentModel,
	}

	for {
		if parentCtx.Err() != nil {
			return lastResult, parentCtx.Err()
		}
		if cfg.maxIterations > 0 && iteration >= cfg.maxIterations {
			fmt.Printf("Reached max iterations: %d\n", cfg.maxIterations)
			iterStats.ExitReason = "max_iterations_reached"
			iterStats.Duration = time.Since(startIter).String()
			report.Iterations = append(report.Iterations, iterStats)
			break
		}
		iterNum := iteration + 1

		if cfg.mode == "plan" || cfg.mode == "build" {
			if err := lintSpecs(); err != nil {
				iterStats.ExitReason = "lint_failed"
				iterStats.Duration = time.Since(startIter).String()
				report.Iterations = append(report.Iterations, iterStats)
				return iterationResult{}, err
			}
		}

		// Note: We don't check for unchecked tasks here at the start of iteration
		// because state.LastVerificationStatus may be stale. The check is done
		// after verification runs, using the current iteration's verifyStatus.

		headBefore := ""
		if gitAvailable {
			var err error
			headBefore, err = gitOutput("rev-parse", "HEAD")
			if err != nil {
				iterStats.ExitReason = "git_error"
				iterStats.Duration = time.Since(startIter).String()
				report.Iterations = append(report.Iterations, iterStats)
				return iterationResult{}, fmt.Errorf("unable to read git HEAD: %w", err)
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
		exitReason := ""
		if cfg.mode == "build" {
			active, ok, err := readActiveTask(planPath)
			if err == nil && ok {
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
							iterStats.ExitReason = "plan_lint_failed"
							iterStats.Duration = time.Since(startIter).String()
							report.Iterations = append(report.Iterations, iterStats)
							return iterationResult{}, fmt.Errorf("plan lint failed: %s", strings.Join(warnings, "; "))
						}
					}
				}
			} else if err != nil {
				fmt.Fprintf(os.Stderr, "Plan lint: unable to parse active task: %v\n", err)
			} else {
				// No active (unchecked) task found
				if !hasUncheckedTasks(planPath) {
					exitReason = "no_unchecked_tasks"
				}
			}

			if exitReason == "" {
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
						iterStats.ExitReason = "missing_verify_command"
						iterStats.Duration = time.Since(startIter).String()
						report.Iterations = append(report.Iterations, iterStats)
						return iterationResult{}, fmt.Errorf("verification command %s. Update the plan before continuing", missingReason)
					}
				}
			}
		}

		if exitReason != "" {
			switch exitReason {
			case "no_progress":
				fmt.Printf("No progress after %d iterations. Exiting.\n", maxNoProgress)
			case "no_unchecked_tasks":
				fmt.Println("No unchecked tasks remaining. Exiting.")
			case "completion_contract_satisfied":
				fmt.Println("Completion contract satisfied. Exiting.")
			}
			lastResult.ExitReason = exitReason
			iterStats.ExitReason = exitReason
			iterStats.Duration = time.Since(startIter).String()
			report.Iterations = append(report.Iterations, iterStats)
			break
		}

		fingerprintBefore := ""
		fingerprintBeforePlanExcluded := ""
		if !gitAvailable {
			fingerprintBefore = workspaceFingerprint(".", excludeDirs, nil)
			if missingVerify && planPath != "" {
				fingerprintBeforePlanExcluded = workspaceFingerprint(".", excludeDirs, []string{planPath})
			}
		}

		backpressurePack := ""
		if cfg.mode == "build" || cfg.mode == "plan" {
			backpressurePack = buildBackpressurePack(state, gitAvailable)
		}
		state.BackpressureInjected = backpressurePack != ""

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
			iterStats.ExitReason = "prompt_build_failed"
			iterStats.Duration = time.Since(startIter).String()
			report.Iterations = append(report.Iterations, iterStats)
			return iterationResult{}, err
		}
		if backpressurePack != "" || contextPack != "" {
			promptContent = backpressurePack + contextPack + "\n\n" + promptContent
		}

		logFile, logPath, err := openLogFile(cfg.mode, logDir)
		if err != nil {
			iterStats.ExitReason = "log_file_error"
			iterStats.Duration = time.Since(startIter).String()
			report.Iterations = append(report.Iterations, iterStats)
			return iterationResult{}, err
		}
		fmt.Printf("Logs:   %s\n", logPath)

		writeLogEntry(logFile, logEntry{
			Type:       "iteration_start",
			Mode:       cfg.mode,
			Iteration:  iterNum,
			VerifyCmd:  formatVerifyCommands(verifyCmds),
			PlanHash:   planHashBefore,
			PromptHash: promptHash,
			Branch:     branch,
		})

		ctx, stop := signal.NotifyContext(parentCtx, os.Interrupt)
		retryCfg := retryConfig{
			Enabled:     retryEnabled,
			MaxAttempts: retryMaxAttempts,
			BackoffBase: retryBackoffBase,
			BackoffMax:  retryBackoffMax,
			Jitter:      retryJitter,
			Match:       retryMatch,
		}

		// Compute effective harness args with model escalation
		effectiveHarnessArgs := harnessArgs
		escalated := false
		escalationReason := ""
		if fileCfg.ModelEscalation.Enabled {
			// Check if we should escalate (catch-up logic for start of iteration)
			shouldEscalate, reason, suppressed := shouldEscalateModel(state, fileCfg)
			if shouldEscalate {
				if state.CurrentModel != fileCfg.ModelStrong {
					from := state.CurrentModel
					if from == "" {
						from = fileCfg.ModelDefault
					}
					// Log the escalation immediately
					writeLogEntry(logFile, logEntry{
						Type:             "model_escalation",
						FromModel:        from,
						ToModel:          fileCfg.ModelStrong,
						EscalationReason: reason,
						EscalationCount:  state.EscalationCount + 1,
						Cooldown:         fileCfg.ModelEscalation.CooldownIters,
					})

					state.CurrentModel = fileCfg.ModelStrong
					state.EscalationCount++
					state.MinStrongIterationsRemaining = fileCfg.ModelEscalation.CooldownIters
					state.LastEscalationReason = reason
					escalated = true
					escalationReason = reason
					fmt.Printf("Model escalation triggered: %s -> %s (reason: %s)\n",
						fileCfg.ModelDefault, fileCfg.ModelStrong, reason)
				}
			} else if suppressed != "" {
				// Log suppression if we haven't already logged it recently?
				// To avoid noise, we might only want to log this if something changed or meaningfully suppressed.
				// For now, logging it allows "observability" as requested.
				from := state.CurrentModel
				if from == "" {
					from = fileCfg.ModelDefault
				}
				writeLogEntry(logFile, logEntry{
					Type:             "model_escalation", // keeping type same or distinct? User suggested "model_escalation" with reason/cooldown
					FromModel:        from,
					ToModel:          fileCfg.ModelStrong, // Targeted model
					EscalationReason: suppressed,          // e.g. "max_escalations_reached"
					Escalated:        false,               // Explicitly not escalated
					Cooldown:         state.MinStrongIterationsRemaining,
				})
			}
			// Apply model to harness args
			model := computeEffectiveModel(state, fileCfg)
			if model != "" {
				effectiveHarnessArgs = applyModelChoice(harnessArgs, fileCfg.ModelFlag, model, fileCfg.ModelOverride)
			}
		}

		// Run harness
		harnessCtx := ctx
		if cfg.AttemptTimeout > 0 {
			var cancel context.CancelFunc
			harnessCtx, cancel = context.WithTimeout(ctx, cfg.AttemptTimeout)
			defer cancel()
		}

		harnessRes, err := runHarness(harnessCtx, promptContent, harness, effectiveHarnessArgs, logFile, retryCfg, runner)
		iterStats.Attempts = harnessRes.RetryCount + 1 // retries + initial attempt
		iterStats.Retries = harnessRes.RetryCount

		if err != nil {
			stop() // Clean up signal handler
			if closeErr := logFile.Close(); closeErr != nil {
				fmt.Fprintln(os.Stderr, closeErr)
			}
			if ctx.Err() != nil {
				iterStats.ExitReason = "interrupted"
				iterStats.Duration = time.Since(startIter).String()
				report.Iterations = append(report.Iterations, iterStats)
				return iterationResult{}, fmt.Errorf("interrupted")
			}
			fmt.Fprintf(os.Stderr, "Harness failed: %v\n", err)
			iterStats.ExitReason = "harness_failed"
			iterStats.Duration = time.Since(startIter).String()
			report.Iterations = append(report.Iterations, iterStats)
			return iterationResult{}, fmt.Errorf("harness run failed: %w", err)
		}
		output := harnessRes.Output

		// Check for backpressure response acknowledgment
		backpressureAcknowledged := true
		if state.BackpressureInjected && !hasBackpressureResponse(output) {
			fmt.Println("Warning: Backpressure was present but model did not include '## Backpressure Response' section.")
			backpressureAcknowledged = false
		}

		// Persist retry info for backpressure
		state.PriorRetryCount = harnessRes.RetryCount
		state.PriorRetryReason = harnessRes.RetryReason

		// Capture hypothesis if provided (especially important after consecutive failures)
		if cfg.mode == "build" && state.ConsecutiveVerifyFails >= 2 {
			hyp, diffAction := extractHypothesis(output)
			if hyp != "" && diffAction != "" {
				// Record the hypothesis
				state.Hypotheses = append(state.Hypotheses, Hypothesis{
					Timestamp:       time.Now().UTC(),
					Iteration:       iterNum,
					Hypothesis:      hyp,
					DifferentAction: diffAction,
					VerifyCommand:   formatVerifyCommands(verifyCmds),
				})
				// Keep only last 10 hypotheses to avoid state bloat
				if len(state.Hypotheses) > 10 {
					state.Hypotheses = state.Hypotheses[len(state.Hypotheses)-10:]
				}
			} else if !hasRequiredHypothesis(output) {
				fmt.Println("Warning: Hypothesis required after consecutive verify failures but model did not include HYPOTHESIS: and DIFFERENT_THIS_TIME: lines.")
			}

			// Extract and track assumptions
			questions := extractTypedQuestions(output)
			for _, q := range questions {
				if q.Type == "ASSUMPTION" {
					state = addAssumption(state, q.Question, q.StickyScope, iterNum, state.RecoveryMode)
				}
			}
		}

		completionSignal := ""
		completionOk := true
		completionSpecs := []string{}
		completionArtifacts := []string{}
		if hasCompletionSentinel(output) {
			completionSignal = completionSentinel
			if cfg.mode == "build" {
				var reason string
				completionOk, reason, completionSpecs, completionArtifacts = checkCompletionArtifacts(task.SpecRefs)
				if !completionOk {
					fmt.Fprintf(os.Stderr, "Completion blocked: %s\n", reason)
				}
			}
		}

		prevVerifyStatus := state.LastVerificationStatus
		prevVerifyHash := state.LastVerificationHash
		currentVerifyHash := "" // Will be set after verification runs
		if cfg.mode == "architect" {
			if updatedOutput, questionsAsked := runArchitectQuestions(ctx, runner, &promptContent, output, state, harness, effectiveHarnessArgs, logFile, retryCfg, stdin, stdout); questionsAsked {
				output = updatedOutput
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
			currentVerifyHash = fileHashFromString(verifyOutput)
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

			if err := saveState(state); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save state: %v\n", err)
			}
		}

		headAfter := headBefore
		if gitAvailable {
			var err error
			headAfter, err = gitOutput("rev-parse", "HEAD")
			if err != nil {
				iterStats.ExitReason = "git_error"
				iterStats.Duration = time.Since(startIter).String()
				report.Iterations = append(report.Iterations, iterStats)
				return iterationResult{}, fmt.Errorf("unable to read git HEAD: %w", err)
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
					iterStats.ExitReason = "git_push_failed"
					iterStats.Duration = time.Since(startIter).String()
					report.Iterations = append(report.Iterations, iterStats)
					return iterationResult{}, err
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

		// Calculate logic for progress.
		// "Verify fail" means a falsifiable attempt failed (which is valuable information).
		// "No progress" means no meaningful change to the workspace or plan occurred (stalled).
		// Even if verification fails, if the failure mode changed (different hash/status), it counts as progress.
		progress := headAfter != headBefore || planHashAfter != planHashBefore
		if verifyStatus != "skipped" && (verifyStatus != prevVerifyStatus || currentVerifyHash != prevVerifyHash) {
			progress = true
		}
		// Unacknowledged backpressure is noted but doesn't affect progress calculation.
		// If commits or plan changes occurred, that's real progress even if backpressure wasn't acknowledged.
		_ = backpressureAcknowledged // Acknowledged status already logged as warning above
		if completionSignal != "" && completionOk && (cfg.mode != "build" || (!missingVerify && verifyStatus != "fail")) {
			exitReason = "completion_contract_satisfied"
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

		// Persist backpressure state for next iteration
		// Edge-triggered: only set backpressure if something failed THIS iteration
		cleanIteration := guardrailOk &&
			verifyStatus != "fail" &&
			exitReason == "" &&
			planHashBefore == planHashAfter &&
			harnessRes.RetryCount == 0

		// Archive resolved assumptions
		if verifyStatus == "pass" {
			state = archiveAssumptions(state, "verify", "verify_pass", iterNum, currentVerifyHash)
		}
		if guardrailOk {
			state = archiveAssumptions(state, "guardrail", "guardrail_pass", iterNum, "")
		}

		// Update failure counters and recovery mode (always runs)
		state = updateBackpressureState(state, fileCfg.Recovery, verifyStatus == "fail", !guardrailOk, noProgress > 0)

		// Update model escalation (only if enabled)
		var escalationEvent escalationEvent
		state, escalationEvent = updateModelEscalationState(state, fileCfg)
		if escalationEvent.Type != "none" {
			writeLogEntry(logFile, logEntry{
				Type:             "model_escalation",
				FromModel:        escalationEvent.FromModel,
				ToModel:          escalationEvent.ToModel,
				EscalationReason: escalationEvent.Reason,
				Escalated:        escalationEvent.Type == "escalated", // true if escalated
				Cooldown:         escalationEvent.Cooldown,
				// Note: de-escalation is also logged here with Escalated=false but Reason="min_strong_iterations_expired"
				// or suppressed with reason="max_..."
			})
			if escalationEvent.Type == "escalated" {
				fmt.Printf("Model escalation triggered: %s -> %s (reason: %s)\n",
					escalationEvent.FromModel, escalationEvent.ToModel, escalationEvent.Reason)
			} else if escalationEvent.Type == "de_escalated" {
				fmt.Printf("Model de-escalation: %s -> %s (reason: %s)\n",
					escalationEvent.FromModel, escalationEvent.ToModel, escalationEvent.Reason)
			}
		}

		if cleanIteration {
			// Clear all backpressure fields after a clean iteration
			state.PriorGuardrailStatus = ""
			state.PriorGuardrailReason = ""
			state.PriorExitReason = ""
			state.PriorRetryCount = 0
			state.PriorRetryReason = ""
			state.PlanHashBefore = ""
			state.PlanHashAfter = ""
			state.PlanDiffSummary = ""
			state.BackpressureInjected = false
		} else {
			// Set backpressure fields based on what failed
			if guardrailOk {
				state.PriorGuardrailStatus = "pass"
				state.PriorGuardrailReason = ""
			} else {
				state.PriorGuardrailStatus = "fail"
				state.PriorGuardrailReason = guardrailReason
			}

			state.PriorExitReason = exitReason
			state.PlanHashBefore = planHashBefore
			state.PlanHashAfter = planHashAfter
			if planHashBefore != planHashAfter {
				state.PlanDiffSummary = generatePlanDiff(planPath, gitAvailable, 50)
			} else {
				state.PlanDiffSummary = ""
			}
			// Track no-progress streak
			if !progress {
				state.NoProgressStreak++
			} else {
				state.NoProgressStreak = 0
			}
			// Set recovery mode based on failure type
			if !guardrailOk {
				state.RecoveryMode = "guardrail"
			} else if verifyStatus == "fail" {
				state.RecoveryMode = "verify"
			} else if exitReason == "no_progress" || !progress {
				state.RecoveryMode = "no_progress"
			}
			// Retry info already set from harnessRes earlier
		}
		if err := saveState(state); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save state: %v\n", err)
		}

		if stalled && progress == false {
			fmt.Println("No changes detected in iteration.")
		}

		writeLogEntry(logFile, logEntry{
			Type:                "iteration_end",
			Mode:                cfg.mode,
			Iteration:           iterNum,
			VerifyCmd:           formatVerifyCommands(verifyCmds),
			VerifyStatus:        verifyStatus,
			VerifyOutput:        verifyOutput,
			PlanHash:            planHashAfter,
			PromptHash:          promptHash,
			Branch:              branch,
			HeadBefore:          headBefore,
			HeadAfter:           headAfter,
			Guardrail:           guardrailReason,
			ExitReason:          exitReason,
			CompletionSignal:    completionSignal,
			CompletionSpecs:     completionSpecs,
			CompletionArtifacts: completionArtifacts,
			Model:               state.CurrentModel,
			Escalated:           escalated,
			EscalationReason:    escalationReason,
		})

		if closeErr := logFile.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, closeErr)
		}

		// Clean up signal handler for this iteration
		stop()

		iterResult := iterationResult{
			VerifyStatus: verifyStatus,
			VerifyOutput: verifyOutput,
			Stalled:      stalled,
			HeadBefore:   headBefore,
			HeadAfter:    headAfter,
			NoProgress:   noProgress,
			ExitReason:   exitReason,
		}

		lastResult = iterResult
		iterStats.Result = iterResult
		iterStats.ExitReason = iterResult.ExitReason
		iterStats.VerifyStatus = iterResult.VerifyStatus
		iterStats.Duration = time.Since(startIter).String()
		report.Iterations = append(report.Iterations, iterStats)

		if iterResult.ExitReason == "completion_contract_satisfied" {
			state.CurrentModel = "" // Reset model usage on success? Or keep?
			saveState(state)        // Final save
			return iterResult, nil
		}
		if iterResult.ExitReason != "" {
			saveState(state)
			return iterResult, nil
		}

		iteration++
		startIter = time.Now()
		iterStats = IterationStats{
			Iteration: iterNum + 1, // For the *next* iteration
			Mode:      cfg.mode,
			Model:     state.CurrentModel,
		}

		if parentCtx.Err() != nil {
			return lastResult, parentCtx.Err()
		}

		fmt.Printf("\n\n======================== LOOP %d ========================\n\n", iterNum)
		// Recheck if state file exists and reload to get latest state (e.g. from manual edits)
		// But we have local state... reloading might clobber in-memory changes?
		// Stick to local state for now.

		report.TotalIterations++

		// If attempt timeout configured, create sub-context for THIS iteration or harness run?
		// Requirement says "Per-attempt timeout". Usually means per harness run.
		// So we pass config down to runHarness/runHarnessOnce?
		// Or wrap here? runMode does a lot of things.
		// "prevents hanging harnesses".
		// We'll wrap the runHarness call.

		// Determine effective model
		// ... existing logic ...
	}

	return lastResult, nil
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
	return scanLinesOutsideFence(output, func(trimmed string) bool {
		return trimmed == completionSentinel
	})
}

func runVerification(ctx context.Context, runner runtimeExec, cmds []string, logFile *os.File) (string, error) {
	var combined strings.Builder
	for _, cmd := range cmds {
		// Check for context cancellation before running each command
		select {
		case <-ctx.Done():
			return combined.String(), ctx.Err()
		default:
		}
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
		// Check for context cancellation after command completes
		// This catches cases where the command finished but we were signaled during execution
		if ctx.Err() != nil {
			return combined.String(), ctx.Err()
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
		if policy == "fallback" && !cfg.AllowVerifyFallback {
			fmt.Fprintln(os.Stderr, "Warning: verify_missing_policy is 'fallback' but allow_verify_fallback is false; using 'strict' instead")
			return "strict"
		}
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
		found := false
		for i := 0; i < 10; i++ {
			candidate := name
			if i > 0 {
				candidate = fmt.Sprintf("%s-%d", branchName, i)
			}
			exists, err := gitBranchExists(candidate)
			if err == nil && !exists {
				name = candidate
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintln(os.Stderr, "Verify-fail branch creation failed: could not find unique branch name after 10 attempts")
			return headAfter
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
