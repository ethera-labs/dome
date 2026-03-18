package test

import (
	"encoding/hex"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	numOfTxs                    = 25
	numOfAccounts               = 25
	numOfTxsForMultipleAccounts = 5
	numOfAccountsForMultipleTxs = 5
	delay                       = 100 * time.Millisecond
)

// TestStressBridgeSameAccount sends numOfTxs bridge XTs from the same account pair with delay.
func TestStressBridgeSameAccount(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address

	transferredAmount := big.NewInt(500000000000000000)
	mintedAmount := new(big.Int).Mul(transferredAmount, big.NewInt(numOfTxs))

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
	initialBalanceB, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
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
	balanceBAfter, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)

	expectedSentAmount := new(big.Int).Mul(transferredAmount, big.NewInt(numOfTxs))
	expectedBalanceA := new(big.Int).Sub(initialBalanceA, expectedSentAmount)
	expectedBalanceB := new(big.Int).Add(initialBalanceB, expectedSentAmount)
	require.Equal(t, expectedBalanceA, balanceAAfter)
	require.Equal(t, expectedBalanceB, balanceBAfter)
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
	for _, acc := range accountsOnRollupB {
		balance, err := acc.GetTokensBalance(ctx, tokenAddress, TokenABI)
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
	mintedAmount := new(big.Int).Mul(transferredAmount, big.NewInt(numOfTxsForMultipleAccounts))

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
	for _, acc := range accountsOnRollupB {
		balance, err := acc.GetTokensBalance(ctx, tokenAddress, TokenABI)
		require.NoError(t, err)
		expected := new(big.Int).Mul(transferredAmount, big.NewInt(numOfTxsForMultipleAccounts))
		require.Equal(t, 0, balance.Cmp(expected))
	}
}

// TestStressAtoBAndBtoA sends bridge XTs back and forth between A and B.
func TestStressAtoBAndBtoA(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address

	mintedAndTransferredAmount := big.NewInt(1000000000000000000)

	tx, hash, err := helpers.SendMintTx(t, TestAccountA, mintedAndTransferredAmount, TokenABI)
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.NotNil(t, hash)

	initialBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	initialBalanceB, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)

	nonceA, err := TestAccountA.GetNonce(ctx)
	require.NoError(t, err)
	nonceB, err := TestAccountB.GetNonce(ctx)
	require.NoError(t, err)

	var txsAtoBa []*types.Transaction
	var txsAtoBb []*types.Transaction
	var txsBtoAb []*types.Transaction
	var txsBtoAa []*types.Transaction

	totalNumOfTxs := (numOfTxs + 1) / 2
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
	require.Equal(t, initialBalanceA, balanceAAfter)
	require.Equal(t, initialBalanceB, balanceBAfter)
}

// TestStressNormalTxsMixWithCrossRollupTxs interleaves normal self-transfers with bridge XTs.
func TestStressNormalTxsMixWithCrossRollupTxs(t *testing.T) {
	ctx := t.Context()
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address

	transferredAmount := big.NewInt(500000000000000000)
	mintedAmount := new(big.Int).Mul(transferredAmount, big.NewInt(numOfTxs))

	tx, hash, err := helpers.SendMintTx(t, TestAccountA, mintedAmount, TokenABI)
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.NotNil(t, hash)

	initialBalanceA, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	initialBalanceB, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)

	nonceA, err := TestAccountA.GetNonce(ctx)
	require.NoError(t, err)
	nonceB, err := TestAccountB.GetNonce(ctx)
	require.NoError(t, err)

	var txsSelfMove []*types.Transaction
	var txsBridgeA []*types.Transaction
	var txsBridgeB []*types.Transaction

	selfMoveBalanceAmount := big.NewInt(100000000000000000)
	for i := 0; i < numOfTxs; i++ {
		selfNonceA := nonceA + uint64(2*i)
		bridgeNonceA := nonceA + uint64(2*i+1)
		bridgeNonceB := nonceB + uint64(i)

		// Self-move balance on rollup A (normal tx, sent directly)
		selfTx, selfHash, err := helpers.SendSelfMoveBalanceTxWithNonce(ctx, TestAccountA, selfNonceA, selfMoveBalanceAmount)
		require.NoError(t, err)
		require.NotNil(t, selfTx)
		require.NotNil(t, selfHash)
		txsSelfMove = append(txsSelfMove, selfTx)
		time.Sleep(delay)

		// Cross-rollup bridge XT (via sidecar)
		txA, txB, err := helpers.SendBridgeTxWithNonce(t, TestAccountA, bridgeNonceA, TestAccountB, bridgeNonceB, transferredAmount, BridgeABI)
		require.NoError(t, err)
		require.NotNil(t, txA)
		require.NotNil(t, txB)
		txsBridgeA = append(txsBridgeA, txA)
		txsBridgeB = append(txsBridgeB, txB)
		time.Sleep(delay)
	}

	for _, tx := range txsSelfMove {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range txsBridgeA {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupA)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}
	for _, tx := range txsBridgeB {
		_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), TestRollupB)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status, "tx %s", tx.Hash().Hex())
	}

	balanceAAfter, err := TestAccountA.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	balanceBAfter, err := TestAccountB.GetTokensBalance(ctx, tokenAddress, TokenABI)
	require.NoError(t, err)
	require.Equal(t, new(big.Int).Sub(initialBalanceA, mintedAmount), balanceAAfter)
	require.Equal(t, new(big.Int).Add(initialBalanceB, mintedAmount), balanceBAfter)
}
