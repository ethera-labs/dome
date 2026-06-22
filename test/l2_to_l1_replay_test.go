// Replay / idempotency test for the L2->L1 withdrawal flow.
//
// Two phases (same shape as l2_to_l1_eth_test.go):
//
//   _Withdraw  — burn ETH on L2 via ComposeL2Bridge.bridgeETHTo, extract the
//                MessagePassed event and save withdrawal state to a state file.
//   _Replay    — load state, prove the withdrawal twice, wait for the dispute
//                window, finalize it twice, and assert:
//                  * first prove + first finalize succeed
//                  * re-prove either reverts cleanly OR is a no-op, with the
//                    first proof intact (numProofSubmitters stays >= 1)
//                  * re-finalize REVERTS with the
//                    OptimismPortal_AlreadyFinalized custom-error selector
//                  * portal.finalizedWithdrawals(hash) is still true after the
//                    failed re-finalize (i.e. the first finalize wasn't broken)
//
// Maturity window: identical to the regular L2->L1 finalize test. _Replay
// polls checkWithdrawal for up to ~1h and fails (no skip) if the proof never
// matures — re-run after the dispute game window has elapsed.
package test

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

// Same withdrawal amount as the regular L2->L1 ETH test — the replay logic
// doesn't care about magnitude, just that the proof is valid.
var l2ToL1ReplayAmount = big.NewInt(10_000_000_000_000_000) // 0.01 ETH

func l2ToL1ReplayStateName(src *rollup.Rollup) string {
	return fmt.Sprintf(".l2-to-l1-replay-eth-state-%s.json", src.Name())
}

func TestL2ToL1Replay_ETH_Withdraw_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "l1")
	RequireL1(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1ReplayWithdraw(t, configs.ChainNameRollupA, TestAccountA, TestRollupA)
}

func TestL2ToL1Replay_ETH_Withdraw_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "l1")
	RequireL1(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1ReplayWithdraw(t, configs.ChainNameRollupB, TestAccountB, TestRollupB)
}

func TestL2ToL1Replay_ETH_Replay_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "l1")
	RequireL1(t)
	runL2ToL1ReplayReplay(t, configs.ChainNameRollupA, TestRollupA)
}

func TestL2ToL1Replay_ETH_Replay_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "l1")
	RequireL1(t)
	runL2ToL1ReplayReplay(t, configs.ChainNameRollupB, TestRollupB)
}

// ---- Phase 1: Withdraw (mirrors l2_to_l1_eth_test.go) -----------------------

func runL2ToL1ReplayWithdraw(t *testing.T, src configs.ChainName, ac *accounts.Account, srcChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()

	l1BalBefore, err := TestL1Account.GetBalance(ctx)
	require.NoError(t, err)
	l2BalBefore, err := ac.GetBalance(ctx)
	require.NoError(t, err)
	helpers.LogAssertOK("L2 has sufficient balance: %s want>=%s", l2BalBefore, l2ToL1ReplayAmount)
	require.GreaterOrEqualf(t, l2BalBefore.Cmp(l2ToL1ReplayAmount), 0,
		"L2 balance %s < withdraw amount %s", l2BalBefore, l2ToL1ReplayAmount)

	l2BridgeAddr := l2BridgeAddressFor(src)
	calldata, err := helpers.PackBridgeETHToOnL2(ComposeL2BridgeABI,
		ac.GetAddress(), uint32(helpers.L1MinGasLimitNewToken), []byte{})
	require.NoError(t, err)

	tx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: l2BridgeAddr, Value: l2ToL1ReplayAmount, Gas: helpers.L1BridgeGasLimit,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: calldata,
	}, ac)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, tx, srcChain.RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), srcChain)
	require.NoError(t, err)
	helpers.LogAssertOK("L2 burn tx receipt status=Successful (%s)", tx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)

	wtx, withdrawalHash, err := helpers.ExtractMessagePassed(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("MessagePassed event extracted: withdrawalHash=%s l2Block=%d",
		withdrawalHash.Hex(), receipt.BlockNumber.Uint64())

	state := l2ToL1ETHState{
		Source:             srcChain.Name(),
		WithdrawalHash:     withdrawalHash,
		WithdrawalNonce:    wtx.Nonce.String(),
		WithdrawalSender:   wtx.Sender,
		WithdrawalTarget:   wtx.Target,
		WithdrawalValue:    wtx.Value.String(),
		WithdrawalGasLimit: wtx.GasLimit.String(),
		WithdrawalData:     wtx.Data,
		L2TxHash:           tx.Hash(),
		L2Block:            receipt.BlockNumber.Uint64(),
		L1BalanceBefore:    l1BalBefore.String(),
		Amount:             l2ToL1ReplayAmount.String(),
	}
	require.NoError(t, helpers.SaveJSONState(l2ToL1ReplayStateName(srcChain), state))
}

