// Go port of scripts/l2-to-l2-ETH.ts. Bridges native ETH between two rollups
// via a composed cross-chain transaction (source: bridgeEthTo, dest: receiveETH).
package test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

// l2ToL2ETHBridgeAmount mirrors the 0.01 ETH default used by the TS script.
var l2ToL2ETHBridgeAmount = big.NewInt(10_000_000_000_000_000) // 0.01 ETH

func TestL2ToL2_ETH_AtoB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "b")
	if TestXTMode == configs.XTSubmissionRPC {
		RequireTSRuntime(t)
	}
	amount := helpers.ParseBridgeAmountOverride(l2ToL2ETHBridgeAmount)
	runL2ToL2ETH(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB, amount)
}

func TestL2ToL2_ETH_BtoA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "a")
	if TestXTMode == configs.XTSubmissionRPC {
		RequireTSRuntime(t)
	}
	amount := helpers.ParseBridgeAmountOverride(l2ToL2ETHBridgeAmount)
	runL2ToL2ETH(t, TestAccountB, TestRollupB, TestAccountA, TestRollupA, amount)
}

func runL2ToL2ETH(
	t *testing.T,
	from *accounts.Account,
	fromChain *rollup.Rollup,
	to *accounts.Account,
	toChain *rollup.Rollup,
	amount *big.Int,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	srcBalBefore, err := from.GetBalance(ctx)
	require.NoError(t, err)
	dstBalBefore, err := to.GetBalance(ctx)
	require.NoError(t, err)

	helpers.LogAssertOK("source has sufficient balance: %s >= bridge amount %s", srcBalBefore, amount)
	require.GreaterOrEqual(t, srcBalBefore.Cmp(amount), 0,
		"source balance %s < bridge amount %s", srcBalBefore, amount)

	sessionID := transactions.GenerateRandomSessionID()
	logger.Info("[L2->L2 ETH %s->%s] sessionId=%s amount=%s wei", fromChain.Name(), toChain.Name(), sessionID, amount)

	srcCalldata, err := helpers.PackBridgeEthTo(BridgeABI, sessionID, toChain.ChainID(), to.GetAddress())
	require.NoError(t, err)

	srcTx, srcBytes, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     amount,
		Gas:       3_000_000,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      srcCalldata,
	}, from)
	require.NoError(t, err)

	dstCalldata, err := helpers.PackReceiveETH(BridgeABI,
		fromChain.ChainID(), toChain.ChainID(),
		bridgeAddr, to.GetAddress(), sessionID,
	)
	require.NoError(t, err)

	dstTx, dstBytes, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       3_000_000,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      dstCalldata,
	}, to)
	require.NoError(t, err)

	instanceID, committed, err := helpers.SubmitXTWaitCommitted(ctx, map[uint64][][]byte{
		uint64(fromChain.ChainID().Int64()): {srcBytes},
		uint64(toChain.ChainID().Int64()):   {dstBytes},
	}, uint64(fromChain.ChainID().Int64()), 90*time.Second)
	require.NoError(t, err)
	helpers.LogAssertXTOK(srcTx.Hash().Hex(), dstTx.Hash().Hex(), instanceID)
	require.True(t, committed, "XT should commit / submit successfully")

	// Wait for tx receipts on both chains.
	helpers.LogAssertOK("source tx receipt status=Successful (%s)", srcTx.Hash().Hex())
	waitReceipt(t, ctx, srcTx, fromChain)
	helpers.LogAssertOK("dest tx receipt status=Successful (%s)", dstTx.Hash().Hex())
	waitReceipt(t, ctx, dstTx, toChain)

	// Assertions: source decreased by >= amount (includes gas), but not by
	// more than amount + a sane gas budget (catches double-drain regressions).
	srcFinal, err := helpers.WaitForETHBalanceChange(ctx, fromChain.RPCURL(), from.GetAddress(),
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool {
			return new(big.Int).Sub(srcBalBefore, cur).Cmp(amount) >= 0
		})
	require.NoError(t, err, "source balance never dropped by >= bridge amount")
	srcDecrease := new(big.Int).Sub(srcBalBefore, srcFinal)
	helpers.LogAssertOK("source decreased by >= bridge amount: decrease=%s want>=%s", srcDecrease, amount)
	// (no require needed for the lower bound — WaitForETHBalanceChange already
	// poll-waited until this condition held.)

	// 0.1 ETH upper bound (well above 20 gwei × 3M gas = 0.06 ETH worst case);
	// asserts we didn't get double-drained.
	gasBudget := new(big.Int).Mul(big.NewInt(100_000_000_000_000_000), big.NewInt(1))
	helpers.LogAssertOK("source decrease within sane gas budget: decrease=%s want<=amount(%s)+gas(%s)", srcDecrease, amount, gasBudget)
	require.LessOrEqualf(t, srcDecrease.Cmp(new(big.Int).Add(amount, gasBudget)), 0,
		"source decrease too large (double-drain?): got=%s, want<=%s+gas",
		srcDecrease, amount)

	// The destination wallet pays gas for receiveETH, so the net balance change
	// is (amount - dstGas). The TS script only checks dstIncrease > 0 (it can't
	// know the gas cost up front either). We match that.
	dstFinal, err := helpers.WaitForETHBalanceChange(ctx, toChain.RPCURL(), to.GetAddress(),
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool {
			return cur.Cmp(dstBalBefore) > 0
		})
	require.NoError(t, err, "dest balance never increased")

	dstIncrease := new(big.Int).Sub(dstFinal, dstBalBefore)
	helpers.LogAssertOK("dest balance increased: delta=%s want>0", dstIncrease)
	require.Positivef(t, dstIncrease.Sign(),
		"dest balance increase should be positive: got=%s", dstIncrease)
	helpers.LogAssertOK("dest gain <= bridged amount: delta=%s want<=%s", dstIncrease, amount)
	require.LessOrEqualf(t, dstIncrease.Cmp(amount), 0,
		"dest gained more than bridged: got=%s, bridged=%s", dstIncrease, amount)
}

func waitReceipt(t *testing.T, ctx context.Context, tx *types.Transaction, r *rollup.Rollup) {
	t.Helper()
	_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), r)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s reverted on %s", tx.Hash().Hex(), r.Name())
}
