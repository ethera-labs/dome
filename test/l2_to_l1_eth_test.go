// Go port of scripts/l2-to-l1-ETH.ts. Two-phase test:
//
//   _Withdraw   — call ComposeL2Bridge.bridgeETHTo on the source rollup,
//                 extract MessagePassed event, save withdrawal state.
//   _Finalize   — load state, wait for the dispute game that covers the L2
//                 block, build the storage proof, call
//                 portal.proveWithdrawalTransaction, wait for maturity, call
//                 finalizeWithdrawalTransaction.
//
// The maturity wait can be hours on real testnets. _Finalize polls
// checkWithdrawal up to (DefaultPollAttempts × 6) ≈ 1h and fails if the
// proof is not yet mature — re-run after the dispute window has elapsed.
package test

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

type l2ToL1ETHState struct {
	Source             string         `json:"source"`
	WithdrawalHash     common.Hash    `json:"withdrawalHash"`
	WithdrawalNonce    string         `json:"withdrawalNonce"`
	WithdrawalSender   common.Address `json:"withdrawalSender"`
	WithdrawalTarget   common.Address `json:"withdrawalTarget"`
	WithdrawalValue    string         `json:"withdrawalValue"`
	WithdrawalGasLimit string         `json:"withdrawalGasLimit"`
	WithdrawalData     []byte         `json:"withdrawalData"`
	L2TxHash           common.Hash    `json:"l2TxHash"`
	L2Block            uint64         `json:"l2Block"`
	L1BalanceBefore    string         `json:"l1BalanceBefore"`
	Amount             string         `json:"amount"`
}

func l2ToL1ETHStateName(src *rollup.Rollup) string {
	return fmt.Sprintf(".l2-to-l1-eth-state-%s.json", src.Name())
}

// Default withdrawal amount: 0.01 ETH.
var l2ToL1ETHAmount = big.NewInt(10_000_000_000_000_000)

func TestL2ToL1_ETH_Withdraw_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "l1")
	RequireL1(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1ETHWithdraw(t, configs.ChainNameRollupA, TestAccountA, TestRollupA)
}

func TestL2ToL1_ETH_Withdraw_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "l1")
	RequireL1(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1ETHWithdraw(t, configs.ChainNameRollupB, TestAccountB, TestRollupB)
}

func TestL2ToL1_ETH_Finalize_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "l1")
	RequireL1(t)
	runL2ToL1ETHFinalize(t, configs.ChainNameRollupA, TestRollupA)
}

func TestL2ToL1_ETH_Finalize_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "l1")
	RequireL1(t)
	runL2ToL1ETHFinalize(t, configs.ChainNameRollupB, TestRollupB)
}

func l2BridgeAddressFor(rollupName configs.ChainName) common.Address {
	if rollupName == configs.ChainNameRollupA {
		return configs.Values.L2.Contracts[configs.ContractNameL2BridgeA].Address
	}
	return configs.Values.L2.Contracts[configs.ContractNameL2BridgeB].Address
}

func portalAddressFor(rollupName configs.ChainName) common.Address {
	if rollupName == configs.ChainNameRollupA {
		return configs.Values.L1.Contracts[configs.ContractNamePortalA].Address
	}
	return configs.Values.L1.Contracts[configs.ContractNamePortalB].Address
}

