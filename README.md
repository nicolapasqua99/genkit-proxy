# Claude Code Go Template

Claude Code configuration scaffolding for **Go module** projects.
Fork or copy this repo to start a new Go project that ships with a pinned
toolchain, opinionated lint/format/test commands, and Claude Code rule
files already wired up.

## What's bundled

| File | Purpose |
|------|---------|
| `go.mod` | Module root. Module path, the `go`/`toolchain` version pin, and the (centralized) dependency graph. |
| `.go-version` | Single-line Go version mirroring the `toolchain` pin, for version managers (`goenv`/`asdf`/`gimme`) and CI. |
| `.golangci.yml` | `golangci-lint` v2 config: linters + the `gofmt`/`goimports` formatters. |
| `Makefile` | Task runner wrapping build / vet / lint / fmt / test / vuln / licenses / watch. |
| `.air.toml` | `air` live-reload config. |
| `Dockerfile` / `.dockerignore` | Multi-stage Go build → distroless image, for container / Cloud Run deploys. |
| `.gitignore` | Ignores binaries, coverage output, and `go.work`. |
| `cmd/`, `internal/` | Standard Go layout dirs — add binaries under `cmd/<name>/` and private packages under `internal/<name>/`. |
| `CLAUDE.md` | Top-level rules surfaced to Claude Code. |
| `.claude/*.md` | Per-task rule files (see table below). |

## Bootstrap a project from this template

```bash
# 1. Clone and rename
git clone https://github.com/ninoverse/claude-mit-go-template my-project
cd my-project
rm -rf .git && git init

# 2. Set your module path (rewrites go.mod + run the goimports local-prefixes)
go mod edit -module github.com/<you>/<my-project>
#    then update local-prefixes in .golangci.yml to match.

# 3. Install the auxiliary tools (once per machine)
make tools

# 4. Add your first package or binary
mkdir -p internal/<your-pkg>      # or: mkdir -p cmd/<your-binary>

# 5. Verify the toolchain and module
go build ./...
golangci-lint run ./...
gotestsum -- -race ./...
```

## Daily commands

```bash
go build ./...
air                              # live reload
golangci-lint run ./...
golangci-lint fmt                # check-only: golangci-lint fmt --diff
gotestsum -- -race ./...
govulncheck ./...
go-licenses check ./...
```

Or via the Makefile: `make build`, `make lint`, `make fmt`, `make test-race`, `make vuln`, `make licenses`, `make ci`.

## Rule files

| File | Purpose |
|------|---------|
| `.claude/branch-naming.md` | Branch prefix and format conventions |
| `.claude/commit-conventions.md` | Conventional Commits rules |
| `.claude/pr-guidelines.md` | PR title, description template, size guidance |
| `.claude/testing-requirements.md` | Test gates (fmt, lint, test, vuln, licenses) |
| `.claude/file-naming.md` | Module layout and Go naming conventions |
| `.claude/code-review.md` | Review checklist (lint, error handling, docs, deps) |
| `.claude/package-workflow.md` | Step-by-step procedure to add a package |
| `.claude/execution-order.md` | Branching strategy for package groups |
