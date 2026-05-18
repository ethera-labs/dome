package test

import (
	"encoding/hex"
	"math/big"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var (
	numOfTxs                    = intEnv("DOME_STRESS_NUM_TXS", 25)
	numOfAccounts               = intEnv("DOME_STRESS_NUM_ACCOUNTS", 25)
	numOfTxsForMultipleAccounts = intEnv("DOME_STRESS_NUM_TXS_FOR_MULTIPLE_ACCOUNTS", 5)
	numOfAccountsForMultipleTxs = intEnv("DOME_STRESS_NUM_ACCOUNTS_FOR_MULTIPLE_TXS", 5)
	numMixedTxs                 = intEnv("DOME_STRESS_NUM_MIXED_TXS", 25)
)

const delay = 100 * time.Millisecond

func intEnv(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}

	return value
}

// TestStressBridgeSameAccount sends numOfTxs bridge XTs from the same account pair with delay.
func TestStressBridgeSameAccount(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	bridgeAddress := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	transferredAmount := big.NewInt(500000000000000000)
	mintedAmount := new(big.Int).Mul(transferredAmount, big.NewInt(int64(numOfTxs)))

	tx, hash, err := helpers.SendMintTx(t, TestAccountA, mintedAmount, TokenABI)
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.NotNil(t, hash)

	startingNonceA, err := TestAccountA.GetNonce(ctx)
	require.NoError(t, err)
	startingNonceB, err := TestAccountB.GetNonce(ctx)
	require.NoError(t, err)

	initialBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	wrappedTokenB, err := helpers.PredictWrappedTokenAddress(ctx, TestRollupB, BridgeABI, bridgeAddress, tokenAddress, TestRollupA.ChainID())
	require.NoError(t, err)
	initialWrappedBalanceB, err := helpers.GetTokenBalanceOrZero(ctx, TestAccountB, wrappedTokenB, TokenABI)
	require.NoError(t, err)

	var txsA []*types.Transaction
	var txsB []*types.Transaction

	for i := 0; i < numOfTxs; i++ {
		logger.Info("Creating set of txs with nonce %d and %d", startingNonceA+uint64(i), startingNonceB+uint64(i))
		txA, txB, err := helpers.SendBridgeTxWithNonce(t, TestAccountA, startingNonceA+uint64(i), TestAccountB, startingNonceB+uint64(i), transferredAmount, BridgeABI)
		txsA = append(txsA, txA)
		txsB = append(txsB, txB)
		require.NoError(t, err)
		require.NotNil(t, txA)
		require.NotNil(t, txB)
		time.Sleep(delay)
	}

	for _, tx := range txsA {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	}
	for _, tx := range txsB {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupB)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	}

	balanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	wrappedBalanceBAfter, err := helpers.GetTokenBalanceOrZero(ctx, TestAccountB, wrappedTokenB, TokenABI)
	require.NoError(t, err)

	expectedSentAmount := new(big.Int).Mul(transferredAmount, big.NewInt(int64(numOfTxs)))
	expectedBalanceA := new(big.Int).Sub(initialBalanceA, expectedSentAmount)
	expectedWrappedBalanceB := new(big.Int).Add(initialWrappedBalanceB, expectedSentAmount)
	require.Equal(t, expectedBalanceA, balanceAAfter)
	require.Equal(t, expectedWrappedBalanceB, wrappedBalanceBAfter)
}