func runL2ToL1ETHWithdraw(t *testing.T, src configs.ChainName, ac *accounts.Account, srcChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()

	l1BalBefore, err := TestL1Account.GetBalance(ctx)
	require.NoError(t, err)
	l2BalBefore, err := ac.GetBalance(ctx)
	require.NoError(t, err)
	helpers.LogAssertOK("L2 has sufficient balance: %s want>=%s", l2BalBefore, l2ToL1ETHAmount)
	require.GreaterOrEqualf(t, l2BalBefore.Cmp(l2ToL1ETHAmount), 0,
		"L2 balance %s < withdraw amount %s", l2BalBefore, l2ToL1ETHAmount)

	l2BridgeAddr := l2BridgeAddressFor(src)
	calldata, err := helpers.PackBridgeETHToOnL2(ComposeL2BridgeABI,
		ac.GetAddress(), uint32(helpers.L1MinGasLimitNewToken), []byte{})
	require.NoError(t, err)

	tx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: l2BridgeAddr, Value: l2ToL1ETHAmount, Gas: helpers.L1BridgeGasLimit,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: calldata,
	}, ac)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, tx, srcChain.RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), srcChain)
	require.NoError(t, err)
	helpers.LogAssertOK("L2 burn tx receipt status=Successful (%s)", tx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)

	// Source side: the L2 balance must drop by at least the withdrawn amount
	// (any further delta is gas paid on the burn tx).
	l2BalAfter, err := ac.GetBalance(ctx)
	require.NoError(t, err)
	l2Decrease := new(big.Int).Sub(l2BalBefore, l2BalAfter)
	helpers.LogAssertOK("L2 source decreased by >= withdraw amount: delta=%s want>=%s", l2Decrease, l2ToL1ETHAmount)
	require.GreaterOrEqualf(t, l2Decrease.Cmp(l2ToL1ETHAmount), 0,
		"L2 source should have decreased by >= withdraw amount, got delta=%s", l2Decrease)

	wtx, withdrawalHash, err := helpers.ExtractMessagePassed(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("MessagePassed event extracted: withdrawalHash=%s l2Block=%d", withdrawalHash.Hex(), receipt.BlockNumber.Uint64())

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
		Amount:             l2ToL1ETHAmount.String(),
	}
	require.NoError(t, helpers.SaveJSONState(l2ToL1ETHStateName(srcChain), state))
}

func runL2ToL1ETHFinalize(t *testing.T, src configs.ChainName, srcChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()

	var state l2ToL1ETHState
	found, err := helpers.LoadJSONState(l2ToL1ETHStateName(srcChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run TestL2ToL1_ETH_Withdraw_%s first",
			l2ToL1ETHStateName(srcChain), srcChain.Name())
	}

	portalAddr := portalAddressFor(src)
	wtx := stateToWithdrawalTx(state)

	// 1. Already finalized?
	var alreadyFinalized bool
	err = helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &alreadyFinalized, state.WithdrawalHash)
	require.NoError(t, err)
	if alreadyFinalized {
		logger.Info("[L2->L1 ETH %s] already finalized", srcChain.Name())
		require.NoError(t, helpers.DeleteJSONState(l2ToL1ETHStateName(srcChain)))
		return
	}

	// 2. Already proven? (numProofSubmitters > 0)
	var numSubmitters *big.Int
	err = helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"numProofSubmitters", &numSubmitters, state.WithdrawalHash)
	require.NoError(t, err)
	if numSubmitters.Sign() == 0 {
		require.NoError(t, proveL2ToL1Withdrawal(t, portalAddr, srcChain, state, wtx))
	} else {
		logger.Info("[L2->L1 ETH %s] already proven (%d submitters); skipping prove step",
			srcChain.Name(), numSubmitters)
	}

	// 3. Wait for maturity, then finalize. If maturity never lands within the
	//    poll window, fail rather than skip — re-run when the dispute game
	//    period has elapsed.
	require.NoErrorf(t,
		helpers.WaitForProofMaturity(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
			state.WithdrawalHash, TestL1Account.GetAddress(),
			20*time.Second, helpers.DefaultPollAttempts*6,
		),
		"[L2->L1 ETH %s] proof not yet mature — re-run finalize after the dispute window",
		srcChain.Name(),
	)

	finalizeData, err := helpers.PackFinalizeWithdrawal(ComposePortalABI, wtx)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, finalizeData)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 finalize tx confirmed for withdrawalHash=%s", state.WithdrawalHash.Hex())

	// Portal should now report the withdrawal as finalized.
	var finalizedNow bool
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &finalizedNow, state.WithdrawalHash))
	helpers.LogAssertOK("portal.finalizedWithdrawals(%s) == true (got=%v)", state.WithdrawalHash.Hex(), finalizedNow)
	require.True(t, finalizedNow, "portal.finalizedWithdrawals should be true after finalize call")

	// Assert L1 ETH balance increased — but the wallet also paid gas for the
	// finalize tx, so we check the value-component is at least there minus a
	// gas allowance.
	l1Before, _ := new(big.Int).SetString(state.L1BalanceBefore, 10)
	l1After, err := TestL1Account.GetBalance(ctx)
	require.NoError(t, err)
	delta := new(big.Int).Sub(l1After, l1Before)
	amount, _ := new(big.Int).SetString(state.Amount, 10)
	// allow up to 0.01 ETH of cumulative gas across prove+finalize.
	gasBudget := new(big.Int).Mul(big.NewInt(10), big.NewInt(1_000_000_000_000_000)) // 0.01 ETH
	helpers.LogAssertOK("L1 balance after finalize reflects withdrawal: delta=%s want>=%s-gas(%s)",
		delta, amount, gasBudget)
	require.GreaterOrEqualf(t, delta.Cmp(new(big.Int).Sub(amount, gasBudget)), 0,
		"L1 balance after finalize should reflect withdrawal: delta=%s want>=%s",
		delta, new(big.Int).Sub(amount, gasBudget))

	require.NoError(t, helpers.DeleteJSONState(l2ToL1ETHStateName(srcChain)))
}

