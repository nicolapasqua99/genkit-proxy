# Execution Order & Branching Strategy

Defines the branch / PR structure for work in this module.

---

## Branching and PR strategy

See `.claude/branch-naming.md` for the branch name format.

| Work type | Branch prefix | One PR per |
|-----------|--------------|-----------|
| Foundation scaffold | `chore/` | whole scaffold |
| Toolchain / config bump | `chore/` | one PR |
| Package group | `feat/` | group (e.g. `feat/storage-packages`) |
| Single isolated package | `feat/` | package |
| Rename / refactor | `refactor/` | logical rename unit |
| Docs / rules | `docs/` | one PR |

**Draft PR rule:** open a draft PR at the group's **first commit**. Push every
subsequent commit to that same PR. Mark ready for review only when these all
pass cleanly:

```bash
golangci-lint fmt --diff
golangci-lint run ./...
go vet ./...
gotestsum -- -race ./...
```

---

## Within each group

- Build **one package at a time**.
- Follow the 9-step checklist in `.claude/package-workflow.md` for each.
- Stop and confirm with the user after each package before starting the next.
- Existing packages in scope get an **audit-pass** (lint + tests + a read-through);
  only commit if a real defect is found.

## Audit-pass checklist (existing packages)

1. Read the package's `.go` files — check for outdated deps, missing doc
   comments on exported identifiers, and unchecked errors in non-test paths.
2. Run `golangci-lint run ./internal/<pkg>/...` and
   `gotestsum -- -race ./internal/<pkg>/...`.
3. Surface anything broken. Only commit if a fix is needed; use an isolated commit.