// ---- Phase 2: Replay (prove twice, finalize twice) --------------------------

func runL2ToL1ReplayReplay(t *testing.T, src configs.ChainName, srcChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()

	var state l2ToL1ETHState
	found, err := helpers.LoadJSONState(l2ToL1ReplayStateName(srcChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run TestL2ToL1Replay_ETH_Withdraw_%s first",
			l2ToL1ReplayStateName(srcChain), srcChain.Name())
	}

	portalAddr := portalAddressFor(src)
	wtx := stateToWithdrawalTx(state)

	// Pre-flight invariant: we want a fresh withdrawal. If state points at one
	// already finalized, the replay assertions can't run meaningfully.
	var alreadyFinalized bool
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &alreadyFinalized, state.WithdrawalHash))
	helpers.LogAssertOK("portal.finalizedWithdrawals(%s) == false at start (got=%v)",
		state.WithdrawalHash.Hex(), alreadyFinalized)
	require.Falsef(t, alreadyFinalized,
		"withdrawal %s is already finalized — re-run TestL2ToL1Replay_ETH_Withdraw_%s for a fresh one",
		state.WithdrawalHash.Hex(), srcChain.Name())

	// ---- Prove #1 -----------------------------------------------------------
	gameIndex, outProof, storageProof := buildL2ToL1Proof(t, srcChain, state, portalAddr)
	proveCalldata, err := helpers.PackProveWithdrawal(ComposePortalABI, wtx, gameIndex, outProof, storageProof)
	require.NoError(t, err)

	var numSubmittersBefore *big.Int
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"numProofSubmitters", &numSubmittersBefore, state.WithdrawalHash))
	helpers.LogAssertOK("portal.numProofSubmitters(%s) before prove #1: %s (want 0)",
		state.WithdrawalHash.Hex(), numSubmittersBefore)
	require.Zerof(t, numSubmittersBefore.Sign(),
		"expected no proof submitters yet, got %s", numSubmittersBefore)

	prove1Tx, prove1Receipt, err := helpers.SendL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, proveCalldata)
	require.NoError(t, err, "prove #1 should succeed")
	helpers.LogAssertOK("prove #1 tx receipt status=Successful (%s)", prove1Tx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, prove1Receipt.Status,
		"prove #1 tx reverted: %s", prove1Tx.Hash().Hex())

	var numSubmittersAfter1 *big.Int
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"numProofSubmitters", &numSubmittersAfter1, state.WithdrawalHash))
	helpers.LogAssertOK("portal.numProofSubmitters(%s) after prove #1: %s (want >= 1)",
		state.WithdrawalHash.Hex(), numSubmittersAfter1)
	require.GreaterOrEqualf(t, numSubmittersAfter1.Cmp(big.NewInt(1)), 0,
		"expected at least one proof submitter after prove #1, got %s", numSubmittersAfter1)

	// ---- Prove #2 (replay): observe revert OR no-op; assert idempotency -----
	// The portal may either revert (e.g. already proven by same submitter) or
	// silently update the submitter's entry. Either is acceptable — what we
	// REQUIRE is that prove #1 isn't undone.
	prove2Tx, _, prove2Bytes, err := sendRawL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, proveCalldata)
	require.NoError(t, err, "prove #2 should at least submit (network-wise)")
	_ = prove2Bytes
	_, prove2Receipt, err := transactions.GetTransactionDetails(ctx, prove2Tx.Hash(), TestL1)
	require.NoError(t, err)

	switch prove2Receipt.Status {
	case types.ReceiptStatusSuccessful:
		helpers.LogAssertOK("prove #2 was a no-op (status=Successful, %s) — acceptable per spec",
			prove2Tx.Hash().Hex())
	case types.ReceiptStatusFailed:
		// If it reverted, surface the decoded selector via eth_call so the
		// telemetry is useful — we don't pin a specific error name because the
		// portal version is the source of truth and may change.
		sel, callErr := simulateRevertSelector(ctx, TestL1.RPCURL(), TestL1Account.GetAddress(), portalAddr, proveCalldata)
		if callErr == nil {
			helpers.LogAssertOK("prove #2 reverted with selector=0x%x (%s) — acceptable per spec",
				sel, lookupPortalErrorName(sel))
		} else {
			helpers.LogAssertOK("prove #2 reverted (selector decode failed: %v) — acceptable per spec", callErr)
		}
	default:
		t.Fatalf("prove #2 tx has unexpected receipt status: %d", prove2Receipt.Status)
	}

	var numSubmittersAfter2 *big.Int
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"numProofSubmitters", &numSubmittersAfter2, state.WithdrawalHash))
	helpers.LogAssertOK("portal.numProofSubmitters(%s) after prove #2: %s (want >= 1)",
		state.WithdrawalHash.Hex(), numSubmittersAfter2)
	require.GreaterOrEqualf(t, numSubmittersAfter2.Cmp(big.NewInt(1)), 0,
		"first proof must not have been wiped by re-prove: got %s",
		numSubmittersAfter2)

	// ---- Wait for maturity --------------------------------------------------
	require.NoErrorf(t,
		helpers.WaitForProofMaturity(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
			state.WithdrawalHash, TestL1Account.GetAddress(),
			20*time.Second, helpers.DefaultPollAttempts*6,
		),
		"[L2->L1 replay %s] proof not yet mature — re-run after the dispute window",
		srcChain.Name(),
	)

	// ---- Finalize #1 --------------------------------------------------------
	finalizeCalldata, err := helpers.PackFinalizeWithdrawal(ComposePortalABI, wtx)
	require.NoError(t, err)
	finalize1Tx, finalize1Receipt, err := helpers.SendL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, finalizeCalldata)
	require.NoError(t, err, "finalize #1 should succeed")
	helpers.LogAssertOK("finalize #1 tx receipt status=Successful (%s)", finalize1Tx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, finalize1Receipt.Status)

	var finalizedAfter1 bool
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &finalizedAfter1, state.WithdrawalHash))
	helpers.LogAssertOK("portal.finalizedWithdrawals(%s) == true after finalize #1 (got=%v)",
		state.WithdrawalHash.Hex(), finalizedAfter1)
	require.Truef(t, finalizedAfter1,
		"portal.finalizedWithdrawals should be true after finalize #1: hash=%s",
		state.WithdrawalHash.Hex())

	// ---- Finalize #2 (replay): assert revert with OptimismPortal_AlreadyFinalized
	// Pre-flight via eth_call so we can decode the exact selector before
	// spending L1 gas.
	expectedSelector := ComposePortalABI.Errors["OptimismPortal_AlreadyFinalized"].ID.Bytes()[:4]
	helpers.LogAssertOK("re-finalize eth_call reverts with OptimismPortal_AlreadyFinalized (selector=0x%x) — pre-submit",
		expectedSelector)
	gotSelector, callErr := simulateRevertSelector(ctx, TestL1.RPCURL(), TestL1Account.GetAddress(), portalAddr, finalizeCalldata)
	require.NoErrorf(t, callErr,
		"re-finalize eth_call should return a revert selector: %v", callErr)
	require.Equalf(t, hexutil.Encode(expectedSelector), hexutil.Encode(gotSelector),
		"re-finalize should revert with OptimismPortal_AlreadyFinalized; got selector=0x%x (%s)",
		gotSelector, lookupPortalErrorName(gotSelector))

	// Submit the on-chain re-finalize for symmetry with prove #2 and to get a
	// receipt-level revert confirmation.
	finalize2Tx, _, _, err := sendRawL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, finalizeCalldata)
	require.NoError(t, err, "re-finalize submission should not fail at network layer")
	_, finalize2Receipt, err := transactions.GetTransactionDetails(ctx, finalize2Tx.Hash(), TestL1)
	require.NoError(t, err)
	helpers.LogAssertOK("re-finalize tx receipt status=Failed (%s) — re-finalize must revert on-chain",
		finalize2Tx.Hash().Hex())
	require.Equalf(t, uint64(types.ReceiptStatusFailed), finalize2Receipt.Status,
		"re-finalize should have reverted on-chain: tx=%s status=%d",
		finalize2Tx.Hash().Hex(), finalize2Receipt.Status)

	// Invariant: finalize state is unchanged by the failed re-finalize.
	var finalizedAfter2 bool
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &finalizedAfter2, state.WithdrawalHash))
	helpers.LogAssertOK("portal.finalizedWithdrawals(%s) == true after re-finalize (got=%v)",
		state.WithdrawalHash.Hex(), finalizedAfter2)
	require.Truef(t, finalizedAfter2,
		"portal.finalizedWithdrawals should still be true after failed re-finalize: hash=%s",
		state.WithdrawalHash.Hex())

	logger.Info("[L2->L1 replay %s] state file %s cleaned up",
		srcChain.Name(), l2ToL1ReplayStateName(srcChain))
	require.NoError(t, helpers.DeleteJSONState(l2ToL1ReplayStateName(srcChain)))
}

