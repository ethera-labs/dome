package helpers

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

const (
	defaultDecisionTimeout = 3 * time.Minute
	mailboxLabelSendTokens = "SEND_TOKENS"

	BridgeSendGasLimit    = 900000
	BridgeReceiveGasLimit = 3000000
)

const cetFactoryABIJSON = `[
	{"inputs":[],"name":"cetFactory","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"address","name":"remoteToken","type":"address"},{"internalType":"uint256","name":"remoteChainId","type":"uint256"}],"name":"predictAddress","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"}
]`

type BridgeMessageHeader struct {
	ChainSrc  *big.Int
	ChainDest *big.Int
	Sender    common.Address
	Receiver  common.Address
	SessionID *big.Int `abi:"sessionId"`
	Label     string
}

func PackBridgeERC20To(
	bridgeABI abi.ABI,
	chainDest *big.Int,
	tokenAddress common.Address,
	amount *big.Int,
	receiver common.Address,
	sessionID *big.Int,
) ([]byte, error) {
	return bridgeABI.Pack("bridgeERC20To", chainDest, tokenAddress, amount, receiver, sessionID)
}

func PackBridgeReceiveTokens(
	bridgeABI abi.ABI,
	chainSrc *big.Int,
	chainDest *big.Int,
	bridgeAddress common.Address,
	receiver common.Address,
	sessionID *big.Int,
) ([]byte, error) {
	header := BridgeMessageHeader{
		ChainSrc:  chainSrc,
		ChainDest: chainDest,
		Sender:    bridgeAddress,
		Receiver:  receiver,
		SessionID: sessionID,
		Label:     mailboxLabelSendTokens,
	}
	return bridgeABI.Pack("receiveTokens", header)
}

func PredictWrappedTokenAddress(
	ctx context.Context,
	localRollup *rollup.Rollup,
	bridgeABI abi.ABI,
	bridgeAddress common.Address,
	remoteToken common.Address,
	remoteChainID *big.Int,
) (common.Address, error) {
	client, err := ethclient.DialContext(ctx, localRollup.RPCURL())
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to connect to RPC URL %s: %w", localRollup.RPCURL(), err)
	}
	defer client.Close()

	factoryABI, err := abi.JSON(strings.NewReader(cetFactoryABIJSON))
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to parse CET factory ABI: %w", err)
	}

	factoryCall, err := bridgeABI.Pack("cetFactory")
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to pack cetFactory call: %w", err)
	}
	factoryResult, err := client.CallContract(ctx, ethereum.CallMsg{To: &bridgeAddress, Data: factoryCall}, nil)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to call cetFactory: %w", err)
	}
	factoryValues, err := bridgeABI.Unpack("cetFactory", factoryResult)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to unpack cetFactory result: %w", err)
	}
	factoryAddress, ok := factoryValues[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected cetFactory result type %T", factoryValues[0])
	}

	predictCall, err := factoryABI.Pack("predictAddress", remoteToken, remoteChainID)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to pack predictAddress call: %w", err)
	}
	predictResult, err := client.CallContract(ctx, ethereum.CallMsg{To: &factoryAddress, Data: predictCall}, nil)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to call predictAddress: %w", err)
	}
	predictValues, err := factoryABI.Unpack("predictAddress", predictResult)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to unpack predictAddress result: %w", err)
	}
	wrappedAddress, ok := predictValues[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected predictAddress result type %T", predictValues[0])
	}

	return wrappedAddress, nil
}

