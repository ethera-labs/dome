package test

import (
	"bytes"
	"math/big"
	"sync"
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

var (
	mintedAmount      = big.NewInt(9000000000000000000) // 9 tokens
	transferredAmount = big.NewInt(100000000000000000)  // 0.1 tokens
)

// TestMintTokensCrossRollup mints tokens on both chains as a single atomic XT.
func TestMintTokensCrossRollup(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address

	initialTokenBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	initialTokenBalanceB, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)

	// Build mint tx for chain A
	calldataA, err := TokenABI.Pack("mint", TestAccountA.GetAddress(), mintedAmount)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, TestAccountA)
	require.NoError(t, err)

	// Build mint tx for chain B
	calldataB, err := TokenABI.Pack("mint", TestAccountB.GetAddress(), mintedAmount)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
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
	require.True(t, committed, "mint XT should be committed")

	// Verify both transactions on-chain in parallel
	type txResult struct {
		tx      *types.Transaction
		receipt *types.Receipt
		err     error
	}

	var wg sync.WaitGroup
	resultA := make(chan txResult, 1)
	resultB := make(chan txResult, 1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		tx, receipt, err := transactions.GetTransactionDetails(ctx, txA.Hash(), TestRollupA)
		resultA <- txResult{tx: tx, receipt: receipt, err: err}
	}()
	go func() {
		defer wg.Done()
		tx, receipt, err := transactions.GetTransactionDetails(ctx, txB.Hash(), TestRollupB)
		resultB <- txResult{tx: tx, receipt: receipt, err: err}
	}()
	wg.Wait()

	resA := <-resultA
	require.NoError(t, resA.err)
	assert.Equal(t, resA.receipt.Status, types.ReceiptStatusSuccessful)
	assert.Equal(t, *resA.tx.To(), tokenAddress)
	assert.True(t, bytes.Equal(resA.tx.Data(), calldataA))

	resB := <-resultB
	require.NoError(t, resB.err)
	assert.Equal(t, resB.receipt.Status, types.ReceiptStatusSuccessful)
	assert.Equal(t, *resB.tx.To(), tokenAddress)
	assert.True(t, bytes.Equal(resB.tx.Data(), calldataB))

	// Verify balances
	tokenBalanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	tokenBalanceBAfter, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	assert.Equal(t, initialTokenBalanceA.Add(initialTokenBalanceA, mintedAmount), tokenBalanceAAfter)
	assert.Equal(t, initialTokenBalanceB.Add(initialTokenBalanceB, mintedAmount), tokenBalanceBAfter)
}

// TestSendCrossTxBridgeFromAToB bridges tokens from chain A to chain B via sidecar.
func TestSendCrossTxBridgeFromAToB(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	initialTokenBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	initialTokenBalanceB, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)

	sessionID := transactions.GenerateRandomSessionID()

	// Chain A: bridge.send
	calldataA, err := BridgeABI.Pack("send",
		TestRollupB.ChainID(),
		tokenAddress,
		TestAccountA.GetAddress(),
		TestAccountB.GetAddress(),
		transferredAmount,
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, TestAccountA)
	require.NoError(t, err)

	// Chain B: bridge.receiveTokens
	calldataB, err := BridgeABI.Pack("receiveTokens",
		TestRollupA.ChainID(),
		TestAccountA.GetAddress(),
		TestAccountB.GetAddress(),
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, TestAccountB)
	require.NoError(t, err)

	// Submit XT
	xtTxs := map[string][]string{
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 60*time.Second)
	require.NoError(t, err)
	require.True(t, committed, "bridge A->B XT should be committed")

	// Verify both transactions
	var wg sync.WaitGroup
	type txResult struct {
		tx      *types.Transaction
		receipt *types.Receipt
		err     error
	}
	resultA := make(chan txResult, 1)
	resultB := make(chan txResult, 1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		tx, receipt, err := transactions.GetTransactionDetails(ctx, txA.Hash(), TestRollupA)
		resultA <- txResult{tx: tx, receipt: receipt, err: err}
	}()
	go func() {
		defer wg.Done()
		tx, receipt, err := transactions.GetTransactionDetails(ctx, txB.Hash(), TestRollupB)
		resultB <- txResult{tx: tx, receipt: receipt, err: err}
	}()
	wg.Wait()

	resA := <-resultA
	require.NoError(t, resA.err)
	assert.Equal(t, resA.receipt.Status, types.ReceiptStatusSuccessful)
	assert.Equal(t, *resA.tx.To(), bridgeAddr)
	assert.True(t, bytes.Equal(resA.tx.Data(), calldataA))

	resB := <-resultB
	require.NoError(t, resB.err)
	assert.Equal(t, resB.receipt.Status, types.ReceiptStatusSuccessful)
	assert.Equal(t, *resB.tx.To(), bridgeAddr)
	assert.True(t, bytes.Equal(resB.tx.Data(), calldataB))

	// Verify balances
	tokenBalanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	tokenBalanceBAfter, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	assert.Equal(t, initialTokenBalanceA.Sub(initialTokenBalanceA, transferredAmount), tokenBalanceAAfter)
	assert.Equal(t, initialTokenBalanceB.Add(initialTokenBalanceB, transferredAmount), tokenBalanceBAfter)
}

