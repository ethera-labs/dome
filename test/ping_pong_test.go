package test

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPingPong(t *testing.T) {
	ctx := t.Context()

	sessionID := transactions.GenerateRandomSessionID()
	pingPongAddress := configs.Values.L2.Contracts[configs.ContractNamePingPong].Address

	// Build ping tx on rollup A
	calldataA, err := pingPongABI.Pack("ping",
		TestRollupB.ChainID(),
		pingPongAddress, // pongSender: PingPong contract on B (same address on both chains)
		pingPongAddress, // pingReceiver: PingPong contract on B
		sessionID,
		[]byte("Hello from rollup A"),
	)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        pingPongAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, TestAccountA)
	require.NoError(t, err)

	// Build pong tx on rollup B
	calldataB, err := pingPongABI.Pack("pong",
		TestRollupA.ChainID(),
		pingPongAddress, // pingSender: PingPong contract on A (same address on both chains)
		sessionID,
		[]byte("Hello from rollup B"),
	)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        pingPongAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, TestAccountB)
	require.NoError(t, err)

	// Submit XT to sidecar
	xtTxs := map[string][]string{
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 60*time.Second)
	require.NoError(t, err)
	require.True(t, committed, "ping-pong XT should be committed")

	// Verify tx A
	tx, receipt, err := transactions.GetTransactionDetails(ctx, txA.Hash(), TestRollupA)
	require.NoError(t, err)
	assert.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	assert.Equal(t, pingPongAddress, *tx.To())
	assert.True(t, bytes.Equal(tx.Data(), calldataA))

	// Verify tx B
	tx, receipt, err = transactions.GetTransactionDetails(ctx, txB.Hash(), TestRollupB)
	require.NoError(t, err)
	assert.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	assert.Equal(t, pingPongAddress, *tx.To())
	assert.True(t, bytes.Equal(tx.Data(), calldataB))
}
