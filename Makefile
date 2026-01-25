.PHONY: help test test-short test-coverage test-coverage-func test-coverage-html test-integration test-e2e test-all test-race test-failfast benchmark lint clean install-tools

# Default target
help:
	@echo "Email Server Test Orchestration"
	@echo "================================"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Test Targets:"
	@echo "  test               - Run all unit tests with verbose output"
	@echo "  test-short         - Run unit tests, skip long-running tests"
	@echo "  test-coverage      - Run all tests and generate coverage report"
	@echo "  test-coverage-html - Generate HTML coverage report"
	@echo "  test-coverage-func - Show coverage by function"
	@echo "  test-integration   - Run integration tests only"
	@echo "  test-e2e           - Run end-to-end tests only"
	@echo "  test-all           - Run all tests: unit + integration + e2e"
	@echo "  test-race          - Run tests with race detector"
	@echo "  test-failfast      - Run tests, stop on first failure"
	@echo ""
	@echo "Performance Targets:"
	@echo "  benchmark          - Run benchmarks"
	@echo ""
	@echo "Code Quality Targets:"
	@echo "  lint               - Run vet and staticcheck"
	@echo ""
	@echo "Utility Targets:"
	@echo "  install-tools      - Install required tools"
	@echo "  clean              - Clean test artifacts"

# Run all unit tests
test:
	go test -v ./...

# Run unit tests only, skip long-running integration and e2e tests
test-short:
	go test -v -short ./...

# Run all tests and generate coverage report
test-coverage:
	go test -v -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "Coverage report generated: coverage.out"

# Generate HTML coverage report
test-coverage-html: test-coverage
	go tool cover -html=coverage.out -o coverage.html
	@echo "HTML coverage report generated: coverage.html"
	@echo "Open in browser: file://$(PWD)/coverage.html"

# Show coverage by function
test-coverage-func: test-coverage
	@echo ""
	@echo "Coverage Summary:"
	@echo "================="
	go tool cover -func=coverage.out | tail -1

# Run only integration tests
test-integration:
	go test -v -timeout=5m ./tests/integration/...
	@echo ""
	@echo "Integration tests completed"

# Run only end-to-end tests
test-e2e:
	go test -v -timeout=10m ./tests/e2e/...
	@echo ""
	@echo "End-to-end tests completed"

# Run all tests: unit, integration, and e2e
test-all: test test-integration test-e2e
	@echo ""
	@echo "All tests completed successfully"

# Run tests with race detector
test-race:
	go test -v -race ./...
	@echo ""
	@echo "Race detector tests completed"

# Run tests with failfast (stop on first failure)
test-failfast:
	go test -v -failfast ./...
	@echo ""
	@echo "Tests completed"

# Run benchmarks
benchmark:
	go test -v -run=^$$ -bench=. -benchmem ./...
	@echo ""
	@echo "Benchmarks completed"

# Run code quality checks
lint:
	@echo "Running go vet..."
	go vet ./...
	@echo ""
	@echo "Running staticcheck (if installed)..."
	@which staticcheck > /dev/null && staticcheck ./... || echo "staticcheck not installed, skipping"
	@echo ""
	@echo "Linting completed"

# Install development tools
install-tools:
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo ""
	@echo "Development tools installed"

# Clean test artifacts
clean:
	rm -f coverage.out coverage.html
	go clean -testcache
	@echo "Test artifacts cleaned"

# Development workflow target - commonly used during development
dev: test-short lint
	@echo ""
	@echo "Development checks completed"

# CI workflow target - run everything
ci: lint test-coverage test-integration test-e2e
	@echo ""
	@echo "CI pipeline completed successfully"

# Quick validation target
quick: test-short
	@echo ""
	@echo "Quick validation completed"

# Verbose integration test run
test-integration-verbose:
	go test -v -timeout=10m -run TestDatabaseIntegration -timeout=30s ./tests/integration/...
	go test -v -timeout=10m -run TestSMTPtoIMAP -timeout=30s ./tests/integration/...
	go test -v -timeout=10m -run TestAdminAPI -timeout=30s ./tests/integration/...
	go test -v -timeout=10m -run TestQueueDelivery -timeout=30s ./tests/integration/...

# Build the project
build:
	go build -v ./...
	@echo ""
	@echo "Build completed successfully"

# Run build and all checks
all: clean install-tools lint build test-all
	@echo ""
	@echo "Complete build and test cycle completed successfully"
