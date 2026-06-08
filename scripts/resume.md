# Scripts Reference

## Architecture Overview

All scripts test the Universal Shared Bridge system across 3 networks: **hoodi** (default), **sepolia-prod**, **sepolia-stage**. Set `BRIDGE_NETWORK=<network>` env var to switch. Config is centralized in `config.ts`.

Cross-chain (L2-to-L2) transactions use two submission methods:
- **Compose sequencer RPC** (hoodi, sepolia-prod): `eth_sendXTransaction` with `encodeXtMessage` on rollup RPCs
- **Sidecar REST API** (sepolia-stage): `POST /xt` with `{transactions: {chainId: [signedTx]}}`

Smart Account (SA) scripts use ERC-4337 (Kernel v3.1, multichain ECDSA validator). On sepolia-stage, the SDK's `compose_buildSignedUserOpsTx` is unavailable, so `sa-compose-helper.ts` manually builds `handleOps` txs from signed UserOps and submits via sidecar.

---

## Helper Files

### `xt-submit.ts`
Routes cross-chain TX submission. Exports `submitXt(entries, sourceRpc)`.
- If `SIDECAR_URL` set: POSTs to sidecar REST API
- Otherwise: uses SDK's `encodeXtMessage` + `eth_sendXTransaction` RPC

### `sa-chains.ts`
Exports `rollupA`, `rollupB` (viem chain definitions) and `accountAbstractionContracts` using addresses from `config.ts`. Import this instead of SDK's hardcoded chains.

### `sa-compose-helper.ts`
Drop-in wrapper for `composeUnpreparedUserOps`. Exports `composeOps()` and `composeAndSubmit()`.
- **Standard mode** (hoodi/sepolia-prod): passes through to SDK directly
- **Sidecar mode** (sepolia-stage): intercepts SDK's RPC calls via Proxy on `publicClient`:
  - `compose_buildSignedUserOpsTx` -> manually builds EntryPoint v0.7 `handleOps` txs from the SDK's signed canonical UserOps
  - `eth_sendXTransaction` -> submits collected raw txs via sidecar
- Bumps UserOp gas fees to 2 Gwei priority / 5 Gwei max (bundler minimum, applied before signing)

---

## L1 -> L2 Scripts

### `l1-to-l2-ETH.ts`
Bridges native ETH from L1 to an L2 rollup.
- **Args:** `--dest <a|b> --amount <ETH>`
- **Flow:** Call `bridgeETHTo` on ComposeL1Bridge with value -> poll L2 for ETH arrival
- **Asserts:** L1 decreased by >= amount; L2 increased by exact amount

### `l1-to-l2-new-token.ts`
Deploys a new ERC-20 on L1, bridges to L2 (deploys CET on L2).
- **Args:** `--dest <a|b>`
- **Flow:** Deploy MintableToken on L1 -> mint 100 -> predict CET via CETFactory -> approve -> `bridgeERC20To` with extraData (name/symbol/decimals) -> poll L2 for CET
- **Asserts:** L1 balance = 0; L2 CET = 100

### `l1-to-l2-existing-token.ts`
Bridges a previously-deployed token (reuses state across runs).
- **Args:** `--dest <a|b>`
- **Flow:** First run: deploy + max approve + save state. Subsequent: mint 100 + bridge + poll
- **Asserts:** L1 decreased by 100; CET deployed; L2 CET increased by 100
- **State file:** `.l1-to-l2-existing-token-state-{rollup}`

### `l1-to-l2-eth-stress.ts`
Stress test: bridges ETH from N accounts concurrently.
- **Args:** `--dest <a|b> [--num-acc <N>] [--new-wallets]`
- **Flow:** Generate N deterministic accounts -> fund 0.1 ETH each -> broadcast N bridge txs concurrently with retry logic -> poll L2
- **Asserts:** All L2 balances increased by 0.01 ETH
- **Special:** Retries handle portal gas metering failures (OutOfGas). MIN_GAS_LIMIT=21000, BRIDGE_GAS_LIMIT=5M.

### `l1-to-l2-new-token-stress.ts`
Stress test: bridges tokens from N accounts. First account deploys CET on L2.
- **Args:** `--dest <a|b> [--num-acc <N>] [--new-wallets]`
- **Flow:** Deploy StressToken -> mint 100 to each account -> account 0 bridges first with 2.5M gas (deploys CET), rest use 200K -> retry loop -> poll
- **Asserts:** All L1 balances = 0; all CET = 100

---

## L2 -> L2 Scripts (EOA)

### `l2-to-l2-ETH.ts`
Bridges ETH between rollups using composed cross-chain transaction.
- **Args:** `--source <a|b> --dest <a|b> --amount <ETH>`
- **Flow:** Build source tx (`bridgeEthTo`) + dest tx (`receiveETH`) with matching sessionId/MessageHeader -> sign both -> `submitXt` -> poll balances
- **Asserts:** Source decreased by >= amount; dest increased

### `l2-to-l2-new-token.ts`
Deploys token on source rollup, bridges to dest (deploys CET).
- **Args:** `--source <a|b> --dest <a|b>`
- **Flow:** Deploy + mint 100 + approve -> predict CET -> build composed TX (source: `bridgeERC20To`, dest: `receiveTokens`) -> submitXt -> poll
- **Asserts:** Source decreased by 100; CET deployed; dest CET = 100

