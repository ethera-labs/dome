// Go port of scripts/l2-to-l2-SA-new-token.ts. Smart-account variant of
// l2_to_l2_new_token: deploy MintableToken on source via the funder EOA, mint
// 100 to the SA, predict CET on dest, then have the SA execute
// approve+bridgeERC20To on source and receiveTokens+transfer (to the EOA) on
// dest as a composed UserOp.
package test

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

func TestL2ToL2_SA_NewToken_AtoB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "b")
	RequireAA(t)
	RequireTSRuntime(t)
	runL2ToL2SANewToken(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func TestL2ToL2_SA_NewToken_BtoA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "a")
	RequireAA(t)
	RequireTSRuntime(t)
	runL2ToL2SANewToken(t, TestAccountB, TestRollupB, TestAccountA, TestRollupA)
}

func runL2ToL2SANewToken(
	t *testing.T,
	srcFunder *accounts.Account,
	srcChain *rollup.Rollup,
	dstFunder *accounts.Account,
	dstChain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	pk := configs.Values.WalletPrivateKey
	bridgeAmt := helpers.ParseBridgeAmountOverride(l2ToL2TokenBridgeAmount)

	// 1. SA + EntryPoint deposit.
	saAddr, _, err := helpers.SACreateAccount(ctx, pk, uint64(srcChain.ChainID().Int64()),
		[]uint64{uint64(srcChain.ChainID().Int64()), uint64(dstChain.ChainID().Int64())})
	require.NoError(t, err)
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, srcFunder, saAddr, helpers.MinEntryPointDeposit))
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, dstFunder, saAddr, helpers.MinEntryPointDeposit))

	// 2. Deploy token on source, mint 100 to SA.
	tokenAddr, _, err := helpers.DeployMintableToken(ctx, srcFunder, "SANewToken", "SANT", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)

	mintData, err := tokenABI.Pack("mint", saAddr, bridgeAmt)
	require.NoError(t, err)
	mintTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: tokenAddr, Value: big.NewInt(0), Gas: helpers.GasMint,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: mintData,
	}, srcFunder)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, mintTx, srcChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, mintTx, srcChain)

	// 3. Predict CET on dest.
	cetAddr, err := helpers.PredictCetAddress(ctx, dstChain, CetFactoryABI, tokenAddr, srcChain.ChainID())
	require.NoError(t, err)
	logger.Info("[SA L2->L2 NewToken %s->%s] sa=%s token=%s cet=%s",
		srcChain.Name(), dstChain.Name(), saAddr.Hex(), tokenAddr.Hex(), cetAddr.Hex())

	// 4. Snapshots.
	saSrcBefore, err := getERC20BalanceAt(ctx, srcChain.RPCURL(), tokenAddr, saAddr, tokenABI)
	require.NoError(t, err)
	eoaDstBefore := readBalanceOrZero(t, dstChain.RPCURL(), cetAddr, srcFunder.GetAddress(), tokenABI)

	// 5. Build UserOp calls.
	sessionID := transactions.GenerateRandomSessionID()
	// Source: approve bridge, then bridgeERC20To.
	approveData, err := tokenABI.Pack("approve", bridgeAddr, bridgeAmt)
	require.NoError(t, err)
	bridgeData, err := helpers.PackBridgeERC20To(BridgeABI, dstChain.ChainID(),
		tokenAddr, bridgeAmt, saAddr, sessionID)
	require.NoError(t, err)
	// Dest: receiveTokens + transfer CET to the EOA.
	receiveData, err := helpers.PackBridgeReceiveTokens(BridgeABI,
		srcChain.ChainID(), dstChain.ChainID(), bridgeAddr, saAddr, sessionID)
	require.NoError(t, err)
	transferData, err := tokenABI.Pack("transfer", srcFunder.GetAddress(), bridgeAmt)
	require.NoError(t, err)

	calls := []helpers.UserOpCall{
		{ChainID: uint64(srcChain.ChainID().Int64()), To: tokenAddr, Value: "0", Data: hexutil.Encode(approveData)},
		{ChainID: uint64(srcChain.ChainID().Int64()), To: bridgeAddr, Value: "0", Data: hexutil.Encode(bridgeData)},
		{ChainID: uint64(dstChain.ChainID().Int64()), To: bridgeAddr, Value: "0", Data: hexutil.Encode(receiveData)},
		{ChainID: uint64(dstChain.ChainID().Int64()), To: cetAddr, Value: "0", Data: hexutil.Encode(transferData)},
	}

	// 6. Submit. Use the same gas overrides as the original TS scripts —
	//    default estimation undershoots cross-chain calls.
	overrides := helpers.StandardSATokenBridgeGasOverrides(
		uint64(srcChain.ChainID().Int64()), uint64(dstChain.ChainID().Int64()),
	)
	if TestXTMode == configs.XTSubmissionRPC {
		hashes, err := helpers.SAComposeAndSubmit(ctx, pk, calls, overrides)
		require.NoError(t, err)
		require.Len(t, hashes, 2)
		waitComposedReceipts(t, ctx, hashes, srcChain, dstChain)
	} else {
		require.NoError(t, submitSAViaSidecar(t, ctx, pk, calls, srcChain, dstChain, srcFunder, dstFunder, overrides))
	}

	// 7. Assertions.
	saSrcAfter, err := getERC20BalanceAt(ctx, srcChain.RPCURL(), tokenAddr, saAddr, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("SA source token balance == 0 after bridge (got=%s)", saSrcAfter)
	require.Equalf(t, 0, saSrcAfter.Sign(), "SA source balance should be 0 after bridge, got=%s", saSrcAfter)
	helpers.LogAssertOK("SA source token: before=%s after=%s want delta=%s", saSrcBefore, saSrcAfter, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Sub(saSrcBefore, bridgeAmt).Cmp(saSrcAfter),
		"SA source delta mismatch")

	eoaDstFinal, err := helpers.WaitForTokenBalanceChange(ctx, dstChain.RPCURL(),
		cetAddr, srcFunder.GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(eoaDstBefore) > 0 })
	require.NoError(t, err, "EOA CET balance never increased on dest")
	helpers.LogAssertOK("CET contract deployed at %s on %s", cetAddr.Hex(), dstChain.Name())
	require.NoError(t, helpers.AssertContractDeployed(ctx, dstChain.RPCURL(), cetAddr),
		"CET contract should be deployed on dest after first bridge")
	helpers.LogAssertOK("EOA dest CET balance: before=%s final=%s want delta=%s",
		eoaDstBefore, eoaDstFinal, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Add(eoaDstBefore, bridgeAmt).Cmp(eoaDstFinal),
		"EOA CET balance mismatch: got=%s want=%s",
		eoaDstFinal, new(big.Int).Add(eoaDstBefore, bridgeAmt))
}

// getERC20BalanceAt reads token.balanceOf(owner) from rpcURL using the given
// token ABI. Tolerates not-yet-deployed contracts by returning 0.
func getERC20BalanceAt(
	ctx context.Context,
	rpcURL string,
	token, owner common.Address,
	tokenABI abi.ABI,
) (*big.Int, error) {
	bal, err := helpers.WaitForTokenBalanceChange(ctx, rpcURL, token, owner, tokenABI,
		0, 1, func(*big.Int) bool { return true })
	if err != nil {
		return big.NewInt(0), nil
	}
	return bal, nil
}