// TestStressBridgeDifferentAccounts spawns numOfAccounts accounts and sends 1 bridge XT from each.
func TestStressBridgeDifferentAccounts(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	bridgeAddress := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	mintedAndTransferredAmount := big.NewInt(1000000000000000000)

	accountsOnRollupA := make([]*accounts.Account, numOfAccounts)
	accountsOnRollupB := make([]*accounts.Account, numOfAccounts)
	for i := 0; i < numOfAccounts; i++ {
		pk, err := crypto.GenerateKey()
		require.NoError(t, err)
		pkHex := hex.EncodeToString(crypto.FromECDSA(pk))
		accountsOnRollupA[i], err = accounts.NewRollupAccount(pkHex, TestRollupA)
		require.NoError(t, err)
		accountsOnRollupB[i], err = accounts.NewRollupAccount(pkHex, TestRollupB)
		require.NoError(t, err)
	}

	logger.Info("Distributing 0.1 eth to all accounts...")
	err := transactions.DistributeEth(ctx, TestAccountA, accountsOnRollupA, big.NewInt(100000000000000000))
	require.NoError(t, err)
	err = transactions.DistributeEth(ctx, TestAccountB, accountsOnRollupB, big.NewInt(100000000000000000))
	require.NoError(t, err)

	logger.Info("Minting tokens to all accounts...")
	var mintWg sync.WaitGroup
	for _, acc := range accountsOnRollupA {
		acc := acc
		mintWg.Add(1)
		go func() {
			defer mintWg.Done()
			_, _, err := helpers.SendMintTx(t, acc, mintedAndTransferredAmount, TokenABI)
			require.NoError(t, err)
		}()
	}
	mintWg.Wait()

	logger.Info("Approving tokens for the bridge contract...")
	var approveWg sync.WaitGroup
	for _, acc := range accountsOnRollupA {
		acc := acc
		approveWg.Add(1)
		go func() {
			defer approveWg.Done()
			_, _, err := helpers.ApproveTokens(t, acc, bridgeAddress, TokenABI)
			require.NoError(t, err)
		}()
	}
	approveWg.Wait()

	var txsA []*types.Transaction
	var txsB []*types.Transaction
	for i := range len(accountsOnRollupA) {
		txA, txB, err := helpers.SendBridgeTx(t, accountsOnRollupA[i], accountsOnRollupB[i], mintedAndTransferredAmount, BridgeABI)
		txsA = append(txsA, txA)
		txsB = append(txsB, txB)
		require.NoError(t, err)
		require.NotNil(t, txA)
		require.NotNil(t, txB)
		time.Sleep(delay)
	}

	for _, tx := range txsA {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range txsB {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupB)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}

	for _, acc := range accountsOnRollupA {
		balance, err := acc.GetTokensBalance(ctx, tokenAddress, TokenABI)
		require.NoError(t, err)
		require.Equal(t, 0, balance.Cmp(big.NewInt(0)))
	}
	wrappedTokenB, err := helpers.PredictWrappedTokenAddress(ctx, TestRollupB, BridgeABI, bridgeAddress, tokenAddress, TestRollupA.ChainID())
	require.NoError(t, err)
	for _, acc := range accountsOnRollupB {
		balance, err := helpers.GetTokenBalanceOrZero(ctx, acc, wrappedTokenB, TokenABI)
		require.NoError(t, err)
		require.Equal(t, 0, balance.Cmp(mintedAndTransferredAmount))
	}
}

