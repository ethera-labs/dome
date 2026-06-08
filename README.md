# Dome

A Go-based E2E test framework for cross-rollup transactions on the Compose stack. Drives L2↔L2,
L1↔L2, and L2→L1 bridge flows for both EOA and ERC-4337 smart-account senders against either a
local testnet or one of three remote networks (hoodi, sepolia-prod, sepolia-stage).

Two cross-chain submission modes:

- **Sidecar REST** (`xt-submission: sidecar`) — POSTs `{transactions: {chainId: [signedTx]}}`
  to the Compose Sidecar's `/xt` endpoint and polls instance status. Used by `local` and
  `sepolia-stage`.
- **Compose sequencer RPC** (`xt-submission: rpc`) — calls `eth_sendXTransaction` on the source
  rollup with an SDK-encoded payload. Used by `hoodi` and `sepolia-prod`. Encoding is done by a
  thin TypeScript helper (`scripts/encode-xt.ts`) that Go shells out to.

Smart-account tests rely on ZeroDev Kernel v3.1 with multichain ECDSA signing — that signing
can't be reimplemented in Go, so Go shells out to `scripts/sa-helper.ts` for `create-account` /
`create-userops` / `compose-and-submit`. Everything else (signing standard txs, building
`handleOps` packed UserOps, sidecar submission, receipt polling, balance assertions) stays in
Go.

## Quick Start

### 1. Install dependencies

```bash
make deps             # Go modules
make scripts-install  # Node deps in scripts/ — required for rpc-mode and SA tests
```

### 2. Build the test binary

```bash
make build
```

This auto-generates `configs/config.yaml` from `configs/config.example.yaml` if it doesn't exist,
then compiles `bin/dome` (the test binary with `configs/config.yaml` embedded).

### 3. Pick a network

Four configs ship with the repo:

| File                                 | Network         | XT submission |
|--------------------------------------|-----------------|---------------|
| `configs/config.example.yaml`        | `local`         | sidecar       |
| `configs/config.hoodi.yaml`          | `hoodi`         | rpc           |
| `configs/config.sepolia-prod.yaml`   | `sepolia-prod`  | rpc           |
| `configs/config.sepolia-stage.yaml`  | `sepolia-stage` | sidecar       |

For local-testnet runs, edit `configs/config.yaml` with your sidecar URL, both rollups' keys/ids/
RPCs, and the contract addresses. For remote networks, the per-network YAMLs are pre-filled.

### 4. Run tests

```bash
# Local-testnet (uses the embedded config.yaml)
make test                                # all tests
make test-info TEST_NAME=TestBridge      # specific test
make smoke-test                          # smoke suite
make stress-test                         # stress suite

# Remote networks (no rebuild needed — CONFIG_PATH switches at runtime)
make test-hoodi          TEST_NAME='^TestL2ToL2_ETH_AtoB$'
make test-sepolia-prod   TEST_FILE=l2_to_l2_eth_test
make test-sepolia-stage  TEST_FILE=l2_to_l2_sa_new_token_test SOURCE=a DEST=b
```

## Test categories

44 test functions across 15 files, all under `test/`:

