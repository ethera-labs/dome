// L2 -> L1 ETH withdrawal driven by an ERC-4337 smart account on L2.
//
// Unlike item 1 (L1 SA → L2), this flow works on every configured network
// because the AA stack is deployed on L2 — the SA initiates the withdrawal
// on L2 via a UserOp, and an EOA on L1 (the funder) does the prove + finalize.
//
// Two-phase:
//   _Withdraw  — SA calls ComposeL2Bridge.bridgeETHTo on the source rollup
//                inside a UserOp. We assert the resulting MessagePassed.sender
//                IS the SA (not the funder EOA) — that's the only test-meaningful
//                difference vs. the EOA L2→L1 ETH test.
//   _Finalize  — same as the EOA finalize but loaded from the SA state file.
package test

import (
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

type l2ToL1SAETHState struct {
	Source             string         `json:"source"`
	SAAddr             common.Address `json:"saAddr"`
	L1Receiver         common.Address `json:"l1Receiver"`
	WithdrawalHash     common.Hash    `json:"withdrawalHash"`
	WithdrawalNonce    string         `json:"withdrawalNonce"`
	WithdrawalSender   common.Address `json:"withdrawalSender"`
	WithdrawalTarget   common.Address `json:"withdrawalTarget"`
	WithdrawalValue    string         `json:"withdrawalValue"`
	WithdrawalGasLimit string         `json:"withdrawalGasLimit"`
	WithdrawalData     []byte         `json:"withdrawalData"`
	L2TxHash           common.Hash    `json:"l2TxHash"`
	L2Block            uint64         `json:"l2Block"`
	L1ReceiverBefore   string         `json:"l1ReceiverBefore"`
	Amount             string         `json:"amount"`
}

func l2ToL1SAETHStateName(src *rollup.Rollup) string {
	return fmt.Sprintf(".l2-to-l1-sa-eth-state-%s.json", src.Name())
}

var l2ToL1SAETHAmount = big.NewInt(10_000_000_000_000_000) // 0.01 ETH

func TestL2ToL1_SA_ETH_Withdraw_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "l1")
	RequireL1(t)
	RequireAA(t)
	RequireTSRuntime(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1SAETHWithdraw(t, configs.ChainNameRollupA, TestAccountA, TestRollupA)
}

func TestL2ToL1_SA_ETH_Withdraw_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "l1")
	RequireL1(t)
	RequireAA(t)
	RequireTSRuntime(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1SAETHWithdraw(t, configs.ChainNameRollupB, TestAccountB, TestRollupB)
}

func TestL2ToL1_SA_ETH_Finalize_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "l1")
	RequireL1(t)
	runL2ToL1SAETHFinalize(t, configs.ChainNameRollupA, TestRollupA)
}

func TestL2ToL1_SA_ETH_Finalize_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "l1")
	RequireL1(t)
	runL2ToL1SAETHFinalize(t, configs.ChainNameRollupB, TestRollupB)
}