### `l2-to-l2-existing-token.ts`
Bridges previously-deployed token between rollups.
- **Args:** `--source <a|b> --dest <a|b>`
- **Flow:** Two-phase via state file. First: deploy + max approve. Subsequent: mint + compose + submit + poll
- **Asserts:** Source decreased by 100; CET deployed; dest CET = 100
- **State file:** `.l2-to-l2-existing-token-state-{src}-{dst}`

### `l2-to-l2.ts`
Manual two-step bridge for native ERC-20 or CET. Unlike composed scripts, send and receive are separate operations.
- **Subcommands:** `send-erc20`, `send-cet --cet-address <addr>`, `receive --session-id <id> --sender <addr>`
- **Special:** Generates sessionId per spec: `version << 240 | keccak256(addr, nonce, block, salt) >> 16`

---

## L2 -> L2 Scripts (Smart Account / ERC-4337)

All SA scripts use `@ssv-labs/ethera-sdk` for Kernel v3.1 smart accounts with multichain ECDSA validator. They suppress SDK gas estimation warnings (cross-chain calls fail when simulated individually).

### `l2-to-l2-SA-ETH.ts`
Bridges ETH between rollups using smart accounts.
- **Args:** `--source <a|b> --dest <a|b> --amount <ETH>`
- **Flow:** Create SAs on both chains -> fund EntryPoint (0.05 ETH min) + fund SA with bridge amount -> build UserOps (source: `bridgeEthTo`, dest: `receiveETH`) -> `composeAndSubmit` -> wait receipts
- **Asserts:** Source SA ETH decreased; dest SA ETH increased

### `l2-to-l2-SA-new-token.ts`
Deploys token, bridges via SA, transfers CET to EOA on dest.
- **Args:** `--source <a|b> --dest <a|b>`
- **Flow:** Create SAs -> fund EntryPoint -> deploy token (EOA), mint 100 to SA -> predict CET -> UserOps: source=approve+bridgeERC20To, dest=receiveTokens+transfer CET to EOA -> `composeOps` + send
- **Gas overrides:** src callGas=3M, dst callGas=5M, dst verificationGas=3.5M
- **Asserts:** SA source balance = 0; CET deployed; EOA received 100 CET

### `l2-to-l2-SA-existing-token.ts`
Two-phase SA token bridge (deploy once, bridge repeatedly).
- **Args:** `--source <a|b> --dest <a|b>`
- **Flow:** State file pattern. First: deploy token. Subsequent: mint to SA + compose + assert
- **State file:** `.l2-to-l2-SA-existing-token-state-{src}-{dst}`
- **Asserts:** SA decreased by 100; CET deployed; EOA received 100 CET

### `l2-to-l2-SA-existing-token-stress.ts`
Stress test: N smart accounts bridging tokens concurrently.
- **Args:** `--source <a|b> --dest <a|b> [--num-acc <N>] [--same-smart-acc]`
- **Two modes:**
  - Multi-SA (default): N independent smart accounts, compose in parallel
  - Same-SA (`--same-smart-acc`): single SA, sequential (single-user throughput test)
- **Flow:** Generate N signers -> create SAs -> auto-bridge ETH from L1 if needed -> fund EntryPoint -> deploy/load token -> mint to SAs -> compose+submit all -> collect receipts
- **Asserts:** Per-account: source decreased by 100; dest EOA received 100 CET

---

## L2 -> L1 Scripts

### `l2-to-l1-ETH.ts`
Withdraws ETH from L2 back to L1. Two-step process (proof maturity can take hours).
- **Args:** `withdraw --source <a|b> --amount <ETH>` or `finalize --source <a|b>` or both
- **Withdraw:** Call `bridgeETHTo` on ComposeL2Bridge -> extract MessagePassed event -> save state
- **Finalize:** Wait for dispute game -> build storage proof (`eth_getProof` on L2ToL1MessagePasser) -> `proveWithdrawalTransaction` -> wait maturity -> `finalizeWithdrawalTransaction`
- **Asserts:** L1 ETH increased
- **State file:** `.l2-to-l1-ETH-state-{src}`

### `l2-to-l1-token.ts`
Full L1->L2->L1 token round-trip with withdrawal proof.
- **Args:** `withdraw --source <a|b>` or `finalize --source <a|b>` or both
- **Withdraw:** Deploy token on L1 -> mint 100 -> bridge L1->L2 -> poll CET arrival -> burn CET via `bridgeERC20To` back to L1 -> extract MessagePassed -> save state
- **Finalize:** Same proving/finalizing flow as ETH script
- **Asserts:** L1 token balance restored after full round-trip
- **State file:** `.l2-to-l1-token-state-{src}`

---

## Test Suites (`suites/`)

### `setup.json`
Defines 28 test entries. Each has `name`, `script`, `args`, `enabled`, `runTwice?`. Tests are ordered: L1->L2 ETH first (funds rollups), then L1->L2 tokens, L2->L2 EOA, L2->L1, L2->L2 SA, stress tests last.

### `runner.ts`
Sequential test runner. Features:
- `--resume` flag with `.suite-resume` state file
- `runTwice` support for existing-token deploy+bridge pattern
- Fail-fast on first failure
- Per-test elapsed time, summary table at end
- Cleans old state files on fresh run

### Suite entry points
- `full-suite-hoodi-prod.ts` -> sets `BRIDGE_NETWORK=hoodi`
- `full-suite-sepolia-prod.ts` -> sets `BRIDGE_NETWORK=sepolia`
- `full-suite-sepolia-stage.ts` -> sets `BRIDGE_NETWORK=sepolia-stage`
