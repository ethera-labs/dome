# To-do — Missing tests

Inventory of test coverage gaps in this framework. Items are ordered by value,
not by implementation order.

---

## High-value gaps

### 1. L1 ↔ L2 with smart accounts - DONE 
Bridge ETH and ERC-20 from L1 to a rollup using an ERC-4337 smart account
(rather than the funder EOA). Mirrors the existing L1 → L2 tests but the L1
sender is the SA. Exercises the L1 portal under a UserOp / `handleOps` envelope.

- New file: `test/l1_to_l2_sa_eth_test.go`
- New file: `test/l1_to_l2_sa_new_token_test.go`
- Notes: EntryPoint on L1 needs a deposit for the SA; gas overrides may differ
  from L2-side bridging.

### 2. L2 → L1 with smart accounts - DONE
Withdraw ETH and ERC-20 from a rollup back to L1 with the SA as sender.
Uses the same prove/finalize machinery from `helpers/l2_withdraw.go` but the
`MessagePassed` event will name the SA (not an EOA) as `sender`.

- New file: `test/l2_to_l1_sa_eth_test.go`
- New file: `test/l2_to_l1_sa_token_test.go`
- Notes: verify portal accepts SA-originated withdrawals; double-check the
  proof's `withdrawalSender` matches the SA address.

### 3. CET → original token redemption (same-L2 round-trip) - DONE
Burn a CET on the *same* L2 it was minted on via
`ComposeL2ToL2Bridge.redeemWrappedCET(wrappedCET, coreCET, amount)` and verify
the corresponding core CET balance increases. Today we only exercise this path
indirectly via the L1↔L2 round-trip.

- New file: `test/l2_redeem_wrapped_cet_test.go`
- Helper: `internal/helpers/test_cet_deploy.go` (deploys a minimal
  IComposableERC20-compatible TestCET as either CORE or WRAPPED).
- Notes: `redeemWrappedCET` only checks `wrappedCET.remoteAsset() == coreCET`
  and `coreCET.cetType() == CORE`, so the test deploys both contracts itself
  and authorizes the production bridge on each. No XT, no L1, single rollup.

### 4. Replay-attack and idempotency - DONE
Submit the same XT twice (same sessionId, same source+dest txs) and assert the
second attempt is rejected cleanly. Likewise, call
`portal.proveWithdrawalTransaction` twice with the same proof and assert the
second call reverts (or is a no-op) without breaking the first proof.

- New file: `test/xt_replay_test.go` — bridges ETH then re-submits with the
  same sessionId; asserts dest-side `receiveETH` eth_call reverts with the
  mailbox's `MessageAlreadyConsumed()` selector AND the sidecar aborts XT_2
  AND both account balances are unchanged after the abort. **Passing on
  sepolia-stage (A→B + B→A).**
- New file: `test/l2_to_l1_replay_test.go` — two-phase. Withdraw on L2, then
  prove twice (re-prove may revert or no-op; first proof must stand) and
  finalize twice. Re-finalize must revert with `OptimismPortal_AlreadyFinalized`
  and `finalizedWithdrawals[hash]` must stay true. Embeds a minimal
  DisputeGameFactory/FaultDisputeGame ABI (just `gameCount`, `gameAtIndex`,
  `extraData`) because the stage config doesn't ship a `dispute-game-factory`
  entry. **Phase 1 (Withdraw) passes on sepolia-stage.** Phase 2 (Replay)
  *cannot* run on stage today: stage's DGF has `gameCount=0` (no dispute
  games published yet) and `proofMaturityDelaySeconds = 604800` (7 days), so
  no covering game exists for the L2 burn block and the proof would never
  mature inside the test's 2h poll budget. Run the Replay phase on a network
  that publishes dispute games (e.g. hoodi) and re-run after the maturity
  window has elapsed against a Phase-1 state file from at least 7 days ago.
  Same constraint as the existing `TestL2ToL1_ETH_Finalize_*`.

### 5. Wrong-destination / wrong-receiver scenarios
Bridge with bad inputs and assert predictable failures:
- `destChainId` of an unregistered chain
- `receiver = address(0)`
- `receiver = contract` that reverts on receive
- bridge to a receiver that doesn't expect the token (no ERC-20 hooks)

