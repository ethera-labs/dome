# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Go-based E2E test framework for the Compose cross-rollup stack. Exercises L2↔L2, L1↔L2, and L2→L1
bridge flows for both EOA and ERC-4337 smart-account senders, against a local testnet or one of
three remote networks (`hoodi`, `sepolia-prod`, `sepolia-stage`).

Two cross-chain submission modes:

- **Sidecar REST** (`xt-submission: sidecar`) — `POST /xt` to the Compose Sidecar, poll instance
  status. Used by `local` and `sepolia-stage`.
- **Compose sequencer RPC** (`xt-submission: rpc`) — `eth_sendXTransaction` on the source rollup.
  Used by `hoodi` and `sepolia-prod`. The payload encoding (SDK's `encodeXtMessage`) is done by
  `scripts/encode-xt.ts`, which Go shells out to.

Smart-account tests use ZeroDev Kernel v3.1 with multichain ECDSA signing. The signing logic stays
in TS (`scripts/sa-helper.ts`); Go consumes the signed canonical UserOps, packs them into
EntryPoint v0.7 `handleOps`, signs the outer tx, and submits.

## Development Commands

### Build and tooling

```bash
make build           # Build bin/dome test binary (auto-copies config.example.yaml → config.yaml)
make scripts-install # Install Node deps in scripts/ (needed for rpc-mode and SA tests)
make format          # go fmt
make lint            # golangci-lint
make deps            # go mod download + tidy
make clean           # rm -rf bin/
make docker-build    # Build dome:latest container
```

### Running tests

Tests are compiled into `bin/dome`. Each per-network Makefile target sets `CONFIG_PATH` to the
matching YAML, then invokes the binary with optional filter/override env vars.

```bash
# Local-testnet (uses embedded configs/config.yaml)
make test                                # all tests
make test-info TEST_NAME=TestBridge      # specific
make smoke-test                          # smoke suite
make stress-test                         # stress suite

# Remote networks
make test-hoodi          TEST_NAME='^TestL2ToL2_ETH_AtoB$'
make test-sepolia-prod   TEST_FILE=l2_to_l2_eth_test
make test-sepolia-stage  TEST_FILE=l2_to_l2_sa_new_token_test SOURCE=a DEST=b AMOUNT=25

# Binary directly
CONFIG_PATH=./configs/config.hoodi.yaml LOG_LEVEL=INFO \
  ./bin/dome -test.v -test.run='^TestL2ToL2_ETH_AtoB$'
```

### Makefile knobs

The `test-hoodi` / `test-sepolia-prod` / `test-sepolia-stage` targets accept:

| Variable     | Effect                                                                       |
|--------------|------------------------------------------------------------------------------|
| `TEST_FILE`  | Short filename (e.g. `l2_to_l2_eth_test`). Expanded to `-test.run` regex matching every `Test*` function in that file via `grep`. |
| `TEST_NAME`  | Raw `-test.run` regex.                                                       |
| `SOURCE`     | `a`, `b`, or `l1`. Tests whose direction doesn't match `t.Skip()` cleanly.   |
| `DEST`       | Same.                                                                        |
| `AMOUNT`     | ETH (for ETH tests) or token-units (for token tests); multiplied by 10^18.   |
| `AMOUNT_WEI` | Raw wei. Takes precedence over `AMOUNT`.                                     |

`SOURCE`/`DEST` are read inside tests via `helpers.ApplyDirectionFilter(t, src, dst)`. `AMOUNT*`
is read via `helpers.ParseBridgeAmountOverride(default)`.

Log levels via `LOG_LEVEL` env (DEBUG, INFO).

### Configuration

Configs ship as one YAML per network:

| File                                | Network         | XT submission | L1 | AA |
|-------------------------------------|-----------------|---------------|----|----|
| `configs/config.example.yaml`       | `local`         | sidecar       | —  | —  |
| `configs/config.hoodi.yaml`         | `hoodi`         | rpc           | ✓  | ✓  |
| `configs/config.sepolia-prod.yaml`  | `sepolia-prod`  | rpc           | ✓  | ✓  |
| `configs/config.sepolia-stage.yaml` | `sepolia-stage` | sidecar       | ✓  | ✓  |

Switch by setting `CONFIG_PATH=configs/config.<network>.yaml`.

Loading: `CONFIG_PATH` (external) → embedded `configs/config.yaml` (compile-time) → panic.

YAML shape (top-level):

```yaml
network: hoodi | sepolia-prod | sepolia-stage | local
xt-submission: sidecar | rpc
wallet-private-key: <hex without 0x>   # back-compat: falls back to chain-configs.rollup-a.pk

l1:                                    # optional — absent on local
  rpc-url: http://...
  chain-id: 560048
  contracts:
    compose-l1-bridge-rollup-a: { address, abi }
    compose-l1-bridge-rollup-b: { address, abi }
    compose-portal-rollup-a:    { address, abi }
    compose-portal-rollup-b:    { address, abi }
    dispute-game-factory:       { address, abi }   # required for L2->L1 finalize

l2:
  sidecar-url: ""                       # required when xt-submission=sidecar
  chain-configs:
    rollup-a: { id, rpc-url, bundler-url? }
    rollup-b: { id, rpc-url, bundler-url? }
  contracts:
    bridge:        # ComposeL2ToL2Bridge       (required)
    mailbox:       # UniversalBridgeMailbox    (required)
    cet-factory:   # CetFactory                (required)
    token:         # MockL2ERC20               (local only)
    compose-l2-bridge-rollup-a: { … }   # required for L2->L1
    compose-l2-bridge-rollup-b: { … }
    eth-liquidity:                      # optional
  aa:                                   # optional — absent on local
    kernel-impl: 0x…
    kernel-factory: 0x…
    multichain-validator: 0x…
```

Validation runs at `configs` package init. Bad config → panic on binary startup with a joined
list of every offending field.

Tests that depend on a missing optional section call `RequireL1(t)` / `RequireAA(t)` /
`RequireL2BridgePerRollup(t)` / `RequireTSRuntime(t)` to skip cleanly.

## Architecture

### Directory structure

```
dome/
├── bin/dome                          # Compiled test binary
├── build/Dockerfile
├── configs/                          # YAML configs + embed glue
│   ├── config.go                     # Schema, validation, IsLocal/HasL1/HasAA helpers
│   ├── config.yaml                   # gitignored, embedded at compile time
│   ├── config.example.yaml
│   ├── config.{hoodi,sepolia-prod,sepolia-stage}.yaml
├── internal/
│   ├── accounts/                     # Account, NewRollupAccount, GetBalance, GetNonce
│   ├── helpers/
│   │   ├── bridge_txs.go             # PackBridgeERC20To, PackBridgeReceiveTokens, MessageHeader
│   │   ├── eth_bridge.go             # PackBridgeEthTo, PackReceiveETH
│   │   ├── erc20.go                  # SendMintTx, ApproveTokens, MintAndApproveCtx
│   │   ├── erc20_deploy.go           # MintableTokenABI + bytecode, DeployMintableToken
│   │   ├── cet.go                    # PredictCetAddress
│   │   ├── gas.go                    # GasTipCap/FeeCap + per-call gas constants
│   │   ├── l1_bridge.go              # PackBridgeETHTo, PackBridgeERC20ToL1, EncodeERC20ExtraData, SendL1Tx
│   │   ├── l2_withdraw.go            # ExtractMessagePassed, FindCoveringDisputeGame, BuildWithdrawalProof, ProveWithdrawal/FinalizeWithdrawal helpers
│   │   ├── eth_proof.go              # Raw eth_getProof JSON-RPC wrapper
│   │   ├── poll.go                   # PollUntil, WaitForETHBalanceChange, WaitForTokenBalanceChange
│   │   ├── sa_helper.go              # SA-side: SACreateAccount/CreateUserOps/ComposeAndSubmit, EntryPoint v0.7 PackedUserOperation, PackHandleOps, BuildHandleOpsRawTx, EnsureEntryPointDeposit
│   │   ├── session_id.go             # GenerateSessionIDV1
│   │   ├── state_file.go             # LoadJSONState/SaveJSONState/DeleteJSONState
│   │   ├── test_overrides.go         # ApplyDirectionFilter, ParseBridgeAmountOverride
│   │   ├── xt_submit.go              # SubmitXTRaw/Pair/WaitCommitted; sidecar vs rpc dispatch
│   ├── logger/                       # DEBUG/INFO logger
│   ├── rollup/                       # Rollup descriptor (name, chainID, rpcURL)
│   └── transactions/
│       ├── transactions.go           # CreateTransaction(WithNonce), SendTransaction, GetTransactionDetails, DistributeEth
│       └── cross_tx.go               # Sidecar HTTP client: SubmitXT, GetXTStatus, WaitForDecision
├── scripts/                          # TypeScript helpers + reference TS scripts
│   ├── encode-xt.ts                  # SDK encodeXtMessage shell-out (rpc mode)
│   ├── sa-helper.ts                  # SA: create-account, create-userops, compose-and-submit
│   ├── package.json, tsconfig.json
│   └── *.ts                          # Original TS test scripts (reference)
└── test/                             # 44 Go tests in 15 files
    ├── config.go                     # setup(), global rollups/accounts/ABIs, Require* helpers
    ├── erc20_balance.go              # Tolerant ERC-20 balance read helper
    ├── smoke_test.go                 # TestMain entry
    ├── bridge_test.go, stress_test.go, uncorrelated_tx_test.go, xt_*_test.go
    ├── l2_to_l2_{eth,new_token,existing_token,manual}_test.go
    ├── l1_to_l2_{eth,new_token,existing_token,eth_stress,new_token_stress}_test.go
    ├── l2_to_l1_{eth,token}_test.go
    └── l2_to_l2_sa_{eth,new_token,existing_token,existing_token_stress}_test.go
```

### Core packages

**`configs/`** — Hybrid loading: external (via `CONFIG_PATH`) overrides embedded.
`configs.Values` global is populated at package init. Helpers: `IsLocal()`, `HasL1()`, `HasAA()`,
`HasL2BridgePerRollup()`, `HasBundlers()`. `ActiveNetwork` exposes the active network string.

**`internal/transactions/`**
- `transactions.go`: `CreateTransaction` (auto-nonce) and `CreateTransactionWithNonce` produce
  signed EIP-1559 DynamicFee txs. `SendTransaction` broadcasts. `GetTransactionDetails` polls
  for the receipt (30 × 600ms retries). `DistributeEth` mass-funds N recipients from one sender
  with sequential nonces.
- `cross_tx.go`: `SubmitXT(ctx, sidecarURL, transactions)` → `XTResponse{InstanceID, Status}`.
  `WaitForDecision` polls `GET /xt/:id` until committed/aborted.

**`internal/helpers/xt_submit.go`** — `SubmitXTRaw`/`SubmitXTPair`/`SubmitXTWaitCommitted` route
to sidecar or RPC mode based on `configs.Values.XTSubmission`. RPC mode calls `encodeXTPayload`
(shells out to `scripts/encode-xt.ts`) and then POSTs `eth_sendXTransaction` to the source
rollup. `HasTSRuntime()` checks `npx` is on PATH.

**`internal/helpers/sa_helper.go`** — Smart-account integration:
- `SACreateAccount` → SA address + deployment status (TS shell-out).
- `SACreateUserOps` → signed canonical UserOps for given calls (TS shell-out).
- `SAComposeAndSubmit` → full SDK compose+send+wait; returns `[]SAComposedTx{Hash, ChainID}`.
- `PackHandleOps` → ABI-encode EntryPoint v0.7 `handleOps([packedOps], beneficiary)`.
- `PackCanonicalAsV07` → canonical UserOp → `PackedUserOperation`.
- `BuildHandleOpsRawTx` → outer EIP-1559 tx to EntryPoint, signed, ready for sidecar.
- `EnsureEntryPointDeposit` → tops SA's `EntryPoint.balanceOf` up to `MinEntryPointDeposit`.
- `CheckUserOpSuccess` → parses `UserOperationEvent` in a receipt; returns `(success bool, gasUsed)`.
- `StandardSATokenBridgeGasOverrides(src, dst)` → src callGas 3M, dst callGas 5M, dst verifGas
  3.5M. Required for cross-chain SA calls; default estimator undershoots.

**`internal/helpers/l1_bridge.go`, `l2_withdraw.go`, `eth_proof.go`** — L1↔L2 + L2→L1 bridge and
withdrawal proof building (storage proof via raw `eth_getProof`, dispute-game lookup, prove +
finalize against `ComposePortal`).

**`internal/helpers/test_overrides.go`** — `ApplyDirectionFilter(t, src, dst)` checks
`BRIDGE_SOURCE`/`BRIDGE_DEST` env vars and skips on mismatch. `ParseBridgeAmountOverride(default)`
reads `BRIDGE_AMOUNT_WEI` (raw wei) or `BRIDGE_AMOUNT` (decimal ETH/tokens × 10^18).

### Cross-rollup tx flow

**Sidecar mode** (local, sepolia-stage):

1. Sign txs per chain (Go: `transactions.CreateTransaction`).
2. POST `{transactions: {chainId: [hex…]}}` → `<sidecar>/xt`; returns `{instance_id, status}`.
3. `WaitForDecision` polls `GET /xt/:id` until `committed` / `aborted`.
4. Verify receipts on each chain's RPC.

**RPC mode** (hoodi, sepolia-prod):

1. Sign txs per chain (Go).
2. `helpers.SubmitXTRaw` shells out to `scripts/encode-xt.ts` to compute the XT payload.
3. POST `eth_sendXTransaction(payload)` to the source rollup RPC.
4. Compose sequencer cross-includes both txs; Go polls receipts on each rollup.

### Smart-account flow (ERC-4337 v0.7)

For each test:

1. **Derive SA address** — `SACreateAccount` shells out to `sa-helper.ts create-account`.
2. **Fund EntryPoint** — `EnsureEntryPointDeposit` checks `EntryPoint.balanceOf(SA)` and tops it
   up to 0.05 ETH via `depositTo` if needed (called on both rollups).
3. **Build UserOp calls** — Go-side ABI encoding for `bridgeEthTo`/`bridgeERC20To`/`receiveETH`/
   `receiveTokens`/`approve`/`transfer` (whatever the test exercises).
4. **Submit**:
   - **rpc mode** → `SAComposeAndSubmit` (shells out to `sa-helper.ts compose-and-submit`).
     TS does the full SDK flow including `await send.wait()`; returns `[]SAComposedTx{Hash, ChainID}`.
     Go routes each hash to the matching rollup via `waitComposedReceipts`.
   - **sidecar mode** → `SACreateUserOps` returns signed canonical UserOps; Go packs them with
     `PackCanonicalAsV07` + `PackHandleOps`, signs the outer tx with `BuildHandleOpsRawTx`,
     submits via `SubmitXTWaitCommitted`. After receipts land, `CheckUserOpSuccess` reads the
     `UserOperationEvent` to verify inner success (on sepolia-stage this can be `false` due to
     the documented `MessageNotFound()` issue — tests skip with a clear reason).

### Two-phase tests (state files)

Tests with long-lived flows persist state to `test/.<name>-state-<id>.json`:

- L1→L2 / L2→L2 existing-token (EOA + SA): `_DeployPhase` then `_BridgePhase`.
- L2→L2 manual: `_SendERC20` then (after coordinator relay) `_Receive`.
- L2→L1: `_Withdraw` saves `MessagePassed`; `_Finalize` finds the dispute game, builds the
  storage proof via `eth_getProof`, proves, waits for maturity, finalizes.

`_Finalize` calls `t.Skip()` (not fail) when the proof isn't yet mature.

### XT submission format (sidecar)

```json
{
  "transactions": {
    "100003": ["0x<rlp-signed-tx>"],
    "200005": ["0x<rlp-signed-tx>"]
  }
}
```

Chain IDs as strings, signed tx bytes 0x-prefixed.

### Sidecar API

| Endpoint  | Method | Purpose                            |
|-----------|--------|------------------------------------|
| `/xt`     | POST   | Submit a cross-chain transaction   |
| `/xt/:id` | GET    | Poll status (committed/aborted)    |
| `/health` | GET    | Liveness check                     |

### Wrapped-CET semantics

`ComposeL2ToL2Bridge.receiveTokens` mints a deterministic wrapper-CET (predicted by
`CetFactory.predictAddress(sourceToken, sourceChainID)`) instead of the destination's original
ERC-20. Destination assertions look up the CET via `helpers.PredictCetAddress` and read
`balanceOf` at that address. The source-side ERC-20 stays escrowed in the bridge.

### Local-testnet port mappings

| Service         | Chain A | Chain B |
|-----------------|---------|---------|
| op-geth RPC     | 18545   | 28545   |
| op-rbuilder RPC | 17545   | 27545   |
| Sidecar API     | 17090   | 27090   |
| Blockscout      | 19000   | 29000   |

## Key technical details

### Transaction types
- All txs are EIP-1559 `DynamicFeeTx`. `GasTipCap` / `GasFeeCap` defaults in
  `internal/helpers/gas.go`.
- Nonces via `PendingNonceAt()`. Stress and parallel-bridge tests precompute nonce ranges to
  avoid pending-pool races.
- Gas per call type lives in `internal/helpers/gas.go` (mint / approve / native / bridgeERC20To /
  receiveTokens) and `l1_bridge.go` (L1 portal calls). Override via constants — never inline.

### Wallet model
- One `wallet-private-key` at top-level (the EOA, shared across all rollups).
- The same key is used to derive the SA address (deterministic via Kernel factory).
- Stress tests derive child accounts from the master key via
  `keccak256(masterPK || i)` (mirrors the TS script's `deriveAccounts`).

### Known issues
- **SA on sepolia-stage**: `handleOps` outer tx succeeds but inner UserOp reverts with
  `MessageNotFound()`. SA tests detect this via `CheckUserOpSuccess` and `t.Skip("known issue
  on stage: …")`.
- **Compose sequencer receipt latency**: in rpc mode, `eth_sendXTransaction` may return before
  the actual chain txs land. The TS helper `sa-helper.ts compose-and-submit` blocks on
  `await send.wait()` to mitigate this; if receipts still don't appear, the SDK has likely
  timed out and the txs aren't being sequenced — investigate at the network layer.

## Module path

`github.com/ethera-labs/dome`

## Go version

1.25
