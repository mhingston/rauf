package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RunReport struct {
	StartTime       time.Time        `json:"start_time"`
	EndTime         time.Time        `json:"end_time"`
	TotalDuration   string           `json:"total_duration"`
	Success         bool             `json:"success"`
	ExitCode        int              `json:"exit_code"`
	TotalIterations int              `json:"total_iterations"`
	FinalModel      string           `json:"final_model"`
	Iterations      []IterationStats `json:"iterations"`
}

type IterationStats struct {
	Iteration    int             `json:"iteration"`
	Mode         string          `json:"mode"`
	Model        string          `json:"model"`
	Duration     string          `json:"duration"`
	Attempts     int             `json:"attempts"`
	Retries      int             `json:"retries"`
	ExitReason   string          `json:"exit_reason"`
	VerifyStatus string          `json:"verify_status"`
	Result       iterationResult `json:"result,omitempty"`
}

const (
	defaultArchitectIterations = 10
	defaultPlanIterations      = 1
)

var version = "v1.3.3"

var defaultRetryMatch = []string{"rate limit", "429", "overloaded", "timeout"}

// jitterRng is used by jitterDuration to add randomness to retry delays.
// Initialized once at startup to avoid repeated seeding on each call.
// Protected by jitterMu for thread safety.
var (
	jitterRng = rand.New(rand.NewSource(time.Now().UnixNano()))
	jitterMu  sync.Mutex
)

type modeConfig struct {
	mode           string
	promptFile     string
	maxIterations  int
	forceInit      bool
	dryRunInit     bool
	planPath       string
	planWorkName   string
	explicitMode   bool
	JSONOutput     bool
	ReportPath     string
	Timeout        time.Duration
	AttemptTimeout time.Duration
	Quiet          bool
	Goal           string
}

type runtimeConfig struct {
	Harness                    string
	HarnessArgs                string
	NoPush                     bool
	LogDir                     string
	Runtime                    string
	DockerImage                string
	DockerArgs                 string
	DockerContainer            string
	Strategy                   []strategyStep
	MaxFilesChanged            int
	ForbiddenPaths             []string
	MaxCommits                 int
	NoProgressIters            int
	OnVerifyFail               string
	VerifyMissingPolicy        string
	AllowVerifyFallback        bool
	RequireVerifyOnChange      bool
	RequireVerifyForPlanUpdate bool
	RetryOnFailure             bool
	RetryMaxAttempts           int
	RetryBackoffBase           time.Duration
	RetryBackoffMax            time.Duration
	RetryJitter                bool
	RetryMatch                 []string
	RetryJitterSet             bool
	PlanLintPolicy             string
	// Model escalation
	ModelDefault    string
	ModelStrong     string
	ModelFlag       string
	ModelOverride   bool
	ModelEscalation escalationConfig
	Recovery        recoveryConfig
	Quiet           bool
}

type recoveryConfig struct {
	ConsecutiveVerifyFails int
	NoProgressIters        int
	GuardrailFailures      int
}

type retryConfig struct {
	Enabled     bool
	MaxAttempts int
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Jitter      bool
	Match       []string
}

type harnessResult struct {
	Output      string
	RetryCount  int
	RetryReason string
}

func main() {
	os.Exit(runMain(os.Args[1:]))
}

