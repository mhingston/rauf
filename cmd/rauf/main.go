package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultArchitectIterations = 10
	defaultPlanIterations      = 1
)

var version = "dev"

var defaultRetryMatch = []string{"rate limit", "429", "overloaded", "timeout"}

type modeConfig struct {
	mode          string
	promptFile    string
	maxIterations int
	forceInit     bool
	dryRunInit    bool
	importStage   string
	importSlug    string
	importDir     string
	importForce   bool
	planPath      string
	planWorkName  string
	explicitMode  bool
}

type runtimeConfig struct {
	Harness                    string
	HarnessArgs                string
	Model                      map[string]string
	NoPush                     bool
	Yolo                       bool
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
}

type retryConfig struct {
	Enabled     bool
	MaxAttempts int
	BackoffBase time.Duration
	BackoffMax  time.Duration
	Jitter      bool
	Match       []string
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if cfg.mode == "init" {
		if err := runInit(cfg.forceInit, cfg.dryRunInit); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if cfg.mode == "plan-work" {
		if err := runPlanWork(cfg.planWorkName); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if cfg.mode == "import" {
		if err := runImportSpecfirst(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if cfg.mode == "version" {
		fmt.Printf("rauf %s\n", version)
		return
	}
	if cfg.mode == "help" {
		printUsage()
		return
	}

	fileCfg, _, err := loadConfig("rauf.yaml")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	branch, err := gitOutput("branch", "--show-current")
	gitAvailable := err == nil && branch != ""
	planPath := resolvePlanPath(branch, gitAvailable, "IMPLEMENTATION_PLAN.md")
	cfg.planPath = planPath

	model := defaultModel(cfg.mode)
	if fileCfg.Model != nil {
		if configuredModel, ok := fileCfg.Model[cfg.mode]; ok && configuredModel != "" {
			model = configuredModel
		}
	}
	if override := envFirst("RAUF_MODEL_OVERRIDE"); override != "" {
		model = override
	}

	yolo := fileCfg.Yolo
	if value, ok := envBool("RAUF_YOLO"); ok {
		yolo = value
	}
	yoloEnabled := cfg.mode == "build" && yolo

	noPush := fileCfg.NoPush
	if value, ok := envBool("RAUF_NO_PUSH"); ok {
		noPush = value
	}

	harness := fileCfg.Harness
	if harness == "" {
		harness = "claude"
	}
	if override := envFirst("RAUF_HARNESS"); override != "" {
		harness = override
	}

	harnessArgs := fileCfg.HarnessArgs
	if override := envFirst("RAUF_HARNESS_ARGS"); override != "" {
		harnessArgs = override
	}

	logDir := fileCfg.LogDir
	if override := envFirst("RAUF_LOG_DIR"); override != "" {
		logDir = override
	}

	runtime := fileCfg.Runtime
	if override := envFirst("RAUF_RUNTIME"); override != "" {
		runtime = override
	}
	dockerImage := fileCfg.DockerImage
	if override := envFirst("RAUF_DOCKER_IMAGE"); override != "" {
		dockerImage = override
	}
	dockerArgs := fileCfg.DockerArgs
	if override := envFirst("RAUF_DOCKER_ARGS"); override != "" {
		dockerArgs = override
	}
	dockerContainer := fileCfg.DockerContainer
	if override := envFirst("RAUF_DOCKER_CONTAINER"); override != "" {
		dockerContainer = override
	}

	onVerifyFail := fileCfg.OnVerifyFail
	if override := envFirst("RAUF_ON_VERIFY_FAIL"); override != "" {
		onVerifyFail = override
	}
	verifyMissingPolicy := fileCfg.VerifyMissingPolicy
	if override := envFirst("RAUF_VERIFY_MISSING_POLICY"); override != "" {
		verifyMissingPolicy = override
	}
	allowVerifyFallback := fileCfg.AllowVerifyFallback
	if value, ok := envBool("RAUF_ALLOW_VERIFY_FALLBACK"); ok {
		allowVerifyFallback = value
	}
	requireVerifyOnChange := fileCfg.RequireVerifyOnChange
	if value, ok := envBool("RAUF_REQUIRE_VERIFY_ON_CHANGE"); ok {
		requireVerifyOnChange = value
	}
	requireVerifyForPlanUpdate := fileCfg.RequireVerifyForPlanUpdate
	if value, ok := envBool("RAUF_REQUIRE_VERIFY_FOR_PLAN_UPDATE"); ok {
		requireVerifyForPlanUpdate = value
	}

	retryEnabled := fileCfg.RetryOnFailure
	if value, ok := envBool("RAUF_RETRY"); ok {
		retryEnabled = value
	}

	retryMaxAttempts := fileCfg.RetryMaxAttempts
	if override := envFirst("RAUF_RETRY_MAX"); override != "" {
		if v, err := strconv.Atoi(override); err == nil && v >= 0 {
			retryMaxAttempts = v
		}
	}

	retryBackoffBase := fileCfg.RetryBackoffBase
	if override := envFirst("RAUF_RETRY_BACKOFF_BASE"); override != "" {
		if v, err := time.ParseDuration(override); err == nil {
			retryBackoffBase = v
		}
	}

	retryBackoffMax := fileCfg.RetryBackoffMax
	if override := envFirst("RAUF_RETRY_BACKOFF_MAX"); override != "" {
		if v, err := time.ParseDuration(override); err == nil {
			retryBackoffMax = v
		}
	}

	retryJitter := fileCfg.RetryJitter
	retryJitterSet := fileCfg.RetryJitterSet
	if value, ok := envBool("RAUF_RETRY_NO_JITTER"); ok {
		retryJitter = !value
		retryJitterSet = true
	}

	retryMatch := append([]string(nil), fileCfg.RetryMatch...)
	if override := envFirst("RAUF_RETRY_MATCH"); override != "" {
		retryMatch = splitCommaList(override)
	}

	fileCfg.DockerContainer = dockerContainer
	fileCfg.OnVerifyFail = onVerifyFail
	fileCfg.VerifyMissingPolicy = verifyMissingPolicy
	fileCfg.AllowVerifyFallback = allowVerifyFallback
	fileCfg.RequireVerifyOnChange = requireVerifyOnChange
	fileCfg.RequireVerifyForPlanUpdate = requireVerifyForPlanUpdate

	if retryEnabled {
		if !retryJitterSet {
			retryJitter = true
		}
		if len(retryMatch) == 0 {
			retryMatch = append([]string(nil), defaultRetryMatch...)
		}
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Mode:   %s\n", cfg.mode)
	fmt.Printf("Prompt: %s\n", cfg.promptFile)
	if gitAvailable {
		fmt.Printf("Branch: %s\n", branch)
	} else {
		fmt.Println("Git:    disabled (workspace fingerprint mode)")
	}
	if cfg.maxIterations > 0 {
		fmt.Printf("Max:    %d iterations\n", cfg.maxIterations)
	}
	fmt.Printf("Model:  %s\n", model)
	if yoloEnabled {
		fmt.Println("YOLO:   enabled (build only)")
	}
	fmt.Printf("Harness: %s\n", harness)
	if runtime != "" && runtime != "host" {
		fmt.Printf("Runtime: %s\n", runtime)
		if dockerImage != "" {
			fmt.Printf("Docker:  %s\n", dockerImage)
		}
		if dockerContainer != "" {
			fmt.Printf("Container: %s\n", dockerContainer)
		}
	}
	fmt.Printf("OnVerifyFail: %s\n", onVerifyFail)
	fmt.Printf("VerifyMissing: %s\n", normalizeVerifyMissingPolicy(fileCfg))
	fmt.Printf("RequireVerifyOnChange: %t\n", fileCfg.RequireVerifyOnChange)
	fmt.Printf("RequireVerifyForPlanUpdate: %t\n", fileCfg.RequireVerifyForPlanUpdate)
	strategyActive := len(fileCfg.Strategy) > 0 && !cfg.explicitMode
	fmt.Printf("Strategy: %t\n", strategyActive)
	if retryEnabled {
		fmt.Printf("Retry:  enabled (max %d, base %s, max %s)\n", retryMaxAttempts, retryBackoffBase, retryBackoffMax)
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if _, err := os.Stat(cfg.promptFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s not found\n", cfg.promptFile)
		os.Exit(1)
	}
	if hasAgentsPlaceholders("AGENTS.md") {
		fmt.Println("Warning: AGENTS.md still has placeholder commands. Update it before running build.")
	}

	state := loadState()
	dockerArgsList, err := splitArgs(dockerArgs)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runner := runtimeExec{
		Runtime:         runtime,
		DockerImage:     dockerImage,
		DockerArgs:      dockerArgsList,
		DockerContainer: dockerContainer,
	}

	if len(fileCfg.Strategy) > 0 && !cfg.explicitMode {
		runStrategy(cfg, fileCfg, runner, state, gitAvailable, branch, planPath, model, yoloEnabled, harness, harnessArgs, noPush, logDir, retryEnabled, retryMaxAttempts, retryBackoffBase, retryBackoffMax, retryJitter, retryMatch)
		return
	}

	runMode(cfg, fileCfg, runner, state, gitAvailable, branch, planPath, model, yoloEnabled, harness, harnessArgs, noPush, logDir, retryEnabled, retryMaxAttempts, retryBackoffBase, retryBackoffMax, retryJitter, retryMatch, 0)
}

func parseArgs(args []string) (modeConfig, error) {
	cfg := modeConfig{
		mode:          "build",
		promptFile:    "PROMPT_build.md",
		maxIterations: 0,
		forceInit:     false,
		dryRunInit:    false,
		importStage:   "requirements",
		importSlug:    "",
		importDir:     ".specfirst",
		importForce:   false,
		planPath:      "IMPLEMENTATION_PLAN.md",
		explicitMode:  false,
	}

	if len(args) == 0 {
		return cfg, nil
	}
	cfg.explicitMode = true

	switch args[0] {
	case "--help", "-h", "help":
		cfg.mode = "help"
		return cfg, nil
	case "--version", "version":
		cfg.mode = "version"
		return cfg, nil
	case "import":
		cfg.mode = "import"
		if err := parseImportArgs(args[1:], &cfg); err != nil {
			return cfg, err
		}
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
		if len(args) > 1 {
			max, err := parsePositiveInt(args[1])
			if err != nil {
				return cfg, err
			}
			cfg.maxIterations = max
		}
	case "plan":
		cfg.mode = "plan"
		cfg.promptFile = "PROMPT_plan.md"
		cfg.maxIterations = defaultPlanIterations
		if len(args) > 1 {
			max, err := parsePositiveInt(args[1])
			if err != nil {
				return cfg, err
			}
			cfg.maxIterations = max
		}
	default:
		max, err := parsePositiveInt(args[0])
		if err != nil {
			return cfg, fmt.Errorf("invalid mode or max iterations: %q", args[0])
		}
		cfg.mode = "build"
		cfg.promptFile = "PROMPT_build.md"
		cfg.maxIterations = max
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

func parseImportArgs(args []string, cfg *modeConfig) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--stage":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --stage")
			}
			cfg.importStage = args[i]
		case "--slug":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --slug")
			}
			cfg.importSlug = args[i]
		case "--specfirst-dir":
			i++
			if i >= len(args) {
				return fmt.Errorf("missing value for --specfirst-dir")
			}
			cfg.importDir = args[i]
		case "--force":
			cfg.importForce = true
		default:
			return fmt.Errorf("unknown import flag: %q", arg)
		}
	}
	return nil
}

func loadConfig(path string) (runtimeConfig, bool, error) {
	cfg := runtimeConfig{
		Model:               make(map[string]string),
		RetryMaxAttempts:    3,
		RetryBackoffBase:    2 * time.Second,
		RetryBackoffMax:     30 * time.Second,
		RetryJitter:         true,
		RetryMatch:          append([]string(nil), defaultRetryMatch...),
		NoProgressIters:     2,
		OnVerifyFail:        "soft_reset",
		VerifyMissingPolicy: "strict",
		PlanLintPolicy:      "warn",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return cfg, false, fmt.Errorf("failed to read %s: %w", path, err)
	}

	if err := parseConfigBytes(data, &cfg); err != nil {
		return cfg, true, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return cfg, true, nil
}

func parseConfigBytes(data []byte, cfg *runtimeConfig) error {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	section := ""
	var strategyCurrent *strategyStep
	var inForbiddenPaths bool
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		key, value, ok := splitYAMLKeyValue(trimmed)
		if ok {
			value = stripQuotes(value)
		}

		if indent == 0 {
			section = ""
			strategyCurrent = nil
			inForbiddenPaths = false
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
			case "yolo":
				if v, ok := parseBool(value); ok {
					cfg.Yolo = v
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
					inForbiddenPaths = true
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
				cfg.RetryMatch = splitCommaList(value)
			case "plan_lint_policy":
				cfg.PlanLintPolicy = value
			case "model":
				section = "model"
			case "strategy":
				section = "strategy"
			}
			continue
		}

		if section == "model" {
			if cfg.Model == nil {
				cfg.Model = make(map[string]string)
			}
			cfg.Model[key] = value
			continue
		}

		if section == "forbidden_paths" && inForbiddenPaths {
			if strings.HasPrefix(trimmed, "-") {
				item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				item = stripQuotes(item)
				if item != "" {
					cfg.ForbiddenPaths = append(cfg.ForbiddenPaths, item)
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
				strategyCurrent = &cfg.Strategy[len(cfg.Strategy)-1]
				continue
			}
			if strategyCurrent != nil && ok {
				assignStrategyField(strategyCurrent, key, value)
			}
		}
	}

	return scanner.Err()
}

func assignStrategyField(step *strategyStep, key, value string) {
	switch key {
	case "mode":
		step.Mode = value
	case "model":
		step.Model = value
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

func runHarness(ctx context.Context, prompt string, harness, harnessArgs, model string, yoloEnabled bool, logFile *os.File, retry retryConfig, runner runtimeExec) (string, error) {
	attempts := 0
	for {
		output, err := runHarnessOnce(ctx, prompt, harness, harnessArgs, model, yoloEnabled, logFile, runner)
		if err == nil {
			return output, nil
		}
		if ctx.Err() != nil {
			return output, err
		}
		if !retry.Enabled || retry.MaxAttempts == 0 {
			return output, err
		}
		if !shouldRetryOutput(output, retry.Match) {
			return output, err
		}
		if attempts >= retry.MaxAttempts {
			return output, err
		}
		attempts++
		delay := backoffDuration(retry.BackoffBase, retry.BackoffMax, attempts, retry.Jitter)
		fmt.Fprintf(os.Stderr, "Harness error matched retry rule; sleeping %s before retry %d/%d\n", delay, attempts, retry.MaxAttempts)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return output, ctx.Err()
		case <-timer.C:
		}
	}
}

func runHarnessOnce(ctx context.Context, prompt string, harness, harnessArgs, model string, yoloEnabled bool, logFile *os.File, runner runtimeExec) (string, error) {
	args := []string{}
	switch harness {
	case "claude":
		args = append(args, "-p")
		if yoloEnabled {
			args = append(args, "--dangerously-skip-permissions")
		}
		args = append(args,
			"--output-format=stream-json",
			"--model", model,
			"--verbose",
		)
	default:
		// Generic harness that reads prompt from stdin.
	}
	if harnessArgs != "" {
		extraArgs, err := splitArgs(harnessArgs)
		if err != nil {
			return "", err
		}
		args = append(args, extraArgs...)
	}

	buffer := &limitedBuffer{max: 8 * 1024}

	cmd, err := runner.command(ctx, harness, args...)
	if err != nil {
		return "", err
	}
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = io.MultiWriter(os.Stdout, logFile, buffer)
	cmd.Stderr = io.MultiWriter(os.Stderr, logFile, buffer)
	cmd.Env = os.Environ()

	return buffer.String(), cmd.Run()
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

func defaultModel(mode string) string {
	if mode == "build" {
		return "sonnet"
	}
	return "opus"
}

func gitOutput(args ...string) (string, error) {
	output, err := gitOutputRaw(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func gitOutputRaw(args ...string) (string, error) {
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

func gitPush(branch string) error {
	cmd := exec.Command("git", "push", "origin", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err == nil {
		return nil
	}

	fallback := exec.Command("git", "push", "-u", "origin", branch)
	fallback.Stdout = os.Stdout
	fallback.Stderr = os.Stderr
	fallback.Env = os.Environ()
	if err := fallback.Run(); err != nil {
		return errors.New("git push failed after fallback")
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
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	return cmd.Run()
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
	for scanner.Scan() {
		if taskLine.MatchString(scanner.Text()) {
			return true
		}
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
		_, _ = hasher.Write([]byte(path))
		_, _ = hasher.Write(data)
		return nil
	})
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  rauf init [--force] [--dry-run]")
	fmt.Println("  rauf import [--stage <id>] [--slug <slug>] [--specfirst-dir <path>] [--force]")
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
	fmt.Println("  rauf import --stage requirements --slug user-auth")
	fmt.Println("  rauf plan-work \"add oauth\"")
	fmt.Println("")
	fmt.Println("Env:")
	fmt.Println("  RAUF_YOLO=1             Enable --dangerously-skip-permissions (build only)")
	fmt.Println("  RAUF_MODEL_OVERRIDE=x   Override model selection for all modes")
	fmt.Println("  RAUF_HARNESS=claude     Harness command (default: claude)")
	fmt.Println("  RAUF_HARNESS_ARGS=...   Extra harness args (non-claude harnesses)")
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
			return parseBool(value)
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
		b.buf = append(b.buf[:0], p[len(p)-b.max:]...)
		return len(p), nil
	}
	if len(b.buf)+len(p) > b.max {
		drop := len(b.buf) + len(p) - b.max
		b.buf = b.buf[drop:]
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	return string(b.buf)
}

func shouldRetryOutput(output string, match []string) bool {
	if len(match) == 0 {
		return false
	}
	lower := strings.ToLower(output)
	for _, token := range match {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if token == "*" {
			return true
		}
		if strings.Contains(lower, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

func backoffDuration(base, max time.Duration, attempt int, jitter bool) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := base * time.Duration(1<<uint(attempt-1))
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
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	factor := 0.5 + rng.Float64()
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

type specfirstState struct {
	StageOutputs map[string]specfirstStageOutput `json:"stage_outputs"`
}

type specfirstStageOutput struct {
	PromptHash string   `json:"prompt_hash"`
	Files      []string `json:"files"`
}

type artifactFile struct {
	name    string
	content string
}

func runImportSpecfirst(cfg modeConfig) error {
	if cfg.importStage == "" {
		return fmt.Errorf("stage is required")
	}
	statePath := filepath.Join(cfg.importDir, "state.json")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", statePath, err)
	}

	var state specfirstState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		return fmt.Errorf("failed to parse %s: %w", statePath, err)
	}

	stageOutput, ok := state.StageOutputs[cfg.importStage]
	if !ok {
		return fmt.Errorf("stage %q not found in %s", cfg.importStage, statePath)
	}
	if stageOutput.PromptHash == "" {
		return fmt.Errorf("stage %q has no prompt hash in %s", cfg.importStage, statePath)
	}
	if len(stageOutput.Files) == 0 {
		return fmt.Errorf("stage %q has no output files in %s", cfg.importStage, statePath)
	}

	slug := cfg.importSlug
	if slug == "" {
		slug = slugFromFiles(stageOutput.Files, cfg.importStage)
	}
	slug = slugify(slug)
	if slug == "" {
		return fmt.Errorf("unable to derive a valid slug")
	}

	specsDir := "specs"
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		return err
	}
	specPath := filepath.Join(specsDir, slug+".md")
	if _, err := os.Stat(specPath); err == nil && !cfg.importForce {
		return fmt.Errorf("spec file exists: %s (use --force to overwrite)", specPath)
	}

	files, err := readSpecfirstArtifacts(cfg.importDir, cfg.importStage, stageOutput.PromptHash, stageOutput.Files)
	if err != nil {
		return err
	}

	artifactTitle := titleFromArtifacts(files, slug)
	content := buildSpecFromArtifact(slug, artifactTitle, cfg.importStage, stageOutput.PromptHash, files)
	return os.WriteFile(specPath, []byte(content), 0o644)
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

func readSpecfirstArtifacts(root, stage, hash string, files []string) ([]artifactFile, error) {
	artifacts := make([]artifactFile, 0, len(files))
	for _, name := range files {
		path := filepath.Join(root, "artifacts", stage, hash, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read artifact %s: %w", path, err)
		}
		artifacts = append(artifacts, artifactFile{name: name, content: string(data)})
	}
	return artifacts, nil
}

func slugFromFiles(files []string, fallback string) string {
	if len(files) == 1 {
		base := filepath.Base(files[0])
		ext := filepath.Ext(base)
		return strings.TrimSuffix(base, ext)
	}
	return fallback
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

func titleFromArtifacts(files []artifactFile, fallback string) string {
	for _, file := range files {
		lines := strings.Split(file.content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") && len(line) > 2 {
				return strings.TrimSpace(strings.TrimPrefix(line, "# "))
			}
		}
	}
	return fallback
}

func buildSpecFromArtifact(slug, title, stage, hash string, files []artifactFile) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("id: " + slug + "\n")
	b.WriteString("status: draft # draft | approved\n")
	b.WriteString("version: 0.1.0\n")
	b.WriteString("owner: <optional>\n")
	b.WriteString("source: specfirst\n")
	b.WriteString("stage: " + stage + "\n")
	b.WriteString("artifact: " + hash + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## 1. Context & User Story\n")
	b.WriteString("Imported from SpecFirst " + stage + " artifact. See Appendix.\n\n")
	b.WriteString("## 2. Non-Goals\n")
	b.WriteString("- TBD\n\n")
	b.WriteString("## 3. Contract (SpecFirst)\n")
	b.WriteString("Contract format: <TypeScript | JSON Schema | OpenAPI | SQL | UI State | CLI | Other>\n\n")
	b.WriteString("TBD\n\n")
	b.WriteString("## 4. Scenarios (Acceptance Criteria)\n")
	b.WriteString("### Scenario: TBD\n")
	b.WriteString("Given ...\n")
	b.WriteString("When ...\n")
	b.WriteString("Then ...\n\n")
	b.WriteString("Verification:\n")
	b.WriteString("- TBD: add harness\n\n")
	b.WriteString("## 5. Constraints / NFRs\n")
	b.WriteString("- Performance: TBD\n")
	b.WriteString("- Security: TBD\n")
	b.WriteString("- Compatibility: TBD\n")
	b.WriteString("- Observability: TBD\n\n")
	b.WriteString("## 6. Open Questions / Assumptions\n")
	b.WriteString("- Assumption: TBD\n")
	b.WriteString("- Open question: TBD\n\n")
	b.WriteString("## Appendix: SpecFirst " + stage + " Artifact\n")
	for _, file := range files {
		b.WriteString("\n### " + file.name + "\n\n")
		b.WriteString(strings.TrimRight(file.content, "\n"))
		b.WriteString("\n")
	}
	return b.String()
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

## 3. Contract (SpecFirst)
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
yolo: false
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
retry_on_failure: false
retry_max_attempts: 3
retry_backoff_base: 2s
retry_backoff_max: 30s
retry_jitter: true
retry_match: "rate limit,429,overloaded,timeout"
model:
  architect: opus
  plan: opus
  build: sonnet
strategy:
  - mode: plan
    model: opus
    iterations: 1
  - mode: build
    model: sonnet
    iterations: 5
    until: verify_pass
`
