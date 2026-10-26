.DEFAULT_GOAL := help

GO ?= $(or $(shell sh scripts/find-go.sh),go)
GOFMT ?= $(shell go_path=$$(command -v "$(GO)" 2>/dev/null); \
	if [ -n "$$go_path" ] && [ -x "$$(dirname "$$go_path")/gofmt" ]; then \
		printf '%s/gofmt\n' "$$(dirname "$$go_path")"; \
	else printf '%s\n' gofmt; fi)
PYTHON ?= python3
LOG ?= pastetrail.jsonl
RUN_ARGS ?=
TEST_FLAGS ?=
export CGO_ENABLED ?= 1

ifeq ($(OS),Windows_NT)
BINARY := bin/pastetrail.exe
else
BINARY := bin/pastetrail
endif

.PHONY: help build run doctor test test-race test-integration test-tools coverage coverage-check fmt fmt-check vet check package clean require-go require-gofmt

help:
	@printf '%s\n' \
	  'make build             Build bin/pastetrail' \
	  'make run               Build and run the daemon; log to pastetrail.jsonl' \
	  'make doctor            Build and report platform capabilities' \
	  'make test              Run Go tests' \
	  'make test-race         Run Go tests with the race detector' \
	  'make test-integration  Run Go tests including Linux X11 integration' \
	  'make test-tools        Run Python packaging and coverage tests' \
	  'make coverage          Generate coverage.out and coverage.html' \
	  'make coverage-check    Generate coverage and enforce the CI threshold' \
	  'make fmt               Format Go source files' \
	  'make fmt-check         Report unformatted Go source files' \
	  'make vet               Run Go diagnostics' \
	  'make check             Check formatting, vet, Go tests, and Python tests' \
	  'make package           Build the native release archive in dist/' \
	  'make clean             Remove build, release, and coverage artifacts' \
	  '' \
	  'Overrides: GO, GOFMT, PYTHON, LOG, RUN_ARGS, TEST_FLAGS, CGO_ENABLED'

build test test-race test-integration coverage vet package: | require-go
fmt fmt-check: | require-gofmt

require-go:
	@command -v "$(GO)" >/dev/null 2>&1 || { \
	  printf '%s\n' 'Go was not found. Install Go 1.26+ or use make GO=/path/to/go <target>.' >&2; \
	  exit 1; \
	}

require-gofmt:
	@command -v "$(GOFMT)" >/dev/null 2>&1 || { \
	  printf '%s\n' 'gofmt was not found. Install Go 1.26+ or use make GOFMT=/path/to/gofmt <target>.' >&2; \
	  exit 1; \
	}

build:
	mkdir -p bin
	"$(GO)" build -o "$(BINARY)" ./cmd/pastetrail

run: build
	exec "./$(BINARY)" daemon --log "$(LOG)" $(RUN_ARGS)

doctor: build
	"./$(BINARY)" doctor

test:
	"$(GO)" test $(TEST_FLAGS) ./...

test-race:
	"$(GO)" test -race $(TEST_FLAGS) ./...

test-integration:
	@pastetrail_os=$$("$(GO)" env GOOS) || exit $$?; \
	if [ "$$pastetrail_os" != linux ]; then \
	  printf '%s\n' 'X11 integration tests require Linux and the dependencies listed in README.md.' >&2; \
	  exit 1; \
	fi
	"$(GO)" test -tags=integration $(TEST_FLAGS) ./...

test-tools:
	"$(PYTHON)" -m unittest discover -s scripts -p 'test_*.py'

coverage:
	"$(GO)" test $(TEST_FLAGS) -coverpkg=./... -coverprofile=coverage.out ./...
	"$(GO)" tool cover -func=coverage.out
	"$(GO)" tool cover -html=coverage.out -o coverage.html

coverage-check: coverage
	"$(PYTHON)" scripts/coverage.py coverage.out

fmt:
	"$(GOFMT)" -w .

fmt-check:
	@unformatted=$$("$(GOFMT)" -l .) || exit $$?; \
	if [ -n "$$unformatted" ]; then \
	  printf 'Go files need formatting:\n%s\n' "$$unformatted"; \
	  exit 1; \
	fi

vet:
	"$(GO)" vet ./...

check: fmt-check vet test test-tools

package:
	GO="$(GO)" "$(PYTHON)" scripts/package.py

clean:
	rm -rf bin dist coverage.out coverage.html