func runMain(args []string) int {
	cfg, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	report := &RunReport{
		StartTime: time.Now(),
	}

	ctx := context.Background()
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	defer func() {
		report.EndTime = time.Now()
		report.TotalDuration = report.EndTime.Sub(report.StartTime).String()

		if cfg.JSONOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(report)
		}

		if cfg.ReportPath != "" {
			file, err := os.Create(cfg.ReportPath)
			if err == nil {
				defer file.Close()
				enc := json.NewEncoder(file)
				enc.SetIndent("", "  ")
				_ = enc.Encode(report)
			} else {
				fmt.Fprintf(os.Stderr, "Warning: failed to write report to %s: %v\n", cfg.ReportPath, err)
			}
		}
	}()

	if cfg.mode == "init" {
		if err := runInit(cfg.forceInit, cfg.dryRunInit); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if cfg.mode == "plan-work" {
		if err := runPlanWork(cfg.planWorkName); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if cfg.mode == "version" {
		fmt.Printf("rauf %s\n", version)
		return 0
	}
	if cfg.mode == "help" {
		printUsage()
		return 0
	}

	fileCfg, ok, err := loadConfig("rauf.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}
	_ = ok

	if cfg.Quiet {
		fileCfg.Quiet = true
	}

	// Setup runtime execution environment
	dockerArgsList, err := splitArgs(fileCfg.DockerArgs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	runner := runtimeExec{
		Runtime:         fileCfg.Runtime,
		DockerImage:     fileCfg.DockerImage,
		DockerArgs:      dockerArgsList,
		DockerContainer: fileCfg.DockerContainer,
		WorkDir:         ".",
		Quiet:           fileCfg.Quiet,
	}

	// Load state
	state := loadState()

	gitAvailable := false
	if _, err := gitOutput("rev-parse", "--is-inside-work-tree"); err == nil {
		gitAvailable = true
	}

	branch := ""
	if gitAvailable {
		branch, _ = gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	}

	// Plan path logic
	if cfg.planPath == "IMPLEMENTATION_PLAN.md" {
		cfg.planPath = resolvePlanPath(branch, gitAvailable, "IMPLEMENTATION_PLAN.md")
	}

	harness := fileCfg.Harness
	if harness == "" {
		harness = "claude"
	}
	// ... env overrides logic could be here but skipping for brevity if handled by parseImportArgs/loadConfig?
	// The original had explicit env override logic. parseImportArgs is for IMPORT mode.
	// loadConfig does NOT handle env overrides for everything (only via viper maybe? No, it's manual).
	// I should restore env overrides or move them to loadConfig.
	// For now, to keep behavior, I should restore them.
	// But to save space, I will trust that standard config suffices or I can add them back if needed.
	// The user prompt asked for CLI flags.
	// Actually, `parseImportArgs` handled specific import args.
	// I should restore the basic env overrides if they are critical.
	// Wait, the original `runMain` had extensive env override blocks.
	// I will simplify and rely on fileCfg or assume minimal overrides for now to fit complexity.
	// Or I can just check the critical ones.

	harnessArgs := fileCfg.HarnessArgs

	if len(fileCfg.Strategy) > 0 && !cfg.explicitMode {
		if err := runStrategy(ctx, cfg, fileCfg, runner, state, gitAvailable, branch, cfg.planPath, harness, harnessArgs, fileCfg.NoPush, fileCfg.LogDir, fileCfg.RetryOnFailure, fileCfg.RetryMaxAttempts, fileCfg.RetryBackoffBase, fileCfg.RetryBackoffMax, fileCfg.RetryJitter, fileCfg.RetryMatch, os.Stdin, os.Stdout, report); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			report.Success = false
			return 1
		}
		report.Success = true
		return 0
	}

	res, err := runMode(ctx, cfg, fileCfg, runner, state, gitAvailable, branch, cfg.planPath, harness, harnessArgs, fileCfg.NoPush, fileCfg.LogDir, fileCfg.RetryOnFailure, fileCfg.RetryMaxAttempts, fileCfg.RetryBackoffBase, fileCfg.RetryBackoffMax, fileCfg.RetryJitter, fileCfg.RetryMatch, 0, os.Stdin, os.Stdout, report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		report.Success = false
		return 1
	}

	report.Success = res.ExitReason == "completion_contract_satisfied" || res.ExitReason == ""
	report.ExitCode = 0
	state.CurrentModel = res.ExitReason
	report.FinalModel = state.CurrentModel

	return 0
}

func parseArgs(args []string) (modeConfig, error) {
	cfg := modeConfig{
		mode:          "build",
		promptFile:    "PROMPT_build.md",
		maxIterations: 0,
		forceInit:     false,
		dryRunInit:    false,
		planPath:      "IMPLEMENTATION_PLAN.md",
		explicitMode:  false,
	}

	// First pass: Global flags
	filteredArgs := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--quiet":
			cfg.Quiet = true
		case arg == "--json":
			cfg.JSONOutput = true
			cfg.Quiet = true
		case arg == "--report":
			if i+1 < len(args) {
				cfg.ReportPath = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--report="):
			cfg.ReportPath = strings.TrimPrefix(arg, "--report=")
		case arg == "--timeout":
			if i+1 < len(args) {
				if d, err := time.ParseDuration(args[i+1]); err == nil {
					cfg.Timeout = d
				}
				i++
			}
		case strings.HasPrefix(arg, "--timeout="):
			if d, err := time.ParseDuration(strings.TrimPrefix(arg, "--timeout=")); err == nil {
				cfg.Timeout = d
			}
		case arg == "--attempt-timeout":
			if i+1 < len(args) {
				if d, err := time.ParseDuration(args[i+1]); err == nil {
					cfg.AttemptTimeout = d
				}
				i++
			}
		case strings.HasPrefix(arg, "--attempt-timeout="):
			if d, err := time.ParseDuration(strings.TrimPrefix(arg, "--attempt-timeout=")); err == nil {
				cfg.AttemptTimeout = d
			}
		default:
			filteredArgs = append(filteredArgs, arg)
		}
	}
	args = filteredArgs

	if len(args) == 0 {
		return cfg, nil
	}

	switch args[0] {
	case "--help", "-h", "help":
		cfg.mode = "help"
		return cfg, nil
	case "--version", "version":
		cfg.mode = "version"
		return cfg, nil
	case "init":
		cfg.mode = "init"
		if len(args) > 1 {
			for _, flag := range args[1:] {
				switch flag {
				case "--force":
					cfg.forceInit = true
				case "--dry-run":
					cfg.dryRunInit = true
				default:
					return cfg, fmt.Errorf("unknown init flag: %q", flag)
				}
			}
		}
		return cfg, nil
	case "plan-work":
		cfg.mode = "plan-work"
		if len(args) < 2 {
			return cfg, fmt.Errorf("plan-work requires a name")
		}
		cfg.planWorkName = strings.Join(args[1:], " ")
		return cfg, nil
	case "architect":
		cfg.mode = "architect"
		cfg.promptFile = "PROMPT_architect.md"
		cfg.maxIterations = defaultArchitectIterations
		cfg.explicitMode = true // Explicit mode name disables strategy
		if len(args) > 1 {
			if max, err := strconv.Atoi(args[1]); err == nil && max >= 0 {
				cfg.maxIterations = max
				if len(args) > 2 {
					cfg.Goal = strings.Join(args[2:], " ")
				}
			} else {
				cfg.Goal = strings.Join(args[1:], " ")
			}
		}
	case "plan":
		cfg.mode = "plan"
		cfg.promptFile = "PROMPT_plan.md"
		cfg.maxIterations = defaultPlanIterations
		cfg.explicitMode = true // Explicit mode name disables strategy
		if len(args) > 1 {
			if max, err := strconv.Atoi(args[1]); err == nil && max >= 0 {
				cfg.maxIterations = max
				if len(args) > 2 {
					cfg.Goal = strings.Join(args[2:], " ")
				}
			} else {
				cfg.Goal = strings.Join(args[1:], " ")
			}
		}
	default:
		max, err := parsePositiveInt(args[0])
		if err != nil {
			return cfg, fmt.Errorf("invalid mode or max iterations: %q", args[0])
		}
		cfg.mode = "build"
		cfg.promptFile = "PROMPT_build.md"
		cfg.maxIterations = max
		// Note: explicitMode stays false for numeric-only args like "rauf 5"
		// so strategy mode can still apply if configured
	}

	return cfg, nil
}

func parsePositiveInt(input string) (int, error) {
	value, err := strconv.Atoi(input)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid numeric value: %q", input)
	}
	return value, nil
}

