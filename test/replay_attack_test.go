// Replay-attack and idempotency test for composed XTs.
//
// Bridges ETH from one rollup to the other (XT_1), waits for commit, then
// re-submits the SAME (chainSrc, chainDest, bridge, receiver, sessionId,
// "SEND_ETH") under fresh nonces (XT_2). The mailbox's destination-side
// `readMessage` checks `consumedKeys[key]` and reverts with
// `MessageAlreadyConsumed` when the key was already consumed by XT_1's
// receiveETH, so the sidecar must abort XT_2 atomically. Asserts:
//
//   - XT_1 commits and balances move as expected (sanity)
//   - eth_call simulation of XT_2's destination-side `receiveETH` reverts
//     with the mailbox's MessageAlreadyConsumed() 4-byte selector
//   - XT_2 sidecar decision == aborted (committed=false)
//   - source + destination ETH balances are unchanged across XT_2 (the sidecar
//     refuses to include the txs on-chain, so neither account pays gas)
package test

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

var xtReplayAmount = big.NewInt(5_000_000_000_000_000) // 0.005 ETH

// replayTestRPCSkipReason: the central invariance this test asserts —
// committed=false plus zero side-effects across XT_2 — is the sidecar's
// atomic-abort contract. RPC-mode sequencers don't expose a "committed/aborted"
// decision, and there's no guarantee both legs are dropped together when the
// destination would revert. The contract-level layer (mailbox's
// MessageAlreadyConsumed) is already exercised by the eth_call simulation that
// every receiveETH call goes through, so skipping on RPC mode doesn't lose
// coverage of the actual replay-protection contract.
const replayTestRPCSkipReason = "replay-attack test asserts the sidecar's atomic-abort guarantee; RPC mode provides no equivalent — destination-side MessageAlreadyConsumed revert is exercised by the standard L2↔L2 tests"

