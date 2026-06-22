// Go port of scripts/l2-to-l1-token.ts. Full L1->L2->L1 token round-trip.
// Two-phase:
//
//   _Withdraw — deploy token on L1, mint, bridge L1->L2, poll CET arrival,
//               burn CET via ComposeL2Bridge.bridgeERC20To, save state.
//   _Finalize — prove + wait for maturity + finalize. Asserts L1 token balance
//               restored.
package test

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

type l2ToL1TokenState struct {
	Source             string         `json:"source"`
	TokenAddress       common.Address `json:"tokenAddress"`
	PredictedCET       common.Address `json:"predictedCET"`
	WithdrawalHash     common.Hash    `json:"withdrawalHash"`
	WithdrawalNonce    string         `json:"withdrawalNonce"`
	WithdrawalSender   common.Address `json:"withdrawalSender"`
	WithdrawalTarget   common.Address `json:"withdrawalTarget"`
	WithdrawalValue    string         `json:"withdrawalValue"`
	WithdrawalGasLimit string         `json:"withdrawalGasLimit"`
	WithdrawalData     []byte         `json:"withdrawalData"`
	L2TxHash           common.Hash    `json:"l2TxHash"`
	L2Block            uint64         `json:"l2Block"`
}

func l2ToL1TokenStateName(src *rollup.Rollup) string {
	return fmt.Sprintf(".l2-to-l1-token-state-%s.json", src.Name())
}