// TestSendCrossTxBridgeFromBToA bridges tokens from chain B to chain A via sidecar.
func TestSendCrossTxBridgeFromBToA(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	initialTokenBalanceB, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	initialTokenBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)

	sessionID := transactions.GenerateRandomSessionID()

	// Chain B: bridge.send
	calldataB, err := BridgeABI.Pack("send",
		TestRollupA.ChainID(),
		tokenAddress,
		TestAccountB.GetAddress(),
		TestAccountA.GetAddress(),
		transferredAmount,
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, TestAccountB)
	require.NoError(t, err)

	// Chain A: bridge.receiveTokens
	calldataA, err := BridgeABI.Pack("receiveTokens",
		TestRollupB.ChainID(),
		TestAccountB.GetAddress(),
		TestAccountA.GetAddress(),
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, TestAccountA)
	require.NoError(t, err)

	// Submit XT
	xtTxs := map[string][]string{
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 60*time.Second)
	require.NoError(t, err)
	require.True(t, committed, "bridge B->A XT should be committed")

	// Verify both transactions
	var wg sync.WaitGroup
	type txResult struct {
		tx      *types.Transaction
		receipt *types.Receipt
		err     error
	}
	resultB := make(chan txResult, 1)
	resultA := make(chan txResult, 1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		tx, receipt, err := transactions.GetTransactionDetails(ctx, txB.Hash(), TestRollupB)
		resultB <- txResult{tx: tx, receipt: receipt, err: err}
	}()
	go func() {
		defer wg.Done()
		tx, receipt, err := transactions.GetTransactionDetails(ctx, txA.Hash(), TestRollupA)
		resultA <- txResult{tx: tx, receipt: receipt, err: err}
	}()
	wg.Wait()

	resB := <-resultB
	require.NoError(t, resB.err)
	assert.Equal(t, resB.receipt.Status, types.ReceiptStatusSuccessful)
	assert.Equal(t, *resB.tx.To(), bridgeAddr)
	assert.True(t, bytes.Equal(resB.tx.Data(), calldataB))

	resA := <-resultA
	require.NoError(t, resA.err)
	assert.Equal(t, resA.receipt.Status, types.ReceiptStatusSuccessful)
	assert.Equal(t, *resA.tx.To(), bridgeAddr)
	assert.True(t, bytes.Equal(resA.tx.Data(), calldataA))

	// Verify balances
	tokenBalanceBAfter, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	tokenBalanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	assert.Equal(t, initialTokenBalanceB.Sub(initialTokenBalanceB, transferredAmount), tokenBalanceBAfter)
	assert.Equal(t, initialTokenBalanceA.Add(initialTokenBalanceA, transferredAmount), tokenBalanceAAfter)
}

