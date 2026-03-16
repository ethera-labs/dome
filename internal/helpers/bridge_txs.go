package helpers

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/transactions"
)

const defaultDecisionTimeout = 60 * time.Second

// SendBridgeTx sends a bridge transaction from ac1 to ac2 via the sidecar.
// Returns the signed transactions for both chains (for receipt checking).
func SendBridgeTx(
	t *testing.T,
	ac1 *accounts.Account,
	ac2 *accounts.Account,
	amount *big.Int,
	bridgeABI abi.ABI,
) (*types.Transaction, *types.Transaction, error) {
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	sessionID := transactions.GenerateRandomSessionID()

	calldataA, err := bridgeABI.Pack("send",
		ac2.GetRollup().ChainID(),
		configs.Values.L2.Contracts[configs.ContractNameToken].Address,
		ac1.GetAddress(),
		ac2.GetAddress(),
		amount,
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransaction(t.Context(), transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, ac1)
	require.NoError(t, err)

	calldataB, err := bridgeABI.Pack("receiveTokens",
		ac1.GetRollup().ChainID(),
		ac2.GetAddress(),
		ac2.GetAddress(),
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(t.Context(), transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, ac2)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		ac1.GetRollup().ChainID().String(): {hexutil.Encode(signedBytesA)},
		ac2.GetRollup().ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	xtResp, err := transactions.SubmitXT(t.Context(), configs.Values.L2.SidecarURL, xtTxs)
	require.NoError(t, err)

	committed, err := transactions.WaitForDecision(t.Context(), configs.Values.L2.SidecarURL, xtResp.InstanceID, defaultDecisionTimeout)
	require.NoError(t, err)
	require.True(t, committed, "bridge XT should be committed")

	logger.Info("Bridge XT committed: %s (txA: %s, txB: %s)", xtResp.InstanceID, txA.Hash(), txB.Hash())
	return txA, txB, nil
}

// SendBridgeTxWithNonce sends a bridge transaction with explicit nonces via the sidecar.
func SendBridgeTxWithNonce(
	t *testing.T,
	ac1 *accounts.Account,
	ac1Nonce uint64,
	ac2 *accounts.Account,
	ac2Nonce uint64,
	amount *big.Int,
	bridgeABI abi.ABI,
) (*types.Transaction, *types.Transaction, error) {
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	sessionID := transactions.GenerateRandomSessionID()

	calldataA, err := bridgeABI.Pack("send",
		ac2.GetRollup().ChainID(),
		configs.Values.L2.Contracts[configs.ContractNameToken].Address,
		ac1.GetAddress(),
		ac2.GetAddress(),
		amount,
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransactionWithNonce(t.Context(), transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, ac1, ac1Nonce)
	require.NoError(t, err)

	calldataB, err := bridgeABI.Pack("receiveTokens",
		ac1.GetRollup().ChainID(),
		ac2.GetAddress(),
		ac2.GetAddress(),
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransactionWithNonce(t.Context(), transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, ac2, ac2Nonce)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		ac1.GetRollup().ChainID().String(): {hexutil.Encode(signedBytesA)},
		ac2.GetRollup().ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	xtResp, err := transactions.SubmitXT(t.Context(), configs.Values.L2.SidecarURL, xtTxs)
	require.NoError(t, err)

	committed, err := transactions.WaitForDecision(t.Context(), configs.Values.L2.SidecarURL, xtResp.InstanceID, defaultDecisionTimeout)
	require.NoError(t, err)
	require.True(t, committed, "bridge XT should be committed")

	logger.Info("Bridge XT committed: %s (txA: %s, txB: %s)", xtResp.InstanceID, txA.Hash(), txB.Hash())
	return txA, txB, nil
}

// SubmitXTAndWait is a generic helper that submits an XT and waits for its decision.
// Returns (instanceID, committed, error).
func SubmitXTAndWait(ctx context.Context, xtTxs map[string][]string, timeout time.Duration) (string, bool, error) {
	xtResp, err := transactions.SubmitXT(ctx, configs.Values.L2.SidecarURL, xtTxs)
	if err != nil {
		return "", false, fmt.Errorf("failed to submit XT: %w", err)
	}

	committed, err := transactions.WaitForDecision(ctx, configs.Values.L2.SidecarURL, xtResp.InstanceID, timeout)
	if err != nil {
		return xtResp.InstanceID, false, fmt.Errorf("failed waiting for decision: %w", err)
	}

	return xtResp.InstanceID, committed, nil
}
