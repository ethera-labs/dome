// Go port of scripts/l1-to-l2-new-token.ts. Deploys a fresh MintableToken on
// L1, mints, predicts the CET address on the destination rollup, approves the
// L1 bridge, and bridges 100 tokens. Polls the predicted CET on L2 for the
// balance.
package test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
)

// 100 * 1e18. Reuses the L2->L2 token amount constant from the new-token test
// to keep amounts consistent across the suite.
var l1ToL2TokenBridgeAmount = l2ToL2TokenBridgeAmount

func TestL1ToL2_NewToken_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "l1", "a")
	RequireL1(t)
	runL1ToL2NewToken(t, configs.ChainNameRollupA, TestRollupA, TestAccountA)
}

func TestL1ToL2_NewToken_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "l1", "b")
	RequireL1(t)
	runL1ToL2NewToken(t, configs.ChainNameRollupB, TestRollupB, TestAccountB)
}

func runL1ToL2NewToken(
	t *testing.T,
	destRollup configs.ChainName,
	destChain *rollup.Rollup,
	destAccount *accounts.Account,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAmt := helpers.ParseBridgeAmountOverride(l1ToL2TokenBridgeAmount)

	bridgeAddr, err := helpers.L1BridgeAddressFor(destRollup)
	require.NoError(t, err)

	// 1. Deploy MintableToken on L1.
	tokenAddr, _, err := helpers.DeployMintableToken(ctx, TestL1Account, "BridgeTest", "BT", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	logger.Info("[L1->%s NewToken] deployed token=%s", destChain.Name(), tokenAddr.Hex())

	// 2. Mint to self.
	mintData, err := tokenABI.Pack("mint", TestL1Account.GetAddress(), bridgeAmt)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasMint, mintData)
	require.NoError(t, err)

	// 3. Predict CET on dest.
	cetAddr, err := helpers.PredictCetAddress(ctx, destChain, CetFactoryABI, tokenAddr, TestL1.ChainID())
	require.NoError(t, err)
	logger.Info("[L1->%s NewToken] predicted CET on %s = %s", destChain.Name(), destChain.Name(), cetAddr.Hex())

	// 4. ABI-encode extraData (name, symbol, decimals, bytes).
	extraData, err := helpers.EncodeERC20ExtraData("BridgeTest", "BT", 18)
	require.NoError(t, err)

	// 5. Approve the L1 bridge for the bridge amount.
	approveData, err := tokenABI.Pack("approve", bridgeAddr, bridgeAmt)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasApprove, approveData)
	require.NoError(t, err)

	// Snapshots.
	l1Before, err := TestL1Account.GetTokensBalance(ctx, tokenAddr, tokenABI)
	require.NoError(t, err)

	// 6. Bridge.
	bridgeData, err := helpers.PackBridgeERC20ToL1(ComposeL1BridgeABI,
		tokenAddr, cetAddr, TestL1Account.GetAddress(),
		bridgeAmt, uint32(helpers.L1MinGasLimitNewToken), extraData)
	require.NoError(t, err)
	bridgeTx, _, err := helpers.SendL1Tx(ctx, TestL1Account, bridgeAddr, big.NewInt(0), helpers.L1BridgeGasLimit, bridgeData)
	require.NoError(t, err)
	logger.Info("[L1->%s NewToken] L1 bridge tx confirmed: %s", destChain.Name(), bridgeTx.Hash().Hex())

	// 7. Assert L1 token balance dropped by exactly the bridge amount.
	l1After, err := TestL1Account.GetTokensBalance(ctx, tokenAddr, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 token balance: before=%s after=%s want delta=%s", l1Before, l1After, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Sub(l1Before, bridgeAmt).Cmp(l1After),
		"L1 token mismatch: got=%s want=%s", l1After, new(big.Int).Sub(l1Before, bridgeAmt))

	// 8. Poll L2 CET, then verify the CET contract actually got deployed.
	cetFinal, err := helpers.WaitForTokenBalanceChange(ctx, destChain.RPCURL(),
		cetAddr, destAccount.GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(big.NewInt(0)) > 0 })
	require.NoError(t, err, "CET balance never appeared on L2")
	helpers.LogAssertOK("CET contract deployed at %s on %s", cetAddr.Hex(), destChain.Name())
	require.NoError(t, helpers.AssertContractDeployed(ctx, destChain.RPCURL(), cetAddr),
		"CET contract should be deployed on L2 after first bridge")
	helpers.LogAssertOK("L2 CET balance == bridge amount: got=%s want=%s", cetFinal, bridgeAmt)
	require.Equalf(t, 0, cetFinal.Cmp(bridgeAmt),
		"L2 CET mismatch: got=%s want=%s", cetFinal, bridgeAmt)
}
