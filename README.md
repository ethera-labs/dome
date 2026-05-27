# Dome

A Go-based E2E test framework for cross-rollup blockchain transactions. Tests cross-chain transaction (XT) functionality
between multiple rollup networks using the Compose Sidecar's HTTP API for atomic cross-chain coordination.

## Features

- **Cross-Rollup Transactions**: Test atomic transaction execution across multiple rollups via Compose Sidecar
- **Sidecar HTTP API**: Submits XTs as JSON to the sidecar's `/xt` endpoint and polls for decisions
- **Embedded Configuration**: Config is embedded at compile time for self-contained binaries
- **Test Binary**: Compiles tests into a standalone executable for easy distribution
- **Docker Support**: Production-ready multi-stage Dockerfile for containerized deployments

## Quick Start

### 1. Install Dependencies

```bash
make deps
```

### 2. Build Test Binary

```bash
make build
```

This will:

- Create `bin/dome` test binary
- Auto-generate `configs/config.yaml` from `configs/config.example.yaml` if it doesn't exist
- Embed the config into the binary

### 3. Configure

Edit `configs/config.yaml` with your rollup and sidecar details:

```yaml
l2:
  sidecar-url: http://localhost:17090  # Compose Sidecar HTTP API

  chain-configs:
    rollup-a:
      pk: 0000...  # Private key for funded account
      id: 77777    # Chain ID
      rpc-url: http://localhost:18545

    rollup-b:
      pk: 0000...  # Private key for funded account
      id: 88888    # Chain ID
      rpc-url: http://localhost:28545

  contracts:
    bridge: # ComposeL2ToL2Bridge (bridgeERC20To + receiveTokens)
      address: 0x...
      abi: '[...]'
    token: # MockL2ERC20 (standard ERC-20 surface + mint/burn)
      address: 0x...
      abi: '[...]'
    mailbox: # UniversalBridgeMailbox (write/readMessage; sourced for diagnostics)
      address: 0x...
      abi: '[...]'
    cet-factory: # CetFactory.predictAddress(...) - wrapped CET lookup on the destination side
      address: 0x...
      abi: '[...]'
```

After editing config, rebuild to embed changes:

```bash
make build
```

### 4. Run Tests

```bash
# Run all tests
make test

# Run with INFO logging
make test-info

# Run with DEBUG logging
make test-debug

# Run specific test
make test-info TEST_NAME=TestSendCrossTxBridge

# Run specific test suites
make smoke-test    # Smoke tests
make stress-test   # Stress tests
```

Run the binary directly:

```bash
# Use embedded config
./bin/dome -test.v -test.run=TestSendCrossTxBridge
LOG_LEVEL=INFO ./bin/dome -test.v

# Use external config file
CONFIG_PATH=./configs/config.yaml ./bin/dome -test.v
CONFIG_PATH=/path/to/custom-config.yaml LOG_LEVEL=DEBUG ./bin/dome -test.v
```

## Project Structure

```
dome/
├── bin/              # Compiled test binary (bin/dome)
├── build/            # Build artifacts
│   └── Dockerfile    # Multi-stage Docker build
├── configs/          # Configuration management
│   ├── config.go                 # Config structs, validation, embed logic
│   ├── config.yaml               # Main config (gitignored, embedded at compile time)
│   └── config.example.yaml       # Template for config.yaml
├── internal/         # Core framework (private packages)
│   ├── accounts/     # Account management for blockchain interactions
│   ├── helpers/      # Test helper functions (bridge, mint, approve)
│   ├── logger/       # Centralized logging (DEBUG/INFO levels)
│   ├── rollup/       # Rollup configuration and connection
│   └── transactions/ # Transaction creation and sidecar submission
└── test/             # Test files
    ├── config.go     # Test setup and shared variables
    └── *_test.go     # Test implementations
```

## How It Works

### Cross-Rollup Transaction Flow

1. **Create Transactions**: Sign separate transactions for each rollup (RollupA, RollupB)
2. **Submit XT**: POST signed transactions as JSON to sidecar's `/xt` endpoint
3. **Wait for Decision**: Poll `GET /xt/:id` until the sidecar returns committed or aborted
4. **Verify**: If committed, check transaction receipts on each chain's RPC

### Wrapped-CET Token Semantics

`ComposeL2ToL2Bridge.receiveTokens` mints a **deterministic wrapped-CET** on the destination
chain (predicted from `(sourceToken, sourceChainID)` via `CetFactory.predictAddress`) rather than
crediting the destination's original ERC-20. Destination-side balance assertions therefore
read the CET balance via `helpers.PredictCetAddress`, not the source token. The source-side
ERC-20 is locked in the bridge contract for the lifetime of the bridged supply.

### Gas Budgets

Per-call defaults live in `internal/helpers/gas.go`, sized against measured worst-case
plus margin for mailbox-root recomputation under bursty load:

| Constant             | Value     | Used for                                    |
|----------------------|-----------|---------------------------------------------|
| `GasMint`            | 200,000   | `MockL2ERC20.mint`                          |
| `GasApprove`         | 200,000   | `MockL2ERC20.approve`                       |
| `GasNativeTransfer`  | 50,000    | EOA self-/cross-transfer                    |
| `GasBridgeERC20To`   | 800,000   | source-side `bridgeERC20To`                 |
| `GasBridgeReceive`   | 1,500,000 | destination-side `receiveTokens`            |
| `GasBridgeReceiveLo` | 200,000   | intentionally-OOG receive (abort scenarios) |

### Sidecar API

| Endpoint  | Method | Purpose                            |
|-----------|--------|------------------------------------|
| `/xt`     | POST   | Submit cross-chain transaction     |
| `/xt/:id` | GET    | Poll XT status (committed/aborted) |
| `/health` | GET    | Sidecar liveness check             |

### Configuration

Configuration supports both embedded and external loading for maximum flexibility:

**Embedded Config (Default)**: Uses Go's `//go:embed` directive to embed `configs/config.yaml` at compile time.

**External Config (Recommended for Production)**: Set `CONFIG_PATH` environment variable to load config from external
file.

**Loading Priority**:

1. Check `CONFIG_PATH` environment variable
2. If set, try to load from that path
3. If not set, use embedded config
4. Panic if config is invalid

### Local-Testnet Port Mappings

| Service         | Chain A | Chain B |
|-----------------|---------|---------|
| op-geth RPC     | 18545   | 28545   |
| op-rbuilder RPC | 17545   | 27545   |
| Sidecar API     | 17090   | 27090   |

## Development Commands

```bash
make build          # Build test binary
make test           # Run all tests
make test-info      # Run with INFO logging
make test-debug     # Run with DEBUG logging
make smoke-test     # Run smoke tests only
make stress-test    # Run stress tests only
make format         # Format code
make lint           # Run linter
make clean          # Clean build artifacts
make deps           # Download dependencies
make docker-build   # Build Docker image
```

## Docker Deployment

```bash
# Build
make docker-build

# Run with custom config (recommended)
docker run --rm \
  -v $(pwd)/configs/config.yaml:/app/config.yaml \
  -e CONFIG_PATH=/app/config.yaml \
  dome:latest -test.v -test.run=TestSendCrossTxBridge

# Run with DEBUG logging
docker run --rm \
  -v $(pwd)/configs/config.yaml:/app/config.yaml \
  -e CONFIG_PATH=/app/config.yaml \
  -e LOG_LEVEL=DEBUG \
  dome:latest -test.v
```

## Dependencies

- **go-ethereum**: Ethereum client library for RPC and transaction handling
- **gopkg.in/yaml.v3**: Configuration parsing
- **testify**: Test assertions