func TestL2ToL1_Token_Withdraw_RollupA(t *testing.T) {
	RequireL1(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1TokenWithdraw(t, configs.ChainNameRollupA, TestAccountA, TestRollupA)
}

func TestL2ToL1_Token_Withdraw_RollupB(t *testing.T) {
	RequireL1(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1TokenWithdraw(t, configs.ChainNameRollupB, TestAccountB, TestRollupB)
}

func TestL2ToL1_Token_Finalize_RollupA(t *testing.T) {
	RequireL1(t)
	runL2ToL1TokenFinalize(t, configs.ChainNameRollupA, TestAccountA, TestRollupA)
}

func TestL2ToL1_Token_Finalize_RollupB(t *testing.T) {
	RequireL1(t)
	runL2ToL1TokenFinalize(t, configs.ChainNameRollupB, TestAccountB, TestRollupB)
}

func runL2ToL1TokenWithdraw(t *testing.T, src configs.ChainName, ac *accounts.Account, srcChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()
	bridgeAmt := helpers.ParseBridgeAmountOverride(l1ToL2TokenBridgeAmount)

	l1BridgeAddr, err := helpers.L1BridgeAddressFor(src)
	require.NoError(t, err)

	// Phase 1: L1 -> L2 (deploy, mint, bridge, poll CET).
	tokenAddr, _, err := helpers.DeployMintableToken(ctx, TestL1Account, "RoundTripToken", "RTT", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)

	mintData, err := tokenABI.Pack("mint", TestL1Account.GetAddress(), bridgeAmt)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasMint, mintData)
	require.NoError(t, err)

	cetAddr, err := helpers.PredictCetAddress(ctx, srcChain, CetFactoryABI, tokenAddr, TestL1.ChainID())
	require.NoError(t, err)
	logger.Info("[L2->L1 Token %s] cet=%s", srcChain.Name(), cetAddr.Hex())

	extraData, err := helpers.EncodeERC20ExtraData("RoundTripToken", "RTT", 18)
	require.NoError(t, err)
	approveData, err := tokenABI.Pack("approve", l1BridgeAddr, bridgeAmt)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasApprove, approveData)
	require.NoError(t, err)

	bridgeData, err := helpers.PackBridgeERC20ToL1(ComposeL1BridgeABI,
		tokenAddr, cetAddr, TestL1Account.GetAddress(),
		bridgeAmt, uint32(helpers.L1MinGasLimitNewToken), extraData)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, l1BridgeAddr, big.NewInt(0), helpers.L1BridgeGasLimit, bridgeData)
	require.NoError(t, err)

	// Wait for CET on L2.
	_, err = helpers.WaitForTokenBalanceChange(ctx, srcChain.RPCURL(),
		cetAddr, ac.GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(bridgeAmt) >= 0 })
	require.NoError(t, err, "CET never arrived on L2")
	helpers.LogAssertOK("L1->L2: CET contract deployed on %s at %s", srcChain.Name(), cetAddr.Hex())
	require.NoError(t, helpers.AssertContractDeployed(ctx, srcChain.RPCURL(), cetAddr),
		"CET contract should be deployed on L2 after first bridge")
	logger.Info("[L2->L1 Token %s] CET 100 received on L2", srcChain.Name())

	// Phase 2: L2 -> L1 burn (ComposeL2Bridge.bridgeERC20To).
	l2BridgeAddr := l2BridgeAddressFor(src)
	burnData, err := helpers.PackBridgeERC20ToOnL2(ComposeL2BridgeABI,
		cetAddr, tokenAddr, TestL1Account.GetAddress(),
		bridgeAmt, uint32(helpers.L1MinGasLimitNewToken), []byte{})
	require.NoError(t, err)
	tx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: l2BridgeAddr, Value: big.NewInt(0), Gas: helpers.L1BridgeGasLimit,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: burnData,
	}, ac)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, tx, srcChain.RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), srcChain)
	require.NoError(t, err)
	helpers.LogAssertOK("L2 burn tx receipt status=Successful (%s)", tx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)

	// CET should be burned (balance == 0) on the source rollup after burn.
	cetAfter, err := ac.GetTokensBalance(ctx, cetAddr, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("CET balance == 0 after burn on %s (got=%s)", srcChain.Name(), cetAfter)
	require.Equalf(t, 0, cetAfter.Sign(), "CET balance should be 0 after burn, got=%s", cetAfter)

	wtx, withdrawalHash, err := helpers.ExtractMessagePassed(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("MessagePassed event extracted: withdrawalHash=%s l2Block=%d",
		withdrawalHash.Hex(), receipt.BlockNumber.Uint64())

	state := l2ToL1TokenState{
		Source:             srcChain.Name(),
		TokenAddress:       tokenAddr,
		PredictedCET:       cetAddr,
		WithdrawalHash:     withdrawalHash,
		WithdrawalNonce:    wtx.Nonce.String(),
		WithdrawalSender:   wtx.Sender,
		WithdrawalTarget:   wtx.Target,
		WithdrawalValue:    wtx.Value.String(),
		WithdrawalGasLimit: wtx.GasLimit.String(),
		WithdrawalData:     wtx.Data,
		L2TxHash:           tx.Hash(),
		L2Block:            receipt.BlockNumber.Uint64(),
	}
	require.NoError(t, helpers.SaveJSONState(l2ToL1TokenStateName(srcChain), state))
}

func runL2ToL1TokenFinalize(t *testing.T, src configs.ChainName, ac *accounts.Account, srcChain *rollup.Rollup) {
	t.Helper()
	_ = ac
	ctx := t.Context()
	bridgeAmt := helpers.ParseBridgeAmountOverride(l1ToL2TokenBridgeAmount)

	var state l2ToL1TokenState
	found, err := helpers.LoadJSONState(l2ToL1TokenStateName(srcChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run TestL2ToL1_Token_Withdraw_%s first",
			l2ToL1TokenStateName(srcChain), srcChain.Name())
	}

	portalAddr := portalAddressFor(src)
	wtx := l2ToL1TokenWtx(state)

	// 1. Already finalized?
	var alreadyFinalized bool
	err = helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &alreadyFinalized, state.WithdrawalHash)
	require.NoError(t, err)
	if alreadyFinalized {
		require.NoError(t, helpers.DeleteJSONState(l2ToL1TokenStateName(srcChain)))
		return
	}

	// 2. Prove if not yet proven.
	var numSubmitters *big.Int
	err = helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"numProofSubmitters", &numSubmitters, state.WithdrawalHash)
	require.NoError(t, err)
	if numSubmitters.Sign() == 0 {
		require.NoError(t, proveL2ToL1TokenWithdrawal(t, portalAddr, srcChain, state, wtx))
	}

	// 3. Wait for maturity. Fail (not skip) if it doesn't mature in the poll
	//    window — re-run once the dispute window has elapsed.
	require.NoErrorf(t,
		helpers.WaitForProofMaturity(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
			state.WithdrawalHash, TestL1Account.GetAddress(),
			20*time.Second, helpers.DefaultPollAttempts*6,
		),
		"[L2->L1 Token %s] proof not yet mature — re-run finalize after the dispute window",
		srcChain.Name(),
	)

	// 4. Finalize.
	finalizeData, err := helpers.PackFinalizeWithdrawal(ComposePortalABI, wtx)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, finalizeData)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 finalize tx confirmed for withdrawalHash=%s", state.WithdrawalHash.Hex())

	// Portal should now report the withdrawal as finalized.
	var finalizedNow bool
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &finalizedNow, state.WithdrawalHash))
	helpers.LogAssertOK("portal.finalizedWithdrawals(%s) == true (got=%v)", state.WithdrawalHash.Hex(), finalizedNow)
	require.True(t, finalizedNow, "portal.finalizedWithdrawals should be true after finalize call")

	// 5. Assert L1 token balance restored on L1.
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	bal, err := TestL1Account.GetTokensBalance(ctx, state.TokenAddress, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 token balance restored after round-trip: got=%s want=%s", bal, bridgeAmt)
	require.Equalf(t, 0, bal.Cmp(bridgeAmt),
		"L1 token balance should equal bridged amount after round-trip: got=%s want=%s",
		bal, bridgeAmt)

	require.NoError(t, helpers.DeleteJSONState(l2ToL1TokenStateName(srcChain)))
}

