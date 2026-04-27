package test

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
)

// TestCommittedXtCanStillFailAfterDestinationStateDrift is a best-effort
// reproducer for the current architecture gap:
//  1. a destination-chain XT tx simulates successfully against latest canonical state,
//  2. a queued normal mempool tx later mutates that state before block execution,
//  3. sidecar has already decided "committed", but the destination XT tx never lands.
//
// The test intentionally demonstrates that nonce reservation is not equivalent
// to state locking. This race is environment-sensitive; if the destination leg
// still lands on the current stack, the reproducer is skipped rather than
// failing the full suite.
func TestCommittedXtCanStillFailAfterDestinationStateDrift(t *testing.T) {
	ctx := t.Context()
	ensureXtEndpoint(t)

	const (
		waitCommittedTimeout = 20 * time.Second
	)

	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	driftAmount := new(big.Int).Mul(transferredAmount, big.NewInt(2))
	xtSourceA, xtDestinationB := newFundedAccountPair(t)

	ownerB := newFundedAccountOnRollup(t, TestRollupB, TestAccountB)
	_, _, err := helpers.SendMintTx(t, ownerB, new(big.Int).Set(driftAmount), TokenABI)
	require.NoError(t, err)
	_, _, err = helpers.ApproveTokens(t, ownerB, xtDestinationB.GetAddress(), TokenABI)
	require.NoError(t, err)

	allowanceBefore := getTokenAllowance(t, ctx, ownerB.GetRollup(), tokenAddress, ownerB.GetAddress(), xtDestinationB.GetAddress())
	require.True(t, allowanceBefore.Sign() > 0, "owner must approve spender before XT simulation")

	ownerNonce, err := ownerB.GetNonce(ctx)
	require.NoError(t, err)

	revokeQueuedTx := mustCreateApproveTxWithNonce(t, ctx, ownerB, xtDestinationB.GetAddress(), big.NewInt(0), ownerNonce+1)
	_, err = transactions.SendTransaction(ctx, revokeQueuedTx, ownerB.GetRollup().RPCURL())
	require.NoError(t, err)
	logger.Info("Queued destination-side revoke tx with future nonce: %s", revokeQueuedTx.Hash())

	originTx, signedOriginTx, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        xtSourceA.GetAddress(),
		Value:     big.NewInt(0),
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
	}, xtSourceA)
	require.NoError(t, err)

	destinationCalldata, err := TokenABI.Pack(
		"transferFrom",
		ownerB.GetAddress(),
		xtDestinationB.GetAddress(),
		new(big.Int).Set(transferredAmount),
	)
	require.NoError(t, err)

	destinationTx, signedDestinationTx, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      destinationCalldata,
	}, xtDestinationB)
	require.NoError(t, err)

	submitResp, err := submitSignedXT(ctx, xtSourceA, xtDestinationB, signedOriginTx, signedDestinationTx)
	require.NoError(t, err)
	require.NotEmpty(t, submitResp.InstanceID)
	require.Equal(t, "submitted", submitResp.Status)

	committed, err := transactions.WaitForDecision(ctx, configs.Values.L2.SidecarURL, submitResp.InstanceID, waitCommittedTimeout)
	require.NoError(t, err)
	require.True(t, committed, "XT must have simulated and decided commit before we unlock the revoke tx")

	// Unlock the queued revoke only after sidecar already committed the XT. The
	// revoke itself was invisible to simulation because sidecar simulates against
	// latest canonical state, not pending mempool state.
	unlockTx := mustCreateSelfTransferWithNonce(t, ctx, ownerB, ownerNonce, big.NewInt(1))
	_, err = transactions.SendTransaction(ctx, unlockTx, ownerB.GetRollup().RPCURL())
	require.NoError(t, err)

	requireSuccessfulReceipt(t, unlockTx.Hash(), ownerB.GetRollup())
	requireSuccessfulReceipt(t, revokeQueuedTx.Hash(), ownerB.GetRollup())

	allowanceAfter := getTokenAllowance(t, ctx, ownerB.GetRollup(), tokenAddress, ownerB.GetAddress(), xtDestinationB.GetAddress())
	require.Zero(t, allowanceAfter.Sign(), "queued revoke must execute before XT block execution")

	originReceipt := requireSuccessfulReceipt(t, originTx.Hash(), xtSourceA.GetRollup())

	_, destinationReceipt, err := transactions.GetTransactionDetails(ctx, destinationTx.Hash(), TestRollupB)
	if err == nil {
		require.NotNil(t, destinationReceipt)
		t.Skipf(
			"state-drift race did not reproduce on this stack: destination XT tx landed in block %d despite the queued revoke",
			destinationReceipt.BlockNumber.Uint64(),
		)
	}

	requireNoReceipt(t, destinationTx.Hash(), TestRollupB)
	t.Logf("origin XT tx was included in block %d while destination XT tx never landed", originReceipt.BlockNumber.Uint64())
}

