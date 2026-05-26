# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is a Go-based E2E test framework for blockchain cross-rollup transactions. The project tests cross-chain transaction (XT) functionality between multiple rollup networks using the Compose Sidecar's HTTP API for atomic cross-chain coordination.

## Development Commands

### Building and Testing
```bash
make build          # Build test binary (bin/dome)
make format         # Format code with go fmt
make lint           # Run golangci-lint
make deps           # Download and tidy dependencies
make clean          # Clean build artifacts (removes bin/)
make docker-build   # Build Docker image (dome:latest)
```

### Running Tests

Tests are compiled into a binary (`bin/dome`) and all test targets automatically build this binary if needed. Tests require configuration in `configs/config.yaml` (see Configuration Setup below).

```bash
# Run all tests (automatically builds binary first)
make test

# Run tests with specific log levels
make test-info                           # INFO log level, all tests
make test-info TEST_NAME=TestBridge      # INFO log level, specific test
make test-debug                          # DEBUG log level, all tests
make test-debug TEST_NAME=TestBridge     # DEBUG log level, specific test

# Run specific test suites
make smoke-test                          # Run smoke tests only
make stress-test                         # Run stress tests only

# Run the test binary directly (with embedded config)
./bin/dome -test.v -test.run=TestSendCrossTxBridge
LOG_LEVEL=INFO ./bin/dome -test.v

# Run with external config file
CONFIG_PATH=./configs/config.yaml ./bin/dome -test.v
CONFIG_PATH=/path/to/custom.yaml LOG_LEVEL=DEBUG ./bin/dome -test.v
```

Log levels are controlled via the `LOG_LEVEL` environment variable (DEBUG, INFO).

### Configuration Setup

Configuration supports both embedded and external loading:

**Embedded Config (Default)**: Configuration is embedded at compile time using `//go:embed` from `configs/config.yaml`

**External Config**: Set `CONFIG_PATH` environment variable to load from an external file (ideal for Docker and production)

**Structure:**
```yaml
l2:
  sidecar-url: http://localhost:17090  # Compose Sidecar HTTP API
  chain-configs:
    rollup-a:
      pk: 0x...        # Private key for funded account on rollup-a
      id: 77777        # Chain ID for rollup-a
      rpc-url: http://localhost:17545  # op-rbuilder (builder includes txs in blocks)
    rollup-b:
      pk: 0x...        # Private key for funded account on rollup-b
      id: 88888        # Chain ID for rollup-b
      rpc-url: http://localhost:27545  # op-rbuilder (builder includes txs in blocks)
  contracts:
    bridge:           # ComposeL2ToL2Bridge
      address: 0x...
      abi: ''
    token:            # MockL2ERC20 (standard ERC-20 + mint/burn)
      address: 0x...
      abi: ''
    mailbox:          # UniversalBridgeMailbox
      address: 0x...
      abi: ''
    cet-factory:      # CetFactory.predictAddress(remoteAsset, remoteChainID)
      address: 0x...
      abi: ''
```

**Setup steps:**
1. If `configs/config.yaml` doesn't exist, `make build` will automatically copy it from `configs/config.example.yaml`
2. Populate `configs/config.yaml` with sidecar URL, both rollups' keys/ids/RPCs, and the four contract entries (addresses + ABI JSON). Addresses come from `local-testnet/.localnet/networks/<chain>/contracts.json` after the L2 deploy.
3. Rebuild the binary with `make build` to embed the updated config (for embedded use)
   - OR set `CONFIG_PATH` environment variable to use external config (recommended for Docker/production)

**Validation**: Config validation happens at package init time. The binary will panic on startup if:
- `sidecar-url` is not set
- Both `rollup-a` and `rollup-b` configs are not present
- Any field (`pk`, `id`, `rpc-url`) is missing or zero-valued
- The four contracts (`bridge`, `token`, `mailbox`, `cet-factory`) are not all present
- Any contract address or ABI is empty

## Architecture

### Directory Structure

```
dome/
├── bin/              # Compiled test binary (bin/dome)
├── build/            # Build artifacts
│   └── Dockerfile    # Multi-stage Docker build
├── configs/          # Configuration management
│   ├── config.go     # Config structs, validation, and embed logic
│   ├── config.yaml   # Main config file (gitignored, embedded at compile time)
│   └── config.example.yaml  # Template for config.yaml
├── internal/         # Core framework (private packages)
│   ├── accounts/     # Account management for blockchain interactions
│   ├── helpers/      # Test helper functions (bridge, mint, approve)
│   ├── logger/       # Centralized logging with DEBUG/INFO levels
│   ├── rollup/       # Rollup configuration and connection
│   └── transactions/ # Transaction creation and sidecar submission
└── test/             # Test files
    ├── config.go     # Test setup and shared test variables
    └── *_test.go     # Test implementations
```

