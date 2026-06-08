// Go port of scripts/l1-to-l2-ETH.ts. Bridges native ETH from L1 to one of the
// two rollups via ComposeL1Bridge.bridgeETHTo and waits for op-node to derive
// the deposit tx on L2.
package test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
)

// 0.01 ETH per the TS default.
var l1ToL2ETHBridgeAmount = big.NewInt(10_000_000_000_000_000)

func TestL1ToL2_ETH_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "l1", "a")
	RequireL1(t)
	runL1ToL2ETH(t, configs.ChainNameRollupA, TestRollupA, TestAccountA)
}

func TestL1ToL2_ETH_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "l1", "b")
	RequireL1(t)
	runL1ToL2ETH(t, configs.ChainNameRollupB, TestRollupB, TestAccountB)
}

func runL1ToL2ETH(
	t *testing.T,
	destRollup configs.ChainName,
	destChain *rollup.Rollup,
	destAccount *accounts.Account,
) {
	t.Helper()
	ctx := t.Context()

	bridgeAmount := helpers.ParseBridgeAmountOverride(l1ToL2ETHBridgeAmount)

	bridgeAddr, err := helpers.L1BridgeAddressFor(destRollup)
	require.NoError(t, err)

	l1BalBefore, err := TestL1Account.GetBalance(ctx)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 has sufficient balance: %s want>=%s", l1BalBefore, bridgeAmount)
	require.GreaterOrEqualf(t, l1BalBefore.Cmp(bridgeAmount), 0,
		"L1 balance %s < bridge amount %s", l1BalBefore, bridgeAmount)

	l2BalBefore, err := destAccount.GetBalance(ctx)
	require.NoError(t, err)

	logger.Info("[L1->%s ETH] L1 bridge=%s amount=%s wei", destChain.Name(), bridgeAddr.Hex(), bridgeAmount)

	calldata, err := helpers.PackBridgeETHTo(ComposeL1BridgeABI,
		TestL1Account.GetAddress(), uint32(helpers.L1MinGasLimitNewToken), []byte{})
	require.NoError(t, err)

	tx, _, err := helpers.SendL1Tx(ctx, TestL1Account, bridgeAddr, bridgeAmount, helpers.L1BridgeGasLimit, calldata)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 bridge tx confirmed: %s", tx.Hash().Hex())

	// L1 should have decreased by at least the bridge amount (excludes gas).
	l1BalAfter, err := TestL1Account.GetBalance(ctx)
	require.NoError(t, err)
	l1Delta := new(big.Int).Sub(l1BalBefore, l1BalAfter)
	helpers.LogAssertOK("L1 decreased by >= bridge amount: delta=%s want>=%s", l1Delta, bridgeAmount)
	require.GreaterOrEqualf(t, l1Delta.Cmp(bridgeAmount), 0,
		"L1 should have decreased by >= bridge amount, got delta=%s", l1Delta)

	// Poll L2 — op-node derives the deposit tx after a few minutes.
	l2Final, err := helpers.WaitForETHBalanceChange(ctx, destChain.RPCURL(), destAccount.GetAddress(),
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(l2BalBefore) > 0 })
	require.NoError(t, err, "L2 balance never increased")

	l2Increase := new(big.Int).Sub(l2Final, l2BalBefore)
	helpers.LogAssertOK("L2 deposit credited exact bridge amount: delta=%s want==%s", l2Increase, bridgeAmount)
	require.Equalf(t, 0, l2Increase.Cmp(bridgeAmount),
		"L2 increase mismatch: got=%s want=%s", l2Increase, bridgeAmount)
}