func TestXTReplay_ETH_AtoB(t *testing.T) {
	if TestXTMode != configs.XTSubmissionSidecar {
		t.Skip(replayTestRPCSkipReason)
	}
	helpers.ApplyDirectionFilter(t, "a", "b")
	runXTReplayETH(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func TestXTReplay_ETH_BtoA(t *testing.T) {
	if TestXTMode != configs.XTSubmissionSidecar {
		t.Skip(replayTestRPCSkipReason)
	}
	helpers.ApplyDirectionFilter(t, "b", "a")
	runXTReplayETH(t, TestAccountB, TestRollupB, TestAccountA, TestRollupA)
}

func runXTReplayETH(
	t *testing.T,
	from *accounts.Account,
	fromChain *rollup.Rollup,
	to *accounts.Account,
	toChain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	amount := helpers.ParseBridgeAmountOverride(xtReplayAmount)

	srcBalBefore, err := from.GetBalance(ctx)
	require.NoError(t, err)
	dstBalBefore, err := to.GetBalance(ctx)
	require.NoError(t, err)

	helpers.LogAssertOK("source has sufficient balance: %s >= bridge amount %s", srcBalBefore, amount)
	require.GreaterOrEqualf(t, srcBalBefore.Cmp(amount), 0,
		"source balance %s < bridge amount %s", srcBalBefore, amount)

	// Use a fresh, deterministic sessionId so XT_1 and XT_2 collide on the
	// mailbox key (chainSrc, chainDest, bridgeAddr, receiver, sessionId, label).
	sessionID := transactions.GenerateRandomSessionID()
	logger.Info("[XT replay %s->%s] sessionId=%s amount=%s wei",
		fromChain.Name(), toChain.Name(), sessionID, amount)

	// ---- XT_1: build, submit, commit -------------------------------------
	srcTx1, srcBytes1, dstTx1, dstBytes1 := buildXTReplayPair(t, ctx, from, fromChain, to, toChain, bridgeAddr, sessionID, amount)

	instance1, committed1, err := helpers.SubmitXTWaitCommitted(ctx, map[uint64][][]byte{
		uint64(fromChain.ChainID().Int64()): {srcBytes1},
		uint64(toChain.ChainID().Int64()):   {dstBytes1},
	}, uint64(fromChain.ChainID().Int64()), 90*time.Second)
	require.NoError(t, err)
	helpers.LogAssertXTOK(srcTx1.Hash().Hex(), dstTx1.Hash().Hex(), instance1)
	require.True(t, committed1, "XT_1 should commit")

	helpers.LogAssertOK("XT_1 source tx receipt status=Successful (%s)", srcTx1.Hash().Hex())
	waitReceipt(t, ctx, srcTx1, fromChain)
	helpers.LogAssertOK("XT_1 dest tx receipt status=Successful (%s)", dstTx1.Hash().Hex())
	waitReceipt(t, ctx, dstTx1, toChain)

	// Sanity: source dropped by >= amount, dest rose by >0 — full balance
	// assertions live in TestL2ToL2_ETH_*; here we only need the side-effects
	// for the replay invariance check below.
	srcBalAfter1, err := from.GetBalance(ctx)
	require.NoError(t, err)
	dstBalAfter1, err := to.GetBalance(ctx)
	require.NoError(t, err)
	helpers.LogAssertOK("XT_1 moved value: src delta=%s (want>=%s), dst delta=%s (want>0)",
		new(big.Int).Sub(srcBalBefore, srcBalAfter1), amount,
		new(big.Int).Sub(dstBalAfter1, dstBalBefore))
	require.GreaterOrEqualf(t,
		new(big.Int).Sub(srcBalBefore, srcBalAfter1).Cmp(amount), 0,
		"XT_1 should have moved >= amount off source: before=%s after=%s amount=%s",
		srcBalBefore, srcBalAfter1, amount)
	require.Positivef(t,
		new(big.Int).Sub(dstBalAfter1, dstBalBefore).Sign(),
		"XT_1 should have moved value to dest: before=%s after=%s",
		dstBalBefore, dstBalAfter1)

	// ---- XT_2 simulation: assert dest-side receiveETH would revert -------
	// MessageAlreadyConsumed() selector lives in the mailbox ABI; the bridge
	// bubbles it up. Pre-flight via eth_call gives a deterministic, decoded
	// revert assertion BEFORE we ever submit XT_2.
	dstCalldata2, err := helpers.PackReceiveETH(BridgeABI,
		fromChain.ChainID(), toChain.ChainID(),
		bridgeAddr, to.GetAddress(), sessionID,
	)
	require.NoError(t, err)

	expectedSelector := MailboxABI.Errors["MessageAlreadyConsumed"].ID.Bytes()[:4]
	helpers.LogAssertOK("XT_2 dst receiveETH eth_call reverts with MessageAlreadyConsumed (selector=0x%x) — pre-submit simulation",
		expectedSelector)
	gotSelector, callErr := simulateRevertSelector(ctx, toChain.RPCURL(), to.GetAddress(), bridgeAddr, dstCalldata2)
	require.NoErrorf(t, callErr, "simulation should return a revert selector, not a transport error")
	require.Equalf(t, hexutil.Encode(expectedSelector), hexutil.Encode(gotSelector),
		"dst receiveETH should revert with MessageAlreadyConsumed; got selector=0x%x", gotSelector)

	// ---- XT_2: build with new nonces, submit, expect abort ---------------
	srcTx2, srcBytes2, dstTx2, dstBytes2 := buildXTReplayPair(t, ctx, from, fromChain, to, toChain, bridgeAddr, sessionID, amount)
	// Sanity: ensure XT_2 actually has new tx hashes (i.e. new nonces).
	require.NotEqualf(t, srcTx1.Hash(), srcTx2.Hash(),
		"XT_2 source tx must be distinct from XT_1 (auto-nonce should advance): both=%s",
		srcTx1.Hash().Hex())
	require.NotEqualf(t, dstTx1.Hash(), dstTx2.Hash(),
		"XT_2 dest tx must be distinct from XT_1 (auto-nonce should advance): both=%s",
		dstTx1.Hash().Hex())

	instance2, committed2, err := helpers.SubmitXTWaitCommitted(ctx, map[uint64][][]byte{
		uint64(fromChain.ChainID().Int64()): {srcBytes2},
		uint64(toChain.ChainID().Int64()):   {dstBytes2},
	}, uint64(fromChain.ChainID().Int64()), 90*time.Second)
	require.NoError(t, err)
	helpers.LogAssertOK("XT_2 sidecar decision: committed=%v (instance=%s) — want aborted",
		committed2, instance2)
	require.Falsef(t, committed2,
		"XT_2 should be aborted by sidecar — sessionId %s already consumed by XT_1", sessionID)

	// ---- post-XT_2 invariance: balances unchanged ------------------------
	// The sidecar atomically aborts before either tx lands on-chain, so neither
	// account paid gas. Compare strictly against the post-XT_1 snapshot.
	srcBalAfter2, err := from.GetBalance(ctx)
	require.NoError(t, err)
	dstBalAfter2, err := to.GetBalance(ctx)
	require.NoError(t, err)

	helpers.LogAssertOK("XT_2 source balance unchanged after abort: got=%s want=%s",
		srcBalAfter2, srcBalAfter1)
	require.Zerof(t, srcBalAfter2.Cmp(srcBalAfter1),
		"source balance must not change across aborted XT_2: post-XT_1=%s post-XT_2=%s",
		srcBalAfter1, srcBalAfter2)

	helpers.LogAssertOK("XT_2 dest balance unchanged after abort: got=%s want=%s",
		dstBalAfter2, dstBalAfter1)
	require.Zerof(t, dstBalAfter2.Cmp(dstBalAfter1),
		"dest balance must not change across aborted XT_2: post-XT_1=%s post-XT_2=%s",
		dstBalAfter1, dstBalAfter2)
}

// buildXTReplayPair signs a fresh (source, dest) tx pair for an ETH bridge XT
// at the given sessionId. Auto-nonce picks the next pending nonce for each
// account, so calling this twice in a row yields two pairs with adjacent
// nonces but identical calldata.
func buildXTReplayPair(
	t *testing.T,
	ctx context.Context,
	from *accounts.Account,
	fromChain *rollup.Rollup,
	to *accounts.Account,
	toChain *rollup.Rollup,
	bridgeAddr common.Address,
	sessionID *big.Int,
	amount *big.Int,
) (srcTx *typesTx, srcBytes []byte, dstTx *typesTx, dstBytes []byte) {
	t.Helper()

	srcCalldata, err := helpers.PackBridgeEthTo(BridgeABI, sessionID, toChain.ChainID(), to.GetAddress())
	require.NoError(t, err)
	srcTx, srcBytes, err = transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     amount,
		Gas:       3_000_000,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      srcCalldata,
	}, from)
	require.NoError(t, err)

	dstCalldata, err := helpers.PackReceiveETH(BridgeABI,
		fromChain.ChainID(), toChain.ChainID(),
		bridgeAddr, to.GetAddress(), sessionID,
	)
	require.NoError(t, err)
	dstTx, dstBytes, err = transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       3_000_000,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      dstCalldata,
	}, to)
	require.NoError(t, err)
	return srcTx, srcBytes, dstTx, dstBytes
}

