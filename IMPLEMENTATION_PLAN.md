# Implementation Plan

## Requirements / Acceptance Criteria (Sharp Edges)

### 1. Artifact Definitions
- **`repo_map.md`**:
    - **Tree**: Depth-limited folder tree (Max Depth: 3).
    - **Entrypoints**: Distinct list of entry points (e.g., `main.go`, `cmd/`, `index.ts`).
    - **Modules/Packages**: Summary of major internal partitions.
    - **Config**: List of config keys and where they are parsed.
    - **Side Effect Boundaries**: Explicitly list files that interact with FS, Network, or Process Execution.
- **`context_pack.md`**:
    - **Summary**: High-level overview of why this pack exists.
    - **Rationale**: Per-file justification for inclusion.
    - **Excerpts**: Code snippets with clear file paths and line ranges.
- **Caps/Limits**:
    - Max files in pack: **12**.
    - Max excerpt lines per file: **50**.
    - Max total pack size: **50KB** (roughly 10k-12k tokens).

### 2. Failure & Fallback Policy
- **Search**: If `rg` is missing, fallback to `git grep`. If both missing, fail loudly.
- **Environment**: If not a git repo, generate uncached maps and warn.
- **Symbols**: If symbol discovery fails, proceed with grep-only results (soft degradation).

### 3. Caching & Invalidation
- **Location**: `.rauf/cache/context/`
- **Key**: `sha256(HEAD_SHA + ToolVersion + ConfigHash)`
- **Invalidation**: 
    - Change in HEAD SHA.
    - Change in dependency files (`go.mod`, `package.json`, `requirements.txt`).
    - Change in `rauf.yaml` context-builder settings.

### 4. Search & Scoring Rules
- **Extraction**: Split task text, remove stopwords, preserve quoted phrases as literal terms.
- **Queries**: Execute 3–6 focused queries (literal, symbol, error-string).
- **Scoring Weights**:
    - `+5`: Direct search hit.
    - `+3`: Call sites of symbols found in hits.
    - `+2`: Tests referencing hit modules.
    - `+2`: Config/CLI-related files.
    - `+1`: Same folder/package as high-score files.
- **Determinism**: Results must be sorted by score, then alphabetically by file path to ensure stable output.

### 5. Integration Point
- **Invocation**: Triggered in `run_loop.go` before `PROMPT_architect.md` and `PROMPT_plan.md` are rendered.
- **Evidence Requirement**: Architect prompt will be updated to *require* referencing specific file/line evidence from the context pack.

## Requirements / Acceptance Criteria (Strict Contracts)

### 1. The Determinism Requirement
- **Goal**: Given identical inputs (task string, repo commit), `repo_map.md` and `context_pack.md` MUST be byte-for-byte identical.
- **Why**: Essential for trust, CI reproducibility, and debugging.

### 2. Repo Map Contract
- **Must Include**:
    - **Tree**: Depth-limited folder tree (Max Depth: 3).
    - **Entrypoints**: Canonical list of entry points.
    - **Modules/Packages**: Partition summary.
    - **Config**: Config keys and parsing locations.
    - **Side Effect Boundaries**: Files interacting with exterior (FS, Network, Process).
- **Constraints**: Max 500 lines (non-truncated if possible).

### 3. Context Pack Contract
- **Must Include**:
    - **Summary**: High-level pack overview.
    - **Rationale**: Per-file/excerpt justification.
    - **Evidence**: Excerpts with paths and 1-indexed line ranges.
    - **Zero Hits Handling**: If no code is found, emit an explicit "No relevant code found" section with search term justification.
- **Hard Caps**:
    - Max files: **12**.
    - Max excerpt lines: **50 per file**.
    - Max total size: **50KB**.

### 4. Failure & Fallback Policy
- **Search**: If `rg` missing -> fallback to `git grep`. If both missing -> fail loudly.
- **Environment**: If not a git repo -> generate uncached map (warn user).
- **Symbols**: If symbol discovery fails -> fallback to grep-only (soft degradation).
- **Zero Results**: Failures in tool execution must not result in an empty file; they must result in an error or a clearly explained "zero-hit" state.

### 5. Caching & Invalidation
- **Location**: `.rauf/cache/context/`
- **Key**: `sha256(HEAD_SHA + ToolVersion + ConfigHash)`
- **Invalidation**: HEAD change OR dependency file change (`go.mod`, `package.json`, etc.).

### 6. Architect/Planner Cognitive Load
- **Evidence Requirement**: Update `PROMPT_architect.md` to *require* referencing file paths and line numbers for all claims.
- **Hallucination Prevention**: If context is insufficient, the model must **RAUF_QUESTION** instead of guessing.

## Proposed Changes
> (from specs/<slug>.md)
- [ ] T1: <task title>
  - Spec: specs/<slug>.md#<section>
  - Verify: <command>
  - Outcome: <what success looks like>
  - Notes: <optional>
