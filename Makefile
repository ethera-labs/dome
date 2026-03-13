TEST_BINARY := bin/dome
DOCKER_IMAGE := dome
DOCKER_TAG := latest

.PHONY: help build test clean run-example run-simple-example deps ensure-config docker-build

# Default target
help:
	@echo "Available targets:"
	@echo "  build           - Build test binary"
	@echo "  test            - Run all tests"
	@echo "  test-verbose    - Run tests with verbose output"
	@echo "  test-info       - Run tests with INFO log level (usage: make test-info TEST_NAME=<test_name>)"
	@echo "  test-debug      - Run tests with DEBUG log level (usage: make test-debug TEST_NAME=<test_name>)"
	@echo "  smoke-test      - Run only smoke tests"
	@echo "  stress-test     - Run only stress tests"
	@echo "  deps            - Download and tidy dependencies"
	@echo "  clean           - Clean build artifacts"
	@echo "  lint            - Run linter"
	@echo "  docker-build    - Build Docker image (usage: make docker-build [DOCKER_TAG=tag])"

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

# Run only smoke tests
smoke-test: build
	@echo "Running smoke tests with INFO log level..."
	LOG_LEVEL=INFO $(TEST_BINARY) -test.v -test.count=1 -test.run="TestMintTokensCrossRollup|TestSendCrossTxBridgeFromAToB|TestSendCrossTxBridgeFromBToA|TestSendOnAAndFailingSelfMoveBalanceOnB|TestSendCrossTxBridgeWithOutOfGasOnB|TestSelfMoveBalanceOnAandreceiveTokensOnB"

# Run only smoke tests
stress-test: build
	@echo "Running stress tests with INFO log level..."
	LOG_LEVEL=INFO $(TEST_BINARY) -test.v -test.count=1 -test.run="TestStressBridgeSameAccount|TestStressBridgeDifferentAccounts|TestStressMultipleAccountsAndMultipleTxs|TestStressAtoBAndBtoA|TestStressNormalTxsMixWithCrossRollupTxs"

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

# Install linter (if not already installed)
install-linter:
	@echo "Installing golangci-lint..."
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.54.2

# Build Docker image
docker-build:
	@echo "Building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	docker build -f build/Dockerfile -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	@echo "Docker image built successfully: $(DOCKER_IMAGE):$(DOCKER_TAG)"