// ---- helpers ---------------------------------------------------------------

// buildL2ToL1Proof finds the dispute game covering state.L2Block and builds the
// storage proof for the withdrawal hash. Pulled into its own helper because
// both prove #1 and prove #2 reuse the same encoded calldata.
func buildL2ToL1Proof(
	t *testing.T,
	srcChain *rollup.Rollup,
	state l2ToL1ETHState,
	portalAddr common.Address,
) (gameIndex uint64, outProof helpers.OutputRootProof, storageProof [][]byte) {
	t.Helper()
	ctx := t.Context()

	var dgfAddr common.Address
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"disputeGameFactory", &dgfAddr))
	var gameType uint32
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"respectedGameType", &gameType))

	idx, coveredBlock, err := helpers.FindCoveringDisputeGame(ctx, TestL1.RPCURL(),
		dgfAddr, helpers.MinimalDisputeGameABI, helpers.MinimalDisputeGameABI,
		gameType, state.L2Block, 30*time.Second, helpers.DefaultPollAttempts*6,
	)
	require.NoError(t, err)
	logger.Info("[L2->L1 replay %s] found game index=%d covering L2 block %d (need %d)",
		srcChain.Name(), idx, coveredBlock, state.L2Block)

	out, sp, err := helpers.BuildWithdrawalProof(ctx, srcChain.RPCURL(), state.WithdrawalHash, coveredBlock)
	require.NoError(t, err)
	return idx, out, sp
}

