# Testing Requirements

## Before merging any change

- [ ] `golangci-lint fmt --diff` *(no formatting drift)*
- [ ] `golangci-lint run ./...` and `go vet ./...`
- [ ] `gotestsum -- -race ./...` *(falls back to `go test -race ./...` if gotestsum is not installed)*
- [ ] `govulncheck ./...` *(known-vulnerability scan)*
- [ ] `go-licenses check ./...` *(dependency licenses)*

All must pass before marking a PR ready for review. `make ci` runs the core gate.

## Test layout

| Test type | Location | When to use |
|-----------|----------|-------------|
| Unit | `<file>_test.go` beside the code, `package <pkg>` (white-box) | Testing internal logic with access to unexported identifiers |
| Black-box | `<file>_test.go`, `package <pkg>_test` | Exercising only the package's exported API, as a consumer would |
| Example | `func Example...()` with a trailing `// Output:` comment | Documented usage that compiles, runs under `go test`, and shows in `go doc` |
| Table-driven | subtests via `t.Run(name, ...)` over a slice of cases | The default shape for unit tests with multiple input/output cases |
| Benchmark | `func Benchmark...(b *testing.B)` in a `_test.go` file | Performance tracking. Optional. |
| Fixtures | files under a `testdata/` directory | Static inputs; the `testdata` name is ignored by the Go toolchain |

## Running specific test types

```bash
go test ./internal/<pkg>/...                  # one package
go test -run TestName ./internal/<pkg>/       # one test
go test -run Example ./...                     # examples only
go test -bench . ./internal/<pkg>/            # benchmarks
go test -race -coverprofile=coverage.txt ./...  # race + coverage
```

## Watching tests during development

```bash
gotestsum --watch -- ./...
```