// simulateRevertSelector calls `to` with `data` as `from` via eth_call and
// returns the 4-byte custom-error selector that the call reverted with.
// Returns a transport error only on connection issues — a successful (non-
// reverting) call is itself an error condition for this helper because the
// caller expected a revert.
func simulateRevertSelector(
	ctx context.Context,
	rpcURL string,
	from, to common.Address,
	data []byte,
) ([]byte, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	if _, err := client.CallContract(ctx, ethereum.CallMsg{
		From: from,
		To:   &to,
		Data: data,
	}, nil); err == nil {
		return nil, errors.New("eth_call returned successfully — expected a revert")
	} else {
		return extractRevertSelector(err)
	}
}

func extractRevertSelector(err error) ([]byte, error) {

	// go-ethereum surfaces revert data via the rpc.DataError interface.
	var de rpc.DataError
	if errors.As(err, &de) {
		raw := de.ErrorData()
		// ErrorData() is typed as `any` and is usually a hex string ("0x...").
		switch v := raw.(type) {
		case string:
			decoded, decErr := hexutil.Decode(v)
			if decErr != nil {
				return nil, decErr
			}
			return takeSelector(decoded), nil
		case json.RawMessage:
			var s string
			if uerr := json.Unmarshal(v, &s); uerr == nil {
				decoded, decErr := hexutil.Decode(s)
				if decErr != nil {
					return nil, decErr
				}
				return takeSelector(decoded), nil
			}
		}
	}
	// Some RPCs flatten the revert reason into the error message. Last-resort
	// parse: "execution reverted: 0x<selector>...".
	if msg := err.Error(); strings.Contains(msg, "0x") {
		idx := strings.Index(msg, "0x")
		hexStr := msg[idx:]
		end := strings.IndexAny(hexStr, " \t\n\"")
		if end > 0 {
			hexStr = hexStr[:end]
		}
		if decoded, decErr := hexutil.Decode(hexStr); decErr == nil && len(decoded) >= 4 {
			return takeSelector(decoded), nil
		}
	}
	return nil, err
}

func takeSelector(b []byte) []byte {
	if len(b) < 4 {
		return b
	}
	return b[:4]
}

// typesTx is an alias so the file doesn't have to import the same package
// twice with different names — keeps the helper signatures tidy.
type typesTx = types.Transaction
