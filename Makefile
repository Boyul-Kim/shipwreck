BINARY  := shipwreck
MAIN    := ./cmd/shipwreck_cli
BIN_DIR := bin
DIST    := dist

GOOS_HOST  := $(shell go env GOOS)
GOEXE_HOST := $(shell go env GOEXE)
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w

# Platforms for `make release`: GOOS/GOARCH pairs.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary into bin/ for the host platform
	@mkdir -p $(BIN_DIR)
	go build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY)$(GOEXE_HOST) $(MAIN)

.PHONY: run
run: ## Run the CLI without installing it
	go run $(MAIN)

.PHONY: install
install: ## Install the binary into $(go env GOPATH)/bin
	go install -ldflags '$(LDFLAGS)' $(MAIN)

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: test-race
test-race: ## Run all tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run tests and open an HTML coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

.PHONY: fmt
fmt: ## Format all Go source
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

.PHONY: check
check: fmt vet test ## Format, vet, and test — run this before committing

.PHONY: cross
cross: ## Verify every supported platform compiles (no output written)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "  build $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -o /dev/null $(MAIN) || exit 1; \
	done

.PHONY: release
release: ## Build release binaries for every platform into dist/
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; \
		if [ "$$os" = "windows" ]; then ext=.exe; fi; \
		out=$(DIST)/$(BINARY)-$(VERSION)-$$os-$$arch$$ext; \
		echo "  $$out"; \
		GOOS=$$os GOARCH=$$arch go build -ldflags '$(LDFLAGS)' -o $$out $(MAIN) || exit 1; \
	done

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) $(DIST) coverage.out

.PHONY: version
version: ## Print the version string the build would stamp
	@echo $(VERSION)