// TestStressMultipleAccountsAndMultipleTxs spawns multiple accounts, each sending multiple bridge XTs.
func TestStressMultipleAccountsAndMultipleTxs(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	bridgeAddress := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	accountsOnRollupA := make([]*accounts.Account, numOfAccountsForMultipleTxs)
	accountsOnRollupB := make([]*accounts.Account, numOfAccountsForMultipleTxs)
	for i := range numOfAccountsForMultipleTxs {
		pk, err := crypto.GenerateKey()
		require.NoError(t, err)
		pkHex := hex.EncodeToString(crypto.FromECDSA(pk))
		accountsOnRollupA[i], err = accounts.NewRollupAccount(pkHex, TestRollupA)
		require.NoError(t, err)
		accountsOnRollupB[i], err = accounts.NewRollupAccount(pkHex, TestRollupB)
		require.NoError(t, err)
	}

	logger.Info("Distributing 0.1 eth to all accounts...")
	err := transactions.DistributeEth(ctx, TestAccountA, accountsOnRollupA, big.NewInt(100000000000000000))
	require.NoError(t, err)
	err = transactions.DistributeEth(ctx, TestAccountB, accountsOnRollupB, big.NewInt(100000000000000000))
	require.NoError(t, err)

	transferredAmount := big.NewInt(1000000000000000000)
	mintedAmount := new(big.Int).Mul(transferredAmount, big.NewInt(int64(numOfTxsForMultipleAccounts)))

	logger.Info("Minting tokens for all accounts on rollup A...")
	for _, acc := range accountsOnRollupA {
		tx, hash, err := helpers.SendMintTx(t, acc, mintedAmount, TokenABI)
		require.NoError(t, err)
		require.NotNil(t, tx)
		require.NotNil(t, hash)
	}

	logger.Info("Approving tokens for the bridge contract...")
	for _, acc := range accountsOnRollupA {
		_, _, err := helpers.ApproveTokens(t, acc, bridgeAddress, TokenABI)
		require.NoError(t, err)
	}

	var noncesA []uint64
	var noncesB []uint64
	for i := 0; i < numOfAccountsForMultipleTxs; i++ {
		nonceA, err := accountsOnRollupA[i].GetNonce(ctx)
		noncesA = append(noncesA, nonceA)
		require.NoError(t, err)
		nonceB, err := accountsOnRollupB[i].GetNonce(ctx)
		noncesB = append(noncesB, nonceB)
		require.NoError(t, err)
	}

	var txsA []*types.Transaction
	var txsB []*types.Transaction

	for i := range accountsOnRollupA {
		for j := 0; j < numOfTxsForMultipleAccounts; j++ {
			txA, txB, err := helpers.SendBridgeTxWithNonce(t, accountsOnRollupA[i], noncesA[i]+uint64(j), accountsOnRollupB[i], noncesB[i]+uint64(j), transferredAmount, BridgeABI)
			require.NoError(t, err)
			require.NotNil(t, txA)
			require.NotNil(t, txB)
			txsA = append(txsA, txA)
			txsB = append(txsB, txB)
			time.Sleep(delay)
		}
	}

	for _, tx := range txsA {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range txsB {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupB)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}

	for _, acc := range accountsOnRollupA {
		balance, err := acc.GetTokensBalance(ctx, tokenAddress, TokenABI)
		require.NoError(t, err)
		require.Equal(t, 0, balance.Cmp(big.NewInt(0)))
	}
	wrappedTokenB, err := helpers.PredictWrappedTokenAddress(ctx, TestRollupB, BridgeABI, bridgeAddress, tokenAddress, TestRollupA.ChainID())
	require.NoError(t, err)
	for _, acc := range accountsOnRollupB {
		balance, err := helpers.GetTokenBalanceOrZero(ctx, acc, wrappedTokenB, TokenABI)
		require.NoError(t, err)
		expected := new(big.Int).Mul(transferredAmount, big.NewInt(int64(numOfTxsForMultipleAccounts)))
		require.Equal(t, 0, balance.Cmp(expected))
	}
}

