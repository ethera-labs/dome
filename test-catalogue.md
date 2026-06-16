# Test Catalogue

A plain-language inventory of every test in the framework. Each entry lists the
file and the directions it covers.

Run any test with:

```bash
make test-<network> TEST_FILE=<file> [SOURCE=a|b|l1] [DEST=a|b|l1] [AMOUNT=<eth-or-tokens>]
```

---

## L1 ↔ L2 (deposit)

1. **Send ETH from L1 to a rollup.** — `l1_to_l2_eth_test.go` (rollup A and rollup B)
2. **Send ETH from L1 to a rollup at stress (N parallel accounts).** — `l1_to_l2_eth_stress_test.go`
3. **Deploy a new ERC-20 on L1 and bridge it to a rollup (CET deployed on L2).** — `l1_to_l2_new_token_test.go`
4. **Bridge a previously-deployed ERC-20 from L1 to a rollup (two-phase: deploy once, bridge repeatedly).** — `l1_to_l2_existing_token_test.go`
5. **Bridge a new ERC-20 from L1 to a rollup at stress (first account deploys CET, the rest reuse it).** — `l1_to_l2_new_token_stress_test.go`

> ### 🚧 BLOCKED — pending contracts deployment
>
> The two tests below are wired up and asserts are complete, but they fail at
> the **precondition step** on every configured network because the Kernel
> multichain ECDSA validator (and on Hoodi, the entire Kernel stack) is **not
> deployed on L1**. They will start passing as-is once the contracts team
> deploys the AA infrastructure on L1. See `to-do.md` item 1 for tracking.
>
> 6. **🚧 Send ETH from L1 to a rollup using a smart account.** — `l1_to_l2_sa_eth_test.go` *(blocked: multichain validator missing on L1)*
> 7. **🚧 Deploy a new ERC-20 on L1 and bridge it to a rollup using a smart account.** — `l1_to_l2_sa_new_token_test.go` *(blocked: multichain validator missing on L1)*

## L2 ↔ L2 (cross-rollup, EOA)

8. **Send ETH between two rollups via composed cross-chain transaction.** — `l2_to_l2_eth_test.go` (A↔B)
9. **Deploy a new ERC-20 on one rollup and bridge it to the other (CET deployed on destination).** — `l2_to_l2_new_token_test.go` (A↔B)
10. **Bridge a previously-deployed ERC-20 between rollups (two-phase).** — `l2_to_l2_existing_token_test.go` (A↔B)
11. **Manually send tokens from one rollup to another (non-atomic, two-step coordinator-relayed).** — `l2_to_l2_manual_test.go`

## L2 ↔ L2 (cross-rollup, Smart Account / ERC-4337)

12. **Send ETH between rollups using a smart account.** — `l2_to_l2_sa_eth_test.go` (A↔B)
13. **Deploy a new ERC-20 and bridge it between rollups using a smart account; CET ends up on the EOA on destination.** — `l2_to_l2_sa_new_token_test.go` (A↔B)
14. **Bridge a previously-deployed ERC-20 between rollups using a smart account (two-phase).** — `l2_to_l2_sa_existing_token_test.go` (A↔B)
15. **Bridge ERC-20 between rollups using N smart accounts at stress (multi-SA mode and single-SA repeated mode).** — `l2_to_l2_sa_existing_token_stress_test.go`

## L2 → L1 (withdrawal)

16. **Withdraw ETH from a rollup back to L1 (two-phase: withdraw + prove/finalize after the dispute window).** — `l2_to_l1_eth_test.go` (per rollup)
17. **Round-trip an ERC-20 L1 → L2 → L1 with full proof + finalize (two-phase).** — `l2_to_l1_token_test.go` (per rollup)

## Original local-testnet suite (sidecar atomic semantics)

