// Receiver-callback edge cases for the L2 bridge. Companion to
// bridge_invalid_args_test.go — but where that file uses pure eth_call
// simulations against bad-input shapes, this file deploys a hostile receiver
// contract on the destination rollup and pushes a real XT through the
// sidecar to verify the abort contract end-to-end.
//
// Scenario covered:
//   1. Receiver is a contract that refuses ETH (no payable receive/fallback).
//      The bridge's receiveETH does `receiver.call{value: amount}("")`; if
//      that returns false, it reverts with TransferFailed. The sidecar
//      simulates the dst tx before committing, sees the revert, and aborts
//      the whole XT atomically. Test asserts:
//        - sidecar decision: committed = false
//        - source ETH balance is unchanged across the aborted XT (neither
//          leg lands on chain, no gas burned)
//
// Sidecar-only by design: in RPC mode there is no analogous atomic-abort
// decision endpoint exposed to the test (we explored this in the
// replay_attack_test gating). Skip cleanly on RPC envs.
package test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
)

// Minimal reverting-receiver bytecode.
//
// Disassembly:
//
//	constructor (11 bytes):
//	  6005      PUSH1 5         ; runtime length
//	  80        DUP1            ; [5, 5]
//	  600b      PUSH1 11        ; runtime offset in initcode
//	  6000      PUSH1 0         ; memory dest
//	  39        CODECOPY        ; memory[0..5] = initcode[11..16]
//	  6000      PUSH1 0
//	  f3        RETURN          ; return memory[0..5]
//	runtime (5 bytes):
//	  6000      PUSH1 0
//	  6000      PUSH1 0
//	  fd        REVERT          ; revert with empty data
//
// Any call (with or without value, calldata) executes the runtime → REVERT.
// A bare ETH transfer (receiver.call{value:x}("")) returns false, which is
// what the bridge's receiveETH translates into TransferFailed.
const revertingReceiverInitCode = "0x600580600b6000396000f360006000fd"

func deployRevertingReceiver(t *testing.T, ctx context.Context, deployer *accounts.Account) common.Address {
	t.Helper()
	nonce, err := deployer.GetNonce(ctx)
	require.NoError(t, err)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   deployer.GetRollup().ChainID(),
		Nonce:     nonce,
		To:        nil, // contract creation
		Gas:       200_000,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Value:     big.NewInt(0),
		Data:      common.FromHex(revertingReceiverInitCode),
	})
	signed, err := types.SignTx(tx, types.NewLondonSigner(deployer.GetRollup().ChainID()), deployer.GetPrivateKey())
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, signed, deployer.GetRollup().RPCURL())
	require.NoError(t, err)
	_, receipt, err := transactions.GetTransactionDetails(ctx, signed.Hash(), deployer.GetRollup())
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, receipt.Status)
	addr := receipt.ContractAddress
	if addr == (common.Address{}) {
		addr = crypto.CreateAddress(deployer.GetAddress(), nonce)
	}
	return addr
}

var bridgeRecvCallbackAmount = big.NewInt(1_000_000_000_000_000) // 0.001 ETH

func TestBridge_RecvCallback_RevertingEthReceiver_AtoB(t *testing.T) {
	if TestXTMode != configs.XTSubmissionSidecar {
		t.Skip("test asserts the sidecar's atomic-abort on dst revert; RPC mode provides no equivalent decision")
	}
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	// 1. Deploy the reverting receiver on B (the destination chain).
	receiverAddr := deployRevertingReceiver(t, ctx, TestAccountB)
	logger.Info("[RECV-CALLBACK A->B] reverting receiver deployed at %s on rollup-b", receiverAddr.Hex())

	// 2. Snapshot source ETH balance — must not move across the aborted XT.
	srcBalBefore, err := TestAccountA.GetBalance(ctx)
	require.NoError(t, err)
	helpers.LogAssertOK("source has sufficient balance: %s >= bridge amount %s", srcBalBefore, bridgeRecvCallbackAmount)
	require.GreaterOrEqual(t, srcBalBefore.Cmp(bridgeRecvCallbackAmount), 0)

	// 3. Defensive: confirm TransferFailed is even declared on BridgeABI so
	//    we catch ABI drift early rather than silently failing the abort
	//    assertion for the wrong reason. (We don't directly assert the
	//    selector below — the sidecar's abort decision is the public-facing
	//    contract; the underlying revert is implementation detail.)
	expected := expectedNamedErrorSelector(t, "TransferFailed")
	helpers.LogAssertOK("expected sidecar-abort cause: receiveETH reverts with TransferFailed (selector=%s)", hexutil.Encode(expected))

	// 4. Build the XT pair: source-side bridgeEthTo and destination-side
	//    receiveETH for the same sessionId / receiver.
	sessionID := transactions.GenerateRandomSessionID()
	srcCalldata, err := helpers.PackBridgeEthTo(BridgeABI, sessionID, TestRollupB.ChainID(), receiverAddr)
	require.NoError(t, err)
	srcTx, srcBytes, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     bridgeRecvCallbackAmount,
		Gas:       3_000_000,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      srcCalldata,
	}, TestAccountA)
	require.NoError(t, err)

	dstCalldata, err := helpers.PackReceiveETH(BridgeABI,
		TestRollupA.ChainID(), TestRollupB.ChainID(),
		bridgeAddr, receiverAddr, sessionID,
	)
	require.NoError(t, err)
	dstTx, dstBytes, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       3_000_000,
		GasTipCap: helpers.GasTipCap,
		GasFeeCap: helpers.GasFeeCap,
		Data:      dstCalldata,
	}, TestAccountB)
	require.NoError(t, err)

	// 5. Submit through the sidecar. The sidecar simulates the destination
	//    leg before committing; the simulation reverts (TransferFailed) so
	//    the whole XT must come back aborted.
	instanceID, committed, err := helpers.SubmitXTWaitCommitted(ctx, map[uint64][][]byte{
		uint64(TestRollupA.ChainID().Int64()): {srcBytes},
		uint64(TestRollupB.ChainID().Int64()): {dstBytes},
	}, uint64(TestRollupA.ChainID().Int64()), 90*time.Second)
	require.NoError(t, err)

	helpers.LogAssertOK("XT sidecar decision: committed=%v (instance=%s src=%s dst=%s) — want aborted",
		committed, instanceID, srcTx.Hash().Hex(), dstTx.Hash().Hex())
	require.Falsef(t, committed,
		"sidecar must abort XT when dst receiveETH would revert (receiver=%s refuses ETH)", receiverAddr.Hex())

	// 6. Source ETH balance unchanged — neither leg landed, no gas burned on
	//    the source side either. This is the sidecar's atomic-abort
	//    guarantee in action.
	srcBalAfter, err := TestAccountA.GetBalance(ctx)
	require.NoError(t, err)
	helpers.LogAssertOK("source balance unchanged after aborted XT: got=%s want=%s", srcBalAfter, srcBalBefore)
	require.Zerof(t, srcBalAfter.Cmp(srcBalBefore),
		"source balance must not change across aborted XT: before=%s after=%s",
		srcBalBefore, srcBalAfter)
}
