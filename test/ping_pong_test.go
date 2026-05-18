package test

import (
	"bytes"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gomath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mailboxLabelPING = "PING"
	mailboxLabelPONG = "PONG"
)

// newOutboxKeyTopic is the keccak256 selector for Mailbox.NewOutboxKey(uint256,bytes32).
var newOutboxKeyTopic = crypto.Keccak256Hash([]byte("NewOutboxKey(uint256,bytes32)"))

// outboxKeyFromReceipt extracts the Mailbox outbox key from the first NewOutboxKey log
// in the receipt. Returns a zero hash if the event is not present.
func outboxKeyFromReceipt(receipt *types.Receipt) common.Hash {
	for _, l := range receipt.Logs {
		if len(l.Topics) > 0 && l.Topics[0] == newOutboxKeyTopic && len(l.Data) >= 32 {
			return common.BytesToHash(l.Data[:32])
		}
	}
	return common.Hash{}
}

// computeMailboxOutboxKey replicates Mailbox.getKey for the outbox direction.
// It mirrors keccak256(abi.encodePacked(chainSrc, chainDest, sender, receiver, sessionId, label))
// from the Mailbox contract, letting tests verify the exact message label (e.g. "PING", "PONG").
func computeMailboxOutboxKey(
	chainSrc, chainDest *big.Int, sender, receiver common.Address, sessionID *big.Int, label []byte,
) common.Hash {
	buf := make([]byte, 0, 32+32+20+20+32+len(label))
	buf = append(buf, gomath.PaddedBigBytes(chainSrc, 32)...)
	buf = append(buf, gomath.PaddedBigBytes(chainDest, 32)...)
	buf = append(buf, sender.Bytes()...)
	buf = append(buf, receiver.Bytes()...)
	buf = append(buf, gomath.PaddedBigBytes(sessionID, 32)...)
	buf = append(buf, label...)
	return crypto.Keccak256Hash(buf)
}

func TestPingPong(t *testing.T) {
	ctx := t.Context()

	pingPongABI, err := abi.JSON(strings.NewReader(configs.Values.L2.Contracts[configs.ContractNamePingPong].ABI))
	require.NoError(t, err)
	if _, ok := pingPongABI.Methods["ping"]; !ok {
		t.Skip("configured ping-pong ABI does not provide ping; skipping legacy ping-pong test for this environment")
	}
	if _, ok := pingPongABI.Methods["pong"]; !ok {
		t.Skip("configured ping-pong ABI does not provide pong; skipping legacy ping-pong test for this environment")
	}

	sessionID := transactions.GenerateRandomSessionID()
	pingPongAddress := configs.Values.L2.Contracts[configs.ContractNamePingPong].Address

	// Build ping tx on rollup A
	calldataA, err := pingPongABI.Pack("ping",
		TestRollupB.ChainID(),
		pingPongAddress, // pongSender: PingPong contract on B (same address on both chains)
		pingPongAddress, // pingReceiver: PingPong contract on B
		sessionID,
		[]byte("Hello from rollup A"),
	)
	require.NoError(t, err)

	txA, signedBytesA, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        pingPongAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataA,
	}, TestAccountA)
	require.NoError(t, err)

	// Build pong tx on rollup B
	calldataB, err := pingPongABI.Pack("pong",
		TestRollupA.ChainID(),
		pingPongAddress, // pingSender: PingPong contract on A (same address on both chains)
		sessionID,
		[]byte("Hello from rollup B"),
	)
	require.NoError(t, err)

	txB, signedBytesB, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        pingPongAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldataB,
	}, TestAccountB)
	require.NoError(t, err)

	// Submit XT to sidecar
	xtTxs := map[string][]string{
		TestRollupA.ChainID().String(): {hexutil.Encode(signedBytesA)},
		TestRollupB.ChainID().String(): {hexutil.Encode(signedBytesB)},
	}

	_, committed, err := helpers.SubmitXTAndWait(ctx, xtTxs, 3*time.Minute)
	require.NoError(t, err)
	require.True(t, committed, "ping-pong XT should be committed")

	// Fetch both receipts in parallel — independent RPC targets.
	type txResult struct {
		tx      *types.Transaction
		receipt *types.Receipt
		err     error
	}
	resultA := make(chan txResult, 1)
	resultB := make(chan txResult, 1)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tx, receipt, err := transactions.GetTransactionDetails(ctx, txA.Hash(), TestRollupA)
		resultA <- txResult{tx, receipt, err}
	}()
	go func() {
		defer wg.Done()
		tx, receipt, err := transactions.GetTransactionDetails(ctx, txB.Hash(), TestRollupB)
		resultB <- txResult{tx, receipt, err}
	}()
	wg.Wait()

	// Verify tx A: ping writes a PING message to the Mailbox outbox (chain A → chain B).
	resA := <-resultA
	require.NoError(t, resA.err)
	assert.Equal(t, types.ReceiptStatusSuccessful, resA.receipt.Status)
	assert.Equal(t, pingPongAddress, *resA.tx.To())
	assert.True(t, bytes.Equal(resA.tx.Data(), calldataA))
	outboxKeyA := outboxKeyFromReceipt(resA.receipt)
	require.NotZero(t, outboxKeyA, "expected NewOutboxKey event from Mailbox on rollup A")
	expectedPingKey := computeMailboxOutboxKey(
		TestRollupA.ChainID(), TestRollupB.ChainID(),
		pingPongAddress, pingPongAddress, sessionID, []byte(mailboxLabelPING),
	)
	assert.Equal(t, expectedPingKey, outboxKeyA, "outbox key should encode a PING message")

	// Verify tx B: pong reads the PING then writes a PONG message to the Mailbox outbox (chain B → chain A).
	resB := <-resultB
	require.NoError(t, resB.err)
	assert.Equal(t, types.ReceiptStatusSuccessful, resB.receipt.Status)
	assert.Equal(t, pingPongAddress, *resB.tx.To())
	assert.True(t, bytes.Equal(resB.tx.Data(), calldataB))
	outboxKeyB := outboxKeyFromReceipt(resB.receipt)
	require.NotZero(t, outboxKeyB, "expected NewOutboxKey event from Mailbox on rollup B")
	// pong writes from the PingPong contract back to itself on chain A (address(this) as receiver).
	expectedPongKey := computeMailboxOutboxKey(
		TestRollupB.ChainID(), TestRollupA.ChainID(),
		pingPongAddress, pingPongAddress, sessionID, []byte(mailboxLabelPONG),
	)
	assert.Equal(t, expectedPongKey, outboxKeyB, "outbox key should encode a PONG message")
}
