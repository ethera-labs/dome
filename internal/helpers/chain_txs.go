package helpers

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func SendSelfMoveBalanceTxWithNonce(
	ctx context.Context, ac *accounts.Account, nonce uint64, amount *big.Int,
) (*types.Transaction, common.Hash, error) {
	txDetails := transactions.TransactionDetails{
		To:        ac.GetAddress(),
		Value:     amount,
		Gas:       GasNativeTransfer,
		GasTipCap: GasTipCap,
		GasFeeCap: GasFeeCap,
		Data:      nil,
	}

	tx, _, err := transactions.CreateTransactionWithNonce(ctx, txDetails, ac, nonce)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("failed to create transaction: %w", err)
	}
	hash, err := transactions.SendTransaction(ctx, tx, ac.GetRollup().RPCURL())
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("failed to send transaction: %w", err)
	}
	logger.Info("Self move balance transaction sent successfully: %s", hash)
	return tx, hash, nil
}