| File                                         | What it does                                                    |
|----------------------------------------------|-----------------------------------------------------------------|
| `bridge_test.go`                             | Original L2↔L2 token bridge (mint, A↔B, abort scenarios)        |
| `stress_test.go`, `uncorrelated_tx_test.go`  | Original local-testnet stress and uncorrelated XT tests         |
| `xt_*_test.go`                               | Original XT race/state-drift scenarios                          |
| `l2_to_l2_eth_test.go`                       | L2↔L2 ETH bridge (composed XT)                                  |
| `l2_to_l2_new_token_test.go`                 | Deploy token + L2↔L2 token bridge (composed XT)                 |
| `l2_to_l2_existing_token_test.go`            | Two-phase: deploy once, bridge repeatedly                       |
| `l2_to_l2_manual_test.go`                    | Two-step (non-atomic) L2→L2; needs coordinator-relayed mailbox  |
| `l1_to_l2_eth_test.go`                       | L1→L2 ETH deposit (ComposeL1Bridge)                             |
| `l1_to_l2_new_token_test.go`                 | L1→L2 token deposit (deploys CET on first call)                 |
| `l1_to_l2_existing_token_test.go`            | Two-phase L1→L2 token                                           |
| `l1_to_l2_eth_stress_test.go`                | N derived accounts each deposit ETH in parallel                 |
| `l1_to_l2_new_token_stress_test.go`          | N derived accounts each deposit tokens in parallel              |
| `l2_to_l1_eth_test.go`                       | L2→L1 ETH withdrawal — two-phase (`_Withdraw` / `_Finalize`)    |
| `l2_to_l1_token_test.go`                     | L2→L1 token round-trip — two-phase                              |
| `l2_to_l2_sa_eth_test.go`                    | Smart-account L2↔L2 ETH                                         |
| `l2_to_l2_sa_new_token_test.go`              | Smart-account L2↔L2 token (transfers CET to EOA on dest)        |
| `l2_to_l2_sa_existing_token_test.go`         | Two-phase smart-account L2↔L2 token                             |
| `l2_to_l2_sa_existing_token_stress_test.go`  | Smart-account stress: N-SA multi and same-SA modes              |

Tests that require config the active network doesn't have (e.g. no `l1` section, no `aa` section)
skip cleanly with a clear reason rather than failing.

## Runtime knobs

The per-network Makefile targets accept:

| Variable    | Effect                                                                       |
|-------------|------------------------------------------------------------------------------|
| `TEST_FILE` | Short test filename (e.g. `l2_to_l2_eth_test`). Expands to a `-test.run` regex matching every `Test*` function defined in that file. |
| `TEST_NAME` | Raw `-test.run` regex (e.g. `^TestL2ToL2_ETH_AtoB$`).                       |
| `SOURCE`    | Source side (`a`, `b`, or `l1`). Tests whose direction doesn't match skip.   |
| `DEST`      | Destination side (`a`, `b`, or `l1`). Same skip-on-mismatch behavior.        |
| `AMOUNT`    | Amount in ETH for ETH tests (e.g. `0.01`) or tokens for token tests (e.g. `25`). Multiplied by 10^18 internally. |
| `AMOUNT_WEI`| Raw wei override. Takes precedence over `AMOUNT`.                            |

Examples:

```bash
# L2->L2 ETH, A->B, 0.005 ETH
make test-sepolia-stage TEST_FILE=l2_to_l2_eth_test SOURCE=a DEST=b AMOUNT=0.005

# L2->L2 new-token via smart account, A->B, 25 tokens
make test-sepolia-prod TEST_FILE=l2_to_l2_sa_new_token_test SOURCE=a DEST=b AMOUNT=25

# L1->L2 deposit, dest = RollupA only
make test-hoodi TEST_FILE=l1_to_l2_eth_test DEST=a

# Just one test, raw regex
make test-hoodi TEST_NAME='^TestL2ToL2_ETH_AtoB$'

# Stress test with a custom account count
BRIDGE_STRESS_ACCOUNTS=50 make test-hoodi TEST_FILE=l1_to_l2_eth_stress_test
```

## Project structure

```
dome/
├── bin/                # Compiled test binary (bin/dome)
├── build/Dockerfile    # Multi-stage Docker build
├── configs/            # YAML configs (one per network) + embed glue
│   ├── config.go
│   ├── config.yaml             # gitignored, embedded at compile time
│   ├── config.example.yaml
│   ├── config.hoodi.yaml
│   ├── config.sepolia-prod.yaml
│   └── config.sepolia-stage.yaml
├── internal/           # Framework packages
│   ├── accounts/       # Account management
│   ├── helpers/        # Tx builders, polling, ABIs, SA helper, withdrawal proof
│   ├── logger/         # DEBUG/INFO logger
│   ├── rollup/         # Rollup descriptor
│   └── transactions/   # CreateTransaction, SendTransaction, SubmitXT
├── scripts/            # TypeScript helpers + reference TS scripts
│   ├── encode-xt.ts    # Encodes eth_sendXTransaction payload (rpc mode)
│   ├── sa-helper.ts    # ERC-4337 SA: create-account, create-userops, compose-and-submit
│   ├── package.json, tsconfig.json
│   └── *.ts            # Original TS test scripts (kept for reference)
└── test/               # Go test files (44 tests across 15 files)
```

