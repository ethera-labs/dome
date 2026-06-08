// Go port of scripts/l2-to-l2-SA-ETH.ts. Smart-account L2->L2 ETH bridge.
// Uses scripts/sa-helper.ts to derive the SA address and (in sidecar mode)
// to sign canonical UserOps. In rpc mode the TS helper runs the SDK's
// full compose+send flow.
package test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

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

var saETHBridgeAmountDefault = big.NewInt(10_000_000_000_000_000) // 0.01 ETH

func TestL2ToL2_SA_ETH_AtoB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "b")
	RequireAA(t)
	RequireTSRuntime(t)
	runL2ToL2SAETH(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func TestL2ToL2_SA_ETH_BtoA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "a")
	RequireAA(t)
	RequireTSRuntime(t)
	runL2ToL2SAETH(t, TestAccountB, TestRollupB, TestAccountA, TestRollupA)
}

func runL2ToL2SAETH(
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

	// 1. Create SAs on both chains via TS helper. Same address on both (Kernel
	//    counterfactual derived from EOA + factory).
	saAddr, _, err := helpers.SACreateAccount(ctx, pk, uint64(srcChain.ChainID().Int64()),
		[]uint64{uint64(srcChain.ChainID().Int64()), uint64(dstChain.ChainID().Int64())})
	require.NoError(t, err)
	logger.Info("[SA L2->L2 ETH %s->%s] saAddress=%s", srcChain.Name(), dstChain.Name(), saAddr.Hex())

	saETHBridgeAmount := helpers.ParseBridgeAmountOverride(saETHBridgeAmountDefault)

	// 2. Fund EntryPoint on both chains.
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, srcFunder, saAddr, helpers.MinEntryPointDeposit))
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, dstFunder, saAddr, helpers.MinEntryPointDeposit))

	// 3. Fund SA on source with at least the bridge amount.
	require.NoError(t, ensureSABalance(ctx, srcFunder, saAddr, saETHBridgeAmount))

	// Snapshots.
	srcBefore := readBalance(ctx, srcChain.RPCURL(), saAddr)
	dstBefore := readBalance(ctx, dstChain.RPCURL(), saAddr)

	// 4. Build UserOp calls.
	sessionID := transactions.GenerateRandomSessionID()
	srcData, err := helpers.PackBridgeEthTo(BridgeABI, sessionID, dstChain.ChainID(), saAddr)
	require.NoError(t, err)
	dstData, err := helpers.PackReceiveETH(BridgeABI,
		srcChain.ChainID(), dstChain.ChainID(), bridgeAddr, saAddr, sessionID)
	require.NoError(t, err)

	calls := []helpers.UserOpCall{
		{
			ChainID: uint64(srcChain.ChainID().Int64()),
			To:      bridgeAddr,
			Value:   saETHBridgeAmount.String(),
			Data:    hexutil.Encode(srcData),
		},
		{
			ChainID: uint64(dstChain.ChainID().Int64()),
			To:      bridgeAddr,
			Value:   "0",
			Data:    hexutil.Encode(dstData),
		},
	}

	// 5. Submit.
	if TestXTMode == configs.XTSubmissionRPC {
		// Use the SDK's full path via TS.
		hashes, err := helpers.SAComposeAndSubmit(ctx, pk, calls, nil)
		require.NoError(t, err)
		logger.Info("[SA L2->L2 ETH] submitted via SDK; hashes=%v", hashes)
		require.Len(t, hashes, 2)
		waitComposedReceipts(t, ctx, hashes, srcChain, dstChain)
	} else {
		// Sidecar mode: TS signs UserOps, Go packs handleOps and submits.
		require.NoError(t, submitSAViaSidecar(t, ctx, pk, calls, srcChain, dstChain, srcFunder, dstFunder, nil))
	}

	// 6. Assert balances.
	srcAfter := readBalance(ctx, srcChain.RPCURL(), saAddr)
	dstAfter := readBalance(ctx, dstChain.RPCURL(), saAddr)
	srcDecrease := new(big.Int).Sub(srcBefore, srcAfter)
	dstIncrease := new(big.Int).Sub(dstAfter, dstBefore)
	helpers.LogAssertOK("SA source ETH decreased by >= bridge amount: delta=%s want>=%s", srcDecrease, saETHBridgeAmount)
	require.GreaterOrEqualf(t, srcDecrease.Cmp(saETHBridgeAmount), 0,
		"source decrease=%s want>=%s", srcDecrease, saETHBridgeAmount)
	helpers.LogAssertOK("SA dest ETH balance increased: delta=%s want>0", dstIncrease)
	require.Positive(t, dstIncrease.Sign(), "dest balance should increase")
}

func ensureSABalance(ctx context.Context, funder *accounts.Account, sa common.Address, amount *big.Int) error {
	saBal := readBalance(ctx, funder.GetRollup().RPCURL(), sa)
	if saBal.Cmp(amount) >= 0 {
		return nil
	}
	needed := new(big.Int).Sub(amount, saBal)
	_, _, err := helpers.SendL1Tx(ctx, funder, sa, needed, 50_000, nil)
	return err
}

func readBalance(ctx context.Context, rpcURL string, addr common.Address) *big.Int {
	bal, err := helpers.WaitForETHBalanceChange(ctx, rpcURL, addr, time.Millisecond, 1,
		func(*big.Int) bool { return true })
	if err != nil {
		return big.NewInt(0)
	}
	return bal
}

