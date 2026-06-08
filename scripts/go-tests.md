# Go Testing Framework — What Go Handles

Everything listed here can be implemented purely in Go using `go-ethereum` (geth), standard `crypto/ecdsa`, and `net/http`. No TypeScript or SDK dependency needed.

---

## Scope

Go handles **all EOA (non-Smart-Account) tests** end-to-end, plus all shared infrastructure (contract calls, balance checks, assertions, polling, submission).

For **Smart Account tests**, Go handles everything EXCEPT UserOp creation and signing — those require a TypeScript helper (see `ethera-sdk.md`).

---

## Network Configuration

Three networks, selected at test time:
- `hoodi` — compose sequencer on rollup RPCs, `eth_sendXTransaction`
- `sepolia` — same as hoodi, different addresses/chain IDs
- `sepolia-stage` — sidecar at `http://127.0.0.1:18080/xt`, no compose sequencer

Key config per network:
- L1 RPC URL, Rollup A/B RPC URLs
- L1/Rollup A/Rollup B chain IDs
- Contract addresses: ComposeL1Bridge (per rollup), ComposePortal (per rollup), ComposeL2ToL2Bridge, CETFactory, UniversalBridgeMailbox, ComposeL2Bridge (per rollup)
- AA contracts (for SA tests): kernelImpl, kernelFactory, multichainValidator
- Sidecar URL (only sepolia-stage)
- Bundler URLs (only sepolia-stage): `bundler-a/b.stage.ethera-labs.io`

---

## Cross-Chain Transaction Submission

Two methods, Go implements both:

### 1. Sidecar (sepolia-stage)
```
POST http://127.0.0.1:18080/xt
Content-Type: application/json

{
  "transactions": {
    "100003": ["0x<signedRawTx>"],
    "200005": ["0x<signedRawTx>"]
  }
}
```
Response: `{"instance_id": "...", "status": "submitted"}`

### 2. Compose Sequencer RPC (hoodi, sepolia-prod)
Encode the signed raw txs into an XT message payload, then:
```
POST <rollup-a-rpc>
{
  "jsonrpc": "2.0",
  "method": "eth_sendXTransaction",
  "params": ["0x<encodedXtPayload>"],
  "id": 1
}
```
The XT payload encoding format is from the SDK's `encodeXtMessage`. You need to reverse-engineer or replicate this encoding (see `ethera-sdk.md` for details on the wire format).

---

## L1 -> L2 Tests (100% Go)

### L1 -> L2 ETH
- **Setup:** Check L1 ETH balance
- **Action:** Call `ComposeL1Bridge.bridgeETHTo(destChainId, wallet, minGasLimit=21000)` with `msg.value = amount`
- **Poll:** Read L2 ETH balance every 10s until it increases (max 60 polls)
- **Assert:** L1 decreased by >= amount; L2 increased by exact amount
- **Gas:** `gasLimit = 5_000_000` for the L1 tx

### L1 -> L2 New Token
- **Setup:** Deploy `MintableToken(name, symbol, decimals)` on L1; mint 100 tokens
- **Predict CET:** Call `CETFactory.predictAddress(tokenAddress, sourceChainId)` on L2
- **Encode extraData:** ABI-encode `(string name, string symbol, uint8 decimals)`
- **Action:** Approve bridge, then `ComposeL1Bridge.bridgeERC20To(destChainId, token, wallet, amount, minGasLimit=200000, extraData)`
- **Poll:** Read CET balance on L2
- **Assert:** L1 balance = 0; CET deployed on L2; L2 CET = 100
- **Note:** First bridge for a new token needs `minGasLimit = 2_500_000` (deploys CET contract on L2)

### L1 -> L2 Existing Token (two-phase)
- **Phase 1 (deploy):** Deploy token, approve max uint256, save token address + CET address to state
- **Phase 2 (bridge):** Mint 100, bridge, poll, assert
- **State:** Persist across test runs (file or test fixture)

### L1 -> L2 ETH Stress
- **Params:** N accounts, amount per account (0.01 ETH)
- **Setup:** Derive N accounts deterministically from master key; fund each with 0.1 ETH on L1
- **Action:** Broadcast N bridge txs concurrently; retry on failure (portal gas metering limits ~32 per block)
- **Poll:** All N L2 balances
- **Assert:** All increased by exact amount

### L1 -> L2 Token Stress
- **Same pattern** but with ERC-20. First account bridges with `minGasLimit=2_500_000` (deploys CET), rest use `200_000`.

---

## L2 -> L2 Tests (EOA — 100% Go)

### L2 -> L2 ETH
- **Build source tx:** Call `ComposeL2ToL2Bridge.bridgeEthTo(sessionId, destChainId, receiver)` with `msg.value = amount`
- **Build dest tx:** Call `ComposeL2ToL2Bridge.receiveETH(msgHeader)` where:
  ```
  msgHeader = {
    chainSrc: sourceChainId,
    chainDest: destChainId,
    sender: COMPOSE_L2_TO_L2_BRIDGE_ADDRESS,
    receiver: walletAddress,
    sessionId: sessionId,
    label: "SEND_ETH"
  }
  ```