// sendRawL1Tx is a SendL1Tx variant that does NOT enforce receipt status==1.
// We use it for prove #2 and finalize #2 because those are EXPECTED to revert.
func sendRawL1Tx(
	ctx context.Context,
	ac *accounts.Account,
	to common.Address,
	value *big.Int,
	gas uint64,
	data []byte,
) (*types.Transaction, *types.Receipt, []byte, error) {
	tx, raw, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        to,
		Value:     value,
		Gas:       gas,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      data,
	}, ac)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("sign l1 tx: %w", err)
	}
	if _, err := transactions.SendTransaction(ctx, tx, ac.GetRollup().RPCURL()); err != nil {
		return tx, nil, raw, fmt.Errorf("send l1 tx: %w", err)
	}
	_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), ac.GetRollup())
	if err != nil {
		return tx, nil, raw, fmt.Errorf("receipt l1 tx: %w", err)
	}
	return tx, receipt, raw, nil
}

// lookupPortalErrorName reverse-maps a 4-byte error selector to the ABI name
// for nicer log messages. Returns "?" if no match.
func lookupPortalErrorName(selector []byte) string {
	if len(selector) < 4 {
		return "?"
	}
	for name, e := range ComposePortalABI.Errors {
		if string(e.ID.Bytes()[:4]) == string(selector) {
			return name
		}
	}
	return "?"
}

