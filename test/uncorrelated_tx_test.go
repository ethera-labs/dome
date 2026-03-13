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
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      nil,
	}, TestAccountA)
	require.NoError(t, err)

	// Chain B: self-transfer with more than balance (should fail)
	_, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        TestAccountB.GetAddress(),
		Value:     new(big.Int).Add(new(big.Int).Set(balanceB), big.NewInt(1000000000000000000)),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      nil,
	}, TestAccountB)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 60*time.Second)
	require.NoError(t, err)
	assert.False(t, committed, "XT should be aborted because B's tx overdrafts")
}
