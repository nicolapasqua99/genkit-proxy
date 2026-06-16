# CLAUDE.md

This file provides strict guidance and architectural rules for Claude Code (claude.ai/code) when working in this repository.

## Commands & Tooling

- **Toolchain:** The Go version is pinned via the `go` and `toolchain` directives in `go.mod` (mirrored in `.go-version`). With `GOTOOLCHAIN=auto` (the default) every contributor automatically downloads the pinned toolchain on first `go` invocation. The full-parity dev tools are `golangci-lint` (lint + format), `gotestsum` (test runner), `govulncheck` (CVE scan), `go-licenses` (license check), and `air` (live reload).
- **Maintain the Build:** Never leave the codebase in a state where build, lint, or tests fail. Run the relevant commands below to verify your work before concluding a task.

```bash
go build ./...                                       # Build all packages
air                                                  # Dev loop / live reload (requires air)
go vet ./...                                          # Vet
golangci-lint run ./...                              # Lint
golangci-lint fmt --diff                             # Format check (write: golangci-lint fmt)
gotestsum -- -race ./...                             # Tests (fallback: go test -race ./...)
gotestsum -- -race -coverprofile=coverage.txt ./...  # Tests + coverage
govulncheck ./...                                    # CVE check
go-licenses check ./...                              # License check
```

Every target above is also wrapped in the `Makefile` (`make build`, `make lint`, `make test-race`, `make ci`, …). Install the auxiliary tools once per machine:

```bash
make tools     # or, individually:
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install gotest.tools/gotestsum@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/google/go-licenses/v2@latest
go install github.com/air-verse/air@latest
```

## Architecture & Module Rules

**Layout:** A single Go module rooted at the repo. The module path is declared in `go.mod`; dependency versions are centralized in that one file. Code follows the standard Go layout:

- `cmd/<binary>/main.go` — one directory per executable (`package main`).
- `internal/<pkg>/` — private packages, importable only within this module.
- `pkg/<pkg>/` — public packages intended for external import (omit if there are none).

**Packages:** One package per directory; the directory name matches the `package` clause. A new package is just a new directory with a `package` declaration — there is no per-package manifest. Cross-package use is a plain `import "github.com/ninoverse/claude-mit-go-template/internal/<pkg>"`. New dependencies are added with `go get` and land in `go.mod`/`go.sum`.

**Go version:** The minimum language version is the `go` directive in `go.mod` (the analog of an MSRV). Do not lower it incidentally. There are no editions, no LTO/codegen profiles, and `gofmt` is non-configurable by design.

## Behavioral Guidelines

**Tradeoff:** Bias toward caution over speed. For trivial tasks, use judgment.

### 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

- State your assumptions explicitly. If uncertain, stop and ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, propose it. Push back when warranted.

### 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked. No abstractions for single-use code.
- No "flexibility" or error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

- Don't "improve" adjacent code, comments, or formatting.
- Match existing style exactly.
- Remove imports/variables/functions that YOUR changes made unused. Don't remove pre-existing dead code unless asked.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

- Transform tasks into verifiable goals (e.g., "Add validation" → "Write tests for invalid inputs, then make them pass").
- For multi-step tasks, state a brief plan and verify each step independently.

---

## Extended Rules (Read Before Acting)

Use your file-reading capabilities to read the exact rules in the `.claude/` directory **before** executing any of the following tasks:

- **Committing code:** Read `.claude/commit-conventions.md`
- **Creating branches:** Read `.claude/branch-naming.md`
- **Reviewing PRs:** Read `.claude/code-review.md`
- **Testing/Verifying:** Read `.claude/testing-requirements.md`
- **Opening PRs:** Read `.claude/pr-guidelines.md`
- **Creating new files:** Read `.claude/file-naming.md`
- **Building a package:** Read `.claude/package-workflow.md`
- **Deciding what to build next / branching strategy:** Read `.claude/execution-order.md`