// TestStressAtoBAndBtoA sends bridge XTs back and forth between A and B.
func TestStressAtoBAndBtoA(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	bridgeAddress := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	mintedAndTransferredAmount := big.NewInt(1000000000000000000)
	totalNumOfTxs := (numOfTxs + 1) / 2
	totalTransferredAmount := new(big.Int).Mul(mintedAndTransferredAmount, big.NewInt(int64(totalNumOfTxs)))

	tx, hash, err := helpers.SendMintTx(t, TestAccountA, totalTransferredAmount, TokenABI)
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.NotNil(t, hash)
	tx, hash, err = helpers.SendMintTx(t, TestAccountB, totalTransferredAmount, TokenABI)
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.NotNil(t, hash)

	initialBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	initialBalanceB, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	wrappedTokenA, err := helpers.PredictWrappedTokenAddress(ctx, TestRollupA, BridgeABI, bridgeAddress, tokenAddress, TestRollupB.ChainID())
	require.NoError(t, err)
	initialWrappedBalanceA, err := helpers.GetTokenBalanceOrZero(ctx, TestAccountA, wrappedTokenA, TokenABI)
	require.NoError(t, err)
	wrappedTokenB, err := helpers.PredictWrappedTokenAddress(ctx, TestRollupB, BridgeABI, bridgeAddress, tokenAddress, TestRollupA.ChainID())
	require.NoError(t, err)
	initialWrappedBalanceB, err := helpers.GetTokenBalanceOrZero(ctx, TestAccountB, wrappedTokenB, TokenABI)
	require.NoError(t, err)

	nonceA, err := TestAccountA.GetNonce(ctx)
	require.NoError(t, err)
	nonceB, err := TestAccountB.GetNonce(ctx)
	require.NoError(t, err)

	var txsAtoBa []*types.Transaction
	var txsAtoBb []*types.Transaction
	var txsBtoAb []*types.Transaction
	var txsBtoAa []*types.Transaction

	for i := 0; i < totalNumOfTxs; i++ {
		aNonceAtoB := nonceA + uint64(2*i)
		bNonceAtoB := nonceB + uint64(2*i)
		bNonceBtoA := nonceB + uint64(2*i+1)
		aNonceBtoA := nonceA + uint64(2*i+1)

		// A -> B
		txA, txB, err := helpers.SendBridgeTxWithNonce(t, TestAccountA, aNonceAtoB, TestAccountB, bNonceAtoB, mintedAndTransferredAmount, BridgeABI)
		txsAtoBa = append(txsAtoBa, txA)
		txsAtoBb = append(txsAtoBb, txB)
		require.NoError(t, err)
		time.Sleep(delay)

		// B -> A
		txB, txA, err = helpers.SendBridgeTxWithNonce(t, TestAccountB, bNonceBtoA, TestAccountA, aNonceBtoA, mintedAndTransferredAmount, BridgeABI)
		txsBtoAb = append(txsBtoAb, txB)
		txsBtoAa = append(txsBtoAa, txA)
		require.NoError(t, err)
		time.Sleep(delay)
	}

	for _, tx := range txsAtoBa {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range txsAtoBb {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupB)
		require.NoError(t, err)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range txsBtoAa {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range txsBtoAb {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupB)
		require.NoError(t, err)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}

	balanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	balanceBAfter, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	wrappedBalanceAAfter, err := helpers.GetTokenBalanceOrZero(ctx, TestAccountA, wrappedTokenA, TokenABI)
	require.NoError(t, err)
	wrappedBalanceBAfter, err := helpers.GetTokenBalanceOrZero(ctx, TestAccountB, wrappedTokenB, TokenABI)
	require.NoError(t, err)
	require.Equal(t, new(big.Int).Sub(initialBalanceA, totalTransferredAmount), balanceAAfter)
	require.Equal(t, new(big.Int).Sub(initialBalanceB, totalTransferredAmount), balanceBAfter)
	require.Equal(t, new(big.Int).Add(initialWrappedBalanceA, totalTransferredAmount), wrappedBalanceAAfter)
	require.Equal(t, new(big.Int).Add(initialWrappedBalanceB, totalTransferredAmount), wrappedBalanceBAfter)
}

// TestStressNormalTxsMixWithCrossRollupTxs submits numMixedTxs interleaved pairs of a
// bridge XT and a normal self-transfer from an independent account. The normal
// transaction uses a separate sender so the test exercises ordinary mempool inclusion
// without competing with the XT sender's reserved nonce lane.
func TestStressNormalTxsMixWithCrossRollupTxs(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	transferredAmount := big.NewInt(500000000000000000)
	mintedAmount := new(big.Int).Mul(transferredAmount, big.NewInt(int64(numMixedTxs)))

	tx, hash, err := helpers.SendMintTx(t, TestAccountA, mintedAmount, TokenABI)
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.NotNil(t, hash)

	initialBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	wrappedTokenB, err := helpers.PredictWrappedTokenAddress(ctx, TestRollupB, BridgeABI, bridgeAddr, tokenAddress, TestRollupA.ChainID())
	require.NoError(t, err)
	initialWrappedBalanceB, err := helpers.GetTokenBalanceOrZero(ctx, TestAccountB, wrappedTokenB, TokenABI)
	require.NoError(t, err)

	normalKeyA, err := crypto.GenerateKey()
	require.NoError(t, err)
	normalAccountA, err := accounts.NewRollupAccount(hex.EncodeToString(crypto.FromECDSA(normalKeyA)), TestRollupA)
	require.NoError(t, err)
	err = transactions.DistributeEth(ctx, TestAccountA, []*accounts.Account{normalAccountA}, big.NewInt(100000000000000000))
	require.NoError(t, err)
	normalStartingNonce, err := normalAccountA.GetNonce(ctx)
	require.NoError(t, err)

	var xtInstanceIDs []string
	var xtTxsA []*types.Transaction
	var xtTxsB []*types.Transaction
	var selfMoveTxs []*types.Transaction

	for i := 0; i < numMixedTxs; i++ {
		sessionID := transactions.GenerateRandomSessionID()

		calldataA, err := helpers.PackBridgeERC20To(BridgeABI,
			TestRollupB.ChainID(),
			tokenAddress,
			transferredAmount,
			TestAccountB.GetAddress(),
			sessionID,
		)
		require.NoError(t, err)

		xtTxA, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
			To:        bridgeAddr,
			Value:     big.NewInt(0),
			Gas:       helpers.BridgeSendGasLimit,
			GasTipCap: big.NewInt(1000000000),
			GasFeeCap: big.NewInt(20000000000),
			Data:      calldataA,
		}, TestAccountA)
		require.NoError(t, err)

		calldataB, err := helpers.PackBridgeReceiveTokens(BridgeABI,
			TestRollupA.ChainID(),
			TestRollupB.ChainID(),
			bridgeAddr,
			TestAccountB.GetAddress(),
			sessionID,
		)
		require.NoError(t, err)

		xtTxB, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
			To:        bridgeAddr,
			Value:     big.NewInt(0),
			Gas:       helpers.BridgeReceiveGasLimit,
			GasTipCap: big.NewInt(1000000000),
			GasFeeCap: big.NewInt(20000000000),
			Data:      calldataB,
		}, TestAccountB)
		require.NoError(t, err)

		xtResp, err := transactions.SubmitXT(ctx, configs.Values.L2.SidecarURL, map[string][]string{
			TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
			TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
		})
		require.NoError(t, err)

		xtInstanceIDs = append(xtInstanceIDs, xtResp.InstanceID)
		xtTxsA = append(xtTxsA, xtTxA)
		xtTxsB = append(xtTxsB, xtTxB)

		selfTx, _, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
			To:        normalAccountA.GetAddress(),
			Value:     big.NewInt(1),
			Gas:       25000,
			GasTipCap: big.NewInt(1000000000),
			GasFeeCap: big.NewInt(20000000000),
		}, normalAccountA, normalStartingNonce+uint64(i))
		require.NoError(t, err)

		_, err = transactions.SendTransaction(ctx, selfTx, TestRollupA.RPCURL())
		require.NoError(t, err)
		selfMoveTxs = append(selfMoveTxs, selfTx)
	}

	for _, instanceID := range xtInstanceIDs {
		committed, err := transactions.WaitForDecision(ctx, configs.Values.L2.SidecarURL, instanceID, 3*time.Minute)
		require.NoError(t, err)
		require.True(t, committed)
	}

	for _, tx := range xtTxsA {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range xtTxsB {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupB)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range selfMoveTxs {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}

	balanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	wrappedBalanceBAfter, err := helpers.GetTokenBalanceOrZero(ctx, TestAccountB, wrappedTokenB, TokenABI)
	require.NoError(t, err)
	expectedSent := new(big.Int).Mul(transferredAmount, big.NewInt(int64(numMixedTxs)))
	require.Equal(t, new(big.Int).Sub(initialBalanceA, expectedSent), balanceAAfter)
	require.Equal(t, new(big.Int).Add(initialWrappedBalanceB, expectedSent), wrappedBalanceBAfter)
}
