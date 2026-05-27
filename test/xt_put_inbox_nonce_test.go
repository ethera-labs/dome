package test

import (
	"context"
	"math/big"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

var gasFundingAmount = big.NewInt(100000000000000000) // 0.1 ETH

func TestRejectedDuplicateNonceXtDoesNotBlockFollowingXt(t *testing.T) {
	ctx := t.Context()
	accountA, accountB := setupAccountsNoApprovals(t)
	ensureXtEndpoint(t)

	nonceA, err := accountA.GetNonce(ctx)
	require.NoError(t, err)
	nonceB, err := accountB.GetNonce(ctx)
	require.NoError(t, err)

	var (
		wg   sync.WaitGroup
		errs = make(chan error, 2)
	)

	submitDuplicate := func() {
		defer wg.Done()
		errs <- sendSimpleXtWithNonce(ctx, accountA, accountB, nonceA, nonceB, big.NewInt(0))
	}

	wg.Add(2)
	go submitDuplicate()
	go submitDuplicate()
	wg.Wait()
	close(errs)

	var successCount, failureCount int
	for err := range errs {
		if err == nil {
			successCount++
			continue
		}
		failureCount++
	}

	require.GreaterOrEqual(t, successCount, 1)
	require.LessOrEqual(t, successCount, 2)
	require.LessOrEqual(t, failureCount, 1)

	nextNonceA, err := accountA.GetNonce(ctx)
	require.NoError(t, err)
	nextNonceB, err := accountB.GetNonce(ctx)
	require.NoError(t, err)
	require.Equal(t, nonceA+1, nextNonceA)
	require.Equal(t, nonceB+1, nextNonceB)

	nextOriginTx, nextDestinationTx, err := sendSimpleXtWithNonceAndReturnTransactions(
		ctx,
		accountA,
		accountB,
		nextNonceA,
		nextNonceB,
		big.NewInt(0),
	)
	require.NoError(t, err)
	requireSuccessfulReceipt(t, nextOriginTx.Hash(), accountA.GetRollup())
	requireSuccessfulReceipt(t, nextDestinationTx.Hash(), accountB.GetRollup())
}

func ensureXtEndpoint(t *testing.T) {
	t.Helper()

	if strings.TrimSpace(os.Getenv("SIDECAR_XT_ENDPOINT")) == "" {
		t.Fatal("SIDECAR_XT_ENDPOINT must be set for nonce and state-drift XT tests")
	}
}

func requireSuccessfulReceipt(t *testing.T, txHash common.Hash, onRollup *rollup.Rollup) *types.Receipt {
	t.Helper()

	_, receipt, err := transactions.GetTransactionDetails(t.Context(), txHash, onRollup)
	require.NoError(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	return receipt
}

func requireNoReceipt(t *testing.T, txHash common.Hash, onRollup *rollup.Rollup) {
	t.Helper()

	_, _, err := transactions.GetTransactionDetails(t.Context(), txHash, onRollup)
	require.Error(t, err)
	require.Contains(t, err.Error(), "transaction receipt not found after")
}

func sendSimpleXtWithNonceAndReturnTransactions(
	ctx context.Context,
	ac1 *accounts.Account,
	ac2 *accounts.Account,
	ac1Nonce uint64,
	ac2Nonce uint64,
	amount *big.Int,
) (*types.Transaction, *types.Transaction, error) {
	txA, signedA, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
		To:        ac1.GetAddress(),
		Value:     amount,
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
	}, ac1, ac1Nonce)
	if err != nil {
		return nil, nil, err
	}

	txB, signedB, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
		To:        ac2.GetAddress(),
		Value:     amount,
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
	}, ac2, ac2Nonce)
	if err != nil {
		return nil, nil, err
	}

	if _, err := submitSignedXT(ctx, ac1, ac2, signedA, signedB); err != nil {
		return nil, nil, err
	}

	return txA, txB, nil
}

func submitSignedXT(
	ctx context.Context,
	ac1 *accounts.Account,
	ac2 *accounts.Account,
	signedA []byte,
	signedB []byte,
) (*transactions.XTResponse, error) {
	xtTxs := map[string][]string{
		ac1.GetRollup().ChainID().String(): {hexutil.Encode(signedA)},
		ac2.GetRollup().ChainID().String(): {hexutil.Encode(signedB)},
	}

	return transactions.SubmitXT(ctx, configs.Values.L2.SidecarURL, xtTxs)
}