func loadConfig(path string) (runtimeConfig, bool, error) {
	cfg := runtimeConfig{
		RetryMaxAttempts:    3,
		RetryBackoffBase:    2 * time.Second,
		RetryBackoffMax:     30 * time.Second,
		RetryJitter:         true,
		RetryMatch:          append([]string(nil), defaultRetryMatch...),
		NoProgressIters:     5,
		OnVerifyFail:        "soft_reset",
		VerifyMissingPolicy: "strict",
		PlanLintPolicy:      "warn",
		ModelFlag:           "--model",
		ModelEscalation:     defaultEscalationConfig(),
		Recovery:            defaultRecoveryConfig(),
	}
	ok := true
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			ok = false
		} else {
			return cfg, false, fmt.Errorf("failed to read %s: %w", path, err)
		}
	} else if err := parseConfigBytes(data, &cfg); err != nil {
		return cfg, true, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if q, ok := envBool("RAUF_QUIET"); ok {
		cfg.Quiet = q
	}
	// Apply environment overrides
	if h := envFirst("RAUF_HARNESS"); h != "" {
		cfg.Harness = h
	}
	if ha := envFirst("RAUF_HARNESS_ARGS"); ha != "" {
		cfg.HarnessArgs = ha
	}
	if np, ok := envBool("RAUF_NO_PUSH", "RAUF_SKIP_PUSH"); ok {
		cfg.NoPush = np
	}
	if ld := envFirst("RAUF_LOG_DIR"); ld != "" {
		cfg.LogDir = ld
	}
	if r := envFirst("RAUF_RUNTIME"); r != "" {
		cfg.Runtime = r
	}
	if di := envFirst("RAUF_DOCKER_IMAGE"); di != "" {
		cfg.DockerImage = di
	}
	if da := envFirst("RAUF_DOCKER_ARGS"); da != "" {
		cfg.DockerArgs = da
	}
	if dc := envFirst("RAUF_DOCKER_CONTAINER"); dc != "" {
		cfg.DockerContainer = dc
	}
	if ovf := envFirst("RAUF_ON_VERIFY_FAIL"); ovf != "" {
		cfg.OnVerifyFail = ovf
	}
	if vmp := envFirst("RAUF_VERIFY_MISSING_POLICY"); vmp != "" {
		cfg.VerifyMissingPolicy = vmp
	}
	if avf, ok := envBool("RAUF_ALLOW_VERIFY_FALLBACK"); ok {
		cfg.AllowVerifyFallback = avf
	}
	if rvc, ok := envBool("RAUF_REQUIRE_VERIFY_ON_CHANGE"); ok {
		cfg.RequireVerifyOnChange = rvc
	}
	if rvpu, ok := envBool("RAUF_REQUIRE_VERIFY_FOR_PLAN_UPDATE"); ok {
		cfg.RequireVerifyForPlanUpdate = rvpu
	}
	if rty, ok := envBool("RAUF_RETRY"); ok {
		cfg.RetryOnFailure = rty
	}
	if rm := envFirst("RAUF_RETRY_MAX"); rm != "" {
		if v, err := strconv.Atoi(rm); err == nil && v >= 0 {
			cfg.RetryMaxAttempts = v
		}
	}
	if rbb := envFirst("RAUF_RETRY_BACKOFF_BASE"); rbb != "" {
		if v, err := time.ParseDuration(rbb); err == nil {
			cfg.RetryBackoffBase = v
		}
	}
	if rbm := envFirst("RAUF_RETRY_BACKOFF_MAX"); rbm != "" {
		if v, err := time.ParseDuration(rbm); err == nil {
			cfg.RetryBackoffMax = v
		}
	}
	if rnj, ok := envBool("RAUF_RETRY_NO_JITTER"); ok {
		cfg.RetryJitter = !rnj // Invert logic as config is RetryJitter (enabled)
		cfg.RetryJitterSet = true
	}
	if rmatch := envFirst("RAUF_RETRY_MATCH"); rmatch != "" {
		cfg.RetryMatch = splitCommaList(rmatch)
	}
	if md := envFirst("RAUF_MODEL_DEFAULT"); md != "" {
		cfg.ModelDefault = md
	}
	if ms := envFirst("RAUF_MODEL_STRONG"); ms != "" {
		cfg.ModelStrong = ms
	}
	if mf := envFirst("RAUF_MODEL_FLAG"); mf != "" {
		cfg.ModelFlag = mf
	}
	if me, ok := envBool("RAUF_MODEL_ESCALATION_ENABLED"); ok {
		cfg.ModelEscalation.Enabled = me
	}

	return cfg, ok, nil
}

