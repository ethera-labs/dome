package helpers

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
)

// maxUint256 returns (2^256 - 1).
func maxUint256() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
}

// sendErc20Call signs, submits, waits, and asserts success for a single ERC-20
// call against the configured token contract.
func sendErc20Call(
	ctx context.Context,
	ac *accounts.Account,
	tokenABI abi.ABI,
	method string,
	gas uint64,
	args ...any,
) (*types.Transaction, common.Hash, error) {
	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	calldata, err := tokenABI.Pack(method, args...)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("pack %s: %w", method, err)
	}

	tx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       gas,
		GasTipCap: GasTipCap,
		GasFeeCap: GasFeeCap,
		Data:      calldata,
	}, ac)
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
		return nil, common.Hash{}, fmt.Errorf("%s transaction reverted: %s", method, hash.Hex())
	}
	logger.Info("%s ok on %s: %s", method, ac.GetRollup().Name(), hash.Hex())
	return tx, hash, nil
}

// SendMintTx mints tokens to the given account.
func SendMintTx(t *testing.T, ac *accounts.Account, amount *big.Int, tokenABI abi.ABI) (*types.Transaction, common.Hash, error) {
	tx, hash, err := sendErc20Call(t.Context(), ac, tokenABI, "mint", GasMint, ac.GetAddress(), amount)
	require.NoError(t, err)
	require.NotNil(t, tx)
	return tx, hash, nil
}

// ApproveTokens approves max uint256 of the configured token to `spender`.
func ApproveTokens(
	t *testing.T,
	ac *accounts.Account,
	spender common.Address,
	tokenABI abi.ABI,
) (*types.Transaction, common.Hash, error) {
	tx, hash, err := sendErc20Call(t.Context(), ac, tokenABI, "approve", GasApprove, spender, maxUint256())
	require.NoError(t, err)
	require.NotNil(t, tx)
	return tx, hash, nil
}

// ApproveTokensCtx is the no-testing.T variant used during package init so the
// main accounts always have a fresh approval for the bridge contract.
func ApproveTokensCtx(
	ctx context.Context,
	ac *accounts.Account,
	spender common.Address,
	tokenABI abi.ABI,
) (*types.Transaction, common.Hash, error) {
	return sendErc20Call(ctx, ac, tokenABI, "approve", GasApprove, spender, maxUint256())
}

// MintAndApproveCtx is a setup helper: mints `amount` to the account and
// ensures the bridge has a max-uint approval. Used by package init so the main
// test accounts have predictable token balances across runs.
func MintAndApproveCtx(
	ctx context.Context,
	ac *accounts.Account,
	bridge common.Address,
	mintAmount *big.Int,
	tokenABI abi.ABI,
) error {
	if _, _, err := sendErc20Call(ctx, ac, tokenABI, "mint", GasMint, ac.GetAddress(), mintAmount); err != nil {
		return fmt.Errorf("mint: %w", err)
	}
	if _, _, err := sendErc20Call(ctx, ac, tokenABI, "approve", GasApprove, bridge, maxUint256()); err != nil {
		return fmt.Errorf("approve: %w", err)
	}
	return nil
}
