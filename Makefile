TEST_BINARY := bin/dome
DOCKER_IMAGE := dome
DOCKER_TAG := latest

.PHONY: help build test clean run-example run-simple-example deps ensure-config docker-build scripts-install full-test-prod-sepolia full-test-prod-hoodi full-test-stage-sepolia

# Default target
help:
	@echo "Available targets:"
	@echo "  build           - Build test binary"
	@echo "  test            - Run all tests"
	@echo "  test-verbose    - Run tests with verbose output"
	@echo "  test-info       - Run tests with INFO log level (usage: make test-info TEST_NAME=<test_name>)"
	@echo "  test-debug      - Run tests with DEBUG log level (usage: make test-debug TEST_NAME=<test_name>)"
	@echo "  test-localnet   - Run tests with local-testnet Docker log capture (usage: make test-localnet [TEST_NAME=<test_name>])"
	@echo "  deps            - Download and tidy dependencies"
	@echo "  clean           - Clean build artifacts"
	@echo "  lint            - Run linter"
	@echo "  lint-fix        - Run linter and auto-fix issues"
	@echo "  docker-build    - Build Docker image (usage: make docker-build [DOCKER_TAG=tag])"
	@echo "  scripts-install - Install Node deps for scripts/ (needed for xt-submission=rpc and SA tests)"
	@echo ""
	@echo "Full-suite runners (per environment) — runs every test file one by one,"
	@echo "saves all output to <env>.log, prints a pass/fail summary table:"
	@echo "  full-test-prod-sepolia   - configs/config.sepolia-prod.yaml  → prod-sepolia.log"
	@echo "  full-test-prod-hoodi     - configs/config.hoodi.yaml         → prod-hoodi.log"
	@echo "  full-test-stage-sepolia  - configs/config.sepolia-stage.yaml → stage-sepolia.log"
	@echo "    (pass NO_STRESS=1 to any of the above to skip the stress group)"

# Ensure config.yaml exists (create from example if needed)
ensure-config:
	@if [ ! -f configs/config.yaml ]; then \
		echo "config.yaml not found, copying from config.example.yaml..."; \
		cp configs/config.example.yaml configs/config.yaml; \
	fi

# Build test binary
build: ensure-config
	@echo "Building test binary..."
	@mkdir -p bin
	go test -c ./test/ -o $(TEST_BINARY)
	@echo "Test binary created at: $(TEST_BINARY)"

# Format the project
format: ensure-config
	@echo "Formatting project..."
	go fmt ./...

# Run tests
test: build
	@echo "Running tests..."
	$(TEST_BINARY) -test.count=1

# Run tests with verbose output
test-verbose: build
	@echo "Running tests with verbose output..."
	$(TEST_BINARY) -test.v -test.count=1

# Run tests with INFO log level
test-info: build
	@if [ -z "$(TEST_NAME)" ]; then \
		echo "Running all tests with INFO log level..."; \
		LOG_LEVEL=INFO $(TEST_BINARY) -test.v -test.count=1; \
	else \
		echo "Running test '$(TEST_NAME)' with INFO log level..."; \
		LOG_LEVEL=INFO $(TEST_BINARY) -test.v -test.count=1 -test.run=$(TEST_NAME); \
	fi

# Run tests with DEBUG log level
test-debug: build
	@if [ -z "$(TEST_NAME)" ]; then \
		echo "Running all tests with DEBUG log level..."; \
		LOG_LEVEL=DEBUG $(TEST_BINARY) -test.v -test.count=1; \
	else \
		echo "Running test '$(TEST_NAME)' with DEBUG log level..."; \
		LOG_LEVEL=DEBUG $(TEST_BINARY) -test.v -test.count=1 -test.run=$(TEST_NAME); \
	fi

# Run tests with local-testnet Docker log capture alongside test output.
# Override CONTAINERS env var (space-separated) to target different container names.
test-localnet: build
	@./scripts/test-with-localnet-logs.sh $(TEST_NAME)

