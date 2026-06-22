// Go port of scripts/l2-to-l2-existing-token.ts. Two-phase test:
//
//   1. _DeployPhase — deploys a MintableToken once on the source rollup,
//      approves the bridge for max uint256, saves state. State is keyed by
//      (source, dest) so A->B and B->A pairs can coexist.
//   2. _BridgePhase — mints 100 to self, composes bridgeERC20To/receiveTokens,
//      asserts source debited and dest CET credited by exactly 100.
//
// Running _BridgePhase before _DeployPhase results in a skip with a clear
// message ("run TestL2ToL2_ExistingToken_..._DeployPhase first").
package test

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

type l2ToL2ExistingTokenState struct {
	TokenAddress common.Address `json:"tokenAddress"`
	PredictedCET common.Address `json:"predictedCET"`
	Source       string         `json:"source"`
	Dest         string         `json:"dest"`
}

func l2ToL2ExistingStateName(srcChain, dstChain *rollup.Rollup) string {
	return fmt.Sprintf(".l2-to-l2-existing-token-state-%s-%s.json", srcChain.Name(), dstChain.Name())
}

func TestL2ToL2_ExistingToken_AtoB_DeployPhase(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "b")
	if TestXTMode == configs.XTSubmissionRPC {
		RequireTSRuntime(t)
	}
	runL2ToL2ExistingTokenDeploy(t, TestAccountA, TestRollupA, TestRollupB)
}

func TestL2ToL2_ExistingToken_BtoA_DeployPhase(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "a")
	if TestXTMode == configs.XTSubmissionRPC {
		RequireTSRuntime(t)
	}
	runL2ToL2ExistingTokenDeploy(t, TestAccountB, TestRollupB, TestRollupA)
}

func TestL2ToL2_ExistingToken_AtoB_BridgePhase(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "b")
	if TestXTMode == configs.XTSubmissionRPC {
		RequireTSRuntime(t)
	}
	runL2ToL2ExistingTokenBridge(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func TestL2ToL2_ExistingToken_BtoA_BridgePhase(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "a")
	if TestXTMode == configs.XTSubmissionRPC {
		RequireTSRuntime(t)
	}
	runL2ToL2ExistingTokenBridge(t, TestAccountB, TestRollupB, TestAccountA, TestRollupA)
}

func runL2ToL2ExistingTokenDeploy(
	t *testing.T,
	from *accounts.Account,
	fromChain *rollup.Rollup,
	toChain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	tokenAddr, _, err := helpers.DeployMintableToken(ctx, from, "ExistingToken", "ET", 18)
	require.NoError(t, err)

	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)

	// Approve max uint256.
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	approveCalldata, err := tokenABI.Pack("approve", bridgeAddr, maxUint)
	require.NoError(t, err)
	approveTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: tokenAddr, Value: big.NewInt(0), Gas: helpers.GasApprove,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: approveCalldata,
	}, from)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, approveTx, fromChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, approveTx, fromChain)

	// Verify the approval actually landed on-chain — silent approve failures
	// would surface much later as a confusing bridge revert.
	allowance, err := readERC20Allowance(ctx, fromChain.RPCURL(), tokenAddr, from.GetAddress(), bridgeAddr)
	require.NoError(t, err)
	helpers.LogAssertOK("allowance after approve covers bridge amount: %s want>=%s", allowance, l2ToL2TokenBridgeAmount)
	require.GreaterOrEqualf(t, allowance.Cmp(l2ToL2TokenBridgeAmount), 0,
		"allowance after approve too small: got=%s want>=%s", allowance, l2ToL2TokenBridgeAmount)

	predictedCET, err := helpers.PredictCetAddress(ctx, toChain, CetFactoryABI, tokenAddr, fromChain.ChainID())
	require.NoError(t, err)

	state := l2ToL2ExistingTokenState{
		TokenAddress: tokenAddr,
		PredictedCET: predictedCET,
		Source:       fromChain.Name(),
		Dest:         toChain.Name(),
	}
	require.NoError(t, helpers.SaveJSONState(l2ToL2ExistingStateName(fromChain, toChain), state))
	logger.Info("[L2->L2 ExistingToken %s->%s] deployed=%s predictedCET=%s — bridge phase can now run",
		fromChain.Name(), toChain.Name(), tokenAddr.Hex(), predictedCET.Hex())
}