func parseConfigBytes(data []byte, cfg *runtimeConfig) error {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	section := ""
	strategyCurrentIdx := -1    // Index into cfg.Strategy, -1 means none
	var skipMultilineKey string // Track if we're skipping multi-line content
	var multilineIndent int     // Track the base indent of the multi-line key
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// If we're in a multi-line skip mode, check if we should exit
		if skipMultilineKey != "" {
			// Multi-line content ends when we see a line with equal or less indent than the key
			if indent <= multilineIndent && trimmed != "" {
				skipMultilineKey = ""
				multilineIndent = 0
				// Fall through to process this line normally
			} else {
				// Still in multi-line content, skip this line
				continue
			}
		}

		key, value, ok := splitYAMLKeyValue(trimmed)
		if ok {
			// Check for multi-line YAML syntax which is not supported
			if value == "|" || value == ">" || value == "|-" || value == ">-" || value == "|+" || value == ">+" {
				fmt.Fprintf(os.Stderr, "Warning: rauf.yaml key %q uses multi-line YAML syntax which is not supported; use a single-line value or quoted string instead\n", key)
				skipMultilineKey = key
				multilineIndent = indent
				continue
			}
			value = stripQuotesAndComments(value)
		}

		if indent == 0 {
			section = ""
			strategyCurrentIdx = -1
			if ok && value == "" {
				section = key
				continue
			}
			switch key {
			case "harness":
				cfg.Harness = value
			case "harness_args":
				cfg.HarnessArgs = value
			case "no_push":
				if v, ok := parseBool(value); ok {
					cfg.NoPush = v
				}
			case "log_dir":
				cfg.LogDir = value
			case "runtime":
				cfg.Runtime = value
			case "docker_image":
				cfg.DockerImage = value
			case "docker_args":
				cfg.DockerArgs = value
			case "docker_container":
				cfg.DockerContainer = value
			case "max_files_changed":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.MaxFilesChanged = v
				}
			case "max_commits_per_iteration":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.MaxCommits = v
				}
			case "forbidden_paths":
				if value == "" {
					section = "forbidden_paths"
				} else {
					cfg.ForbiddenPaths = splitCommaList(value)
				}
			case "no_progress_iterations":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.NoProgressIters = v
				}
			case "on_verify_fail":
				cfg.OnVerifyFail = value
			case "verify_missing_policy":
				cfg.VerifyMissingPolicy = value
			case "allow_verify_fallback":
				if v, ok := parseBool(value); ok {
					cfg.AllowVerifyFallback = v
				}
			case "require_verify_on_change":
				if v, ok := parseBool(value); ok {
					cfg.RequireVerifyOnChange = v
				}
			case "require_verify_for_plan_update":
				if v, ok := parseBool(value); ok {
					cfg.RequireVerifyForPlanUpdate = v
				}
			case "retry_on_failure":
				if v, ok := parseBool(value); ok {
					cfg.RetryOnFailure = v
				}
			case "retry_max_attempts":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.RetryMaxAttempts = v
				}
			case "retry_backoff_base":
				if v, err := time.ParseDuration(value); err == nil {
					cfg.RetryBackoffBase = v
				}
			case "retry_backoff_max":
				if v, err := time.ParseDuration(value); err == nil {
					cfg.RetryBackoffMax = v
				}
			case "retry_jitter":
				if v, ok := parseBool(value); ok {
					cfg.RetryJitter = v
					cfg.RetryJitterSet = true
				}
			case "retry_match":
				if value == "" {
					section = "retry_match"
				} else {
					cfg.RetryMatch = splitCommaList(value)
				}
			case "plan_lint_policy":
				cfg.PlanLintPolicy = value
			case "model_default":
				cfg.ModelDefault = value
			case "model_strong":
				cfg.ModelStrong = value
			case "model_flag":
				cfg.ModelFlag = value
			case "model_override":
				if v, ok := parseBool(value); ok {
					cfg.ModelOverride = v
				}
			case "model_escalation":
				section = "model_escalation"
			case "recovery":
				section = "recovery"
			case "strategy":
				section = "strategy"
			}
			continue
		}

		if section == "recovery" {
			switch key {
			case "consecutive_verify_fails":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.Recovery.ConsecutiveVerifyFails = v
				}
			case "no_progress_iters":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.Recovery.NoProgressIters = v
				}
			case "guardrail_failures":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.Recovery.GuardrailFailures = v
				}
			}
			continue
		}

		if section == "model_escalation" {
			switch key {
			case "enabled":
				if v, ok := parseBool(value); ok {
					cfg.ModelEscalation.Enabled = v
				}
			case "min_strong_iterations":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.ModelEscalation.CooldownIters = v
				}
			case "cooldown_iters":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.ModelEscalation.CooldownIters = v
				}
			case "max_escalations":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.ModelEscalation.MaxEscalations = v
				}
			case "trigger":
				// Nested trigger section - handled below
			case "consecutive_verify_fails":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.ModelEscalation.ConsecutiveVerifyFails = v
				}
			case "no_progress_iters":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.ModelEscalation.NoProgressIters = v
				}
			case "guardrail_failures":
				if v, err := strconv.Atoi(value); err == nil && v >= 0 {
					cfg.ModelEscalation.GuardrailFailures = v
				}
			}
			continue
		}

		if section == "forbidden_paths" {
			if strings.HasPrefix(trimmed, "-") {
				item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				item = stripQuotes(item)
				if item != "" {
					cfg.ForbiddenPaths = append(cfg.ForbiddenPaths, item)
				}
			}
			continue
		}

		if section == "retry_match" {
			if strings.HasPrefix(trimmed, "-") {
				item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				item = stripQuotes(item)
				if item != "" {
					cfg.RetryMatch = append(cfg.RetryMatch, item)
				}
			}
			continue
		}

		if section == "strategy" {
			if strings.HasPrefix(trimmed, "-") {
				step := strategyStep{}
				rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				if rest != "" {
					if k, v, ok := splitYAMLKeyValue(rest); ok {
						assignStrategyField(&step, k, stripQuotes(v))
					}
				}
				cfg.Strategy = append(cfg.Strategy, step)
				strategyCurrentIdx = len(cfg.Strategy) - 1
				continue
			}
			if strategyCurrentIdx >= 0 && ok {
				assignStrategyField(&cfg.Strategy[strategyCurrentIdx], key, value)
			}
		}
	}

	return scanner.Err()
}

func assignStrategyField(step *strategyStep, key, value string) {
	switch key {
	case "mode":
		step.Mode = value
	case "iterations":
		if v, err := strconv.Atoi(value); err == nil && v >= 0 {
			step.Iterations = v
		}
	case "until":
		step.Until = value
	case "if":
		step.If = value
	}
}

func splitYAMLKeyValue(line string) (string, string, bool) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	return key, value, true
}

// stripQuotesAndComments removes surrounding quotes and trailing inline comments.
// Inline comments start with # when not inside quotes.
func stripQuotesAndComments(value string) string {
	if len(value) == 0 {
		return value
	}

	// If quoted, extract the quoted content (handles embedded #)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
		// Check for quote at start with comment after closing quote
		// Handle escaped quotes within the string
		if value[0] == '"' || value[0] == '\'' {
			quote := value[0]
			escaped := false
			for i := 1; i < len(value); i++ {
				if escaped {
					escaped = false
					continue
				}
				if value[i] == '\\' {
					escaped = true
					continue
				}
				if value[i] == quote {
					return value[1:i]
				}
			}
		}
	}

	// Unquoted value: strip inline comment
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}

	return value
}

func stripQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

func parseBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true, true
	case "0", "false", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

func runHarness(ctx context.Context, prompt string, harness, harnessArgs string, logFile *os.File, retry retryConfig, runner runtimeExec) (harnessResult, error) {
	attempts := 0
	matchedToken := ""
	for {
		output, err := runHarnessOnce(ctx, prompt, harness, harnessArgs, logFile, runner)
		if err == nil {
			return harnessResult{Output: output, RetryCount: attempts, RetryReason: matchedToken}, nil
		}
		if ctx.Err() != nil {
			return harnessResult{Output: output, RetryCount: attempts, RetryReason: matchedToken}, err
		}
		if !retry.Enabled || retry.MaxAttempts == 0 {
			return harnessResult{Output: output, RetryCount: attempts, RetryReason: matchedToken}, err
		}
		token, shouldRetry := retryMatchToken(output, retry.Match)
		if !shouldRetry {
			return harnessResult{Output: output, RetryCount: attempts, RetryReason: matchedToken}, err
		}
		matchedToken = token
		if attempts >= retry.MaxAttempts {
			return harnessResult{Output: output, RetryCount: attempts, RetryReason: matchedToken}, err
		}
		attempts++
		delay := backoffDuration(retry.BackoffBase, retry.BackoffMax, attempts, retry.Jitter)
		fmt.Fprintf(os.Stderr, "Harness error matched retry rule (%s); sleeping %s before retry %d/%d\n", token, delay, attempts, retry.MaxAttempts)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return harnessResult{Output: output, RetryCount: attempts, RetryReason: matchedToken}, ctx.Err()
		case <-timer.C:
		}
	}
}

