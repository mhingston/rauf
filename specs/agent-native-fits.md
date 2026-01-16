---
id: agent-native-fits
status: approved # draft | approved
version: 0.1.0
owner: mark
---

# Agent-Native Fit Enhancements

## 1. Context & User Story
As a maintainer of rauf, I want small, additive enhancements that improve agent
capability clarity, loop completion, and auditability without changing the
spec-first core loop, so that rauf stays strict while becoming more agent-native.

## 2. Non-Goals
- New UI or mobile-specific behavior.
- Changing the Architect -> Plan -> Build flow.
- Dynamic API discovery beyond rauf's existing repo/spec/plan context.

## 3. Contract (SpecFirst)
Contract format: CLI / Prompt Output / State Files

- Prompt includes a concise capability map derived from AGENTS.md.
- Prompt optionally includes `.rauf/context.md` if present.
- Build loop can honor an explicit completion signal from the harness output.
- State summary is written to a human-readable file for audit/debug.
- Plan linting can flag tasks that are not atomic (multiple outcomes/verify).

## 4. Scenarios (Acceptance Criteria)
### Scenario: Capability map and optional context are injected
Given AGENTS.md and optional `.rauf/context.md`
When rauf renders prompts
Then the prompt includes a "What You Can Do" capability section and the context
content if the file exists.

Verification:
- go test ./...

### Scenario: Completion is explicit, not heuristic
Given a harness response that includes a completion sentinel
When a build iteration parses the response
Then the loop stops without relying on heuristics.

Verification:
- go test ./...

### Scenario: Plan linting flags non-atomic tasks
Given a task with multiple Verify commands or multiple outcomes
When rauf loads the plan in build mode
Then rauf reports a lint warning or error based on config.

Verification:
- go test ./...

### Scenario: State summary is readable
Given a completed iteration
When rauf writes state
Then a human-readable summary file is updated.

Verification:
- go test ./...

## 5. Constraints / NFRs
- Performance: avoid large prompt bloat; cap context size.
- Security: do not execute new commands for context.
- Compatibility: default behavior unchanged if new files/sentinels absent.
- Observability: log new sections and completion signals in logs.

## 6. Open Questions / Assumptions
- Should plan linting be fatal by default or warning-only?
- What exact completion sentinel format should be supported?