func runL2ToL2ExistingTokenBridge(
	t *testing.T,
	from *accounts.Account,
	fromChain *rollup.Rollup,
	to *accounts.Account,
	toChain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	bridgeAmt := helpers.ParseBridgeAmountOverride(l2ToL2TokenBridgeAmount)

	var state l2ToL2ExistingTokenState
	found, err := helpers.LoadJSONState(l2ToL2ExistingStateName(fromChain, toChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run the corresponding _DeployPhase test first",
			l2ToL2ExistingStateName(fromChain, toChain))
	}

	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)

	// 1. Mint bridge-amount to self.
	mintCalldata, err := tokenABI.Pack("mint", from.GetAddress(), bridgeAmt)
	require.NoError(t, err)
	mintTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: state.TokenAddress, Value: big.NewInt(0), Gas: helpers.GasMint,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: mintCalldata,
	}, from)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, mintTx, fromChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, mintTx, fromChain)

	// Snapshots.
	srcBefore, err := from.GetTokensBalance(ctx, state.TokenAddress, tokenABI)
	require.NoError(t, err)
	dstBefore := readBalanceOrZero(t, toChain.RPCURL(), state.PredictedCET, to.GetAddress(), tokenABI)

	// 2. Compose + submit.
	sessionID := transactions.GenerateRandomSessionID()

	srcCalldata, err := helpers.PackBridgeERC20To(BridgeABI, toChain.ChainID(),
		state.TokenAddress, bridgeAmt, to.GetAddress(), sessionID)
	require.NoError(t, err)
	srcTx, srcBytes, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: bridgeAddr, Value: big.NewInt(0), Gas: 3_000_000,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: srcCalldata,
	}, from)
	require.NoError(t, err)

	dstCalldata, err := helpers.PackBridgeReceiveTokens(BridgeABI,
		fromChain.ChainID(), toChain.ChainID(), bridgeAddr, to.GetAddress(), sessionID)
	require.NoError(t, err)
	dstTx, dstBytes, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: bridgeAddr, Value: big.NewInt(0), Gas: 5_000_000, // may deploy CET on first run
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: dstCalldata,
	}, to)
	require.NoError(t, err)

	instanceID, committed, err := helpers.SubmitXTWaitCommitted(ctx, map[uint64][][]byte{
		uint64(fromChain.ChainID().Int64()): {srcBytes},
		uint64(toChain.ChainID().Int64()):   {dstBytes},
	}, uint64(fromChain.ChainID().Int64()), 90*time.Second)
	require.NoError(t, err)
	helpers.LogAssertXTOK(srcTx.Hash().Hex(), dstTx.Hash().Hex(), instanceID)
	require.True(t, committed, "XT should commit")

	helpers.LogAssertOK("source tx receipt status=Successful (%s)", srcTx.Hash().Hex())
	waitReceipt(t, ctx, srcTx, fromChain)
	helpers.LogAssertOK("dest tx receipt status=Successful (%s)", dstTx.Hash().Hex())
	waitReceipt(t, ctx, dstTx, toChain)

	// 3. Assertions.
	srcAfter, err := from.GetTokensBalance(ctx, state.TokenAddress, tokenABI)
	require.NoError(t, err)
	dstFinal, err := helpers.WaitForTokenBalanceChange(ctx, toChain.RPCURL(),
		state.PredictedCET, to.GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(dstBefore) > 0 })
	require.NoError(t, err)

	helpers.LogAssertOK("CET contract deployed at %s on %s", state.PredictedCET.Hex(), toChain.Name())
	require.NoError(t, helpers.AssertContractDeployed(ctx, toChain.RPCURL(), state.PredictedCET),
		"CET contract should be deployed at predicted address after bridge")
	helpers.LogAssertOK("source token balance: before=%s after=%s want delta=%s", srcBefore, srcAfter, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Sub(srcBefore, bridgeAmt).Cmp(srcAfter),
		"source token balance mismatch: got=%s want=%s",
		srcAfter, new(big.Int).Sub(srcBefore, bridgeAmt))
	helpers.LogAssertOK("dest CET balance: before=%s final=%s want delta=%s", dstBefore, dstFinal, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Add(dstBefore, bridgeAmt).Cmp(dstFinal),
		"dest CET balance mismatch: got=%s want=%s", dstFinal,
		new(big.Int).Add(dstBefore, bridgeAmt))
}
