.DEFAULT_GOAL := help

# Pin golangci-lint to a concrete version; `@latest` is discouraged for it.
GOLANGCI_LINT_VERSION := v2.12.2

.PHONY: help tools build vet fmt fmt-check lint test test-race cover vuln licenses watch ci

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

tools: ## Install dev tools (once per machine)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install gotest.tools/gotestsum@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/google/go-licenses/v2@latest
	go install github.com/air-verse/air@latest

build: ## Build all packages
	go build ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format the code (writes changes)
	golangci-lint fmt

fmt-check: ## Check formatting without writing changes
	golangci-lint fmt --diff

lint: ## Run the linters
	golangci-lint run ./...

test: ## Run tests (falls back to `go test` if gotestsum is missing)
	gotestsum -- ./... || go test ./...

test-race: ## Run tests with the race detector and coverage
	gotestsum -- -race -coverprofile=coverage.txt -covermode=atomic ./...

cover: test-race ## Show the coverage summary
	go tool cover -func=coverage.txt

vuln: ## Scan for known vulnerabilities
	govulncheck ./...

licenses: ## Check dependency licenses
	go-licenses check ./...

watch: ## Dev loop with live reload (requires air)
	air

ci: fmt-check vet lint test-race vuln ## Run the full verification gate
