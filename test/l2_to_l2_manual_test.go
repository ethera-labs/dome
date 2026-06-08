// Go port of scripts/l2-to-l2.ts — the manual two-step (non-atomic) bridge.
//
// Unlike the composed XT flow, source-side bridgeERC20To and destination-side
// receiveTokens are submitted as independent transactions. The destination
// call only succeeds once an external coordinator has copied the mailbox
// message across, so these tests are state-driven (run send first, wait, then
// run receive) and skip cleanly when no state is present.
//
// Subtests:
//   _SendERC20  — deploys MintableToken, mints, approves, calls bridgeERC20To.
//                 Saves state {tokenAddress, sessionId, sender}.
//   _Receive    — loads state, calls receiveTokens on the destination bridge.
package test

import (
	"fmt"
	"math/big"
	"testing"

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

type l2ToL2ManualState struct {
	TokenAddress common.Address `json:"tokenAddress"`
	SessionID    string         `json:"sessionId"` // big.Int as decimal string
	Sender       common.Address `json:"sender"`
	Source       string         `json:"source"`
	Dest         string         `json:"dest"`
}

func l2ToL2ManualStateName(src, dst *rollup.Rollup) string {
	return fmt.Sprintf(".l2-to-l2-manual-state-%s-%s.json", src.Name(), dst.Name())
}

func TestL2ToL2_Manual_SendERC20_AtoB(t *testing.T) {
	runL2ToL2ManualSendERC20(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func TestL2ToL2_Manual_Receive_AtoB(t *testing.T) {
	runL2ToL2ManualReceive(t, TestAccountB, TestRollupA, TestRollupB)
}

func runL2ToL2ManualSendERC20(
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

	tokenAddr, _, err := helpers.DeployMintableToken(ctx, from, "ManualToken", "MAN", 18)
	require.NoError(t, err)

	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)

	// mint
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

	// approve bridge
	approveCalldata, err := tokenABI.Pack("approve", bridgeAddr,
		new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)))
	require.NoError(t, err)
	approveTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: tokenAddr, Value: big.NewInt(0), Gas: helpers.GasApprove,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: approveCalldata,
	}, from)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, approveTx, fromChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, approveTx, fromChain)

	// Use the v1 sessionId scheme that the TS script uses.
	client, err := ethclient.DialContext(ctx, fromChain.RPCURL())
	require.NoError(t, err)
	defer client.Close()
	nonce, err := client.PendingNonceAt(ctx, from.GetAddress())
	require.NoError(t, err)
	bn, err := client.BlockNumber(ctx)
	require.NoError(t, err)
	sessionID := helpers.GenerateSessionIDV1(from.GetAddress(), uint32(nonce), bn, helpers.RandomSaltUint32())

	// bridgeERC20To — NOT atomic; coordinator must relay.
	bridgeCalldata, err := helpers.PackBridgeERC20To(BridgeABI, toChain.ChainID(),
		tokenAddr, bridgeAmt, to.GetAddress(), sessionID)
	require.NoError(t, err)
	bridgeTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: bridgeAddr, Value: big.NewInt(0), Gas: helpers.GasBridgeERC20To,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: bridgeCalldata,
	}, from)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, bridgeTx, fromChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, bridgeTx, fromChain)

	state := l2ToL2ManualState{
		TokenAddress: tokenAddr,
		SessionID:    sessionID.String(),
		Sender:       bridgeAddr, // the bridge is the mailbox sender on the source
		Source:       fromChain.Name(),
		Dest:         toChain.Name(),
	}
	require.NoError(t, helpers.SaveJSONState(l2ToL2ManualStateName(fromChain, toChain), state))
	logger.Info("[L2->L2 Manual %s->%s] send complete; sessionId=%s — waiting for coordinator relay before receive",
		fromChain.Name(), toChain.Name(), sessionID)
}

func runL2ToL2ManualReceive(
	t *testing.T,
	to *accounts.Account,
	fromChain *rollup.Rollup,
	toChain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address

	var state l2ToL2ManualState
	found, err := helpers.LoadJSONState(l2ToL2ManualStateName(fromChain, toChain), &state)
	require.NoError(t, err)
	if !found {
		t.Fatalf("no state file %s — run the corresponding _SendERC20 test first and wait for the coordinator to relay",
			l2ToL2ManualStateName(fromChain, toChain))
	}

	sessionID, ok := new(big.Int).SetString(state.SessionID, 10)
	require.True(t, ok, "decode sessionId %q", state.SessionID)

	calldata, err := helpers.PackBridgeReceiveTokens(BridgeABI,
		fromChain.ChainID(), toChain.ChainID(),
		state.Sender, to.GetAddress(), sessionID)
	require.NoError(t, err)

	tx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: bridgeAddr, Value: big.NewInt(0), Gas: helpers.GasBridgeReceive,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: calldata,
	}, to)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, tx, toChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, tx, toChain)

	// Cleanup state on success.
	require.NoError(t, helpers.DeleteJSONState(l2ToL2ManualStateName(fromChain, toChain)))
	logger.Info("[L2->L2 Manual %s->%s] receive complete", fromChain.Name(), toChain.Name())
}