18. **Mint MockL2ERC20 on both rollups as a single atomic XT.** — `bridge_test.go`
19. **Bridge MockL2ERC20 tokens between rollups via the sidecar (A↔B).** — `bridge_test.go`
20. **Pair a valid bridge send on A with a failing self-transfer on B and check the sidecar aborts both.** — `bridge_test.go`
21. **Pair a valid bridge send on A with an under-gassed receive on B and check the sidecar aborts both.** — `bridge_test.go`
22. **Pair a self-transfer on A with a receiveTokens-without-send on B and check the sidecar aborts both.** — `bridge_test.go`
23. **Pair a valid self-transfer on A with an overdraft on B and verify atomic abort leaves both balances unchanged.** — `uncorrelated_tx_test.go`

## Stress (local-testnet)

24. **Send many bridge XTs back-to-back from the same account.** — `stress_test.go`
25. **Spawn many fresh accounts and have each one bridge once.** — `stress_test.go`
26. **Spawn several accounts and have each one bridge multiple times.** — `stress_test.go`
27. **Bridge back and forth between A ↔ B in alternating XTs.** — `stress_test.go`
28. **Interleave bridge XTs with normal self-transfers from the same account.** — `stress_test.go`

## XT mailbox / nonce edge cases (local-testnet)

29. **Race two XTs that share a mailbox slot.** — `xt_nonce_race_test.go`
30. **Submit two XTs that putInbox the same nonce.** — `xt_put_inbox_nonce_test.go`
31. **Drift the XT state between rollups and check the sidecar detects it.** — `xt_state_drift_test.go`

---

**Direction filter:** every test that exposes two directions (A↔B or per-rollup)
honours `SOURCE` and `DEST` env vars — set them via the `make` knobs to run
exactly one direction.

**Amount override:** add `AMOUNT=0.01` (ETH for ETH tests, tokens for token
tests) or `AMOUNT_WEI=<wei>`.

---

## L2 → L1 (withdrawal, Smart Account / ERC-4337)

These work today on every configured network — unlike the L1↔L2 SA tests, the
SA only ever executes on L2 (where the Kernel + multichain validator are
deployed). The L1 prove + finalize step is plain EOA-driven against the portal,
so no L1 AA infrastructure is required.

32. **Withdraw ETH from a rollup back to L1 using a smart account (two-phase: SA-driven L2 burn + EOA-driven L1 prove/finalize).** — `l2_to_l1_sa_eth_test.go` (per rollup)
33. **Round-trip an ERC-20 L1 → L2 → L1 where the L2 burn is done by a smart account (two-phase).** — `l2_to_l1_sa_token_test.go` (per rollup)

## Same-L2 CET → core redemption

Burn a wrapped CET back into its core CET on the same rollup via
`ComposeL2ToL2Bridge.redeemWrappedCET` — no XT, single rollup. Setup deploys
both CETs in-test (a minimal `IComposableERC20`-compatible contract from
`internal/helpers/test_cet_deploy.go`) and authorizes the production bridge on
each; the redeem path itself is fully production-bridge.

34. **Redeem a wrapped CET back into its core CET on the same rollup.** — `l2_redeem_wrapped_cet_test.go` (per rollup)

## Replay attacks / idempotency

These tests assert the bridge / portal reject duplicate cross-chain operations
without breaking the original successful one. Each test runs a real
production-bridge flow once, then re-submits the same operation under fresh
nonces and decodes the on-chain revert selector.

35. **Submit the same XT (same sessionId) twice — second attempt must be aborted by the sidecar AND eth_call on `receiveETH` must revert with the mailbox's `MessageAlreadyConsumed()` selector. Asserts source + dest ETH balances are unchanged across the aborted XT.** — `xt_replay_test.go` (A↔B)
36. **L2→L1 withdrawal: prove twice (re-prove tolerated as revert or no-op, but first proof must stand) then finalize twice. Re-finalize must revert with `OptimismPortal_AlreadyFinalized` and `finalizedWithdrawals[hash]` must remain true. Two-phase (withdraw + replay).** — `l2_to_l1_replay_test.go` (per rollup).
    > ⚠️ **Stage gate:** Phase 2 can't run on sepolia-stage today — the DGF has `gameCount=0` (no dispute games to prove against) and `proofMaturityDelaySeconds=604800` (7 days). Run Phase 1, wait ≥ 7 days on a network that publishes games, then run Phase 2. Same gate as `TestL2ToL1_ETH_Finalize_*`.