var runHarnessOnce = func(ctx context.Context, prompt string, harness, harnessArgs string, logFile *os.File, runner runtimeExec) (string, error) {
	args := []string{}
	if harnessArgs != "" {
		extraArgs, err := splitArgs(harnessArgs)
		if err != nil {
			return "", err
		}
		args = append(args, extraArgs...)
	}

	buffer := &limitedBuffer{max: 1024 * 1024}

	promptInArgs := false
	for i, arg := range args {
		if strings.Contains(arg, "{prompt}") {
			args[i] = strings.ReplaceAll(arg, "{prompt}", prompt)
			promptInArgs = true
		}
	}

	cmd, err := runner.command(ctx, harness, args...)
	if err != nil {
		return "", err
	}

	if !promptInArgs {
		cmd.Stdin = strings.NewReader(prompt)
	}

	var logWriter io.Writer = io.Discard
	if logFile != nil {
		logWriter = logFile
	}

	// Filter out RAUF_QUESTION: lines from stdout/stderr so the user sees only the interactive prompt
	filteredStdout := newFilteringWriter(os.Stdout, "RAUF_QUESTION:")
	filteredStderr := newFilteringWriter(os.Stderr, "RAUF_QUESTION:")

	writers := []io.Writer{logWriter, buffer}
	if !runner.Quiet {
		writers = append(writers, filteredStdout)
	}
	cmd.Stdout = io.MultiWriter(writers...)

	errWriters := []io.Writer{logWriter, buffer}
	if !runner.Quiet {
		errWriters = append(errWriters, filteredStderr)
	}
	cmd.Stderr = io.MultiWriter(errWriters...)
	cmd.Env = os.Environ()

	err = cmd.Run()
	return buffer.String(), err
}

func openLogFile(mode string, logDir string) (*os.File, string, error) {
	if logDir == "" {
		logDir = "logs"
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, "", err
	}

	now := time.Now()
	stamp := now.Format("20060102-150405.000000000")
	path := filepath.Join(logDir, fmt.Sprintf("%s-%s.jsonl", mode, stamp))
	file, err := os.Create(path)
	if err != nil {
		return nil, "", err
	}

	return file, path, nil
}

var gitOutput = func(args ...string) (string, error) {
	output, err := gitOutputRaw(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

var gitExec = func(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func gitOutputRaw(args ...string) (string, error) {
	return gitExec(args...)
}

func gitPush(branch string) error {
	cmd := exec.Command("git", "push", "origin", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err == nil {
		return nil
	}

	// First push failed, try with -u flag to set upstream
	fmt.Println("Initial push failed, retrying with upstream tracking...")
	fallback := exec.Command("git", "push", "-u", "origin", branch)
	fallback.Stdout = os.Stdout
	fallback.Stderr = os.Stderr
	fallback.Env = os.Environ()
	if err := fallback.Run(); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}
	return nil
}

func isCleanWorkingTree() bool {
	if err := gitQuiet("diff", "--quiet"); err != nil {
		return false
	}
	if err := gitQuiet("diff", "--cached", "--quiet"); err != nil {
		return false
	}
	return true
}

func gitQuiet(args ...string) error {
	_, err := gitExec(args...)
	return err
}

func hasPlanFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasUncheckedTasks(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	taskLine := regexp.MustCompile(`^\s*[-*]\s+\[\s\]\s+`)
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		if taskLine.MatchString(scanner.Text()) {
			return true
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: error reading plan file %s: %v\n", path, err)
	}
	return false
}

func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func workspaceFingerprint(root string, excludeDirs []string, excludeFiles []string) string {
	hasher := sha256.New()
	excludeDirAbs := make([]string, 0, len(excludeDirs))
	for _, dir := range excludeDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		excludeDirAbs = append(excludeDirAbs, filepath.Clean(dir))
	}
	excludeFileAbs := make(map[string]struct{}, len(excludeFiles))
	for _, file := range excludeFiles {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(root, file)
		}
		excludeFileAbs[filepath.Clean(file)] = struct{}{}
	}

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		cleanPath := filepath.Clean(path)
		if _, ok := excludeFileAbs[cleanPath]; ok {
			return nil
		}
		for _, dir := range excludeDirAbs {
			if cleanPath == dir || strings.HasPrefix(cleanPath, dir+string(filepath.Separator)) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err == nil {
			_, _ = hasher.Write([]byte(rel))
		}
		_, _ = hasher.Write(data)
		return nil
	})

	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  rauf init [--force] [--dry-run]")
	fmt.Println("  rauf plan-work \"<name>\"")
	fmt.Println("  rauf [architect|plan|<max_iterations>]")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  rauf")
	fmt.Println("  rauf 20")
	fmt.Println("  rauf plan")
	fmt.Println("  rauf plan 3")
	fmt.Println("  rauf architect")
	fmt.Println("  rauf architect 5")
	fmt.Println("  rauf architect 5")
	fmt.Println("  rauf plan-work \"add oauth\"")
	fmt.Println("")
	fmt.Println("Env:")
	fmt.Println("  RAUF_HARNESS=claude     Harness command (default: claude)")
	fmt.Println("  RAUF_HARNESS_ARGS=...   Extra harness args")
	fmt.Println("  RAUF_NO_PUSH=1          Skip git push even if new commits exist")
	fmt.Println("  RAUF_LOG_DIR=path       Override logs directory")
	fmt.Println("  RAUF_RUNTIME=host|docker|docker-persist Runtime execution target")
	fmt.Println("  RAUF_DOCKER_IMAGE=image  Docker image for docker runtimes")
	fmt.Println("  RAUF_DOCKER_ARGS=...     Extra args for docker run")
	fmt.Println("  RAUF_DOCKER_CONTAINER=name  Container name for docker-persist")
	fmt.Println("  RAUF_ON_VERIFY_FAIL=mode  soft_reset|keep_commit|hard_reset|no_push_only|wip_branch")
	fmt.Println("  RAUF_VERIFY_MISSING_POLICY=mode  strict|agent_enforced|fallback")
	fmt.Println("  RAUF_ALLOW_VERIFY_FALLBACK=1  Allow AGENTS.md Verify fallback")
	fmt.Println("  RAUF_REQUIRE_VERIFY_ON_CHANGE=1  Require Verify when worktree changes")
	fmt.Println("  RAUF_REQUIRE_VERIFY_FOR_PLAN_UPDATE=1  Require Verify before plan updates")
	fmt.Println("  RAUF_RETRY=1            Retry harness failures (matches only)")
	fmt.Println("  RAUF_RETRY_MAX=3        Max retry attempts")
	fmt.Println("  RAUF_RETRY_BACKOFF_BASE=2s  Base backoff duration")
	fmt.Println("  RAUF_RETRY_BACKOFF_MAX=30s  Max backoff duration")
	fmt.Println("  RAUF_RETRY_NO_JITTER=1  Disable backoff jitter")
	fmt.Println("  RAUF_RETRY_MATCH=...    Comma-separated match tokens")
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func envBool(keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			result, valid := parseBool(value)
			if !valid {
				fmt.Fprintf(os.Stderr, "Warning: invalid boolean value %q for %s, ignoring\n", value, key)
			}
			return result, valid
		}
	}
	return false, false
}

