// L1 -> L2 ERC-20 deposit driven by an ERC-4337 smart account on L1.
//
// Flow:
//  1. Precondition: full Kernel AA stack on L1.
//  2. Derive SA address on L1 (same address as the L2 SA via deterministic
//     CREATE2 — assuming the factory has matching code).
//  3. Fund SA's EntryPoint deposit on L1.
//  4. Deploy MintableToken on L1 (funder EOA) and mint the bridge amount to
//     the SA.
//  5. Predict the CET address on the destination L2 via CETFactory.
//  6. Build a single UserOp with TWO calls executed atomically inside the SA:
//       a) token.approve(l1Bridge, bridgeAmt)
//       b) l1Bridge.bridgeERC20To(token, predictedCET, saAddr, bridgeAmt,
//          minGasLimit, extraData=(name, symbol, decimals, 0x))
//  7. Sign via TS helper; pack handleOps in Go; submit to L1; verify outer
//     receipt + UserOperationEvent.success.
//  8. Poll the SA's CET balance on L2; assert exact-amount credit and that
//     the CET contract was actually deployed at the predicted address.
//
// Today this test fails on every configured network because the multichain
// ECDSA validator isn't deployed on L1 (to-do.md item 1).
package test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

// Same default as the L2 SA token tests: 100 * 1e18.
func TestL1ToL2_SA_NewToken_RollupA(t *testing.T) {
	t.Skip(l1SABlockedReason)
	helpers.ApplyDirectionFilter(t, "l1", "a")
	RequireL1(t)
	RequireAA(t)
	RequireTSRuntime(t)
	runL1ToL2SANewToken(t, configs.ChainNameRollupA, TestRollupA)
}

func TestL1ToL2_SA_NewToken_RollupB(t *testing.T) {
	t.Skip(l1SABlockedReason)
	helpers.ApplyDirectionFilter(t, "l1", "b")
	RequireL1(t)
	RequireAA(t)
	RequireTSRuntime(t)
	runL1ToL2SANewToken(t, configs.ChainNameRollupB, TestRollupB)
}