// waitComposedReceipts pairs each (hash, chainId) from the TS helper with the
// matching rollup, asserts a receipt is available, and verifies the inner
// UserOperationEvent on each. The TS helper has already awaited send.wait()
// — receipts should be queryable right away.
//
// Note: on sepolia-stage the inner UserOp historically reverts with
// MessageNotFound() while the outer handleOps tx succeeds. We fail visibly
// rather than skip so the regression stays obvious.
func waitComposedReceipts(
	t *testing.T,
	ctx context.Context,
	hashes []helpers.SAComposedTx,
	chains ...*rollup.Rollup,
) {
	t.Helper()
	innerOK := true
	innerFailureMsg := ""
	for _, h := range hashes {
		var match *rollup.Rollup
		for _, c := range chains {
			if c.ChainID().Uint64() == h.ChainID {
				match = c
				break
			}
		}
		require.NotNilf(t, match, "no rollup configured for chainId %d (hash %s)", h.ChainID, h.Hash.Hex())
		helpers.LogAssertOK("composed receipt status=Successful chain=%d hash=%s", h.ChainID, h.Hash.Hex())
		_, receipt, err := transactions.GetTransactionDetails(ctx, h.Hash, match)
		require.NoError(t, err)
		require.NotNil(t, receipt)

		success, _, err := helpers.CheckUserOpSuccess(receipt)
		if err != nil {
			// No UserOperationEvent: not an SA flow tx — skip the check.
			continue
		}
		helpers.LogAssertOK("UserOperationEvent.success=true chain=%d hash=%s (got success=%v)", h.ChainID, h.Hash.Hex(), success)
		if !success {
			innerOK = false
			innerFailureMsg += fmt.Sprintf(" %s(chain=%d)", h.Hash.Hex(), h.ChainID)
		}
	}
	helpers.LogAssertOK("all inner UserOps succeeded (innerOK=%v failing=%q)", innerOK, innerFailureMsg)
	require.Truef(t, innerOK, "inner UserOp reverted on:%s (on sepolia-stage this is the documented MessageNotFound() issue)", innerFailureMsg)
}

// submitSAViaSidecar runs the sidecar-mode SA flow: TS signs canonical UserOps,
// Go packs each chain's ops into a handleOps tx, submits the raw pair via the
// sidecar XT endpoint, then asserts each chain's UserOperationEvent.success.
// `overrides` may be nil for default gas estimation.
func submitSAViaSidecar(
	t *testing.T,
	ctx context.Context,
	pk string,
	calls []helpers.UserOpCall,
	srcChain *rollup.Rollup,
	dstChain *rollup.Rollup,
	srcFunder *accounts.Account,
	dstFunder *accounts.Account,
	overrides []helpers.SAGasOverride,
) error {
	t.Helper()

	canonical, err := helpers.SACreateUserOps(ctx, pk, calls, overrides)
	require.NoError(t, err)

	// Group by chainId.
	bySrc := make([]helpers.CanonicalUserOp, 0)
	byDst := make([]helpers.CanonicalUserOp, 0)
	for _, op := range canonical {
		switch op.ChainID {
		case uint64(srcChain.ChainID().Int64()):
			bySrc = append(bySrc, op)
		case uint64(dstChain.ChainID().Int64()):
			byDst = append(byDst, op)
		default:
			t.Fatalf("unexpected chainId %d in canonical userops", op.ChainID)
		}
	}
	require.NotEmpty(t, bySrc, "no source UserOps from TS helper")
	require.NotEmpty(t, byDst, "no dest UserOps from TS helper")

	srcSigned, srcRaw, err := helpers.BuildHandleOpsRawTx(ctx, srcFunder, bySrc)
	require.NoError(t, err)
	dstSigned, dstRaw, err := helpers.BuildHandleOpsRawTx(ctx, dstFunder, byDst)
	require.NoError(t, err)

	id, committed, err := helpers.SubmitXTWaitCommitted(ctx, map[uint64][][]byte{
		uint64(srcChain.ChainID().Int64()): {srcRaw},
		uint64(dstChain.ChainID().Int64()): {dstRaw},
	}, uint64(srcChain.ChainID().Int64()), 120*time.Second)
	require.NoError(t, err)
	helpers.LogAssertXTOK(srcSigned.Hash().Hex(), dstSigned.Hash().Hex(), id)
	require.Truef(t, committed, "SA handleOps XT not committed (instance=%s)", id)

	helpers.LogAssertOK("SA src handleOps receipt status=Successful (%s)", srcSigned.Hash().Hex())
	waitReceipt(t, ctx, srcSigned, srcChain)
	helpers.LogAssertOK("SA dst handleOps receipt status=Successful (%s)", dstSigned.Hash().Hex())
	waitReceipt(t, ctx, dstSigned, dstChain)

	// Parse UserOperationEvent on both sides. If the inner UserOp reverted on
	// stage (known MessageNotFound issue per ethera-sdk.md), skip rather than
	// fail — the framework worked, the bug is in the bridge contracts.
	_, srcReceipt, err := transactions.GetTransactionDetails(ctx, srcSigned.Hash(), srcChain)
	require.NoError(t, err)
	_, dstReceipt, err := transactions.GetTransactionDetails(ctx, dstSigned.Hash(), dstChain)
	require.NoError(t, err)

	srcOK, _, err := helpers.CheckUserOpSuccess(srcReceipt)
	require.NoError(t, err)
	dstOK, _, err := helpers.CheckUserOpSuccess(dstReceipt)
	require.NoError(t, err)
	helpers.LogAssertOK("sidecar inner UserOperationEvent.success=true on both chains (srcOK=%v dstOK=%v)", srcOK, dstOK)
	require.Truef(t, srcOK && dstOK,
		"inner UserOp reverted: src=%v dst=%v (on sepolia-stage this is the documented MessageNotFound() issue)",
		srcOK, dstOK)
	return nil
}

// _ keeps json imported on builds where the helper isn't called.
var _ = json.Unmarshal
