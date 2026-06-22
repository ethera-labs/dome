# Ethera SDK — What Must Stay in TypeScript

The `@ssv-labs/ethera-sdk` wraps ZeroDev's Kernel v3.1 smart accounts with a multichain ECDSA validator. The signing scheme (merkle tree of UserOp hashes signed with a single ECDSA key) cannot be trivially reimplemented in Go. Go tests should call a small TypeScript helper for SDK operations.

---

## SDK Functions Used

### `createComposeConfig(options)`
- **Input:** `{wagmi: WagmiConfig, accountAbstractionContracts: {[chainId]: {kernelImpl, kernelFactory, multichainValidator}}}`
- **Output:** Config object with `getPublicClient(chainId)`, `entryPoint`, AA contract refs
- **Does:** Pure config bundling, no RPC calls
- **Why TS:** Tightly coupled to viem/wagmi client types that the other SDK functions require

### `createSmartAccount(params, config)`
- **Input:** `{signer: ViemAccount, chainId: number, multiChainIds: number[]}` + compose config
- **Output:** `{validator, account: {address, createUserOp(calls)}, signer, publicClient}`
- **Does:**
  1. Creates `MultiChainECDSAValidator` from `@zerodev/multi-chain-ecdsa-validator`
  2. Creates Kernel account via `createKernelAccount` from `@zerodev/sdk`
  3. Derives counterfactual SA address (CREATE2 with Kernel-specific salt)
- **RPC calls:** `eth_getCode` (check if deployed), `eth_call` (factory address derivation)
- **Why TS:** ZeroDev Kernel account creation with multichain validator plugin. The initCode construction and address derivation are Kernel-specific.

### `account.createUserOp(calls)`
- **Input:** `UserOPCall[]` where each call is `{to: address, value: bigint, data: hex}`
- **Output:** `{account, signer, chainId, publicClient, userOp: {callData, callGasLimit, verificationGasLimit, preVerificationGas, maxFeePerGas, maxPriorityFeePerGas}}`
- **Does:**
  1. Estimates gas for each call via `eth_estimateGas` (falls back to 900K on failure)
  2. Gets fee data via `estimateFeesPerGas`
  3. Encodes calls into Kernel's `execute` batch calldata
- **RPC calls:** `eth_estimateGas` (per call), `eth_maxPriorityFeePerGas`, `eth_getBlockByNumber`

### `composeUnpreparedUserOps(operations, options)`
- **Input:** Array of `{account, publicClient, userOp}` (one per chain) + callbacks
- **Output:** `{payload, builds, explorerUrls, send()}`
- **Does:**
  1. `prepareUserOperation` per chain — resolves nonce, factory/initCode, skips fee re-estimation if fees are already set as bigints
  2. **Multichain ECDSA signing** — computes `userOpHash` per chain, builds merkle tree, signs merkle root with EOA, attaches `signature = ecdsaSig + merkleRoot + merkleProof` per UserOp
  3. Converts to RPC canonical format (`toRpcUserOpCanonical`)
  4. Calls `compose_buildSignedUserOpsTx` per chain (or intercepted — see below)
  5. Encodes XT payload via `encodeXtMessage`
  6. `send()` calls `eth_sendXTransaction` (or intercepted)
- **RPC calls:** `eth_getTransactionCount`, `eth_getCode`, `compose_buildSignedUserOpsTx` (per chain), `eth_sendXTransaction`
- **Why TS:** The multichain ECDSA signing is the critical blocker. It uses ZeroDev's `@zerodev/multi-chain-ecdsa-validator` which builds a merkle tree of ERC-4337 UserOp hashes and produces a single ECDSA signature valid across all chains.

### `encodeXtMessage(params)`
- **Input:** `{senderId?: string, entries: [{chainId, rawTx: Hex}]}`
- **Output:** `Hex` — encoded payload for `eth_sendXTransaction`
- **Does:** Pure encoding, no RPC calls, no crypto
- **Why TS:** Only because it's currently in the SDK. Could be ported to Go if the wire format is documented/reverse-engineered.

---

## Sepolia-Stage Interception (sa-compose-helper.ts)

On sepolia-stage, the compose sequencer RPC method `compose_buildSignedUserOpsTx` doesn't exist. The helper intercepts the SDK at the transport layer:

### What gets intercepted:
1. **`compose_buildSignedUserOpsTx`** → replaced with manual `handleOps` tx building:
   - Receives signed canonical UserOps from the SDK (post-signing)
   - Packs into EntryPoint v0.7 `PackedUserOperation` format
   - Encodes `handleOps([packedOps], beneficiary)` calldata
   - Signs outer tx with EOA via ethers.js
   - Returns `{hash, raw}` (same shape as compose sequencer would)

2. **`eth_sendXTransaction`** → replaced with sidecar submission:
   - Collects all `{chainId, rawTx}` pairs from step 1
   - POSTs to sidecar: `{"transactions": {"chainId": ["0xrawTx"]}}`