func runL1ToL2SANewToken(t *testing.T, destRollup configs.ChainName, destChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()
	bridgeAmt := helpers.ParseBridgeAmountOverride(l1ToL2TokenBridgeAmount)
	pk := configs.Values.WalletPrivateKey
	l1ChainID := uint64(TestL1.ChainID().Int64())

	// 0. Precondition.
	helpers.LogAssertOK("L1 has the full Kernel v3.1 AA stack deployed (kernel-impl, kernel-factory, multichain-validator)")
	require.NoError(t, helpers.AssertAAContractsDeployed(ctx, TestL1.RPCURL()),
		"AA infrastructure missing on L1 — see to-do.md item 1 (contracts team needs to deploy the multichain validator on L1)")

	l1BridgeAddr, err := helpers.L1BridgeAddressFor(destRollup)
	require.NoError(t, err)

	// 1. Derive SA on L1.
	saAddr, _, err := helpers.SACreateAccount(ctx, pk, l1ChainID, []uint64{l1ChainID})
	require.NoError(t, err)
	helpers.LogAssertOK("SA derived on L1: %s", saAddr.Hex())
	require.NotEqualf(t, [20]byte{}, [20]byte(saAddr),
		"SA address must be non-zero (zero usually means the factory reverted on counterfactual address derivation)")

	// 2. Fund EntryPoint on L1.
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, TestL1Account, saAddr, helpers.MinEntryPointDeposit))
	helpers.LogAssertOK("EntryPoint deposit on L1 >= %s wei for SA=%s", helpers.MinEntryPointDeposit, saAddr.Hex())

	// 3. Deploy MintableToken on L1 + mint to SA.
	tokenAddr, _, err := helpers.DeployMintableToken(ctx, TestL1Account, "SAL1NewToken", "SAL1NT", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	logger.Info("[L1->%s SA NewToken] deployed token=%s", destChain.Name(), tokenAddr.Hex())

	mintData, err := tokenABI.Pack("mint", saAddr, bridgeAmt)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasMint, mintData)
	require.NoError(t, err)

	saTokenBefore, err := getERC20BalanceAt(ctx, TestL1.RPCURL(), tokenAddr, saAddr, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("SA L1 token balance after mint == bridge amount: got=%s want=%s", saTokenBefore, bridgeAmt)
	require.Equalf(t, 0, saTokenBefore.Cmp(bridgeAmt),
		"SA L1 token balance after mint mismatch: got=%s want=%s", saTokenBefore, bridgeAmt)

	// 4. Predict CET on the destination L2.
	cetAddr, err := helpers.PredictCetAddress(ctx, destChain, CetFactoryABI, tokenAddr, TestL1.ChainID())
	require.NoError(t, err)
	helpers.LogAssertOK("predicted CET on %s = %s", destChain.Name(), cetAddr.Hex())

	saCETBefore := readBalanceOrZero(t, destChain.RPCURL(), cetAddr, saAddr, tokenABI)

	// 5. Encode extraData (name, symbol, decimals, bytes).
	extraData, err := helpers.EncodeERC20ExtraData("SAL1NewToken", "SAL1NT", 18)
	require.NoError(t, err)

	// 6. Build UserOp with two calls (approve + bridgeERC20To) on L1.
	approveData, err := tokenABI.Pack("approve", l1BridgeAddr, bridgeAmt)
	require.NoError(t, err)
	bridgeData, err := helpers.PackBridgeERC20ToL1(ComposeL1BridgeABI,
		tokenAddr, cetAddr, saAddr,
		bridgeAmt, uint32(helpers.L1MinGasLimitNewToken), extraData)
	require.NoError(t, err)

	calls := []helpers.UserOpCall{
		{ChainID: l1ChainID, To: tokenAddr, Value: "0", Data: hexutil.Encode(approveData)},
		{ChainID: l1ChainID, To: l1BridgeAddr, Value: "0", Data: hexutil.Encode(bridgeData)},
	}

	// 7. Sign + submit handleOps to L1 directly.
	canonical, err := helpers.SACreateUserOps(ctx, pk, calls, nil)
	require.NoError(t, err)
	require.NotEmpty(t, canonical, "TS helper returned no signed UserOps")
	helpers.LogAssertOK("TS helper produced %d signed UserOp(s) on L1", len(canonical))

	signedTx, _, err := helpers.BuildHandleOpsRawTx(ctx, TestL1Account, canonical)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, signedTx, TestL1.RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(ctx, signedTx.Hash(), TestL1)
	require.NoError(t, err)
	logger.Info("[L1->%s SA NewToken] handleOps tx %s", destChain.Name(), signedTx.Hash().Hex())
	helpers.LogAssertOK("L1 handleOps receipt status=Successful (%s)", signedTx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)

	success, _, err := helpers.CheckUserOpSuccess(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 UserOperationEvent.success=true (got=%v)", success)
	require.True(t, success, "inner UserOp on L1 reverted (approve+bridgeERC20To failed)")

	// 8. Assert L1 token balance is now 0 (escrowed in the L1 bridge).
	saTokenAfter, err := getERC20BalanceAt(ctx, TestL1.RPCURL(), tokenAddr, saAddr, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("SA L1 token balance after bridge == 0: got=%s", saTokenAfter)
	require.Equalf(t, 0, saTokenAfter.Sign(),
		"SA L1 token should be 0 after bridge (escrowed in L1 bridge), got=%s", saTokenAfter)

	// 9. Poll the SA's CET balance on L2 + verify the CET contract was deployed.
	cetFinal, err := helpers.WaitForTokenBalanceChange(ctx, destChain.RPCURL(),
		cetAddr, saAddr, tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(saCETBefore) > 0 })
	require.NoError(t, err, "L2 CET balance for SA never increased — op-node may not have derived the deposit tx")

	helpers.LogAssertOK("CET contract deployed at %s on %s", cetAddr.Hex(), destChain.Name())
	require.NoError(t, helpers.AssertContractDeployed(ctx, destChain.RPCURL(), cetAddr),
		"CET contract should be deployed on L2 after the SA's L1 bridge")

	cetDelta := new(big.Int).Sub(cetFinal, saCETBefore)
	helpers.LogAssertOK("SA L2 CET balance credited exact bridge amount: delta=%s want=%s", cetDelta, bridgeAmt)
	require.Equalf(t, 0, cetDelta.Cmp(bridgeAmt),
		"SA L2 CET balance mismatch: got delta=%s want=%s", cetDelta, bridgeAmt)
}
