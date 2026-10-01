# Development tasks for go-za.
#
# Requires GNU Make and a POSIX shell (on Windows, the one shipped with
# Git for Windows works).

GO      ?= go
EXE     := $(shell $(GO) env GOEXE)
BIN     := za$(EXE)
PKGS    := ./...
COVER   := coverage.out

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  build        Build ./$(BIN)"
	@echo "  install      Install za into GOBIN"
	@echo "  test         Run the unit test suite"
	@echo "  race         Run the unit test suite with the race detector (needs cgo)"
	@echo "  integration  Run tests against the real git, go and uv"
	@echo "  cover        Run tests with coverage and print a summary"
	@echo "  golden       Regenerate scaffold golden files"
	@echo "  fmt          Format all Go files"
	@echo "  fmt-check    Fail if any Go file is not gofmt-formatted"
	@echo "  vet          Run go vet"
	@echo "  tidy         Tidy go.mod"
	@echo "  check        fmt-check + vet + test + build (run before committing)"
	@echo "  clean        Remove build and coverage artifacts"

.PHONY: build
build:
	$(GO) build -trimpath -o $(BIN) ./cmd/za

.PHONY: install
install:
	$(GO) install -trimpath ./cmd/za

.PHONY: test
test:
	$(GO) test $(PKGS)

.PHONY: race
race:
	$(GO) test -race $(PKGS)

.PHONY: integration
integration: export ZA_INTEGRATION = 1
integration:
	$(GO) test -count=1 -run TestRealTools -v ./internal/initcmd

.PHONY: cover
cover:
	$(GO) test -coverprofile=$(COVER) $(PKGS)
	$(GO) tool cover -func=$(COVER)

.PHONY: golden
golden:
	$(GO) test ./internal/scaffold -update
	$(GO) test ./internal/scaffold

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Files not formatted with gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: vet
vet:
	$(GO) vet $(PKGS)

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: check
check: fmt-check vet test build

.PHONY: clean
clean:
	$(RM) $(BIN) $(COVER)