func runL2ToL1SAETHWithdraw(
	t *testing.T,
	src configs.ChainName,
	srcFunder *accounts.Account,
	srcChain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAmount := helpers.ParseBridgeAmountOverride(l2ToL1SAETHAmount)
	pk := configs.Values.WalletPrivateKey
	l2ChainID := uint64(srcChain.ChainID().Int64())
	l1Receiver := TestL1Account.GetAddress()

	// 1. Derive SA on L2.
	saAddr, _, err := helpers.SACreateAccount(ctx, pk, l2ChainID, []uint64{l2ChainID})
	require.NoError(t, err)
	helpers.LogAssertOK("SA derived on %s: %s", srcChain.Name(), saAddr.Hex())
	require.NotEqualf(t, common.Address{}, saAddr,
		"SA address must be non-zero (zero usually means the factory reverted on counterfactual derivation)")

	// 2. Fund EntryPoint on L2 for the SA.
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, srcFunder, saAddr, helpers.MinEntryPointDeposit))
	helpers.LogAssertOK("EntryPoint deposit on %s >= %s wei for SA=%s",
		srcChain.Name(), helpers.MinEntryPointDeposit, saAddr.Hex())

	// 3. Fund SA on L2 with at least the bridge amount.
	require.NoError(t, ensureSABalance(ctx, srcFunder, saAddr, bridgeAmount))
	helpers.LogAssertOK("SA on %s has >= %s wei", srcChain.Name(), bridgeAmount)

	// Snapshot balances on both sides.
	l1BalBefore, err := TestL1Account.GetBalance(ctx)
	require.NoError(t, err)
	saL2Before := readBalance(ctx, srcChain.RPCURL(), saAddr)

	// 4. Build UserOp call: bridgeETHTo(l1Receiver, minGasLimit, 0x) with msg.value=amount.
	l2BridgeAddr := l2BridgeAddressFor(src)
	calldata, err := helpers.PackBridgeETHToOnL2(ComposeL2BridgeABI,
		l1Receiver, uint32(helpers.L1MinGasLimitNewToken), []byte{})
	require.NoError(t, err)

	calls := []helpers.UserOpCall{
		{
			ChainID: l2ChainID,
			To:      l2BridgeAddr,
			Value:   bridgeAmount.String(),
			Data:    hexutil.Encode(calldata),
		},
	}

	// 5. Sign + send handleOps to L2 directly (no sidecar — single-chain UserOp).
	canonical, err := helpers.SACreateUserOps(ctx, pk, calls, nil)
	require.NoError(t, err)
	require.NotEmpty(t, canonical)
	helpers.LogAssertOK("TS helper produced %d signed UserOp(s) on %s", len(canonical), srcChain.Name())

	signedTx, _, err := helpers.BuildHandleOpsRawTx(ctx, srcFunder, canonical)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, signedTx, srcChain.RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(ctx, signedTx.Hash(), srcChain)
	require.NoError(t, err)
	logger.Info("[L2->L1 SA ETH %s] handleOps tx %s", srcChain.Name(), signedTx.Hash().Hex())
	helpers.LogAssertOK("L2 handleOps receipt status=Successful (%s)", signedTx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status,
		"L2 handleOps tx reverted: %s", signedTx.Hash().Hex())

	// 6. Inner UserOp must have succeeded.
	success, _, err := helpers.CheckUserOpSuccess(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("L2 UserOperationEvent.success=true (got=%v)", success)
	require.Truef(t, success, "inner UserOp on L2 reverted (bridgeETHTo failed)")

	// 7. Extract MessagePassed from the inner receipt.
	wtx, withdrawalHash, err := helpers.ExtractMessagePassed(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("MessagePassed event extracted: withdrawalHash=%s l2Block=%d",
		withdrawalHash.Hex(), receipt.BlockNumber.Uint64())

	// 8. Proxy for "SA initiated the withdrawal": SA L2 balance dropped by
	//    exactly the bridge amount (gas is paid from EntryPoint deposit, not
	//    SA balance). Note: the MessagePassed.sender field is the
	//    L2CrossDomainMessenger predeploy in OP-stack — it does NOT carry the
	//    original caller, so we can't assert sender == SA directly here.
	saL2After := readBalance(ctx, srcChain.RPCURL(), saAddr)
	saL2Decrease := new(big.Int).Sub(saL2Before, saL2After)
	helpers.LogAssertOK("SA L2 balance decreased by exactly bridge amount: delta=%s want=%s",
		saL2Decrease, bridgeAmount)
	require.Equalf(t, 0, saL2Decrease.Cmp(bridgeAmount),
		"SA L2 balance delta mismatch: got=%s want=%s (SA should have spent exactly the bridge amount; gas comes from EntryPoint)",
		saL2Decrease, bridgeAmount)

	// Save state for the finalize phase.
	state := l2ToL1SAETHState{
		Source:             srcChain.Name(),
		SAAddr:             saAddr,
		L1Receiver:         l1Receiver,
		WithdrawalHash:     withdrawalHash,
		WithdrawalNonce:    wtx.Nonce.String(),
		WithdrawalSender:   wtx.Sender,
		WithdrawalTarget:   wtx.Target,
		WithdrawalValue:    wtx.Value.String(),
		WithdrawalGasLimit: wtx.GasLimit.String(),
		WithdrawalData:     wtx.Data,
		L2TxHash:           signedTx.Hash(),
		L2Block:            receipt.BlockNumber.Uint64(),
		L1ReceiverBefore:   l1BalBefore.String(),
		Amount:             bridgeAmount.String(),
	}
	require.NoError(t, helpers.SaveJSONState(l2ToL1SAETHStateName(srcChain), state))
}

func runL2ToL1SAETHFinalize(t *testing.T, src configs.ChainName, srcChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()

	var state l2ToL1SAETHState
	found, err := helpers.LoadJSONState(l2ToL1SAETHStateName(srcChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run TestL2ToL1_SA_ETH_Withdraw_%s first",
			l2ToL1SAETHStateName(srcChain), srcChain.Name())
	}
	helpers.LogAssertOK("loaded SA withdrawal state for %s: hash=%s saAddr=%s",
		srcChain.Name(), state.WithdrawalHash.Hex(), state.SAAddr.Hex())

	portalAddr := portalAddressFor(src)
	wtx := saStateToWithdrawalTx(state)

	// Sanity: state file should still name the SA (recorded for audit) and the
	// withdrawal hash should match.
	helpers.LogAssertOK("loaded state SA=%s withdrawalHash=%s", state.SAAddr.Hex(), state.WithdrawalHash.Hex())
	require.NotEqualf(t, common.Address{}, state.SAAddr, "loaded SA address is zero — state file corrupted?")

	// Already finalized?
	var alreadyFinalized bool
	err = helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &alreadyFinalized, state.WithdrawalHash)
	require.NoError(t, err)
	if alreadyFinalized {
		logger.Info("[L2->L1 SA ETH %s] already finalized", srcChain.Name())
		require.NoError(t, helpers.DeleteJSONState(l2ToL1SAETHStateName(srcChain)))
		return
	}

	// Prove if not yet proven.
	var numSubmitters *big.Int
	err = helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"numProofSubmitters", &numSubmitters, state.WithdrawalHash)
	require.NoError(t, err)
	if numSubmitters.Sign() == 0 {
		require.NoError(t, proveSAL2ToL1Withdrawal(t, portalAddr, srcChain, state, wtx))
	} else {
		logger.Info("[L2->L1 SA ETH %s] already proven (%d submitters); skipping prove step",
			srcChain.Name(), numSubmitters)
	}

	// Wait for maturity.
	require.NoErrorf(t,
		helpers.WaitForProofMaturity(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
			state.WithdrawalHash, TestL1Account.GetAddress(),
			20*time.Second, helpers.DefaultPollAttempts*6,
		),
		"[L2->L1 SA ETH %s] proof not yet mature — re-run finalize after the dispute window",
		srcChain.Name(),
	)

	// Finalize.
	finalizeData, err := helpers.PackFinalizeWithdrawal(ComposePortalABI, wtx)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, finalizeData)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 finalize tx confirmed for withdrawalHash=%s", state.WithdrawalHash.Hex())

	// Portal should report finalized.
	var finalizedNow bool
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &finalizedNow, state.WithdrawalHash))
	helpers.LogAssertOK("portal.finalizedWithdrawals(%s) == true (got=%v)",
		state.WithdrawalHash.Hex(), finalizedNow)
	require.True(t, finalizedNow, "portal.finalizedWithdrawals should be true after finalize call")

	// L1 receiver (funder EOA) balance should reflect the withdrawal modulo gas the
	// funder paid for prove+finalize.
	l1Before, _ := new(big.Int).SetString(state.L1ReceiverBefore, 10)
	l1After, err := TestL1Account.GetBalance(ctx)
	require.NoError(t, err)
	delta := new(big.Int).Sub(l1After, l1Before)
	amount, _ := new(big.Int).SetString(state.Amount, 10)
	gasBudget := new(big.Int).Mul(big.NewInt(10), big.NewInt(1_000_000_000_000_000)) // 0.01 ETH
	helpers.LogAssertOK("L1 receiver balance after finalize reflects withdrawal: delta=%s want>=%s-gas(%s)",
		delta, amount, gasBudget)
	require.GreaterOrEqualf(t, delta.Cmp(new(big.Int).Sub(amount, gasBudget)), 0,
		"L1 receiver balance after finalize should reflect withdrawal: delta=%s want>=%s",
		delta, new(big.Int).Sub(amount, gasBudget))

	require.NoError(t, helpers.DeleteJSONState(l2ToL1SAETHStateName(srcChain)))
}

