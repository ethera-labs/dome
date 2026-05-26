package helpers

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
)

const (
	defaultDecisionTimeout = 60 * time.Second
	// LabelSendTokens matches the on-chain label the source bridge writes into
	// the UniversalBridgeMailbox; the destination side must read with the same
	// label.
	LabelSendTokens = "SEND_TOKENS"
)

// MessageHeader mirrors UniversalBridgeMailbox.MessageHeader. Field order and
// types must match the on-chain tuple so abi.Pack encodes the components in
// the right slots.
type MessageHeader struct {
	ChainSrc  *big.Int       `abi:"chainSrc"`
	ChainDest *big.Int       `abi:"chainDest"`
	Sender    common.Address `abi:"sender"`
	Receiver  common.Address `abi:"receiver"`
	SessionId *big.Int       `abi:"sessionId"`
	Label     string         `abi:"label"`
}

// PackBridgeERC20To encodes a call to ComposeL2ToL2Bridge.bridgeERC20To.
func PackBridgeERC20To(
	bridgeABI abi.ABI,
	chainDest *big.Int,
	tokenSrc common.Address,
	amount *big.Int,
	receiver common.Address,
	sessionID *big.Int,
) ([]byte, error) {
	return bridgeABI.Pack("bridgeERC20To", chainDest, tokenSrc, amount, receiver, sessionID)
}

// PackBridgeReceiveTokens encodes a call to ComposeL2ToL2Bridge.receiveTokens.
// `sourceBridge` is the source-chain bridge contract address; the mailbox keys
// messages by (chainSrc, chainDest, sender, receiver, sessionId, label) where
// `sender` is the *source bridge* address (= msg.sender of writeMessage), not
// the end user.
func PackBridgeReceiveTokens(
	bridgeABI abi.ABI,
	chainSrc *big.Int,
	chainDest *big.Int,
	sourceBridge common.Address,
	receiver common.Address,
	sessionID *big.Int,
) ([]byte, error) {
	return bridgeABI.Pack("receiveTokens", MessageHeader{
		ChainSrc:  chainSrc,
		ChainDest: chainDest,
		Sender:    sourceBridge,
		Receiver:  receiver,
		SessionId: sessionID,
		Label:     LabelSendTokens,
	})
}

// signedBridgePair builds and signs the source-side `bridgeERC20To` tx on
// `from`'s chain and the destination-side `receiveTokens` tx on `to`'s chain.
// If `fromNonce`/`toNonce` are nil the auto-nonce path is used.
func signedBridgePair(
	ctx context.Context,
	from, to *accounts.Account,
	fromNonce, toNonce *uint64,
	amount *big.Int,
	bridgeABI abi.ABI,
) (*types.Transaction, []byte, *types.Transaction, []byte, error) {
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	tokenAddr := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	sessionID := transactions.GenerateRandomSessionID()

	srcCalldata, err := PackBridgeERC20To(
		bridgeABI,
		to.GetRollup().ChainID(),
		tokenAddr,
		amount,
		to.GetAddress(),
		sessionID,
	)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("pack bridgeERC20To: %w", err)
	}

	srcTx, srcBytes, err := createBridgeTx(ctx, bridgeAddr, srcCalldata, GasBridgeERC20To, from, fromNonce)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("sign source tx: %w", err)
	}

	dstCalldata, err := PackBridgeReceiveTokens(
		bridgeABI,
		from.GetRollup().ChainID(),
		to.GetRollup().ChainID(),
		bridgeAddr,
		to.GetAddress(),
		sessionID,
	)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("pack receiveTokens: %w", err)
	}

	dstTx, dstBytes, err := createBridgeTx(ctx, bridgeAddr, dstCalldata, GasBridgeReceive, to, toNonce)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("sign destination tx: %w", err)
	}

	return srcTx, srcBytes, dstTx, dstBytes, nil
}

func createBridgeTx(
	ctx context.Context,
	to common.Address,
	calldata []byte,
	gas uint64,
	ac *accounts.Account,
	nonce *uint64,
) (*types.Transaction, []byte, error) {
	details := transactions.TransactionDetails{
		To:        to,
		Value:     big.NewInt(0),
		Gas:       gas,
		GasTipCap: GasTipCap,
		GasFeeCap: GasFeeCap,
		Data:      calldata,
	}
	if nonce == nil {
		return transactions.CreateTransaction(ctx, details, ac)
	}
	return transactions.CreateTransactionWithNonce(ctx, details, ac, *nonce)
}

// SendBridgeTx sends a bridge XT from `from` to `to` via the sidecar and
// asserts it commits. Returns the source-chain and destination-chain
// transactions for downstream receipt verification.
func SendBridgeTx(
	t *testing.T,
	from, to *accounts.Account,
	amount *big.Int,
	bridgeABI abi.ABI,
) (*types.Transaction, *types.Transaction, error) {
	return sendBridgeTx(t, from, to, nil, nil, amount, bridgeABI)
}

// SendBridgeTxWithNonce is `SendBridgeTx` with explicit nonces, used by stress
// tests that pre-compute nonce ranges to avoid the pending-pool race.
func SendBridgeTxWithNonce(
	t *testing.T,
	from *accounts.Account,
	fromNonce uint64,
	to *accounts.Account,
	toNonce uint64,
	amount *big.Int,
	bridgeABI abi.ABI,
) (*types.Transaction, *types.Transaction, error) {
	return sendBridgeTx(t, from, to, &fromNonce, &toNonce, amount, bridgeABI)
}

func sendBridgeTx(
	t *testing.T,
	from, to *accounts.Account,
	fromNonce, toNonce *uint64,
	amount *big.Int,
	bridgeABI abi.ABI,
) (*types.Transaction, *types.Transaction, error) {
	srcTx, srcBytes, dstTx, dstBytes, err := signedBridgePair(t.Context(), from, to, fromNonce, toNonce, amount, bridgeABI)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		from.GetRollup().ChainID().String(): {hexutil.Encode(srcBytes)},
		to.GetRollup().ChainID().String():   {hexutil.Encode(dstBytes)},
	}
	xtResp, err := transactions.SubmitXT(t.Context(), configs.Values.L2.SidecarURL, xtTxs)
	require.NoError(t, err)

	committed, err := transactions.WaitForDecision(t.Context(), configs.Values.L2.SidecarURL, xtResp.InstanceID, defaultDecisionTimeout)
	require.NoError(t, err)
	require.True(t, committed, "bridge XT should be committed")

	logger.Info("Bridge XT committed: %s (src: %s, dst: %s)", xtResp.InstanceID, srcTx.Hash(), dstTx.Hash())
	return srcTx, dstTx, nil
}

// SubmitXTAndWait submits raw signed transactions as an XT and waits for the
// sidecar's commit/abort decision.
func SubmitXTAndWait(ctx context.Context, xtTxs map[string][]string, timeout time.Duration) (string, bool, error) {
	xtResp, err := transactions.SubmitXT(ctx, configs.Values.L2.SidecarURL, xtTxs)
	if err != nil {
		return "", false, fmt.Errorf("submit XT: %w", err)
	}
	committed, err := transactions.WaitForDecision(ctx, configs.Values.L2.SidecarURL, xtResp.InstanceID, timeout)
	if err != nil {
		return xtResp.InstanceID, false, fmt.Errorf("wait for decision: %w", err)
	}
	return xtResp.InstanceID, committed, nil
}
