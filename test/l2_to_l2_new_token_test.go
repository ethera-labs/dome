// Go port of scripts/l2-to-l2-new-token.ts. Deploys a fresh MintableToken on
// the source rollup, mints 100, approves the bridge, and bridges via composed
// XT (source: bridgeERC20To, dest: receiveTokens) to the destination rollup,
// where ComposeL2ToL2Bridge.receiveTokens deploys the wrapped CET.
package test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

var l2ToL2TokenBridgeAmount = new(big.Int).Mul(big.NewInt(100), big.NewInt(1_000_000_000_000_000_000)) // 100 * 1e18

func TestL2ToL2_NewToken_AtoB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "b")
	if TestXTMode == configs.XTSubmissionRPC {
		RequireTSRuntime(t)
	}
	runL2ToL2NewToken(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func TestL2ToL2_NewToken_BtoA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "a")
	if TestXTMode == configs.XTSubmissionRPC {
		RequireTSRuntime(t)
	}
	runL2ToL2NewToken(t, TestAccountB, TestRollupB, TestAccountA, TestRollupA)
}

func runL2ToL2NewToken(
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

	// 1. Deploy MintableToken on source.
	tokenAddr, deployTx, err := helpers.DeployMintableToken(ctx, from, "NewToken", "NEW", 18)
	require.NoError(t, err)
	logger.Info("[L2->L2 NewToken %s->%s] deployed token=%s deployTx=%s",
		fromChain.Name(), toChain.Name(), tokenAddr.Hex(), deployTx.Hash().Hex())

	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)

	// 2. Mint bridge-amount to self and approve the bridge for max.
	mintCalldata, err := tokenABI.Pack("mint", from.GetAddress(), bridgeAmt)
	require.NoError(t, err)
	mintTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: tokenAddr, Value: big.NewInt(0), Gas: helpers.GasMint,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: mintCalldata,
	}, from)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, mintTx, fromChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, mintTx, fromChain)

	approveCalldata, err := tokenABI.Pack("approve", bridgeAddr,
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))) // max uint256
	require.NoError(t, err)
	approveTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: tokenAddr, Value: big.NewInt(0), Gas: helpers.GasApprove,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: approveCalldata,
	}, from)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, approveTx, fromChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, approveTx, fromChain)

	// 3. Predict CET on destination.
	cetAddr, err := helpers.PredictCetAddress(ctx, toChain, CetFactoryABI, tokenAddr, fromChain.ChainID())
	require.NoError(t, err)
	logger.Info("[L2->L2 NewToken %s->%s] predicted CET on %s = %s",
		fromChain.Name(), toChain.Name(), toChain.Name(), cetAddr.Hex())

	// Snapshots.
	srcTokenBefore, err := from.GetTokensBalance(ctx, tokenAddr, tokenABI)
	require.NoError(t, err)
	dstCETBefore := readBalanceOrZero(t, toChain.RPCURL(), cetAddr, to.GetAddress(), tokenABI)

	// 4. Build composed XT.
	sessionID := transactions.GenerateRandomSessionID()

	srcCalldata, err := helpers.PackBridgeERC20To(BridgeABI, toChain.ChainID(),
		tokenAddr, bridgeAmt, to.GetAddress(), sessionID)
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
		To: bridgeAddr, Value: big.NewInt(0),
		Gas:       5_000_000, // first call deploys CET
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: dstCalldata,
	}, to)
	require.NoError(t, err)

	// 5. Submit XT.
	instanceID, committed, err := helpers.SubmitXTWaitCommitted(t.Context(), map[uint64][][]byte{
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

	// 6. Assertions: source balance dropped by full amount, dest CET = amount,
	//    CET contract actually deployed on dest.
	srcTokenAfter, err := from.GetTokensBalance(ctx, tokenAddr, tokenABI)
	require.NoError(t, err)
	dstCETFinal, err := helpers.WaitForTokenBalanceChange(ctx, toChain.RPCURL(),
		cetAddr, to.GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(dstCETBefore) > 0 })
	require.NoError(t, err, "CET balance never increased on dest")

	helpers.LogAssertOK("CET contract deployed at predicted address %s on %s", cetAddr.Hex(), toChain.Name())
	require.NoError(t, helpers.AssertContractDeployed(ctx, toChain.RPCURL(), cetAddr),
		"CET contract should be deployed at predicted address after bridge")
	helpers.LogAssertOK("source token balance: before-bridge=%s after=%s want delta=%s",
		srcTokenBefore, srcTokenAfter, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Sub(srcTokenBefore, bridgeAmt).Cmp(srcTokenAfter),
		"source token balance mismatch: before-bridged != after (got %s vs expected %s)",
		srcTokenAfter, new(big.Int).Sub(srcTokenBefore, bridgeAmt))
	helpers.LogAssertOK("dest CET balance: before=%s final=%s want delta=%s",
		dstCETBefore, dstCETFinal, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Add(dstCETBefore, bridgeAmt).Cmp(dstCETFinal),
		"dest CET balance mismatch: got=%s want=%s", dstCETFinal,
		new(big.Int).Add(dstCETBefore, bridgeAmt))
}

func readBalanceOrZero(
	t *testing.T,
	rpcURL string,
	token common.Address,
	owner common.Address,
	tokenABI any,
) *big.Int {
	t.Helper()
	client, err := ethclient.Dial(rpcURL)
	require.NoError(t, err)
	defer client.Close()
	// CET may not be deployed yet — best-effort read.
	bal, err := readERC20Balance(t.Context(), client, token, owner)
	if err != nil {
		return big.NewInt(0)
	}
	return bal
}
