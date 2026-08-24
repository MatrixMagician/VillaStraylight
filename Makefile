BINARY := villa
PKG := ./...
# LINT_BASE is what `make lint` diffs against to find NEW issues, mirroring CI's
# only-new-issues gate. The standing backlog is deliberately not a local blocker.
LINT_BASE ?= origin/main

# GOLANGCI_VERSION comes from .golangci-version, the SINGLE place the pin lives.
# The CI workflow reads the same file, so local and CI cannot drift onto different
# linters and disagree about what is clean.
GOLANGCI_VERSION := $(shell cat .golangci-version)
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

# gofmt is shipped with the toolchain but is not guaranteed on $PATH (some Go
# installs expose `go` only). Resolve it from GOROOT so `make fmt` works
# regardless of PATH, and stays version-matched to the active toolchain.
GOFMT := $(shell go env GOROOT)/bin/gofmt

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

.PHONY: build-static
build-static: ## Build a CGO-free static binary (SC#4 — must succeed with huh added)
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/$(BINARY)

.PHONY: test
test: ## Run Go tests
	go test $(PKG)

# The race detector is cgo-based, so this target enables CGO for the TEST run
# only — it does NOT affect the shipped binary, which stays CGO-free (build/
# build-static keep CGO_ENABLED=0). internal/websafe is the project's only
# intentionally-concurrent pure core (Loader.Load fans out fetch goroutines),
# so its concurrency invariants MUST be guarded under -race (CR-01/WR-04). We
# run the whole tree under -race so any future concurrent core is covered too.
.PHONY: test-race
test-race: ## Run Go tests under the race detector (cgo test-only; binary stays CGO-free)
	CGO_ENABLED=1 go test -race $(PKG)

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: fmt
fmt: ## Format Go code
	$(GOFMT) -w .

.PHONY: lint
lint: ## Lint this branch's NEW issues at the pinned version; LINT_ALL=1 lints the whole tree
	@if [ -n "$(LINT_ALL)" ]; then \
		$(GOLANGCI) run; \
	else \
		$(GOLANGCI) run --new-from-merge-base=$(LINT_BASE); \
	fi

.PHONY: check
check: vet test test-race ## Run vet + tests (incl. the -race gate, CR-01/WR-04)

.PHONY: tidy
tidy: ## Tidy Go module dependencies
	go mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin villa
