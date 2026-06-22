// L1 -> L2 ETH deposit driven by an ERC-4337 smart account on L1.
//
// Flow:
//  1. Derive the SA address on L1 (chainId = L1).
//  2. Top up the SA's EntryPoint deposit on L1.
//  3. Send the SA enough ETH on L1 to cover the bridge value.
//  4. Build a single UserOp call: `bridgeETHTo(saAddr, minGasLimit, 0x)` with
//     `value = bridgeAmount` on the L1 portal.
//  5. Have the TS helper sign it and Go pack it into a `handleOps` tx; submit
//     to L1 RPC directly (no sidecar — L1 → L2 doesn't go through compose).
//  6. Verify the outer L1 receipt + `UserOperationEvent.success`.
//  7. Poll the SA's L2 balance until the deposit lands; assert it equals
//     exactly the bridge amount (deposits don't pay L2 gas).
//
// Today this test fails on every configured network because the multichain
// ECDSA validator isn't deployed on L1 (see to-do.md item 1). Once the
// contracts team deploys it, the test will pass without further changes.
package test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

var l1ToL2SAETHBridgeAmount = big.NewInt(10_000_000_000_000_000) // 0.01 ETH

// l1SABlockedReason is the permanent skip reason for the L1↔L2 smart-account
// tests. The Kernel multichain ECDSA validator (and on Hoodi the entire
// Kernel stack) is not deployed on L1 yet, so the precondition step fails on
// every configured network. Remove the t.Skip calls once L1 AA infra ships.
// See test-catalogue.md items #6 and #7.
const l1SABlockedReason = "blocked: L1 smart-account infrastructure not deployed (Kernel multichain validator missing on L1) — see test-catalogue.md #6/#7"

func TestL1ToL2_SA_ETH_RollupA(t *testing.T) {
	t.Skip(l1SABlockedReason)
	helpers.ApplyDirectionFilter(t, "l1", "a")
	RequireL1(t)
	RequireAA(t)
	RequireTSRuntime(t)
	runL1ToL2SAETH(t, configs.ChainNameRollupA, TestRollupA, TestAccountA)
}

func TestL1ToL2_SA_ETH_RollupB(t *testing.T) {
	t.Skip(l1SABlockedReason)
	helpers.ApplyDirectionFilter(t, "l1", "b")
	RequireL1(t)
	RequireAA(t)
	RequireTSRuntime(t)
	runL1ToL2SAETH(t, configs.ChainNameRollupB, TestRollupB, TestAccountB)
}

func runL1ToL2SAETH(
	t *testing.T,
	destRollup configs.ChainName,
	destChain *rollup.Rollup,
	_ *accounts.Account, // destAccount not used — SA receives the deposit
) {
	t.Helper()
	ctx := t.Context()
	bridgeAmount := helpers.ParseBridgeAmountOverride(l1ToL2SAETHBridgeAmount)
	pk := configs.Values.WalletPrivateKey
	l1ChainID := uint64(TestL1.ChainID().Int64())

	// 0. Precondition — the Kernel + multichain validator must be on L1.
	//    If not, every subsequent step would fail with an opaque address-zero
	//    or EntryPoint reject.
	helpers.LogAssertOK("L1 has the full Kernel v3.1 AA stack deployed (kernel-impl, kernel-factory, multichain-validator)")
	require.NoError(t, helpers.AssertAAContractsDeployed(ctx, TestL1.RPCURL()),
		"AA infrastructure missing on L1 — see to-do.md item 1 (contracts team needs to deploy the multichain validator on L1)")

	l1BridgeAddr, err := helpers.L1BridgeAddressFor(destRollup)
	require.NoError(t, err)

	// 1. Derive the SA address on L1.
	saAddr, _, err := helpers.SACreateAccount(ctx, pk, l1ChainID, []uint64{l1ChainID})
	require.NoError(t, err)
	helpers.LogAssertOK("SA derived on L1: %s", saAddr.Hex())
	require.NotEqualf(t, [20]byte{}, [20]byte(saAddr),
		"SA address must be non-zero (zero usually means the factory reverted on counterfactual address derivation)")

	// 2. Fund EntryPoint on L1.
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, TestL1Account, saAddr, helpers.MinEntryPointDeposit))
	helpers.LogAssertOK("EntryPoint deposit on L1 >= %s wei for SA=%s", helpers.MinEntryPointDeposit, saAddr.Hex())

	// 3. Fund SA on L1 with at least the bridge amount.
	require.NoError(t, ensureSABalance(ctx, TestL1Account, saAddr, bridgeAmount))
	helpers.LogAssertOK("SA on L1 has >= %s wei", bridgeAmount)

	// Snapshot SA balance on L2 (the deposit receiver).
	l2BalBefore := readBalance(ctx, destChain.RPCURL(), saAddr)

	// 4. Build UserOp call: bridgeETHTo(saAddr, minGasLimit, extraData=0x).
	calldata, err := helpers.PackBridgeETHTo(ComposeL1BridgeABI,
		saAddr, uint32(helpers.L1MinGasLimitNewToken), []byte{})
	require.NoError(t, err)

	calls := []helpers.UserOpCall{
		{
			ChainID: l1ChainID,
			To:      l1BridgeAddr,
			Value:   bridgeAmount.String(),
			Data:    hexutil.Encode(calldata),
		},
	}

	// 5. Sign UserOp via TS helper.
	canonical, err := helpers.SACreateUserOps(ctx, pk, calls, nil)
	require.NoError(t, err)
	require.NotEmptyf(t, canonical, "TS helper returned no signed UserOps")
	helpers.LogAssertOK("TS helper produced %d signed UserOp(s) on L1", len(canonical))

	// 6. Pack handleOps + send to L1.
	signedTx, _, err := helpers.BuildHandleOpsRawTx(ctx, TestL1Account, canonical)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, signedTx, TestL1.RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(ctx, signedTx.Hash(), TestL1)
	require.NoError(t, err)
	logger.Info("[L1->%s SA ETH] handleOps tx confirmed: %s", destChain.Name(), signedTx.Hash().Hex())
	helpers.LogAssertOK("L1 handleOps receipt status=Successful (%s)", signedTx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status,
		"L1 handleOps tx reverted: %s", signedTx.Hash().Hex())

	// 7. Verify inner UserOp succeeded.
	success, _, err := helpers.CheckUserOpSuccess(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 UserOperationEvent.success=true (got=%v)", success)
	require.Truef(t, success, "inner UserOp on L1 reverted (bridgeETHTo failed inside the SA)")

	// 8. Poll L2 for the deposit and assert exact credit.
	l2Final, err := helpers.WaitForETHBalanceChange(ctx, destChain.RPCURL(), saAddr,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(l2BalBefore) > 0 })
	require.NoError(t, err, "L2 SA balance never increased — op-node may not have derived the deposit tx")

	l2Increase := new(big.Int).Sub(l2Final, l2BalBefore)
	helpers.LogAssertOK("L2 SA received exactly the bridged amount: delta=%s want=%s", l2Increase, bridgeAmount)
	require.Equalf(t, 0, l2Increase.Cmp(bridgeAmount),
		"L2 SA balance mismatch: got=%s want=%s", l2Increase, bridgeAmount)
}

