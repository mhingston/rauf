# Implementation Plan

## Feature: <name> (from specs/<slug>.md)
- [ ] T1: <task title>
  - Spec: specs/<slug>.md#<section>
  - Verify: <command>
  - Outcome: <what success looks like>
  - Notes: <optional>

## Feature: Agent-native fit enhancements (from specs/agent-native-fits.md)
- [x] T1: Inject capability map + optional context file into prompts
  - Spec: specs/agent-native-fits.md#4-scenarios-acceptance-criteria
  - Verify: go test ./...
  - Outcome: Prompts include "What You Can Do" from AGENTS.md and `.rauf/context.md` when present without changing defaults.
  - Notes: Cap added context to avoid prompt bloat.
- [ ] T2: Add explicit completion sentinel handling in build loop
  - Spec: specs/agent-native-fits.md#4-scenarios-acceptance-criteria
  - Verify: go test ./...
  - Outcome: Build loop can stop on explicit completion signal from harness output, no heuristic needed.
  - Notes: Keep behavior unchanged if sentinel is absent.
- [ ] T3: Add plan linting for non-atomic tasks
  - Spec: specs/agent-native-fits.md#4-scenarios-acceptance-criteria
  - Verify: go test ./...
  - Outcome: Plan loader flags tasks with multiple Verify commands or multiple outcomes.
  - Notes: Default to warning unless config opts into failure.
- [ ] T4: Write a human-readable state summary file
  - Spec: specs/agent-native-fits.md#4-scenarios-acceptance-criteria
  - Verify: go test ./...
  - Outcome: A summary file updates per iteration to aid audit/debug.
  - Notes: Store next to `state.json` (e.g., `.rauf/state.md`).