- **Sign both** as EIP-1559 txs (type 2), `gasLimit = 3_000_000`
- **Submit atomically** via sidecar or `eth_sendXTransaction`
- **Poll:** Both chain balances (60 polls, 10s interval)
- **Assert:** Source decreased by >= amount; dest increased

### L2 -> L2 New Token
- **Deploy** MintableToken on source, mint 100, approve bridge
- **Predict CET** on dest via CETFactory
- **Build source tx:** `bridgeERC20To(destChainId, token, amount, receiver, sessionId)`
- **Build dest tx:** `receiveTokens(msgHeader)` with `label: "SEND_TOKENS"`
- **Submit + poll + assert**

### L2 -> L2 Existing Token (two-phase)
Same state file pattern as L1->L2 existing token.

### Session ID Generation
```
sessionId = BigInt(Date.now())
```
Simple timestamp-based. Some scripts use a more complex format: `version << 240 | keccak256(addr, nonce, block, salt) >> 16`

---

## L2 -> L1 Tests (100% Go)

### L2 -> L1 ETH (two-step: withdraw + finalize)

**Withdraw:**
1. Call `ComposeL2Bridge.bridgeETHTo(l1ChainId, wallet, 0)` with `msg.value = amount`
2. Extract `MessagePassed` event from receipt (withdrawal hash, nonce, sender, target, value, gasLimit, data)
3. Save state

**Finalize:**
1. Wait for dispute game covering the L2 block (poll `DisputeGameFactory`)
2. Build storage proof: `eth_getProof` on `L2ToL1MessagePasser` contract for the withdrawal hash slot
3. Call `ComposePortal.proveWithdrawalTransaction(withdrawalTx, disputeGameIndex, outputRootProof, storageProof)`
4. Wait for proof maturity (poll, can take hours)
5. Call `ComposePortal.finalizeWithdrawalTransaction(withdrawalTx)`
6. Assert L1 ETH increased

---

## L2 -> L2 Smart Account Tests (Go + TypeScript helper)

Go handles everything EXCEPT steps marked with [TS]:

1. **Setup config** — Go
2. **[TS] Create smart accounts** — call TS helper, get back SA address
3. **Fund EntryPoint** — Go: `EntryPoint.depositTo(saAddress)` if deposit < 0.05 ETH
4. **Fund SA** — Go: send ETH to SA address
5. **Deploy/mint tokens** — Go (for token tests)
6. **[TS] Create + sign UserOps** — call TS helper with call data, get back signed `handleOps` raw txs
7. **Submit via sidecar** — Go: POST to sidecar with raw txs
8. **Poll receipts** — Go: `eth_getTransactionReceipt`
9. **Check UserOp success** — Go: parse `UserOperationEvent` log, check `success` field
10. **Assert balances** — Go

See `ethera-sdk.md` for the TypeScript helper interface.

---

## Common Go Operations

### ABI Encoding
Use `go-ethereum/accounts/abi` to encode:
- `bridgeEthTo(uint256 sessionId, uint256 destChainId, address receiver)`
- `receiveETH((uint256 chainSrc, uint256 chainDest, address sender, address receiver, uint256 sessionId, string label))`
- `bridgeERC20To(uint256 destChainId, address token, uint256 amount, address receiver, uint256 sessionId)`
- `receiveTokens((uint256 chainSrc, uint256 chainDest, address sender, address receiver, uint256 sessionId, string label))`
- `handleOps(PackedUserOperation[] ops, address beneficiary)` (for SA sidecar mode)
- `depositTo(address)`, `balanceOf(address)`, `predictAddress(address, uint256)`

### Transaction Signing
Standard go-ethereum `types.SignTx` with `types.NewLondonSigner(chainId)` for EIP-1559 txs.

### Polling Pattern
```go
for i := 0; i < maxPolls; i++ {
    balance := getBalance(address)
    if balance > balanceBefore {
        return // success
    }
    time.Sleep(pollInterval)
}
t.Fatal("timed out")
```

### UserOperationEvent Parsing
To check if an SA inner call succeeded, parse the `UserOperationEvent` log:
- Topic0: `0x49628fd1471006c1482da88028e9ce4dbb080b815c9b0344d39e5a8e6ec1419f`
- Data layout: `(uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)`
- `success` is at bytes 32-64 of the data field

### Contract ABIs
All ABIs are in:
- `sepolia-prod/L1/abis/` — ComposeL1Bridge.json
- `sepolia-prod/L2/abis/` — ComposeL2ToL2Bridge.json, CETFactory.json
- EntryPoint v0.7 ABI is standard (address: `0x0000000071727De22E5E9d8BAf0edAc6f37da032`)
