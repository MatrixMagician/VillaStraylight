BINARY := villa
PKG := ./...

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the villa control-plane CLI
	go run ./cmd/$(BINARY)

.PHONY: build
build: ## Build the villa control-plane CLI to ./villa
	go build -o $(BINARY) ./cmd/$(BINARY)

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
