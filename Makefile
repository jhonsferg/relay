# Relay - Makefile
# Centralise common tasks for development and CI

.PHONY: all fmt lint test tidy clean help setup

# Default target: check format and run tests
all: fmt lint tidy test

# Setup: install development tools
setup:
	@echo "Installing development tools..."
	go install github.com/evilmartians/lefthook@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	lefthook install

# Formatter: apply gofmt -s -w recursively
fmt:
	@echo "Formatting code..."
	gofmt -s -w .

# Linter: run golangci-lint on the whole project
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Tests: run all tests in the workspace
test:
	@echo "Running tests..."
	go test -v -cover ./...

# Tidy: clean up module dependencies
tidy:
	@echo "Cleaning up go.mod files..."
	go mod tidy
	@for dir in ext/*; do \
		if [ -d "$$dir" ]; then \
			(cd "$$dir" && go mod tidy); \
		fi \
	done

# Clean: remove build artefacts and test caches
clean:
	@echo "Cleaning up..."
	go clean -i ./...
	go clean -testcache

# Help: display available targets
help:
	@echo "Available Makefile targets:"
	@echo "  setup  : install dev tools and setup git hooks"
	@echo "  fmt    : format all go files"
	@echo "  lint   : run static analysis"
	@echo "  test   : run all tests"
	@echo "  tidy   : tidy all go.mod files"
	@echo "  clean  : remove build/test artefacts"
	@echo "  all    : format, lint, tidy and test"