### Core Components

**configs/**: Configuration management with hybrid loading (embedded + external)
- Single YAML file defines sidecar URL, both rollup configs, and contract addresses
- Uses `//go:embed` directive to embed config.yaml into the binary as fallback
- Supports external config loading via `CONFIG_PATH` environment variable
- `configs.Values` global variable provides access to parsed config
- Sidecar URL accessed via: `configs.Values.L2.SidecarURL`
- Chain configs accessed via: `configs.Values.L2.ChainConfigs[configs.ChainNameRollupA]`

**internal/transactions/**: Transaction creation and sidecar interaction
- `transactions.go`: Standard Ethereum transaction creation (EIP-1559 dynamic fee)
- `cross_tx.go`: Sidecar HTTP client (`SubmitXT`, `GetXTStatus`, `WaitForDecision`)
- `CreateTransaction()` creates and signs transactions with account's nonce
- `SendTransaction()` sends signed transactions to RPC endpoints (for non-XT operations)
- `GetTransactionDetails()` polls for transaction confirmation with retry intervals

**internal/helpers/**: Test helper functions
- `SendBridgeTx()` / `SendBridgeTxWithNonce()`: Build and submit bridge XTs via sidecar
- `SubmitXTAndWait()`: Generic submit + wait-for-decision helper
- `SendMintTx()` / `ApproveTokens()`: ERC-20 operations (sent as normal txs)
- `SendSelfMoveBalanceTx()`: ETH self-transfer (sent as normal tx)

### Cross-Rollup Transaction Flow (Sidecar)

1. Create and sign separate transactions for each rollup (RollupA and RollupB)
2. Hex-encode signed transaction bytes (0x-prefixed)
3. Build JSON payload: `{ "transactions": { "chainId": ["0x..."], "chainId2": ["0x..."] } }`
4. POST to sidecar `/xt` endpoint, receive `{ "instance_id": "...", "status": "..." }`
5. Poll `GET /xt/:id` until status is `committed` or `aborted`
6. If committed: verify transaction receipts on each chain's RPC
7. If aborted: neither transaction was executed

### Sidecar API Endpoints Used

| Endpoint          | Method | Purpose                              |
|-------------------|--------|--------------------------------------|
| `/xt`             | POST   | Submit a cross-chain transaction     |
| `/xt/:id`         | GET    | Poll XT status (committed/aborted)   |
| `/health`         | GET    | Sidecar liveness check               |

### Test Structure

**test/config.go**:
- Shared test setup with global variables for rollups, accounts, and sidecar URL
- Loads config from `configs.Values` global
- Parses contract ABIs for Bridge, Token, and CetFactory contracts
- `setup()` mints `setupMintAmount` MockL2ERC20 to each main account and approves the bridge so test bodies start from a predictable token balance

**Test Files**:
- `bridge_test.go`: Cross-rollup token bridge tests (mint, transfer A->B, B->A, failure scenarios)
- `smoke_test.go`: TestMain entry point
- `stress_test.go`: Load and stress testing (same account, different accounts, bidirectional, mixed)
- `uncorrelated_tx_test.go`: Independent transaction failure tests

## Key Technical Details

### Transaction Types
- All transactions use EIP-1559 dynamic fee structure (`DynamicFeeTx`)
- Nonces are managed via `PendingNonceAt()` to handle concurrent transactions
- Gas parameters (GasTipCap, GasFeeCap, Gas) come from `internal/helpers/gas.go` constants per call type (mint / approve / bridgeERC20To / receiveTokens / native)

### Wrapped-CET on the destination chain

`ComposeL2ToL2Bridge.receiveTokens` mints a deterministic wrapper-CET (predicted by
`CetFactory.predictAddress(sourceToken, sourceChainID)`) instead of the destination's
original ERC-20. Destination-side balance assertions therefore must look up the CET
address via `helpers.PredictCetAddress` and read balanceOf at that address. The source
ERC-20 stays escrowed in the bridge.

### XT Submission Format
Cross-rollup transactions use the sidecar's JSON HTTP API:
```json
{
  "transactions": {
    "77777": ["0x<rlp-encoded-signed-tx>"],
    "88888": ["0x<rlp-encoded-signed-tx>"]
  }
}
```
Keys are chain IDs as strings, values are arrays of 0x-prefixed hex-encoded signed transactions.

### Local-Testnet Port Mappings
| Service         | Chain A | Chain B |
|-----------------|---------|---------|
| op-geth RPC     | 18545   | 28545   |
| op-rbuilder RPC | 17545   | 27545   |
| Sidecar API     | 17090   | 27090   |
| Blockscout      | 19000   | 29000   |

## Module Path
`github.com/ethera-labs/dome`

## Go Version
1.25