// TestSendOnAAndFailingSelfMoveBalanceOnB pairs a bridge send on A with an impossible ETH transfer on B.
// The sidecar should abort both.
func TestSendOnAAndFailingSelfMoveBalanceOnB(t *testing.T) {
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address

	initialTokenBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	initialBalanceB, err := TestAccountB.GetBalance(ctx)
	require.NoError(t, err)

	sessionID := transactions.GenerateRandomSessionID()

	// Chain A: bridge.send (valid)
	calldataA, err := BridgeABI.Pack("send",
		TestRollupB.ChainID(),
		tokenAddress,
		TestAccountA.GetAddress(),
		TestAccountB.GetAddress(),
		transferredAmount,
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	_, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, TestAccountA)
	require.NoError(t, err)

	// Chain B: self-transfer with more than balance (should fail)
	balanceB, err := TestAccountB.GetBalance(ctx)
	require.NoError(t, err)

	_, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        TestAccountB.GetAddress(),
		Value:     balanceB.Add(balanceB, big.NewInt(100000)),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      nil,
	}, TestAccountB)
	require.NoError(t, err)

	// Submit XT - should be aborted
	xtTxs := map[string][]string{
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 60*time.Second)
	require.NoError(t, err)
	assert.False(t, committed, "XT should be aborted because B's tx fails")

	// Verify balances unchanged
	tokenBalanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	assert.Equal(t, initialTokenBalanceA, tokenBalanceAAfter)

	balanceBAfter, err := TestAccountB.GetBalance(ctx)
	require.NoError(t, err)
	assert.Equal(t, initialBalanceB, balanceBAfter)
}

// TestSendCrossTxBridgeWithOutOfGasOnB pairs a bridge send on A with an under-gassed receive on B.
// The sidecar should abort both.
func TestSendCrossTxBridgeWithOutOfGasOnB(t *testing.T) {
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address

	initialTokenBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	initialTokenBalanceB, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)

	sessionID := transactions.GenerateRandomSessionID()

	// Chain A: bridge.send (valid)
	calldataA, err := BridgeABI.Pack("send",
		TestRollupB.ChainID(),
		tokenAddress,
		TestAccountA.GetAddress(),
		TestAccountB.GetAddress(),
		transferredAmount,
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	_, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, TestAccountA)
	require.NoError(t, err)

	// Chain B: bridge.receiveTokens with insufficient gas (should OOG)
	calldataB, err := BridgeABI.Pack("receiveTokens",
		TestRollupA.ChainID(),
		TestAccountA.GetAddress(),
		TestAccountB.GetAddress(),
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	_, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       300000, // insufficient gas
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, TestAccountB)
	require.NoError(t, err)

	// Submit XT - should be aborted
	xtTxs := map[string][]string{
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 60*time.Second)
	require.NoError(t, err)
	assert.False(t, committed, "XT should be aborted because B runs out of gas")

	// Verify balances unchanged
	tokenBalanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	assert.Equal(t, initialTokenBalanceA, tokenBalanceAAfter)
	tokenBalanceBAfter, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	assert.Equal(t, initialTokenBalanceB, tokenBalanceBAfter)
}

// TestSelfMoveBalanceOnAandreceiveTokensOnB pairs a self-transfer on A with receiveTokens on B (no matching send).
// The sidecar should abort both.
func TestSelfMoveBalanceOnAandreceiveTokensOnB(t *testing.T) {
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	initialBalanceA, err := TestAccountA.GetBalance(ctx)
	require.NoError(t, err)

	sessionID := transactions.GenerateRandomSessionID()

	// Chain A: self-transfer (valid on its own)
	_, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        TestAccountA.GetAddress(),
		Value:     big.NewInt(500000000000000000), // 0.5 ETH
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      nil,
	}, TestAccountA)
	require.NoError(t, err)

	// Chain B: receiveTokens without matching send (should fail)
	calldataB, err := BridgeABI.Pack("receiveTokens",
		TestRollupA.ChainID(),
		TestAccountA.GetAddress(),
		TestAccountB.GetAddress(),
		sessionID,
		bridgeAddr,
	)
	require.NoError(t, err)

	_, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, TestAccountB)
	require.NoError(t, err)

	// Submit XT - should be aborted
	xtTxs := map[string][]string{
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 60*time.Second)
	require.NoError(t, err)
	assert.False(t, committed, "XT should be aborted because receiveTokens has no matching send")

	// Verify balance unchanged
	balanceAAfter, err := TestAccountA.GetBalance(ctx)
	require.NoError(t, err)
	assert.Equal(t, initialBalanceA, balanceAAfter)
}