## How it works

### Cross-rollup transaction flow

**Sidecar mode** (`local`, `sepolia-stage`):

1. Sign tx per chain in Go.
2. POST `{transactions: {chainId: [hex…]}}` to `<sidecar>/xt`. Sidecar returns `{instance_id, status}`.
3. Poll `GET /xt/<instance_id>` until `committed` or `aborted`.
4. If committed, verify receipts on each chain's RPC.

**RPC mode** (`hoodi`, `sepolia-prod`):

1. Sign tx per chain in Go.
2. Shell out to `scripts/encode-xt.ts` with `[{chainId, rawTx}, …]` → SDK encodes the XT payload.
3. POST `eth_sendXTransaction` to the source rollup RPC with the encoded payload.
4. Wait for receipts on each chain (the compose sequencer cross-includes both txs).

The submission mode is declared in YAML (`xt-submission: sidecar | rpc`) and validated at startup
(sidecar mode requires `sidecar-url`; rpc mode doesn't).

### Smart-account flow (ERC-4337 v0.7)

`scripts/sa-helper.ts` wraps the ethera SDK. Three subcommands:

- `create-account` → returns the SA's deterministic address and whether it's deployed.
- `create-userops` → returns signed canonical UserOps (used by sidecar mode; Go then packs them
  into EntryPoint v0.7 `handleOps` calldata, signs the outer tx, and submits via sidecar).
- `compose-and-submit` → full SDK flow: prepare + sign + compose + send + `await wait()`. Returns
  `(hashes, chainIds)` so Go can route each hash to the right rollup RPC and read receipts.

Both subcommands accept `--gas-overrides '[{"chainId", "callGasLimit", "verificationGasLimit"}]'`
because the SDK's default gas estimator undershoots cross-chain calls. Token tests use
`StandardSATokenBridgeGasOverrides`: src callGas 3M, dst callGas 5M, dst verifGas 3.5M.

### Wrapped-CET semantics

`ComposeL2ToL2Bridge.receiveTokens` mints a deterministic wrapped-CET on the destination chain
(predicted from `(sourceToken, sourceChainID)` via `CetFactory.predictAddress`) rather than
crediting the destination's original ERC-20. Destination-side assertions therefore read the CET
balance via `helpers.PredictCetAddress`. The source-side ERC-20 stays locked in the bridge for
the lifetime of the bridged supply.

### Two-phase tests (state files)

Tests that exercise long-lived flows save state to `test/.<name>-state-<id>.json` between phases
(all gitignored):

- L1→L2 existing-token: `_DeployPhase` deploys + approves, `_BridgePhase` mints + bridges.
- L2→L2 existing-token (EOA and SA): same pattern.
- L2→L2 manual: `_SendERC20` then (after coordinator relay) `_Receive`.
- L2→L1: `_Withdraw` saves the `MessagePassed` event, `_Finalize` waits for the dispute game,
  builds the storage proof via `eth_getProof`, proves, waits for maturity, and finalizes.

`_Finalize` skips with a clear message when the proof isn't yet mature, so it's safe to re-run
later.

### Gas budgets (EOA)

Per-call defaults live in `internal/helpers/gas.go`, sized against measured worst case plus margin
for mailbox-root recomputation under bursty load:

| Constant             | Value     | Used for                                    |
|----------------------|-----------|---------------------------------------------|
| `GasMint`            | 200,000   | `MockL2ERC20.mint`                          |
| `GasApprove`         | 200,000   | `MockL2ERC20.approve`                       |
| `GasNativeTransfer`  | 50,000    | EOA self-/cross-transfer                    |
| `GasBridgeERC20To`   | 800,000   | source-side `bridgeERC20To`                 |
| `GasBridgeReceive`   | 1,500,000 | destination-side `receiveTokens`            |
| `GasBridgeReceiveLo` | 200,000   | intentionally-OOG receive (abort scenarios) |
| `L1BridgeGasLimit`   | 5,000,000 | L1 portal tx (`bridgeETHTo` / `bridgeERC20To`) |
| `L1MinGasLimitETH`   | 21,000    | minimal L2 deposit for ETH                  |
| `L1MinGasLimitNewToken` | 2,500,000 | L2 deposit that also deploys a CET       |
| `L1MinGasLimitERC20`    | 200,000   | L2 deposit for already-deployed CET       |

### Sidecar API

| Endpoint  | Method | Purpose                            |
|-----------|--------|------------------------------------|
| `/xt`     | POST   | Submit cross-chain transaction     |
| `/xt/:id` | GET    | Poll XT status (committed/aborted) |
| `/health` | GET    | Sidecar liveness check             |

### Local-testnet port mappings

| Service         | Chain A | Chain B |
|-----------------|---------|---------|
| op-geth RPC     | 18545   | 28545   |
| op-rbuilder RPC | 17545   | 27545   |
| Sidecar API     | 17090   | 27090   |
| Blockscout      | 19000   | 29000   |

## Development commands

```bash
make build              # Build test binary
make test               # Run all tests (local-testnet, embedded config)
make test-info          # Run with INFO logging
make test-debug         # Run with DEBUG logging
make smoke-test         # Smoke suite (local-testnet)
make stress-test        # Stress suite (local-testnet)
make test-hoodi         # Run against configs/config.hoodi.yaml
make test-sepolia-prod  # Run against configs/config.sepolia-prod.yaml
make test-sepolia-stage # Run against configs/config.sepolia-stage.yaml
make scripts-install    # `cd scripts && npm install --legacy-peer-deps`
make format             # go fmt
make lint               # golangci-lint
make clean              # rm -rf bin/
make deps               # go mod tidy
make docker-build       # Build dome:latest container
```

## Docker

```bash
make docker-build

# Run against a mounted external config
docker run --rm \
  -v $(pwd)/configs/config.hoodi.yaml:/app/config.yaml \
  -e CONFIG_PATH=/app/config.yaml \
  dome:latest -test.v -test.run='^TestL2ToL2_ETH_AtoB$'
```

The container has Go but not Node, so it only supports sidecar-mode EOA tests. RPC mode and SA
tests need the host `npx`/`node` and the installed `scripts/node_modules`.

## Dependencies

- **go-ethereum** — Ethereum client, ABI, tx signing, storage proofs.
- **gopkg.in/yaml.v3** — Config parsing.
- **testify** — Test assertions.
- **@ssv-labs/ethera-sdk**, **@wagmi/core**, **viem**, **ethers** — Required by `scripts/*.ts`
  (only when running rpc-mode or SA tests). Installed via `make scripts-install`.

## Known issues

- **SA on sepolia-stage**: `handleOps` outer tx lands, but the inner UserOp reverts with
  `MessageNotFound()` (selector `0x28915ac7`). The SA tests detect this via the
  `UserOperationEvent.success=false` log and `t.Skip()` with a clear reason. EOA tests on stage
  work fine.
- **Compose-sequencer flakiness on remote networks**: receipts for `eth_sendXTransaction`-submitted
  txs are sometimes delayed past the SDK's default wait window. The TS helper now blocks on
  `await send.wait()` before returning, which usually papers over this — if you see "receipt not
  found" with a hash that does exist on chain, the SDK timed out polling; just re-run.
