// Go port of scripts/l2-to-l2-SA-existing-token.ts. Two-phase SA test that
// mirrors l2_to_l2_existing_token (EOA) — the difference is the bridge UserOp
// runs through the SA + a final transfer CET-on-dest to the EOA.
package test

import (
	"fmt"
	"math/big"
	"testing"

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

type l2ToL2SAExistingState struct {
	TokenAddress common.Address `json:"tokenAddress"`
	PredictedCET common.Address `json:"predictedCET"`
	Source       string         `json:"source"`
	Dest         string         `json:"dest"`
}

func l2ToL2SAExistingStateName(src, dst *rollup.Rollup) string {
	return fmt.Sprintf(".l2-to-l2-SA-existing-token-state-%s-%s.json", src.Name(), dst.Name())
}

func TestL2ToL2_SA_ExistingToken_AtoB_DeployPhase(t *testing.T) {
	RequireAA(t)
	RequireTSRuntime(t)
	runL2ToL2SAExistingDeploy(t, TestAccountA, TestRollupA, TestRollupB)
}

func TestL2ToL2_SA_ExistingToken_BtoA_DeployPhase(t *testing.T) {
	RequireAA(t)
	RequireTSRuntime(t)
	runL2ToL2SAExistingDeploy(t, TestAccountB, TestRollupB, TestRollupA)
}

func TestL2ToL2_SA_ExistingToken_AtoB_BridgePhase(t *testing.T) {
	RequireAA(t)
	RequireTSRuntime(t)
	runL2ToL2SAExistingBridge(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func TestL2ToL2_SA_ExistingToken_BtoA_BridgePhase(t *testing.T) {
	RequireAA(t)
	RequireTSRuntime(t)
	runL2ToL2SAExistingBridge(t, TestAccountB, TestRollupB, TestAccountA, TestRollupA)
}

func runL2ToL2SAExistingDeploy(t *testing.T, funder *accounts.Account, srcChain, dstChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()

	tokenAddr, _, err := helpers.DeployMintableToken(ctx, funder, "ExistingTokenSA", "ETSA", 18)
	require.NoError(t, err)
	cetAddr, err := helpers.PredictCetAddress(ctx, dstChain, CetFactoryABI, tokenAddr, srcChain.ChainID())
	require.NoError(t, err)

	state := l2ToL2SAExistingState{
		TokenAddress: tokenAddr,
		PredictedCET: cetAddr,
		Source:       srcChain.Name(),
		Dest:         dstChain.Name(),
	}
	require.NoError(t, helpers.SaveJSONState(l2ToL2SAExistingStateName(srcChain, dstChain), state))
	logger.Info("[SA L2->L2 ExistingToken %s->%s] deployed=%s cet=%s",
		srcChain.Name(), dstChain.Name(), tokenAddr.Hex(), cetAddr.Hex())
}

func runL2ToL2SAExistingBridge(
	t *testing.T,
	srcFunder *accounts.Account, srcChain *rollup.Rollup,
	dstFunder *accounts.Account, dstChain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	pk := configs.Values.WalletPrivateKey
	bridgeAmt := helpers.ParseBridgeAmountOverride(l2ToL2TokenBridgeAmount)

	var state l2ToL2SAExistingState
	found, err := helpers.LoadJSONState(l2ToL2SAExistingStateName(srcChain, dstChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run the corresponding _DeployPhase test first",
			l2ToL2SAExistingStateName(srcChain, dstChain))
	}

	// SA + EntryPoint deposit.
	saAddr, _, err := helpers.SACreateAccount(ctx, pk, uint64(srcChain.ChainID().Int64()),
		[]uint64{uint64(srcChain.ChainID().Int64()), uint64(dstChain.ChainID().Int64())})
	require.NoError(t, err)
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, srcFunder, saAddr, helpers.MinEntryPointDeposit))
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, dstFunder, saAddr, helpers.MinEntryPointDeposit))

	// Mint 100 to SA on source.
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	mintData, err := tokenABI.Pack("mint", saAddr, bridgeAmt)
	require.NoError(t, err)
	mintTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: state.TokenAddress, Value: big.NewInt(0), Gas: helpers.GasMint,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: mintData,
	}, srcFunder)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, mintTx, srcChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, mintTx, srcChain)

	saSrcBefore, _ := getERC20BalanceAt(ctx, srcChain.RPCURL(), state.TokenAddress, saAddr, tokenABI)
	eoaDstBefore := readBalanceOrZero(t, dstChain.RPCURL(), state.PredictedCET, srcFunder.GetAddress(), tokenABI)

	sessionID := transactions.GenerateRandomSessionID()
	approveData, err := tokenABI.Pack("approve", bridgeAddr, bridgeAmt)
	require.NoError(t, err)
	bridgeData, err := helpers.PackBridgeERC20To(BridgeABI, dstChain.ChainID(),
		state.TokenAddress, bridgeAmt, saAddr, sessionID)
	require.NoError(t, err)
	receiveData, err := helpers.PackBridgeReceiveTokens(BridgeABI,
		srcChain.ChainID(), dstChain.ChainID(), bridgeAddr, saAddr, sessionID)
	require.NoError(t, err)
	transferData, err := tokenABI.Pack("transfer", srcFunder.GetAddress(), bridgeAmt)
	require.NoError(t, err)

	calls := []helpers.UserOpCall{
		{ChainID: uint64(srcChain.ChainID().Int64()), To: state.TokenAddress, Value: "0", Data: hexutil.Encode(approveData)},
		{ChainID: uint64(srcChain.ChainID().Int64()), To: bridgeAddr, Value: "0", Data: hexutil.Encode(bridgeData)},
		{ChainID: uint64(dstChain.ChainID().Int64()), To: bridgeAddr, Value: "0", Data: hexutil.Encode(receiveData)},
		{ChainID: uint64(dstChain.ChainID().Int64()), To: state.PredictedCET, Value: "0", Data: hexutil.Encode(transferData)},
	}

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

	saSrcAfter, _ := getERC20BalanceAt(ctx, srcChain.RPCURL(), state.TokenAddress, saAddr, tokenABI)
	helpers.LogAssertOK("SA source token balance == 0 after bridge (got=%s)", saSrcAfter)
	require.Equalf(t, 0, saSrcAfter.Sign(), "SA source balance should be 0 after bridge, got=%s", saSrcAfter)
	helpers.LogAssertOK("SA source token: before=%s after=%s want delta=%s", saSrcBefore, saSrcAfter, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Sub(saSrcBefore, bridgeAmt).Cmp(saSrcAfter),
		"SA source delta mismatch")

	eoaDstFinal, err := helpers.WaitForTokenBalanceChange(ctx, dstChain.RPCURL(),
		state.PredictedCET, srcFunder.GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(eoaDstBefore) > 0 })
	require.NoError(t, err)
	helpers.LogAssertOK("CET contract deployed at %s on %s", state.PredictedCET.Hex(), dstChain.Name())
	require.NoError(t, helpers.AssertContractDeployed(ctx, dstChain.RPCURL(), state.PredictedCET),
		"CET contract should be deployed on dest after first bridge")
	helpers.LogAssertOK("EOA dest CET balance: before=%s final=%s want delta=%s",
		eoaDstBefore, eoaDstFinal, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Add(eoaDstBefore, bridgeAmt).Cmp(eoaDstFinal),
		"EOA dest CET delta mismatch")
}
