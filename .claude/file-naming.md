# Directories and File Naming

## Module layout

| Path | Contents |
|------|----------|
| `go.mod` / `go.sum` | Module path, the `go`/`toolchain` version pin, dependency graph + checksums. |
| `.golangci.yml` | Lint + format configuration. |
| `Makefile` | Task runner for build / vet / lint / fmt / test / vuln / licenses / watch. |
| `cmd/<binary>/main.go` | One directory per executable; `package main`. |
| `internal/<pkg>/` | Private packages, importable only within this module. |
| `pkg/<pkg>/` | Public packages intended for external import. Omit if there are none. |
| `bin/` | Build output. Git-ignored. |

## Per-package layout

| Path | Contents |
|------|----------|
| `internal/<pkg>/<file>.go` | Implementation; exported identifiers get doc comments. |
| `internal/<pkg>/doc.go` | Optional home for the `// Package <pkg> …` comment. |
| `internal/<pkg>/<file>_test.go` | Tests for that file (`package <pkg>` for white-box, `package <pkg>_test` for black-box). |
| `internal/<pkg>/testdata/` | Fixtures. The `testdata` name is ignored by the Go toolchain. |

## Naming conventions

| Item | Convention | Example |
|------|------------|---------|
| Module path | lowercase; hyphens allowed in the host/repo segment | `github.com/ninoverse/claude-mit-go-template` |
| Package name / directory | short, all-lowercase, no underscores or MixedCaps | `package store`, `internal/store/` |
| Binary | `cmd/<binary>/main.go`, `package main` | `cmd/app/main.go` |
| Source / test file | lowercase (underscores allowed) / `_test.go` suffix | `user_repository.go`, `user_repository_test.go` |
| Exported identifier | `MixedCaps` (leading capital = exported) | `UserRepository`, `FindByID` |
| Unexported identifier | `mixedCaps` (leading lowercase = package-private) | `dbPool`, `findByID` |
| Constants | `MixedCaps` / `mixedCaps` — **not** SCREAMING_SNAKE | `MaxRetries`, `defaultTimeout` |
| Interfaces | `MixedCaps`; single-method interfaces take an `-er` suffix | `Reader`, `UserStore` |
| Initialisms | keep a consistent case throughout | `ID`, `URL`, `HTTPServer`, `userID` |
| Type parameters (generics) | short uppercase letters | `[T any]`, `[K comparable, V any]` |
| Errors | `Err…` sentinel vars, `…Error` types | `ErrNotFound`, `ValidationError` |

Exported vs. unexported is signalled purely by the **first letter's case** — Go
has no `pub` keyword, no `snake_case` identifiers, and no SCREAMING_SNAKE
constants. Choose package names that read well at the call site (`store.New()`,
not `store.NewStore()`).

## Package pattern

One package per directory; the directory name matches the `package` clause.
A new package is simply a new directory with `.go` files sharing a `package`
declaration — there is no per-package manifest:

```go
// internal/store/store.go
package store
```

```text
internal/store/
├── store.go
├── store_test.go
└── doc.go        // optional: holds the `// Package store …` comment
```
