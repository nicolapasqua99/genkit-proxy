# PR Guidelines

## Title

Follow the same format as commit messages: `<type>(<scope>): <description>`.  
Keep it under 72 characters.

## Description template

```markdown
## What
<!-- One-paragraph summary of the change -->

## Why
<!-- Motivation: bug, feature request, refactor reason -->

## How
<!-- Non-obvious implementation decisions -->

## Testing
<!-- How was this manually verified? Screenshots for UI changes. -->
```

## Rules

- One logical change per PR; split unrelated work into separate PRs
- **Update documentation in the same PR as the change.** When a change alters
  observable behaviour — endpoints, request/response fields, metrics,
  configuration/env vars, or the error mapping — update the affected `docs/`
  pages and `README.md` in the same PR so documentation never drifts behind
  `main`. State in the `## Testing` section that the docs were checked (or note
  explicitly that no docs change was needed).
- Rebase onto `main` before requesting review (no unnecessary merge commits)
- All lint checks must pass: `golangci-lint run ./...` and `go vet ./...`
- `go build ./...` must succeed before marking the PR ready for review
- `gotestsum -- -race ./...` must pass (or `go test -race ./...`)
- Link to the relevant section in CLAUDE.md or a `.claude/` rule file if the PR establishes a new pattern

## Size guidance

| Lines changed | Action |
|--------------|--------|
| < 200 | Normal review |
| 200 – 600 | Add context in the description about where to start reading |
| > 600 | Consider splitting — or at minimum call it out and justify it |

## Draft PRs

Use draft status for work-in-progress or when feedback is needed before the implementation is complete. Convert to ready only when `golangci-lint run ./...` and `go build ./...` both pass.