# Run the test binary against a per-network config (no embedded config needed).
#
# Filter selection (one of):
#   TEST_NAME=<regex>      raw -test.run regex                (e.g. "^TestL2ToL2_ETH_AtoB$")
#   TEST_FILE=<file>       short filename, expands to the Test* functions defined in it
#                          (e.g. "l2_to_l2_eth_test" runs everything in test/l2_to_l2_eth_test.go)
#
# Direction + amount overrides (consumed by helpers.ApplyDirectionFilter and
# ParseBridgeAmountOverride inside the tests):
#   SOURCE=a|b|l1          BRIDGE_SOURCE
#   DEST=a|b|l1            BRIDGE_DEST
#   AMOUNT=<eth>           BRIDGE_AMOUNT (decimal ETH, e.g. 0.01)
#   AMOUNT_WEI=<wei>       BRIDGE_AMOUNT_WEI (raw wei, takes precedence over AMOUNT)
test-hoodi: build
	@$(MAKE) _run-tests CONFIG=configs/config.hoodi.yaml

test-sepolia-prod: build
	@$(MAKE) _run-tests CONFIG=configs/config.sepolia-prod.yaml

test-sepolia-stage: build
	@$(MAKE) _run-tests CONFIG=configs/config.sepolia-stage.yaml

# Internal helper. Builds the -test.run regex from TEST_NAME or TEST_FILE and
# forwards SOURCE/DEST/AMOUNT/AMOUNT_WEI as env vars.
.PHONY: _run-tests
_run-tests:
	@if [ -n "$(TEST_FILE)" ]; then \
		FILE="test/$(TEST_FILE).go"; \
		if [ ! -f "$$FILE" ]; then echo "no such test file: $$FILE"; exit 1; fi; \
		TESTS=$$(grep -oE 'func Test[A-Za-z0-9_]+' "$$FILE" | sed 's/^func //' | sort -u | paste -sd'|' -); \
		if [ -z "$$TESTS" ]; then echo "no Test* functions in $$FILE"; exit 1; fi; \
		PATTERN="^($$TESTS)$$"; \
	elif [ -n "$(TEST_NAME)" ]; then \
		PATTERN="$(TEST_NAME)"; \
	else \
		PATTERN=".*"; \
	fi; \
	echo "Running tests against $(CONFIG) (pattern: $$PATTERN)"; \
	CONFIG_PATH=$(CURDIR)/$(CONFIG) LOG_LEVEL=INFO \
		$(if $(SOURCE),BRIDGE_SOURCE=$(SOURCE)) \
		$(if $(DEST),BRIDGE_DEST=$(DEST)) \
		$(if $(AMOUNT),BRIDGE_AMOUNT=$(AMOUNT)) \
		$(if $(AMOUNT_WEI),BRIDGE_AMOUNT_WEI=$(AMOUNT_WEI)) \
		$(TEST_BINARY) -test.v -test.count=1 -test.run="$$PATTERN"

# Full-suite runners: run every test file one by one against a remote network,
# capture all output to <env>.log, and print a per-file pass/fail summary.
# A file is FAIL if at least one test inside printed `--- FAIL:`.
#
# Pass NO_STRESS=1 to skip the entire Stress group, e.g.
#   make full-test-prod-sepolia NO_STRESS=1
FULL_TEST_FLAGS := $(if $(NO_STRESS),--no-stress)

full-test-prod-sepolia: build
	@./scripts/full-test-suite.sh $(FULL_TEST_FLAGS) prod-sepolia

full-test-prod-hoodi: build
	@./scripts/full-test-suite.sh $(FULL_TEST_FLAGS) prod-hoodi

full-test-stage-sepolia: build
	@./scripts/full-test-suite.sh $(FULL_TEST_FLAGS) stage-sepolia

# Install Node dependencies for scripts/ — required once before running any
# test that uses xt-submission=rpc (hoodi, sepolia-prod) or smart-account
# flows. scripts/encode-xt.ts and scripts/sa-helper.ts shell out to npx
# ts-node from this directory.
scripts-install:
	@echo "Installing Node deps in scripts/..."
	cd scripts && npm install --legacy-peer-deps

# Download and tidy dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	go clean

# Run linter
lint: ensure-config
	@echo "Running linter..."
	golangci-lint run -v

# Run linter and auto-fix issues
lint-fix: ensure-config
	@echo "Running linter with auto-fix..."
	golangci-lint run --fix

# Install linter (if not already installed)
install-linter:
	@echo "Installing golangci-lint..."
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin latest

# Build Docker image
docker-build:
	@echo "Building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	docker build -f build/Dockerfile -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	@echo "Docker image built successfully: $(DOCKER_IMAGE):$(DOCKER_TAG)"