- New file: `test/bridge_invalid_args_test.go`
- Notes: each scenario should `t.Fatalf` on a specific revert reason — use
  named error selectors from the contract ABI to confirm the correct branch.

---

## Medium-value gaps

### 6. Two-CET distinct-wrappers test
Bridge token T from A → B (creates CET₁ = `predictAddress(T, chainA)` on B),
then bridge that CET₁ from B → A. Assert the result on A is a **new** CET₂ =
`predictAddress(CET₁, chainB)`, distinct from the original T on A. Confirms
the wrapping is one-way and never short-circuits.

- New file: `test/l2_to_l2_two_cet_roundtrip_test.go`
- Notes: today we touch this in `TestStressAtoBAndBtoA` but never assert the
  CET addresses are distinct from each other and from T.

### 7. L2 ↔ L2 EOA stress on remote networks
Port the local-testnet stress tests (`TestStress*`) to run against
hoodi / sepolia-prod / sepolia-stage. Today they only run on the local
sidecar; the compose-sequencer-RPC path (hoodi, prod) is never stress-tested.

- New file: `test/l2_to_l2_eth_stress_test.go`
- New file: `test/l2_to_l2_existing_token_stress_test.go`
- Notes: use `BRIDGE_STRESS_ACCOUNTS` env var override; reuse the derived
  accounts pattern from `deriveL1Accounts`.

### 8. L2 → L1 withdrawal stress
N parallel withdrawals from the same rollup. Exercises the dispute-game scan,
storage-proof building, and prove/finalize machinery under concurrent load.

- New file: `test/l2_to_l1_eth_stress_test.go`
- Notes: each account does its own withdraw → save N state files → finalize
  phase iterates and proves/finalizes each. Failure modes: same dispute game
  must serve multiple proofs; portal mustn't reject parallel proves.

### 9. EntryPoint deposit exhaustion
SA with an EntryPoint deposit set below the gas cost. The `handleOps` tx
should fail before the inner UserOp runs. Verify the EOA bundler is not
charged and that the SA's balance is untouched.

- New file: `test/l2_to_l2_sa_underfunded_test.go`
- Notes: deliberately top up to N wei below `MinEntryPointDeposit`; check the
  EOA's pre/post-balance and the EntryPoint deposit delta.

### 10. Mixed-asset composed XT
A single composed XT that bridges ETH on chain A and a different ERC-20 on
chain B in the same atomic submission. Confirms the mailbox correctly handles
heterogeneous message labels (`SEND_ETH` + `SEND_TOKENS`) in one XT.

- New file: `test/l2_to_l2_mixed_asset_test.go`
- Notes: requires four signed txs (2 per chain × 2 chains) submitted in a
  single XT payload.

---

## Low-value but cheap to add

### 11. Event-emission asserts
Parse and assert specific events from every bridge test:
- `TokensReceived(address token, uint256 amount)` on dest after `receiveTokens`
- `ETHReceived(address receiver, uint256 amount)` after `receiveETH`
- `MailboxWrite` / `MailboxAckWrite` on source/dest
- `WrappedCETRedeemed` on `redeemWrappedCET` (when test 3 lands)

- Extend: every existing token/ETH test in `test/`
- Notes: add helpers in `internal/helpers/events.go` for parsing each.

### 12. Lockbox balance assertions
On L1, after a successful L1→L2 ETH bridge, `ComposeETHLockbox` balance
should increase by the bridged amount; after L2→L1 finalize, it should
decrease. Same for `ComposeERC20Lockbox` with the token round-trip.

- Extend: `l1_to_l2_eth_test.go`, `l2_to_l1_eth_test.go`, `l2_to_l1_token_test.go`
- Notes: read the lockbox addresses from config (`eth-lockbox`,
  `erc20-lockbox`).

### 13. Mailbox state assertions
After a successful delivery, assert
`UniversalBridgeMailbox.consumedKeys[messageKey] == true` and the mailbox
root matches the per-chain root reported by both sides. Confirms the message
was actually consumed, not just that the bridge state happened to agree.

- Extend: every L2↔L2 bridge test
- Notes: requires reading mailbox storage; use the existing `MailboxABI`.

---

## Out of scope (intentionally not listed)

- Upgrade / pause / access-control tests — belong in the contracts repo.
- Gas-cost regression benchmarks — separate harness.
- Forge / fuzz tests on individual contracts — separate harness.
