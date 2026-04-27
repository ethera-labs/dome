package helpers

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/transactions"
)

// SendMintTx mints tokens to the given account.
func SendMintTx(t *testing.T, ac *accounts.Account, amount *big.Int, tokenABI abi.ABI) (*types.Transaction, common.Hash, error) {
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address

	calldata, err := tokenABI.Pack("mint",
		ac.GetAddress(),
		amount,
	)
	require.NoError(t, err)
	require.NotNil(t, calldata)

	transactionDetails := transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldata,
	}

	tx, _, err := transactions.CreateTransaction(t.Context(), transactionDetails, ac)
	require.NoError(t, err)
	hash, err := transactions.SendTransaction(t.Context(), tx, ac.GetRollup().RPCURL())
	logger.Info("Mint transaction sent successfully: %s", hash)
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(t.Context(), hash, ac.GetRollup())
	require.NoError(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	return tx, hash, nil
}

// ApproveTokens approves max uint256 of tokens to the spender.
// It is used in normal tests for approving tokens from spawned accounts for the bridge contract.
func ApproveTokens(
	t *testing.T,
	ac *accounts.Account,
	spender common.Address,
	tokenABI abi.ABI,
) (*types.Transaction, common.Hash, error) {
	logger.Info("Approving tokens on rollup %s for %s on %s ...", ac.GetRollup().Name(), ac.GetAddress().Hex(), spender.Hex())
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	// set amount to max uint256 (2^256 - 1)
	maxUint256 := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)
	amount := new(big.Int).Sub(maxUint256, big.NewInt(1))

	calldata, err := tokenABI.Pack("approve",
		spender,
		amount,
	)
	require.NoError(t, err)
	require.NotNil(t, calldata)

	transactionDetails := transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldata,
	}

	tx, _, err := transactions.CreateTransaction(t.Context(), transactionDetails, ac)
	require.NoError(t, err)
	hash, err := transactions.SendTransaction(t.Context(), tx, ac.GetRollup().RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(t.Context(), hash, ac.GetRollup())
	require.NoError(t, err)
	require.NotNil(t, receipt)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	logger.Info("Approve transaction executed successfully: %s", hash)
	return tx, hash, nil
}

// ApproveTokensCtx approves for the main accounts the maximum amount of tokens to the spender.
// It is used in config.go without testing context to be sure the main accounts always have the maximum amount of tokens approved.
func ApproveTokensCtx(
	ctx context.Context,
	ac *accounts.Account,
	spender common.Address,
	tokenABI abi.ABI,
) (*types.Transaction, common.Hash, error) {
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	// set amount to max uint256 (2^256 - 1)
	maxUint256 := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), nil)
	amount := new(big.Int).Sub(maxUint256, big.NewInt(1))

	calldata, err := tokenABI.Pack("approve",
		spender,
		amount,
	)
	if err != nil {
		return nil, common.Hash{}, err
	}

	transactionDetails := transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldata,
	}

	tx, _, err := transactions.CreateTransaction(ctx, transactionDetails, ac)
	if err != nil {
		return nil, common.Hash{}, err
	}
	hash, err := transactions.SendTransaction(ctx, tx, ac.GetRollup().RPCURL())
	if err != nil {
		return nil, common.Hash{}, err
	}
	_, receipt, err := transactions.GetTransactionDetails(ctx, hash, ac.GetRollup())
	if err != nil {
		return nil, common.Hash{}, err
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, common.Hash{}, fmt.Errorf("approve transaction failed")
	}
	logger.Info("Approve transaction executed successfully: %s", hash)
	return tx, hash, nil
}
