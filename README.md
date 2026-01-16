# rauf

[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mhingston/rauf)

`rauf` is a spec-first, plan-driven execution loop for software projects.
It enforces explicit specifications, verification-backed planning, and
one-task-at-a-time implementation.

## Why rauf

rauf is designed for:
- Non-trivial systems
- Brownfield codebases
- Work that must remain auditable, reviewable, and reversible

rauf is NOT designed for:
- Quick one-off scripts
- Exploratory throwaway code
- “Just vibe and ship” workflows

## Core loop

1. Architect: define WHAT must be built (`specs/`)
2. Plan: derive tasks from approved specs (`IMPLEMENTATION_PLAN.md` or branch-scoped plan)
3. Build: implement one verified task per iteration

Each phase is isolated and enforced.

## Beyond the Traditional Ralph Loop

`rauf` is a formal, Go-based implementation of the "[Ralph](https://github.com/ghuntley/how-to-ralph-wiggum)" loop philosophy. While a traditional loop might be a simple bash script feeding a CLI, `rauf` provides an orchestration layer designed for production-grade software engineering:

- **Native Orchestration:** Move beyond simple `while true` loops with first-class `Architect -> Plan -> Build` strategies.
- **Enhanced Backpressure:** Persistent state tracking (via `state.json`) ensures that verification failures and loop errors are fed back into the next iteration's context for self-correction.
- **Automatic Context Management:** Native support for Repo Mapping and Spec Indexing ensures the agent always knows exactly where it is and what its source of truth is.
- **Strict Isolation:** Built-in support for Docker runtimes ensures agent commands are sandboxed, protecting your host machine while enabling autonomous execution.
- **Auditability:** Automatic commit-per-task logic creates a clean, verifiable git history of the agent's reasoning and implementation steps.

## Loop mechanics

Each iteration is a fresh harness run. The runner re-reads repo state from disk,
does one unit of work, then exits. The only state that carries across iterations
is via files (especially `IMPLEMENTATION_PLAN.md`) and git history.

Looping does not run forever by default. The runner stops when max iterations
is reached, when there are no unchecked tasks in build mode, or when an
iteration makes no changes (no new commit, clean working tree, no plan changes).
Ctrl+C interrupts the current run and exits immediately.

Harness errors are not retried automatically unless you enable bounded retries
via config or env. If the harness exits non-zero (including rate limits), the
run stops and returns a failure by default.

## Quick start

```bash
rauf init
# edit AGENTS.md to add repo commands

rauf plan-work "add oauth"
# or keep using repo-root IMPLEMENTATION_PLAN.md

rauf architect
# review spec, set status: approved

rauf plan
rauf 5
```

## Files rauf cares about

- `specs/*.md`             — approved specifications
- `IMPLEMENTATION_PLAN.md` — executable task list (or `.rauf/IMPLEMENTATION_PLAN.md` for plan-work)
- `AGENTS.md`              — operational contract
- `PROMPT_*.md`            — agent instructions

Task format note: unchecked tasks must use `- [ ]` or `- [x]` for rauf to detect them.

## Harnesses

By default, rauf uses `claude`, but any stdin/stdout-compatible harness
can be configured.

A “harness” is any executable that:
- Reads a prompt from stdin
- Writes responses to stdout/stderr
- Can operate non-interactively

See `rauf.yaml` for details.

## Common failure modes

- Planner creates tasks without Verify → fix spec or plan
- Builder makes no changes → plan/spec likely already satisfied
- Infinite loops → check verification commands or harness output

## Config (rauf.yaml)

Config lives in `rauf.yaml` at repo root. Environment variables override config.

```yaml
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
plan_lint_policy: warn # warn | fail | off
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
```

Notes:
- `on_verify_fail` controls git hygiene when verification fails; default `soft_reset` keeps changes staged and drops the commit from `HEAD`.
- `verify_missing_policy` controls what happens if a task has no `Verify:` command; default `strict` exits with a clear error.
- `plan_lint_policy` controls whether non-atomic plan tasks are warned or fail the build; default `warn`.
- `runtime: docker-persist` reuses a long-lived container; stop/remove it with `docker stop <name>` / `docker rm <name>` if needed.
- Build agents can emit `RAUF_COMPLETE` to end an iteration early after finishing work.
- `.rauf/context.md` (optional) is injected into prompts as additional context when present.
- `.rauf/state.md` is a human-readable summary of the latest state and verification output.

## SpecFirst import

`rauf import` pulls a completed [SpecFirst](https://github.com/mhingston/SpecFirst) stage artifact into `specs/` once.

Defaults:
- Stage: `requirements`
- SpecFirst dir: `.specfirst`

Example:

```bash
rauf import --stage requirements --slug user-auth
```

## Build

```bash
go build -o rauf ./cmd/rauf
```

## Development

```bash
make fmt
make lint
make test
```

## Logs

Each run writes to `logs/<mode>-<timestamp>.jsonl` in the working directory.
`rauf init` also adds `logs/` to `.gitignore` if it is not already present.

## Environment

```bash
RAUF_YOLO=1             # Enable --dangerously-skip-permissions (build only)
RAUF_MODEL_OVERRIDE=x   # Override model selection for all modes
RAUF_HARNESS=claude     # Harness command (default: claude)
RAUF_HARNESS_ARGS=...   # Extra harness args (non-claude harnesses)
RAUF_NO_PUSH=1          # Skip git push even if new commits exist
RAUF_LOG_DIR=path       # Override logs directory
RAUF_RUNTIME=docker-persist  # Runtime execution target
RAUF_DOCKER_IMAGE=image  # Docker image for docker runtimes
RAUF_DOCKER_ARGS=...     # Extra args for docker run
RAUF_DOCKER_CONTAINER=name  # Container name for docker-persist
RAUF_ON_VERIFY_FAIL=soft_reset  # soft_reset|keep_commit|hard_reset|no_push_only|wip_branch
RAUF_VERIFY_MISSING_POLICY=strict  # strict|agent_enforced|fallback
RAUF_ALLOW_VERIFY_FALLBACK=1  # Allow AGENTS.md Verify fallback
RAUF_REQUIRE_VERIFY_ON_CHANGE=1  # Require Verify when worktree changes
RAUF_REQUIRE_VERIFY_FOR_PLAN_UPDATE=1  # Require Verify before plan updates
RAUF_RETRY=1            # Retry harness failures (matches only)
RAUF_RETRY_MAX=3        # Max retry attempts
RAUF_RETRY_BACKOFF_BASE=2s  # Base backoff duration
RAUF_RETRY_BACKOFF_MAX=30s  # Max backoff duration
RAUF_RETRY_NO_JITTER=1  # Disable backoff jitter
RAUF_RETRY_MATCH=...    # Comma-separated match tokens

```