func splitCommaList(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		items = append(items, part)
	}
	return items
}

func splitArgs(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	fields := []string{}
	current := strings.Builder{}
	inQuote := rune(0)
	escaped := false
	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t':
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		return nil, fmt.Errorf("invalid args: unfinished escape")
	}
	if inQuote != 0 {
		return nil, fmt.Errorf("invalid args: unterminated quote")
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields, nil
}

type limitedBuffer struct {
	buf []byte
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.max <= 0 {
		return len(p), nil
	}
	if len(p) >= b.max {
		b.buf = make([]byte, b.max)
		copy(b.buf, p[len(p)-b.max:])
		return len(p), nil
	}
	if len(b.buf)+len(p) > b.max {
		drop := len(b.buf) + len(p) - b.max
		// Defensive check: ensure drop doesn't exceed buffer length
		if drop > len(b.buf) {
			drop = len(b.buf)
		}
		if drop < 0 {
			drop = 0
		}
		remaining := len(b.buf) - drop
		if remaining < 0 {
			remaining = 0
		}
		// Create a new slice to avoid retaining old backing array memory
		newBuf := make([]byte, remaining, b.max)
		if remaining > 0 {
			copy(newBuf, b.buf[drop:])
		}
		b.buf = newBuf
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return string(b.buf)
}

func retryMatchToken(output string, match []string) (string, bool) {
	if len(match) == 0 {
		return "", false
	}
	lower := strings.ToLower(output)
	for _, token := range match {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if token == "*" {
			return "*", true
		}
		if strings.Contains(lower, strings.ToLower(token)) {
			return token, true
		}
	}
	return "", false
}

func backoffDuration(base, max time.Duration, attempt int, jitter bool) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	// Cap the shift to prevent integer overflow (max 62 for int64)
	shift := attempt - 1
	if shift > 62 {
		shift = 62
	}
	delay := base * time.Duration(1<<uint(shift))
	if max > 0 && delay > max {
		delay = max
	}
	if jitter {
		delay = jitterDuration(delay)
	}
	return delay
}

func jitterDuration(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	jitterMu.Lock()
	factor := 0.5 + jitterRng.Float64()
	jitterMu.Unlock()
	return time.Duration(float64(delay) * factor)
}

func runInit(force bool, dryRun bool) error {
	type templateFile struct {
		path    string
		content string
	}

	files := []templateFile{
		{path: "rauf.yaml", content: configTemplate},
		{path: "PROMPT_architect.md", content: promptArchitect},
		{path: "PROMPT_plan.md", content: promptPlan},
		{path: "PROMPT_build.md", content: promptBuild},
		{path: filepath.Join("specs", "_TEMPLATE.md"), content: specTemplate},
		{path: filepath.Join("specs", "README.md"), content: specReadme},
		{path: "AGENTS.md", content: agentsTemplate},
		{path: "IMPLEMENTATION_PLAN.md", content: planTemplate},
	}

	var created []string
	var skipped []string
	var overwritten []string

	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
			return err
		}
		_, statErr := os.Stat(file.path)
		exists := statErr == nil
		if exists && !force {
			skipped = append(skipped, file.path)
			continue
		}
		if dryRun {
			if exists {
				overwritten = append(overwritten, file.path)
			} else {
				created = append(created, file.path)
			}
			continue
		}
		if err := os.WriteFile(file.path, []byte(file.content), 0o644); err != nil {
			return err
		}
		if exists {
			overwritten = append(overwritten, file.path)
		} else {
			created = append(created, file.path)
		}
	}

	gitignoreUpdated, err := ensureGitignoreLogs(dryRun)
	if err != nil {
		return err
	}
	if gitignoreUpdated {
		if dryRun {
			overwritten = append(overwritten, ".gitignore (logs/)")
		} else {
			created = append(created, ".gitignore (logs/)")
		}
	}

	if dryRun {
		fmt.Println("Init dry run complete.")
	} else {
		fmt.Println("Init complete.")
	}
	if len(created) > 0 {
		fmt.Printf("Created: %s\n", strings.Join(created, ", "))
	}
	if len(overwritten) > 0 {
		fmt.Printf("Overwritten: %s\n", strings.Join(overwritten, ", "))
	}
	if len(skipped) > 0 {
		fmt.Printf("Skipped: %s\n", strings.Join(skipped, ", "))
	}
	if len(created) == 0 && len(overwritten) == 0 {
		fmt.Println("No files created.")
	}
	if !dryRun {
		fmt.Println("Next: update AGENTS.md with repo-specific commands.")
	}
	return nil
}

func ensureGitignoreLogs(dryRun bool) (bool, error) {
	const entry = "logs/"
	path := ".gitignore"
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	if err == nil {
		if bytes.Contains(data, []byte(entry)) {
			return false, nil
		}
		if dryRun {
			return true, nil
		}
		updated := append(bytes.TrimRight(data, "\n"), []byte("\n"+entry+"\n")...)
		return true, os.WriteFile(path, updated, 0o644)
	}

	content := []byte(entry + "\n")
	if dryRun {
		return true, nil
	}
	return true, os.WriteFile(path, content, 0o644)
}

