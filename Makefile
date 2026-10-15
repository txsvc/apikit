.DEFAULT_GOAL := help

DOC_DIR ?= docs/generated

VERSION    := $(shell git describe --tags 2>/dev/null || echo "0.1.0")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "\
  -X github.com/txsvc/apikit.Version=$(VERSION) \
  -X github.com/txsvc/apikit.Commit=$(COMMIT) \
  -X github.com/txsvc/apikit.BuildTime=$(BUILD_TIME) \
  -X github.com/txsvc/apikit/internal/cli.Version=$(VERSION) \
  -X github.com/txsvc/apikit/internal/cli.Build=$(COMMIT)"

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*##"}; {printf "  %-16s %s\n", $$1, $$2}'

.PHONY: build
build: ## Build bin/apikit and bin/akc
	go build -o bin/apikit $(LDFLAGS) ./cmd/apikit
	go build -o bin/akc $(LDFLAGS) ./cmd/akc

.PHONY: test
test: ## Run all Go tests
	go test ./... -count=1
	-rm -f apikit db.test

.PHONY: coverage
coverage: ## Run all tests with a coverage report
	go test ./... -coverprofile=coverage.txt -covermode=atomic
	go tool cover -func=coverage.txt

.PHONY: fmt
fmt: ## gofmt all source files in place
	gofmt -l -w .

.PHONY: vet
vet: ## go vet ./...
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint if installed, otherwise skip
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; skipping (see https://golangci-lint.run)"; \
	fi

.PHONY: check
check: fmt vet lint test check-spec ## Run fmt, vet, lint, test and check-spec - use before committing

.PHONY: doc
doc: ## Generate Go package reference (go doc -all) as text files under docs/generated
	@rm -rf $(DOC_DIR)
	@mkdir -p $(DOC_DIR)
	@mod=$$(go list -m); \
	for p in $$(go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./... | grep -v -e /internal/ -e /examples/); do \
		rel=$${p#$$mod}; rel=$${rel#/}; out="$(CURDIR)/$(DOC_DIR)/$${rel:-$$(basename $$mod)}.txt"; \
		mkdir -p "$$(dirname "$$out")" && go doc -all "$$p" > "$$out" || exit 1; \
	done
	@echo "package docs written to $(DOC_DIR)"

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: clean
clean: ## Remove build output and the coverage profile
	rm -rf build coverage.txt bin/apikit bin/akc

# ---------------------------------------------------------------------------
# Repository-specific targets
# ---------------------------------------------------------------------------

.PHONY: check-spec
check-spec: ## Validate api/openapi.yaml
	go run github.com/pb33f/libopenapi-validator/cmd/validate@latest api/openapi.yaml

.PHONY: server-reset
server-reset: ## Clear bin/data and bootstrap the server with an admin
	rm -rf bin/data
	mkdir -p bin/data
	cd bin && ./apikit --admin-email=hello@micku.me

.PHONY: server-run
server-run: ## Run bin/apikit using the data created by server-reset
	-mv bin/admin_token bin/token
	cd bin && ADMIN_TOKEN=$$(cat token) ./apikit