// SendBridgeTx sends a bridge transaction from ac1 to ac2 via the sidecar.
// Returns the signed transactions for both chains (for receipt checking).
func SendBridgeTx(
	t *testing.T,
	ac1 *accounts.Account,
	ac2 *accounts.Account,
	amount *big.Int,
	bridgeABI abi.ABI,
) (*types.Transaction, *types.Transaction, error) {
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	sessionID := transactions.GenerateRandomSessionID()

	calldataA, err := PackBridgeERC20To(bridgeABI,
		ac2.GetRollup().ChainID(),
		configs.Values.L2.Contracts[configs.ContractNameToken].Address,
		amount,
		ac2.GetAddress(),
		sessionID,
	)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransaction(t.Context(), transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       BridgeSendGasLimit,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, ac1)
	require.NoError(t, err)

	calldataB, err := PackBridgeReceiveTokens(bridgeABI,
		ac1.GetRollup().ChainID(),
		ac2.GetRollup().ChainID(),
		bridgeAddr,
		ac2.GetAddress(),
		sessionID,
	)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(t.Context(), transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       BridgeReceiveGasLimit,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, ac2)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		ac1.GetRollup().ChainID().String(): {hexutil.Encode(signedBytesA)},
		ac2.GetRollup().ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	xtResp, err := transactions.SubmitXT(t.Context(), configs.Values.L2.SidecarURL, xtTxs)
	require.NoError(t, err)

	committed, err := transactions.WaitForDecision(t.Context(), configs.Values.L2.SidecarURL, xtResp.InstanceID, defaultDecisionTimeout)
	require.NoError(t, err)
	require.True(t, committed, "bridge XT should be committed")

	logger.Info("Bridge XT committed: %s (txA: %s, txB: %s)", xtResp.InstanceID, txA.Hash(), txB.Hash())
	return txA, txB, nil
}

// SendBridgeTxWithNonce sends a bridge transaction with explicit nonces via the sidecar.
func SendBridgeTxWithNonce(
	t *testing.T,
	ac1 *accounts.Account,
	ac1Nonce uint64,
	ac2 *accounts.Account,
	ac2Nonce uint64,
	amount *big.Int,
	bridgeABI abi.ABI,
) (*types.Transaction, *types.Transaction, error) {
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	sessionID := transactions.GenerateRandomSessionID()

	calldataA, err := PackBridgeERC20To(bridgeABI,
		ac2.GetRollup().ChainID(),
		configs.Values.L2.Contracts[configs.ContractNameToken].Address,
		amount,
		ac2.GetAddress(),
		sessionID,
	)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransactionWithNonce(t.Context(), transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       BridgeSendGasLimit,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, ac1, ac1Nonce)
	require.NoError(t, err)

	calldataB, err := PackBridgeReceiveTokens(bridgeABI,
		ac1.GetRollup().ChainID(),
		ac2.GetRollup().ChainID(),
		bridgeAddr,
		ac2.GetAddress(),
		sessionID,
	)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransactionWithNonce(t.Context(), transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       BridgeReceiveGasLimit,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, ac2, ac2Nonce)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		ac1.GetRollup().ChainID().String(): {hexutil.Encode(signedBytesA)},
		ac2.GetRollup().ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	xtResp, err := transactions.SubmitXT(t.Context(), configs.Values.L2.SidecarURL, xtTxs)
	require.NoError(t, err)

	committed, err := transactions.WaitForDecision(t.Context(), configs.Values.L2.SidecarURL, xtResp.InstanceID, defaultDecisionTimeout)
	require.NoError(t, err)
	require.True(t, committed, "bridge XT should be committed")

	logger.Info("Bridge XT committed: %s (txA: %s, txB: %s)", xtResp.InstanceID, txA.Hash(), txB.Hash())
	return txA, txB, nil
}

// SubmitXTAndWait is a generic helper that submits an XT and waits for its decision.
// Returns (instanceID, committed, error).
func SubmitXTAndWait(ctx context.Context, xtTxs map[string][]string, timeout time.Duration) (string, bool, error) {
	xtResp, err := transactions.SubmitXT(ctx, configs.Values.L2.SidecarURL, xtTxs)
	if err != nil {
		return "", false, fmt.Errorf("failed to submit XT: %w", err)
	}

	committed, err := transactions.WaitForDecision(ctx, configs.Values.L2.SidecarURL, xtResp.InstanceID, timeout)
	if err != nil {
		return xtResp.InstanceID, false, fmt.Errorf("failed waiting for decision: %w", err)
	}

	return xtResp.InstanceID, committed, nil
}