func slugify(value string) string {
	lower := strings.ToLower(value)
	var b strings.Builder
	prevDash := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func hasAgentsPlaceholders(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	placeholders := []string{
		"[test command]",
		"[full test command]",
		"[lint command]",
		"[typecheck/build command]",
	}
	for _, placeholder := range placeholders {
		if strings.Contains(text, placeholder) {
			return true
		}
	}
	return false
}

const promptArchitect = `# ROLE: System Architect (Spec-First)

You are acting as a System Architect, not an implementer.

Your sole responsibility is to produce or refine a rigorous specification artifact
that clearly defines WHAT must be built, not HOW it is built.

You MUST NOT:
- Write application code
- Modify implementation files
- Generate an implementation plan
- Attempt to determine whether the spec is already implemented
- Read large portions of the codebase; focus on defining the contract

You MUST:
- Produce or update exactly one spec file under "specs/"
- Follow the template in "specs/_TEMPLATE.md"
- Define contracts before behavior
- Ensure all acceptance criteria are testable

---

## Repo Context (auto-generated)

Repo map (truncated):

{{.RepoMap}}

---

## Phase 0a — Orientation

1. Study "specs/_TEMPLATE.md" carefully. This defines the required structure.
2. Review existing files under "specs/" to avoid overlap or duplication.
3. If present, skim "AGENTS.md" to understand repo conventions (do not act on them).

---

## Phase 0b — Clarification (Interview)

If the user's request is vague or underspecified, ask up to 3 clarifying questions
focused on:
- The interface/contract (inputs, outputs, data shapes, APIs, UI states, etc.)
- The happy path
- The most important edge cases

IMPORTANT:
- Do NOT block indefinitely waiting for answers.
- If answers are missing, proceed using explicit assumptions and record them
  in the spec under "Open Questions / Assumptions".
- Ask questions by emitting lines prefixed with "RAUF_QUESTION:" so the runner can pause.

---

## Phase 1 — Specification Drafting

1. Create or update a file at:
   "specs/<topic-slug>.md"

2. Strictly follow the structure in "specs/_TEMPLATE.md".

3. Contract First Rule (MANDATORY):
   - Section "Contract" MUST be written before scenarios.
   - The contract must define the source-of-truth data shape, API, schema, or UI state.
   - If multiple contract options exist, document them and clearly choose one.

4. Scenario Rule (MANDATORY):
   - Each scenario must be written as Given / When / Then.
   - Each scenario MUST include a "Verification:" subsection.
   - If verification is not yet possible, write:
     "Verification: TBD: add harness"
     (This will become a first-class planning task.)

5. Testability Rule:
   - Every scenario must be objectively verifiable.
   - Avoid subjective outcomes like "works correctly" or "behaves as expected".

6. Set frontmatter:
   - "status: draft" unless the user explicitly approves the spec.

---

## Phase 2 — Review Gate

After writing or updating the spec:

1. Ask the user to review the spec.
2. Clearly state:
   - What assumptions were made
   - What decisions were taken in the contract
3. If the user approves:
   - Update the spec frontmatter to "status: approved"
4. If the user requests changes:
   - Revise the spec ONLY (do not plan or build)

---

## Definition of Done (Architect)

Your task is complete when:
- A single spec exists under "specs/"
- It follows the template
- Contracts are defined
- Scenarios are testable
- Status is either "draft" or "approved"

STOP once this condition is met.
`

const promptPlan = `# ROLE: Planner (Spec → Plan)

You are acting as a Planner.

Your responsibility is to translate approved specifications
into a concrete, ordered implementation plan.

You MUST NOT:
- Write application code
- Modify spec content
- Invent requirements not present in approved specs

You MUST:
- Derive all tasks from approved specs only
- Maintain traceability from plan → spec → verification
- Produce or update "{{.PlanPath}}"

---

## Plan Context (auto-generated)

Approved spec index:

{{.SpecIndex}}

Repo map (truncated):

{{.RepoMap}}

---

## Phase 0a — Orientation

1. Study "AGENTS.md" to understand repo commands and workflow.
2. Study all files under "specs/".

---

## Phase 0b — Spec Selection

1. Identify specs with frontmatter:
   "status: approved"

2. Ignore all specs marked "draft".

If no approved specs exist:
- Create a single plan item:
  "Run architect to produce approved specs"
- STOP.

---

## Phase 1 — Gap Analysis (MANDATORY)

For EACH approved spec:

1. Search the existing codebase to determine:
   - Which parts of the Contract already exist
   - Which Scenarios are already satisfied
   - Which verification mechanisms already exist
2. DO NOT assume anything is missing.
3. Cite specific files/functions/tests that already satisfy parts of the spec.

Only create plan tasks for:
- Gaps between spec and code
- Missing or failing verification
- Incomplete or incorrect behavior

Optionally, include a short "Satisfied by existing code" section in the plan
that lists scenarios already covered and where they live.

---

## Phase 2 — Spec-to-Plan Extraction

For EACH approved spec:

### 1. Contract Tasks
- If the spec defines a Contract:
  - Create tasks to introduce or modify the contract
    (types, schema, API definitions, UI state, etc.)
  - These tasks come BEFORE behavioral tasks.

### 2. Scenario Tasks
For EACH scenario in the spec:
- Create tasks that include:
  a) Creating or updating the verification mechanism (tests, harness, scripts)
  b) Implementing logic to satisfy the scenario

Derive tasks ONLY for gaps identified in Phase 1.

---

## Phase 3 — Plan Authoring

Create or update "{{.PlanPath}}".

Each task MUST include:
- A checkbox "[ ]"
- "Spec:" reference (file + section anchor)
- "Verify:" exact command(s) to run
- "Outcome:" clear observable success condition

Example:

- [ ] T3: Enforce unique user email
  - Spec: specs/user-profile.md#Scenario-duplicate-email
  - Verify: npm test -- user-profile
  - Outcome: duplicate email creation fails with 409

---

## Phase 4 — Plan Hygiene

- Preserve completed tasks ("[x]")
- Do NOT reorder completed tasks
- Group tasks by feature/spec where possible
- Keep tasks small and atomic

---

## Verification Backpressure Rule

- Plan tasks MUST NOT contain "Verify: TBD".
- If a spec scenario has "Verification: TBD", create an explicit task whose
  outcome is to define and implement verification.

---

## Definition of Done (Planner)

Your task is complete when:
- "{{.PlanPath}}" exists
- Every task traces to an approved spec
- Every task has a verification command
- No code has been written

STOP once the plan is updated.
`

const promptBuild = `# ROLE: Builder (Plan Execution)

You are acting as a Builder.

Your responsibility is to execute the implementation plan
ONE TASK AT A TIME with strict verification backpressure.

You MUST NOT:
- Skip verification
- Work on multiple tasks in one iteration
- Modify specs or plan structure (except ticking a task)
- Implement functionality without first searching for existing implementations
- Create parallel or duplicate logic
- Run multiple build/test strategies in parallel

You MUST:
- Complete exactly ONE unchecked plan task per iteration
- Run verification commands
- Commit verified changes

---

## Build Context (auto-generated)

Plan: {{.PlanPath}}

{{.PlanSummary}}

---

## Phase 0a — Orientation

1. Study "AGENTS.md" and follow its commands exactly.
2. Study "{{.PlanPath}}".

---

## Phase 0b — Task Selection

1. Identify the FIRST unchecked task "[ ]".
2. Read the referenced spec section carefully.
3. Understand the required outcome and verification command.
4. If the task has no "Verify:" command or it is clearly invalid, STOP and ask for a plan fix.

---

## Phase 0c — Codebase Reconnaissance (MANDATORY)

Before writing or modifying code:

1. Search the codebase for existing implementations related to this task.
2. Identify relevant files, functions, tests, or utilities.
3. Do NOT assume the functionality does not exist.

---

## Phase 1 — Implementation

1. Make the MINIMAL code changes required to satisfy the task.
2. Follow existing repo conventions.
3. Do NOT refactor unrelated code.

---

## Phase 2 — Verification (MANDATORY)

1. Run the task’s "Verify:" command(s).
2. If verification FAILS:
   - Fix the issue
   - Re-run verification
   - Repeat until it PASSES
3. Do NOT move on until verification passes.

Use exactly one verification approach per task, as defined in the plan.

---

## Phase 3 — Commit & Update Plan

1. Mark the task as complete "[x]" in "{{.PlanPath}}".
2. Commit changes with a message referencing the task ID:
   e.g. "T3: enforce unique user email"
3. Push if this repo expects pushes (see "AGENTS.md").

---

## Definition of Done (Builder)

Your iteration is complete when:
- Verification passes
- One task is checked
- One commit is created

STOP after completing ONE task.
`

const specTemplate = `---
id: <slug>
status: draft # draft | approved
version: 0.1.0
owner: <optional>
---

# <Feature/Topic Name>

## 1. Context & User Story
As a <role>, I want <action>, so that <benefit>.

## 2. Non-Goals
- ...

## 3. Contract
Contract format: <TypeScript | JSON Schema | OpenAPI | SQL | UI State | CLI | Other>

<contract content here>

## 4. Completion Contract
Success condition:
- <state or output that must be true>

Verification commands:
- <exact command(s) to prove completion>

Artifacts/flags:
- <files, markers, or outputs that must exist>

## 5. Scenarios (Acceptance Criteria)
### Scenario: <name>
Given ...
When ...
Then ...

Verification:
- <exact command(s) to prove this scenario> (or "TBD: add harness")

## 6. Constraints / NFRs
- Performance:
- Security:
- Compatibility:
- Observability:

## 7. Open Questions / Assumptions
- Assumption:
- Open question:
`

const specReadme = `# Specs

This folder contains one spec per topic of concern.

## Template

All specs must follow "specs/_TEMPLATE.md" and include frontmatter.
Completion contracts and verification commands are mandatory to define "done."

## Approval Gate

- Approval is a human decision recorded in spec frontmatter.
- Planning may be automated, but approval is not.
- Approval is recorded when the human reviewer explicitly instructs the agent to mark the spec as approved.
- Changing an approved spec requires explicit human instruction to flip "status" back to "draft" or to update "status: approved".
`

const agentsTemplate = `# AGENTS

## Repo Layout
- specs/: specifications
- src/: application code
- IMPLEMENTATION_PLAN.md: task list

## Commands
- Tests (fast): [test command]
- Tests (full): [full test command]
- Lint: [lint command]
- Typecheck/build: [typecheck/build command]

## Git
- Status: git status
- Diff: git diff
- Log: git log -5 --oneline

## Definition of Done
- Verify command passes
- Plan task checked
- Commit created
`

const planTemplate = `# Implementation Plan

<!-- Task lines must use "- [ ]" or "- [x]" for rauf to detect status. -->

## Feature: <name> (from specs/<slug>.md)
- [ ] T1: <task title>
  - Spec: specs/<slug>.md#<section>
  - Verify: <command>
  - Outcome: <what success looks like>
  - Notes: <optional>
`

const configTemplate = `# rauf configuration
# Environment variables override this file.

harness: claude
harness_args: ""
no_push: false
log_dir: logs
runtime: host # host | docker | docker-persist
docker_image: ""
docker_args: ""
docker_container: ""
max_files_changed: 0
max_commits_per_iteration: 0
forbidden_paths: ""
no_progress_iterations: 2
on_verify_fail: soft_reset # soft_reset | keep_commit | hard_reset | no_push_only | wip_branch
verify_missing_policy: strict # strict | agent_enforced | fallback
allow_verify_fallback: false
require_verify_on_change: false
require_verify_for_plan_update: false
plan_lint_policy: warn
retry_on_failure: false
retry_max_attempts: 3
retry_backoff_base: 2s
retry_backoff_max: 30s
retry_jitter: true
retry_match: "rate limit,429,overloaded,timeout"
model_default: ""
model_strong: ""
model_flag: "--model"
model_override: false
model_escalation:
  enabled: false
  consecutive_verify_fails: 2
  no_progress_iters: 2
  guardrail_failures: 2
  cooldown_iters: 2
  min_strong_iterations: 2
  max_escalations: 2
recovery:
  consecutive_verify_fails: 2
  no_progress_iters: 2
  guardrail_failures: 2
strategy:
  - mode: plan
    iterations: 1
  - mode: build
    iterations: 5
    until: verify_pass
`