func stateToWithdrawalTx(state l2ToL1ETHState) helpers.WithdrawalTx {
	n, _ := new(big.Int).SetString(state.WithdrawalNonce, 10)
	v, _ := new(big.Int).SetString(state.WithdrawalValue, 10)
	g, _ := new(big.Int).SetString(state.WithdrawalGasLimit, 10)
	return helpers.WithdrawalTx{
		Nonce: n, Sender: state.WithdrawalSender, Target: state.WithdrawalTarget,
		Value: v, GasLimit: g, Data: state.WithdrawalData,
	}
}

func proveL2ToL1Withdrawal(
	t *testing.T,
	portalAddr common.Address,
	srcChain *rollup.Rollup,
	state l2ToL1ETHState,
	wtx helpers.WithdrawalTx,
) error {
	tcontext := t.Context()

	// Read dispute game factory + game type from portal.
	var dgfAddr common.Address
	if err := helpers.CallPortalView(tcontext, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"disputeGameFactory", &dgfAddr); err != nil {
		return fmt.Errorf("portal.disputeGameFactory: %w", err)
	}
	var gameType uint32
	if err := helpers.CallPortalView(tcontext, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"respectedGameType", &gameType); err != nil {
		return fmt.Errorf("portal.respectedGameType: %w", err)
	}

	// Use the package-level minimal DGF/Game ABI so this works on every env,
	// including those whose config doesn't ship a `dispute-game-factory`
	// entry (sepolia-stage, sepolia-prod) or ships the wrong ABI under that
	// key (hoodi). See internal/helpers/dispute_game_abi.go.
	gameIndex, coveredBlock, err := helpers.FindCoveringDisputeGame(tcontext, TestL1.RPCURL(),
		dgfAddr, helpers.MinimalDisputeGameABI, helpers.MinimalDisputeGameABI,
		gameType, state.L2Block, 30*time.Second, helpers.DefaultPollAttempts*6,
	)
	if err != nil {
		return fmt.Errorf("find dispute game: %w", err)
	}
	logger.Info("[L2->L1 ETH %s] found game index=%d covering L2 block %d (need %d)",
		srcChain.Name(), gameIndex, coveredBlock, state.L2Block)

	outProof, storageProof, err := helpers.BuildWithdrawalProof(tcontext, srcChain.RPCURL(),
		state.WithdrawalHash, coveredBlock)
	if err != nil {
		return fmt.Errorf("build proof: %w", err)
	}

	proveData, err := helpers.PackProveWithdrawal(ComposePortalABI, wtx, gameIndex, outProof, storageProof)
	if err != nil {
		return fmt.Errorf("pack prove: %w", err)
	}
	_, _, err = helpers.SendL1Tx(tcontext, TestL1Account, portalAddr, big.NewInt(0), 500_000, proveData)
	if err != nil {
		return fmt.Errorf("send prove: %w", err)
	}
	return nil
}
