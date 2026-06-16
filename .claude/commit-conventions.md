# Commit Conventions

Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification.

## Format

```
<type>(<scope>): <description>

[optional body]
```

- Subject line: max 72 characters, lowercase, no trailing period
- Use imperative mood: "add feature" not "added feature"
- Body: wrap at 72 characters, explain *why* not *what*

## Types

| Type | When to use |
|------|-------------|
| `feat` | New feature or user-visible behaviour |
| `fix` | Bug fix |
| `refactor` | Code change with no behaviour change |
| `style` | Formatting, whitespace — no logic change |
| `docs` | Documentation only |
| `chore` | Build scripts, deps, tooling, CI |
| `perf` | Performance improvement |
| `revert` | Reverts a previous commit |

Append `!` after the type for breaking changes: `feat!: drop support for Go 1.24`.

## Scopes (optional but recommended)

Use the package name or layer being changed: `<package-name>`, `module`, `ci`, `deps`, `config`.

## Examples

```
feat(store): add batch insert API
fix(httpclient): retry budget leak under timeout
refactor(module): move shared error type into an errors package
chore(deps): bump golang.org/x/sync to v0.10.0
docs: document the Go version policy in CLAUDE.md
feat!: drop support for Go 1.24
```

## What to avoid

- Vague messages: `fix stuff`, `update`, `wip`
- Mixing unrelated changes in one commit
- Committing secrets or credentials (they are gitignored for a reason)
