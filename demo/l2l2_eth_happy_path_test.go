package demo

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

const (
	defaultSidecarURL = "http://sepolia-sidecar-a.stage.vnet.ops.ssvlabsinternal.com:8080"
	defaultRollupARPC = "https://op-rbuilder-a.stage.ethera-labs.io"
	defaultRollupBRPC = "https://op-rbuilder-b.stage.ethera-labs.io"

	defaultRollupAChainID = int64(100003)
	defaultRollupBChainID = int64(200005)

	// Same deterministic deployment address on both rollups from
	// ethera-deployments/contracts/sepolia-stage/L2/addresses.toml.
	defaultComposeL2ToL2Bridge = "0x6e166073b5dd5fd53b33fed5a7bd9c104c6c6ebd"

	// 0.001 ETH keeps the demo cheap while still producing a visible balance delta.
	defaultBridgeAmountWei = "1000000000000000"
	defaultSimpleTransferWei = "1"

	defaultDecisionTimeout = 2 * time.Minute

	// Minimal ABI for the ETH happy path only.
	composeL2ToL2BridgeETHABI = `[
		{
			"type":"function",
			"name":"bridgeEthTo",
			"stateMutability":"payable",
			"inputs":[
				{"name":"sessionId","type":"uint256"},
				{"name":"chainDest","type":"uint256"},
				{"name":"receiver","type":"address"}
			],
			"outputs":[]
		},
		{
			"type":"function",
			"name":"receiveETH",
			"stateMutability":"nonpayable",
			"inputs":[
				{
					"name":"msgHeader",
					"type":"tuple",
					"components":[
						{"name":"chainSrc","type":"uint256"},
						{"name":"chainDest","type":"uint256"},
						{"name":"sender","type":"address"},
						{"name":"receiver","type":"address"},
						{"name":"sessionId","type":"uint256"},
						{"name":"label","type":"string"}
					]
				}
			],
			"outputs":[{"name":"amount","type":"uint256"}]
		},
		{
			"type":"event",
			"name":"ETHBridged",
			"anonymous":false,
			"inputs":[
				{"name":"chainDest","type":"uint256","indexed":true},
				{"name":"sender","type":"address","indexed":true},
				{"name":"receiver","type":"address","indexed":true},
				{"name":"amount","type":"uint256","indexed":false},
				{"name":"sessionId","type":"uint256","indexed":false},
				{"name":"messageId","type":"bytes32","indexed":false}
			]
		},
		{
			"type":"event",
			"name":"ETHReceived",
			"anonymous":false,
			"inputs":[
				{"name":"receiver","type":"address","indexed":true},
				{"name":"amount","type":"uint256","indexed":false}
			]
		}
	]`
)

type messageHeader struct {
	ChainSrc  *big.Int
	ChainDest *big.Int
	Sender    common.Address
	Receiver  common.Address
	SessionId *big.Int
	Label     string
}

type ethBridgedEvent struct {
	Amount    *big.Int
	SessionID *big.Int
	MessageID [32]byte
}

type ethReceivedEvent struct {
	Amount *big.Int
}

func TestDemoStageXTHappyPath(t *testing.T) {
	sidecarURL, rollupA, rollupB, accountA, accountB := loadDemoContext(t)
	ctx := t.Context()

	transferAmount := mustBigInt(t, envOrDefault("DOME_DEMO_SIMPLE_TRANSFER_WEI", defaultSimpleTransferWei))

	txA, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        accountA.GetAddress(),
		Value:     new(big.Int).Set(transferAmount),
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      nil,
	}, accountA)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        accountB.GetAddress(),
		Value:     new(big.Int).Set(transferAmount),
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      nil,
	}, accountB)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		rollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		rollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	xtResp, err := transactions.SubmitXT(ctx, sidecarURL, xtTxs)
	require.NoError(t, err)

	committed, err := transactions.WaitForDecision(ctx, sidecarURL, xtResp.InstanceID, defaultDecisionTimeout)
	require.NoError(t, err)
	require.True(t, committed, "demo XT should be committed")

	_, receiptA, err := transactions.GetTransactionDetails(ctx, txA.Hash(), rollupA)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, receiptA.Status)

	_, receiptB, err := transactions.GetTransactionDetails(ctx, txB.Hash(), rollupB)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, receiptB.Status)
}

