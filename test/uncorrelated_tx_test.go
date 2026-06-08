package test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTxASuccessAndTxBFailure pairs a valid self-transfer on A with an overdraft on B.
// The sidecar should abort both.
func TestTxASuccessAndTxBFailure(t *testing.T) {
	ctx := t.Context()

	balanceA, err := TestAccountA.GetBalance(ctx)
	require.NoError(t, err)
	balanceB, err := TestAccountB.GetBalance(ctx)
	require.NoError(t, err)

	assert.True(t, balanceA.Cmp(big.NewInt(0)) > 0, "balanceA should be greater than 0")
	assert.True(t, balanceB.Cmp(big.NewInt(0)) > 0, "balanceB should be greater than 0")

	// Chain A: self-transfer with half balance (valid)
	_, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        TestAccountA.GetAddress(),
		Value:     new(big.Int).Div(new(big.Int).Set(balanceA), big.NewInt(2)),
		Gas:       helpers.GasNativeTransfer,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      nil,
	}, TestAccountA)
	require.NoError(t, err)

	// Chain B: self-transfer with more than balance (should fail)
	_, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        TestAccountB.GetAddress(),
		Value:     new(big.Int).Add(new(big.Int).Set(balanceB), big.NewInt(1000000000000000000)),
		Gas:       helpers.GasNativeTransfer,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      nil,
	}, TestAccountB)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 60*time.Second)
	require.NoError(t, err)
	helpers.LogAssertOK("XT aborted as expected (B's tx overdrafts): committed=%v want=false", committed)
	assert.False(t, committed, "XT should be aborted because B's tx overdrafts")

	// Atomic abort: neither account should have its balance moved (self-transfers
	// don't move ETH, but they also shouldn't be debited gas if the XT aborted
	// before inclusion).
	balanceAAfter, err := TestAccountA.GetBalance(ctx)
	require.NoError(t, err)
	balanceBAfter, err := TestAccountB.GetBalance(ctx)
	require.NoError(t, err)
	helpers.LogAssertOK("A balance unchanged after abort: before=%s after=%s", balanceA, balanceAAfter)
	assert.Equalf(t, 0, balanceAAfter.Cmp(balanceA),
		"A's balance changed after abort: before=%s after=%s", balanceA, balanceAAfter)
	helpers.LogAssertOK("B balance unchanged after abort: before=%s after=%s", balanceB, balanceBAfter)
	assert.Equalf(t, 0, balanceBAfter.Cmp(balanceB),
		"B's balance changed after abort: before=%s after=%s", balanceB, balanceBAfter)
}
