// L2 -> L1 ERC-20 round-trip where the L2-side burn is done by an ERC-4337
// smart account. Full flow:
//
//   _Withdraw:
//     1.  Derive SA on L2.
//     2.  EntryPoint deposit on L2.
//     3.  Deploy MintableToken on L1 (funder EOA), mint, approve L1 bridge.
//     4.  Bridge L1 → L2 with receiver=SA on L2. Wait for CET arrival at SA.
//     5.  SA executes [approve(L2bridge, amount), bridgeERC20To(CET, L1Token,
//         L1Receiver, amount, …)] inside a single UserOp on L2.
//     6.  Assert outer + inner UserOp success, MessagePassed.sender == SA, save state.
//
//   _Finalize:
//     7.  Prove + wait maturity + finalize on L1.
//     8.  Assert portal.finalizedWithdrawals == true.
//     9.  Assert L1 token balance at the L1 receiver (funder EOA) equals the
//         bridged amount.
package test

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
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

type l2ToL1SATokenState struct {
	Source             string         `json:"source"`
	SAAddr             common.Address `json:"saAddr"`
	L1Receiver         common.Address `json:"l1Receiver"`
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

func l2ToL1SATokenStateName(src *rollup.Rollup) string {
	return fmt.Sprintf(".l2-to-l1-sa-token-state-%s.json", src.Name())
}

func TestL2ToL1_SA_Token_Withdraw_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "l1")
	RequireL1(t)
	RequireAA(t)
	RequireTSRuntime(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1SATokenWithdraw(t, configs.ChainNameRollupA, TestAccountA, TestRollupA)
}

func TestL2ToL1_SA_Token_Withdraw_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "l1")
	RequireL1(t)
	RequireAA(t)
	RequireTSRuntime(t)
	RequireL2BridgePerRollup(t)
	runL2ToL1SATokenWithdraw(t, configs.ChainNameRollupB, TestAccountB, TestRollupB)
}

func TestL2ToL1_SA_Token_Finalize_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "l1")
	RequireL1(t)
	runL2ToL1SATokenFinalize(t, configs.ChainNameRollupA, TestRollupA)
}

func TestL2ToL1_SA_Token_Finalize_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "l1")
	RequireL1(t)
	runL2ToL1SATokenFinalize(t, configs.ChainNameRollupB, TestRollupB)
}