func saStateToWithdrawalTx(state l2ToL1SAETHState) helpers.WithdrawalTx {
	n, _ := new(big.Int).SetString(state.WithdrawalNonce, 10)
	v, _ := new(big.Int).SetString(state.WithdrawalValue, 10)
	g, _ := new(big.Int).SetString(state.WithdrawalGasLimit, 10)
	return helpers.WithdrawalTx{
		Nonce: n, Sender: state.WithdrawalSender, Target: state.WithdrawalTarget,
		Value: v, GasLimit: g, Data: state.WithdrawalData,
	}
}

// proveSAL2ToL1Withdrawal mirrors proveL2ToL1Withdrawal (EOA version) but takes
// the SA state shape. The on-chain prove step is identical — the portal trusts
// the L2ToL1MessagePasser storage proof regardless of the original sender.
func proveSAL2ToL1Withdrawal(
	t *testing.T,
	portalAddr common.Address,
	srcChain *rollup.Rollup,
	state l2ToL1SAETHState,
	wtx helpers.WithdrawalTx,
) error {
	ctx := t.Context()

	var dgfAddr common.Address
	if err := helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"disputeGameFactory", &dgfAddr); err != nil {
		return fmt.Errorf("portal.disputeGameFactory: %w", err)
	}
	var gameType uint32
	if err := helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"respectedGameType", &gameType); err != nil {
		return fmt.Errorf("portal.respectedGameType: %w", err)
	}

	gameIndex, coveredBlock, err := helpers.FindCoveringDisputeGame(ctx, TestL1.RPCURL(),
		dgfAddr, helpers.MinimalDisputeGameABI, helpers.MinimalDisputeGameABI,
		gameType, state.L2Block, 30*time.Second, helpers.DefaultPollAttempts*6,
	)
	if err != nil {
		return fmt.Errorf("find dispute game: %w", err)
	}
	logger.Info("[L2->L1 SA ETH %s] game=%d covers L2 block %d", srcChain.Name(), gameIndex, coveredBlock)

	outProof, storageProof, err := helpers.BuildWithdrawalProof(ctx, srcChain.RPCURL(),
		state.WithdrawalHash, coveredBlock)
	if err != nil {
		return fmt.Errorf("build proof: %w", err)
	}

	proveData, err := helpers.PackProveWithdrawal(ComposePortalABI, wtx, gameIndex, outProof, storageProof)
	if err != nil {
		return fmt.Errorf("pack prove: %w", err)
	}
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, proveData)
	return err
}
