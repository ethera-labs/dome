// Go port of scripts/l1-to-l2-existing-token.ts. Two-phase test:
//
//   _DeployPhase  — deploys MintableToken on L1, approves the L1 bridge for
//                   max uint256, saves state.
//   _BridgePhase  — mints 100 to self on L1, bridges to dest rollup, asserts
//                   L1 dropped by 100 and L2 CET = 100.
package test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
)

type l1ToL2ExistingTokenState struct {
	TokenAddress common.Address `json:"tokenAddress"`
	PredictedCET common.Address `json:"predictedCET"`
	Dest         string         `json:"dest"`
}

func l1ToL2ExistingStateName(dest *rollup.Rollup) string {
	return fmt.Sprintf(".l1-to-l2-existing-token-state-%s.json", dest.Name())
}

func TestL1ToL2_ExistingToken_RollupA_DeployPhase(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "l1", "a")
	RequireL1(t)
	runL1ToL2ExistingDeploy(t, configs.ChainNameRollupA, TestRollupA)
}

func TestL1ToL2_ExistingToken_RollupB_DeployPhase(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "l1", "b")
	RequireL1(t)
	runL1ToL2ExistingDeploy(t, configs.ChainNameRollupB, TestRollupB)
}

func TestL1ToL2_ExistingToken_RollupA_BridgePhase(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "l1", "a")
	RequireL1(t)
	runL1ToL2ExistingBridge(t, configs.ChainNameRollupA, TestRollupA, TestAccountA)
}

func TestL1ToL2_ExistingToken_RollupB_BridgePhase(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "l1", "b")
	RequireL1(t)
	runL1ToL2ExistingBridge(t, configs.ChainNameRollupB, TestRollupB, TestAccountB)
}

func runL1ToL2ExistingDeploy(t *testing.T, destRollup configs.ChainName, destChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()

	bridgeAddr, err := helpers.L1BridgeAddressFor(destRollup)
	require.NoError(t, err)

	tokenAddr, _, err := helpers.DeployMintableToken(ctx, TestL1Account, "ExistingTokenL1", "ETL1", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)

	// Approve max uint256.
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	approveData, err := tokenABI.Pack("approve", bridgeAddr, maxUint)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasApprove, approveData)
	require.NoError(t, err)

	cetAddr, err := helpers.PredictCetAddress(ctx, destChain, CetFactoryABI, tokenAddr, TestL1.ChainID())
	require.NoError(t, err)

	state := l1ToL2ExistingTokenState{
		TokenAddress: tokenAddr,
		PredictedCET: cetAddr,
		Dest:         destChain.Name(),
	}
	require.NoError(t, helpers.SaveJSONState(l1ToL2ExistingStateName(destChain), state))
	logger.Info("[L1->%s ExistingToken] deployed token=%s predictedCET=%s",
		destChain.Name(), tokenAddr.Hex(), cetAddr.Hex())
}

func runL1ToL2ExistingBridge(
	t *testing.T,
	destRollup configs.ChainName,
	destChain *rollup.Rollup,
	destAccount *accounts.Account,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAmt := helpers.ParseBridgeAmountOverride(l1ToL2TokenBridgeAmount)

	var state l1ToL2ExistingTokenState
	found, err := helpers.LoadJSONState(l1ToL2ExistingStateName(destChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run the corresponding _DeployPhase test first",
			l1ToL2ExistingStateName(destChain))
	}

	bridgeAddr, err := helpers.L1BridgeAddressFor(destRollup)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)

	// Mint bridge-amount to self on L1.
	mintData, err := tokenABI.Pack("mint", TestL1Account.GetAddress(), bridgeAmt)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, state.TokenAddress, big.NewInt(0), helpers.GasMint, mintData)
	require.NoError(t, err)

	l1Before, err := TestL1Account.GetTokensBalance(ctx, state.TokenAddress, tokenABI)
	require.NoError(t, err)
	cetBefore := readBalanceOrZero(t, destChain.RPCURL(), state.PredictedCET, destAccount.GetAddress(), tokenABI)

	extraData, err := helpers.EncodeERC20ExtraData("ExistingTokenL1", "ETL1", 18)
	require.NoError(t, err)

	// Bridge: choose gas budget based on whether CET is already deployed on L2.
	minGas := uint32(helpers.L1MinGasLimitERC20)
	if cetBefore.Sign() == 0 {
		minGas = uint32(helpers.L1MinGasLimitNewToken)
	}

	bridgeData, err := helpers.PackBridgeERC20ToL1(ComposeL1BridgeABI,
		state.TokenAddress, state.PredictedCET, TestL1Account.GetAddress(),
		bridgeAmt, minGas, extraData)
	require.NoError(t, err)
	bridgeTx, _, err := helpers.SendL1Tx(ctx, TestL1Account, bridgeAddr, big.NewInt(0), helpers.L1BridgeGasLimit, bridgeData)
	require.NoError(t, err)
	logger.Info("[L1->%s ExistingToken] L1 bridge tx %s", destChain.Name(), bridgeTx.Hash().Hex())

	l1After, err := TestL1Account.GetTokensBalance(ctx, state.TokenAddress, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 token balance: before=%s after=%s want delta=%s", l1Before, l1After, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Sub(l1Before, bridgeAmt).Cmp(l1After),
		"L1 token mismatch: got=%s want=%s", l1After, new(big.Int).Sub(l1Before, bridgeAmt))

	cetFinal, err := helpers.WaitForTokenBalanceChange(ctx, destChain.RPCURL(),
		state.PredictedCET, destAccount.GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(cetBefore) > 0 })
	require.NoError(t, err)
	helpers.LogAssertOK("CET contract deployed at %s on %s", state.PredictedCET.Hex(), destChain.Name())
	require.NoError(t, helpers.AssertContractDeployed(ctx, destChain.RPCURL(), state.PredictedCET),
		"CET contract should be deployed on L2 after bridge")
	helpers.LogAssertOK("L2 CET balance: before=%s final=%s want delta=%s", cetBefore, cetFinal, bridgeAmt)
	require.Equalf(t, 0, new(big.Int).Add(cetBefore, bridgeAmt).Cmp(cetFinal),
		"L2 CET mismatch: got=%s want=%s", cetFinal, new(big.Int).Add(cetBefore, bridgeAmt))
}
