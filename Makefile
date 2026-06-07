BINARY := villa
PKG := ./...

# VERSION stamps the build-time villa version (Phase 16, D-09): derived from
# `git describe` (tag-based) with a "dev" fallback for a non-git / untagged tree.
# It is injected via -ldflags -X into main.version (cmd/villa/version.go), the
# single source for the backup manifest's villa_version and the BAK-03 skew
# compare. CGO stays disabled — the binary remains a single static CGO-free build.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the villa control-plane CLI
	go run ./cmd/$(BINARY)

.PHONY: build
build: ## Build the villa control-plane CLI to ./villa (version-stamped)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/$(BINARY)

.PHONY: test
test: ## Run Go tests
	go test $(PKG)

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: fmt
fmt: ## Format Go code
	gofmt -w .

.PHONY: lint
lint: ## Run golangci-lint if installed, else fall back to vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || (echo "golangci-lint not found; running go vet" && go vet $(PKG))

.PHONY: check
check: vet test ## Run vet + tests

.PHONY: tidy
tidy: ## Tidy Go module dependencies
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin villa