func newFundedAccountPair(t *testing.T) (*accounts.Account, *accounts.Account) {
	t.Helper()

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	privateKeyHex := hex.EncodeToString(crypto.FromECDSA(privateKey))
	accountA, err := accounts.NewRollupAccount(privateKeyHex, TestRollupA)
	require.NoError(t, err)
	accountB, err := accounts.NewRollupAccount(privateKeyHex, TestRollupB)
	require.NoError(t, err)

	t.Cleanup(accountA.Close)
	t.Cleanup(accountB.Close)

	err = transactions.DistributeEth(t.Context(), TestAccountA, []*accounts.Account{accountA}, gasFundingAmount)
	require.NoError(t, err)
	err = transactions.DistributeEth(t.Context(), TestAccountB, []*accounts.Account{accountB}, gasFundingAmount)
	require.NoError(t, err)

	return accountA, accountB
}

func newFundedAccountOnRollup(
	t *testing.T,
	onRollup *rollup.Rollup,
	sponsor *accounts.Account,
) *accounts.Account {
	t.Helper()

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	privateKeyHex := hex.EncodeToString(crypto.FromECDSA(privateKey))
	account, err := accounts.NewRollupAccount(privateKeyHex, onRollup)
	require.NoError(t, err)

	t.Cleanup(account.Close)

	err = transactions.DistributeEth(t.Context(), sponsor, []*accounts.Account{account}, gasFundingAmount)
	require.NoError(t, err)

	return account
}

func mustCreateApproveTxWithNonce(
	t *testing.T,
	ctx context.Context,
	ac *accounts.Account,
	spender common.Address,
	amount *big.Int,
	nonce uint64,
) *types.Transaction {
	t.Helper()

	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	calldata, err := TokenABI.Pack("approve", spender, amount)
	require.NoError(t, err)

	tx, _, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldata,
	}, ac, nonce)
	require.NoError(t, err)
	return tx
}

func mustCreateSelfTransferWithNonce(
	t *testing.T,
	ctx context.Context,
	ac *accounts.Account,
	nonce uint64,
	amount *big.Int,
) *types.Transaction {
	t.Helper()

	tx, _, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
		To:        ac.GetAddress(),
		Value:     amount,
		Gas:       25000,
		GasTipCap: big.NewInt(1000000),
		GasFeeCap: big.NewInt(2000000),
	}, ac, nonce)
	require.NoError(t, err)
	return tx
}

func getTokenAllowance(
	t *testing.T,
	ctx context.Context,
	onRollup *rollup.Rollup,
	tokenAddress common.Address,
	owner common.Address,
	spender common.Address,
) *big.Int {
	t.Helper()

	client, err := ethclient.DialContext(ctx, onRollup.RPCURL())
	require.NoError(t, err)
	defer client.Close()

	contract := bind.NewBoundContract(tokenAddress, TokenABI, client, client, client)
	call := &bind.CallOpts{Context: ctx}
	var allowance *big.Int
	err = contract.Call(call, &[]interface{}{&allowance}, "allowance", owner, spender)
	require.NoError(t, err)
	return allowance
}