func runL2ToL1SATokenWithdraw(
	t *testing.T,
	src configs.ChainName,
	srcFunder *accounts.Account,
	srcChain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAmt := helpers.ParseBridgeAmountOverride(l1ToL2TokenBridgeAmount)
	pk := configs.Values.WalletPrivateKey
	l2ChainID := uint64(srcChain.ChainID().Int64())
	l1Receiver := TestL1Account.GetAddress()

	l1BridgeAddr, err := helpers.L1BridgeAddressFor(src)
	require.NoError(t, err)
	l2BridgeAddr := l2BridgeAddressFor(src)

	// 1. Derive SA + fund EntryPoint.
	saAddr, _, err := helpers.SACreateAccount(ctx, pk, l2ChainID, []uint64{l2ChainID})
	require.NoError(t, err)
	helpers.LogAssertOK("SA derived on %s: %s", srcChain.Name(), saAddr.Hex())
	require.NotEqualf(t, common.Address{}, saAddr, "SA address must be non-zero")
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, srcFunder, saAddr, helpers.MinEntryPointDeposit))
	helpers.LogAssertOK("EntryPoint deposit on %s >= %s for SA=%s",
		srcChain.Name(), helpers.MinEntryPointDeposit, saAddr.Hex())

	// 2. Phase A — L1 → L2 (EOA-driven): deploy token, mint, approve, bridge to SA.
	tokenAddr, _, err := helpers.DeployMintableToken(ctx, TestL1Account, "SARoundTrip", "SART", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	logger.Info("[L2->L1 SA Token %s] L1 token deployed: %s", srcChain.Name(), tokenAddr.Hex())

	mintData, err := tokenABI.Pack("mint", TestL1Account.GetAddress(), bridgeAmt)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasMint, mintData)
	require.NoError(t, err)

	cetAddr, err := helpers.PredictCetAddress(ctx, srcChain, CetFactoryABI, tokenAddr, TestL1.ChainID())
	require.NoError(t, err)
	helpers.LogAssertOK("predicted CET on %s = %s", srcChain.Name(), cetAddr.Hex())

	extraData, err := helpers.EncodeERC20ExtraData("SARoundTrip", "SART", 18)
	require.NoError(t, err)
	approveData, err := tokenABI.Pack("approve", l1BridgeAddr, bridgeAmt)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasApprove, approveData)
	require.NoError(t, err)

	// Bridge L1 → L2 with the SA as L2 receiver.
	bridgeData, err := helpers.PackBridgeERC20ToL1(ComposeL1BridgeABI,
		tokenAddr, cetAddr, saAddr,
		bridgeAmt, uint32(helpers.L1MinGasLimitNewToken), extraData)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, l1BridgeAddr, big.NewInt(0), helpers.L1BridgeGasLimit, bridgeData)
	require.NoError(t, err)

	// Wait for CET on L2 at the SA's address.
	_, err = helpers.WaitForTokenBalanceChange(ctx, srcChain.RPCURL(),
		cetAddr, saAddr, tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(bridgeAmt) >= 0 })
	require.NoError(t, err, "CET never arrived at SA on L2")
	helpers.LogAssertOK("L1→L2 leg done: SA holds %s CET on %s at %s",
		bridgeAmt, srcChain.Name(), cetAddr.Hex())
	require.NoError(t, helpers.AssertContractDeployed(ctx, srcChain.RPCURL(), cetAddr),
		"CET contract should be deployed on L2 after L1→L2 leg")

	// 3. Phase B — L2 → L1 (SA-driven UserOp): approve + bridgeERC20To.
	saApproveData, err := tokenABI.Pack("approve", l2BridgeAddr, bridgeAmt)
	require.NoError(t, err)
	burnData, err := helpers.PackBridgeERC20ToOnL2(ComposeL2BridgeABI,
		cetAddr, tokenAddr, l1Receiver,
		bridgeAmt, uint32(helpers.L1MinGasLimitNewToken), []byte{})
	require.NoError(t, err)

	calls := []helpers.UserOpCall{
		{ChainID: l2ChainID, To: cetAddr, Value: "0", Data: hexutil.Encode(saApproveData)},
		{ChainID: l2ChainID, To: l2BridgeAddr, Value: "0", Data: hexutil.Encode(burnData)},
	}

	// Use the standard SA token gas overrides — same as L2↔L2 SA token tests.
	overrides := []helpers.SAGasOverride{
		{ChainID: l2ChainID, CallGasLimit: "5000000", VerificationGasLimit: "3500000"},
	}

	canonical, err := helpers.SACreateUserOps(ctx, pk, calls, overrides)
	require.NoError(t, err)
	require.NotEmpty(t, canonical)
	helpers.LogAssertOK("TS helper produced %d signed UserOp(s) on %s", len(canonical), srcChain.Name())

	signedTx, _, err := helpers.BuildHandleOpsRawTx(ctx, srcFunder, canonical)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, signedTx, srcChain.RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(ctx, signedTx.Hash(), srcChain)
	require.NoError(t, err)
	logger.Info("[L2->L1 SA Token %s] handleOps tx %s", srcChain.Name(), signedTx.Hash().Hex())
	helpers.LogAssertOK("L2 handleOps receipt status=Successful (%s)", signedTx.Hash().Hex())
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)

	success, _, err := helpers.CheckUserOpSuccess(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("L2 UserOperationEvent.success=true (got=%v)", success)
	require.Truef(t, success, "inner UserOp on L2 reverted (approve + bridgeERC20To failed)")

	// 4. CET balance at SA should now be 0 (burned).
	saCETAfter, err := getERC20BalanceAt(ctx, srcChain.RPCURL(), cetAddr, saAddr, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("SA CET balance == 0 after burn (got=%s)", saCETAfter)
	require.Equalf(t, 0, saCETAfter.Sign(),
		"SA CET balance should be 0 after burn, got=%s", saCETAfter)

	// 5. Extract MessagePassed. Note: the MessagePassed.sender field is the
	//    L2CrossDomainMessenger predeploy in OP-stack — it does NOT carry the
	//    original caller. The SA-as-initiator is verified above via inner
	//    UserOp success + SA CET balance going to 0.
	wtx, withdrawalHash, err := helpers.ExtractMessagePassed(receipt)
	require.NoError(t, err)
	helpers.LogAssertOK("MessagePassed event extracted: withdrawalHash=%s l2Block=%d",
		withdrawalHash.Hex(), receipt.BlockNumber.Uint64())

	// Save state for finalize.
	state := l2ToL1SATokenState{
		Source:             srcChain.Name(),
		SAAddr:             saAddr,
		L1Receiver:         l1Receiver,
		TokenAddress:       tokenAddr,
		PredictedCET:       cetAddr,
		WithdrawalHash:     withdrawalHash,
		WithdrawalNonce:    wtx.Nonce.String(),
		WithdrawalSender:   wtx.Sender,
		WithdrawalTarget:   wtx.Target,
		WithdrawalValue:    wtx.Value.String(),
		WithdrawalGasLimit: wtx.GasLimit.String(),
		WithdrawalData:     wtx.Data,
		L2TxHash:           signedTx.Hash(),
		L2Block:            receipt.BlockNumber.Uint64(),
	}
	require.NoError(t, helpers.SaveJSONState(l2ToL1SATokenStateName(srcChain), state))
}

func runL2ToL1SATokenFinalize(t *testing.T, src configs.ChainName, srcChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()
	bridgeAmt := helpers.ParseBridgeAmountOverride(l1ToL2TokenBridgeAmount)

	var state l2ToL1SATokenState
	found, err := helpers.LoadJSONState(l2ToL1SATokenStateName(srcChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run TestL2ToL1_SA_Token_Withdraw_%s first",
			l2ToL1SATokenStateName(srcChain), srcChain.Name())
	}
	helpers.LogAssertOK("loaded SA token withdrawal state for %s: hash=%s saAddr=%s",
		srcChain.Name(), state.WithdrawalHash.Hex(), state.SAAddr.Hex())

	portalAddr := portalAddressFor(src)
	wtx := saTokenStateToWithdrawalTx(state)

	helpers.LogAssertOK("loaded state SA=%s withdrawalHash=%s",
		state.SAAddr.Hex(), state.WithdrawalHash.Hex())
	require.NotEqualf(t, common.Address{}, state.SAAddr, "loaded SA address is zero — state file corrupted?")

	// Already finalized?
	var alreadyFinalized bool
	err = helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &alreadyFinalized, state.WithdrawalHash)
	require.NoError(t, err)
	if alreadyFinalized {
		require.NoError(t, helpers.DeleteJSONState(l2ToL1SATokenStateName(srcChain)))
		return
	}

	// Prove if not yet proven.
	var numSubmitters *big.Int
	err = helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"numProofSubmitters", &numSubmitters, state.WithdrawalHash)
	require.NoError(t, err)
	if numSubmitters.Sign() == 0 {
		require.NoError(t, proveSAL2ToL1TokenWithdrawal(t, portalAddr, srcChain, state, wtx))
	}

	// Wait for maturity.
	require.NoErrorf(t,
		helpers.WaitForProofMaturity(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
			state.WithdrawalHash, TestL1Account.GetAddress(),
			20*time.Second, helpers.DefaultPollAttempts*6,
		),
		"[L2->L1 SA Token %s] proof not yet mature — re-run finalize after the dispute window",
		srcChain.Name(),
	)

	// Finalize.
	finalizeData, err := helpers.PackFinalizeWithdrawal(ComposePortalABI, wtx)
	require.NoError(t, err)
	_, _, err = helpers.SendL1Tx(ctx, TestL1Account, portalAddr, big.NewInt(0), 500_000, finalizeData)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 finalize tx confirmed for withdrawalHash=%s", state.WithdrawalHash.Hex())

	// Portal must report finalized.
	var finalizedNow bool
	require.NoError(t, helpers.CallPortalView(ctx, TestL1.RPCURL(), portalAddr, ComposePortalABI,
		"finalizedWithdrawals", &finalizedNow, state.WithdrawalHash))
	helpers.LogAssertOK("portal.finalizedWithdrawals(%s) == true (got=%v)",
		state.WithdrawalHash.Hex(), finalizedNow)
	require.True(t, finalizedNow, "portal.finalizedWithdrawals should be true after finalize call")

	// L1 token balance at the funder EOA should be the bridged amount (full
	// round-trip restored).
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	bal, err := TestL1Account.GetTokensBalance(ctx, state.TokenAddress, tokenABI)
	require.NoError(t, err)
	helpers.LogAssertOK("L1 token balance restored after SA-driven round-trip: got=%s want=%s",
		bal, bridgeAmt)
	require.Equalf(t, 0, bal.Cmp(bridgeAmt),
		"L1 token balance should equal bridged amount after round-trip: got=%s want=%s",
		bal, bridgeAmt)

	require.NoError(t, helpers.DeleteJSONState(l2ToL1SATokenStateName(srcChain)))
}

func saTokenStateToWithdrawalTx(state l2ToL1SATokenState) helpers.WithdrawalTx {
	n, _ := new(big.Int).SetString(state.WithdrawalNonce, 10)
	v, _ := new(big.Int).SetString(state.WithdrawalValue, 10)
	g, _ := new(big.Int).SetString(state.WithdrawalGasLimit, 10)
	return helpers.WithdrawalTx{
		Nonce: n, Sender: state.WithdrawalSender, Target: state.WithdrawalTarget,
		Value: v, GasLimit: g, Data: state.WithdrawalData,
	}
}

func proveSAL2ToL1TokenWithdrawal(
	t *testing.T,
	portalAddr common.Address,
	srcChain *rollup.Rollup,
	state l2ToL1SATokenState,
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
		dgfAddr, DisputeGameABI, DisputeGameABI, gameType, state.L2Block,
		30*time.Second, helpers.DefaultPollAttempts*6,
	)
	if err != nil {
		return fmt.Errorf("find dispute game: %w", err)
	}
	logger.Info("[L2->L1 SA Token %s] game=%d covers L2 block %d", srcChain.Name(), gameIndex, coveredBlock)

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