### What is NOT intercepted:
- `eth_getCode`, `eth_call`, `eth_getTransactionCount` — pass through to rollup RPC
- `waitForTransactionReceipt` — passes through to rollup RPC
- Gas estimation, fee queries — pass through

### Gas fee bump:
The rollup RPC reports ~1 Mwei priority fee, but the bundler/sidecar requires ~2 Gwei. Before signing, the helper bumps `maxPriorityFeePerGas` to 2 Gwei and `maxFeePerGas` to 5 Gwei on the UserOp data.

---

## TypeScript Helper for Go Tests

Go tests should call a small TS script for SA operations. Suggested interface:

### `sa-helper.ts create-account`
```bash
npx ts-node sa-helper.ts create-account \
  --private-key <hex> \
  --chain-id <number> \
  --multi-chain-ids <id1,id2> \
  --network <hoodi|sepolia|sepolia-stage>
```
**Output (JSON to stdout):**
```json
{
  "smartAccountAddress": "0x...",
  "isDeployed": true
}
```

### `sa-helper.ts create-userops`
```bash
npx ts-node sa-helper.ts create-userops \
  --private-key <hex> \
  --network <hoodi|sepolia|sepolia-stage> \
  --calls '<JSON array of [{chainId, to, value, data}]>'
```
**Output (JSON to stdout):**
```json
{
  "userOps": [
    {
      "chainId": 100003,
      "sender": "0x...",
      "nonce": "0x...",
      "initCode": "0x...",
      "callData": "0x...",
      "callGasLimit": "0x...",
      "verificationGasLimit": "0x...",
      "preVerificationGas": "0x...",
      "maxFeePerGas": "0x...",
      "maxPriorityFeePerGas": "0x...",
      "signature": "0x..."
    }
  ]
}
```
Go then packs these into `handleOps` txs and submits via sidecar.

### `sa-helper.ts compose-and-submit`
For hoodi/sepolia-prod where the full SDK flow is needed:
```bash
npx ts-node sa-helper.ts compose-and-submit \
  --private-key <hex> \
  --network <hoodi|sepolia|sepolia-stage> \
  --calls '<JSON array>'
```
**Output (JSON to stdout):**
```json
{
  "hashes": ["0x...", "0x..."],
  "explorerUrls": ["https://...", "https://..."]
}
```
Go then polls for receipts and runs assertions.

---

## What Go Must Build for SA Sidecar Mode

If Go receives signed canonical UserOps from the TS helper, it needs to:

### 1. Pack into EntryPoint v0.7 PackedUserOperation
```
struct PackedUserOperation {
    address sender;
    uint256 nonce;
    bytes initCode;
    bytes callData;
    bytes32 accountGasLimits;   // pack(verificationGasLimit, callGasLimit) as 2x uint128
    uint256 preVerificationGas;
    bytes32 gasFees;            // pack(maxPriorityFeePerGas, maxFeePerGas) as 2x uint128
    bytes paymasterAndData;     // "0x" (no paymaster)
    bytes signature;
}
```

### 2. Encode handleOps calldata
```
handleOps(PackedUserOperation[] ops, address beneficiary)
```
Beneficiary = EOA address (receives gas refunds).

### 3. Sign outer transaction
Standard EIP-1559 tx to EntryPoint (`0x0000000071727De22E5E9d8BAf0edAc6f37da032`):
- `gasLimit` = sum of all ops' (callGasLimit + verificationGasLimit + preVerificationGas) + 100K overhead
- `maxFeePerGas` / `maxPriorityFeePerGas` from `eth_maxPriorityFeePerGas` on rollup RPC

### 4. Submit via sidecar
POST both signed txs to sidecar as described in `go-tests.md`.

---

## Known Issues

### MessageNotFound() on sepolia-stage
Both ETH and token SA tests revert with `MessageNotFound()` (selector `0x28915ac7`) on sepolia-stage. The `handleOps` outer tx succeeds but inner UserOp calls fail. The compose mailbox system may handle calls from smart accounts differently than EOA calls. Non-SA L2->L2 scripts work fine on stage.

### Priority fee mismatch
Rollup RPCs on sepolia-stage report ~1 Mwei priority fee but the bundler/sidecar rejects anything below ~2 Gwei. The TS helper bumps fees before signing. Go must also apply this bump when building `handleOps` outer txs.

### Bundler endpoints (sepolia-stage only)
- `bundler-a.stage.ethera-labs.io` (chain 100003)
- `bundler-b.stage.ethera-labs.io` (chain 200005)
- Support: `eth_chainId`, `eth_supportedEntryPoints`, `web3_clientVersion`, `ethera_buildSignedUserOpsTx`
- Do NOT support: `eth_getCode`, `eth_call`, `eth_getBalance` — use rollup RPCs for these