func l2ToL1TokenWtx(state l2ToL1TokenState) helpers.WithdrawalTx {
	n, _ := new(big.Int).SetString(state.WithdrawalNonce, 10)
	v, _ := new(big.Int).SetString(state.WithdrawalValue, 10)
	g, _ := new(big.Int).SetString(state.WithdrawalGasLimit, 10)
	return helpers.WithdrawalTx{
		Nonce: n, Sender: state.WithdrawalSender, Target: state.WithdrawalTarget,
		Value: v, GasLimit: g, Data: state.WithdrawalData,
	}
}

func proveL2ToL1TokenWithdrawal(
	t *testing.T,
	portalAddr common.Address,
	srcChain *rollup.Rollup,
	state l2ToL1TokenState,
	wtx helpers.WithdrawalTx,
) error {
	ctx := t.Context()

	var dgfAddr common.Address
	if err := helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"disputeGameFactory", &dgfAddr); err != nil {
		return fmt.Errorf("portal.disputeGameFactory: %w", err)
	}
	var gameType uint32
	if err := helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"respectedGameType", &gameType); err != nil {
		return fmt.Errorf("portal.respectedGameType: %w", err)
	}

	gameIndex, coveredBlock, err := helpers.FindCoveringDisputeGame(ctx, TestL1.RPCURL(),
		dgfAddr, helpers.MinimalDisputeGameABI, helpers.MinimalDisputeGameABI,
		gameType, state.L2Block, 30*time.Second, helpers.DefaultPollAttempts*6,
	)
	if err != nil {
		return fmt.Errorf("find dispute game: %w", err)
	}
	logger.Info("[L2->L1 Token %s] game=%d covers L2 block %d", srcChain.Name(), gameIndex, coveredBlock)

	outProof, storageProof, err := helpers.BuildWithdrawalProof(ctx, srcChain.RPCURL(),
		state.WithdrawalHash, coveredBlock)
	if err != nil {
		return fmt.Errorf("build proof: %w", err)
	}

	proveData, err := helpers.PackProveWithdrawal(ComposePortalABI, wtx, gameIndex, outProof, storageProof)
	if err != nil {
		return fmt.Errorf("pack prove: %w", err)
	}
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, proveData)
	return err
}