func TestDemoStageL2ToL2ETHHappyPath(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DOME_DEMO_ENABLE_BRIDGE_FLOW")) != "1" {
		t.Skip("bridge-specific stage flow is kept as an explicit opt-in until live sidecar mailbox support is aligned")
	}

	sidecarURL, rollupA, rollupB, accountA, accountB := loadDemoContext(t)
	bridgeAddr := common.HexToAddress(envOrDefault("DOME_DEMO_BRIDGE_ADDR", defaultComposeL2ToL2Bridge))
	bridgeAmount := mustBigInt(t, envOrDefault("DOME_DEMO_ETH_AMOUNT_WEI", defaultBridgeAmountWei))

	bridgeABI, err := abi.JSON(strings.NewReader(composeL2ToL2BridgeETHABI))
	require.NoError(t, err)

	ctx := t.Context()
	sessionID := transactions.GenerateRandomSessionID()

	beforeBalanceB, err := accountB.GetBalance(ctx)
	require.NoError(t, err)

	calldataA, err := bridgeABI.Pack("bridgeEthTo", sessionID, rollupB.ChainID(), accountB.GetAddress())
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     new(big.Int).Set(bridgeAmount),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, accountA)
	require.NoError(t, err)

	header := messageHeader{
		ChainSrc:  rollupA.ChainID(),
		ChainDest: rollupB.ChainID(),
		Sender:    bridgeAddr,
		Receiver:  accountB.GetAddress(),
		SessionId: sessionID,
		Label:     "SEND_ETH",
	}

	calldataB, err := bridgeABI.Pack("receiveETH", header)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        bridgeAddr,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, accountB)
	require.NoError(t, err)

	xtTxs := map[string][]string{
		rollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		rollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	xtResp, err := transactions.SubmitXT(ctx, sidecarURL, xtTxs)
	require.NoError(t, err)

	committed, err := transactions.WaitForDecision(ctx, sidecarURL, xtResp.InstanceID, defaultDecisionTimeout)
	require.NoError(t, err)
	require.True(t, committed, "demo XT should be committed")

	_, receiptA, err := transactions.GetTransactionDetails(ctx, txA.Hash(), rollupA)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, receiptA.Status)

	_, receiptB, err := transactions.GetTransactionDetails(ctx, txB.Hash(), rollupB)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, receiptB.Status)

	bridged := findEvent(t, bridgeABI, "ETHBridged", receiptA.Logs)
	var bridgedEvent ethBridgedEvent
	require.NoError(t, bridgeABI.UnpackIntoInterface(&bridgedEvent, "ETHBridged", bridged.Data))
	require.Equal(t, bridgeAmount.String(), bridgedEvent.Amount.String())
	require.Equal(t, sessionID.String(), bridgedEvent.SessionID.String())

	received := findEvent(t, bridgeABI, "ETHReceived", receiptB.Logs)
	var receivedEvent ethReceivedEvent
	require.NoError(t, bridgeABI.UnpackIntoInterface(&receivedEvent, "ETHReceived", received.Data))
	require.Equal(t, bridgeAmount.String(), receivedEvent.Amount.String())

	afterBalanceB, err := accountB.GetBalance(context.Background())
	require.NoError(t, err)

	expectedBalanceB := new(big.Int).Add(beforeBalanceB, bridgeAmount)
	expectedBalanceB.Sub(expectedBalanceB, gasSpent(receiptB))
	require.Equal(t, expectedBalanceB.String(), afterBalanceB.String(), "destination balance should increase by bridged amount minus destination gas")
}

func loadDemoContext(t *testing.T) (string, *rollup.Rollup, *rollup.Rollup, *accounts.Account, *accounts.Account) {
	t.Helper()

	pkA := demoPrivateKey("DOME_DEMO_PK_A")
	pkB := demoPrivateKey("DOME_DEMO_PK_B")
	if pkA == "" {
		pkA = demoPrivateKey("DOME_DEMO_PK")
	}
	if pkB == "" {
		pkB = demoPrivateKey("DOME_DEMO_PK")
	}
	if pkA == "" && pkB == "" {
		t.Skip("set DOME_DEMO_PK or DOME_DEMO_PK_A/DOME_DEMO_PK_B to run the stage demo XT test")
	}
	if pkA == "" || pkB == "" {
		t.Skip("set DOME_DEMO_PK for both rollups, or provide both DOME_DEMO_PK_A and DOME_DEMO_PK_B")
	}

	sidecarURL := envOrDefault("DOME_DEMO_SIDECAR_URL", defaultSidecarURL)
	rollupA := rollup.New(envOrDefault("DOME_DEMO_RPC_A", defaultRollupARPC), big.NewInt(defaultRollupAChainID), "rollup-a")
	rollupB := rollup.New(envOrDefault("DOME_DEMO_RPC_B", defaultRollupBRPC), big.NewInt(defaultRollupBChainID), "rollup-b")

	accountA, err := accounts.NewRollupAccount(pkA, rollupA)
	require.NoError(t, err)
	t.Cleanup(accountA.Close)

	accountB, err := accounts.NewRollupAccount(pkB, rollupB)
	require.NoError(t, err)
	t.Cleanup(accountB.Close)

	return sidecarURL, rollupA, rollupB, accountA, accountB
}

func demoPrivateKey(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(os.Getenv(name)), "0x")
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func mustBigInt(t *testing.T, value string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(value, 10)
	require.True(t, ok, "invalid big integer value %q", value)
	return n
}

func findEvent(t *testing.T, contractABI abi.ABI, name string, logs []*types.Log) *types.Log {
	t.Helper()
	event, ok := contractABI.Events[name]
	require.True(t, ok, "event %s not found in ABI", name)

	for _, entry := range logs {
		if len(entry.Topics) > 0 && entry.Topics[0] == event.ID {
			return entry
		}
	}

	t.Fatalf("event %s not found in receipt logs", name)
	return nil
}

func gasSpent(receipt *types.Receipt) *big.Int {
	if receipt.EffectiveGasPrice == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice)
}
