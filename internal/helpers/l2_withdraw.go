package helpers

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
)

// L2ToL1MessagePasser is the Optimism-style address that emits MessagePassed.
var L2ToL1MessagePasser = common.HexToAddress("0x4200000000000000000000000000000000000016")

// MessagePassedEvent is the canonical MessagePassed log topic.
//
//	MessagePassed(
//	  uint256 indexed nonce, address indexed sender, address indexed target,
//	  uint256 value, uint256 gasLimit, bytes data, bytes32 withdrawalHash
//	)
var MessagePassedTopic = crypto.Keccak256Hash([]byte(
	"MessagePassed(uint256,address,address,uint256,uint256,bytes,bytes32)",
))

// WithdrawalTx mirrors the on-chain tuple `Types.WithdrawalTransaction` —
// the argument shape for ComposePortal.proveWithdrawalTransaction and
// finalizeWithdrawalTransaction.
type WithdrawalTx struct {
	Nonce    *big.Int       `abi:"nonce"`
	Sender   common.Address `abi:"sender"`
	Target   common.Address `abi:"target"`
	Value    *big.Int       `abi:"value"`
	GasLimit *big.Int       `abi:"gasLimit"`
	Data     []byte         `abi:"data"`
}

// OutputRootProof matches Types.OutputRootProof.
type OutputRootProof struct {
	Version                  [32]byte `abi:"version"`
	StateRoot                [32]byte `abi:"stateRoot"`
	MessagePasserStorageRoot [32]byte `abi:"messagePasserStorageRoot"`
	LatestBlockhash          [32]byte `abi:"latestBlockhash"`
}

// ExtractMessagePassed scans a receipt for the L2ToL1MessagePasser
// MessagePassed event and returns the withdrawal tuple + the withdrawalHash.
func ExtractMessagePassed(receipt *types.Receipt) (WithdrawalTx, common.Hash, error) {
	for _, log := range receipt.Logs {
		if log.Address != L2ToL1MessagePasser {
			continue
		}
		if len(log.Topics) < 4 || log.Topics[0] != MessagePassedTopic {
			continue
		}

		nonce := new(big.Int).SetBytes(log.Topics[1].Bytes())
		sender := common.BytesToAddress(log.Topics[2].Bytes())
		target := common.BytesToAddress(log.Topics[3].Bytes())

		// Data: (value uint256, gasLimit uint256, data bytes, withdrawalHash bytes32)
		args := abi.Arguments{
			{Type: mustABIType("uint256")},
			{Type: mustABIType("uint256")},
			{Type: mustABIType("bytes")},
			{Type: mustABIType("bytes32")},
		}
		unpacked, err := args.Unpack(log.Data)
		if err != nil {
			return WithdrawalTx{}, common.Hash{}, fmt.Errorf("decode MessagePassed data: %w", err)
		}
		value, _ := unpacked[0].(*big.Int)
		gasLimit, _ := unpacked[1].(*big.Int)
		data, _ := unpacked[2].([]byte)
		var wh common.Hash
		raw := unpacked[3].([32]byte)
		copy(wh[:], raw[:])

		return WithdrawalTx{
			Nonce:    nonce,
			Sender:   sender,
			Target:   target,
			Value:    value,
			GasLimit: gasLimit,
			Data:     data,
		}, wh, nil
	}
	return WithdrawalTx{}, common.Hash{}, fmt.Errorf("MessagePassed event not found in receipt")
}

// WithdrawalHashStorageSlot computes the slot mapping for the
// `sentMessages[withdrawalHash]` boolean — same encoding the OP portal uses:
// `keccak256(abi.encode(withdrawalHash, 0))`.
func WithdrawalHashStorageSlot(withdrawalHash common.Hash) string {
	var buf [64]byte
	copy(buf[:32], withdrawalHash[:])
	// slot 0 — pad-left zeros, already zero
	hash := crypto.Keccak256Hash(buf[:])
	return hash.Hex()
}

// FindCoveringDisputeGame walks the DisputeGameFactory looking for a game of
// `respectedGameType` that covers `l2Block`. Returns the game index. The TS
// script scans extraData for plausible L2 block numbers; we do the same.
func FindCoveringDisputeGame(
	ctx context.Context,
	l1RPC string,
	dgfAddr common.Address,
	dgfABI abi.ABI,
	gameABI abi.ABI,
	respectedGameType uint32,
	l2Block uint64,
	pollInterval time.Duration,
	maxPolls int,
) (gameIndex uint64, coveredBlock uint64, err error) {
	client, err := ethclient.DialContext(ctx, l1RPC)
	if err != nil {
		return 0, 0, fmt.Errorf("dial l1: %w", err)
	}
	defer client.Close()

	dgf := bind.NewBoundContract(dgfAddr, dgfABI, client, client, client)
	for poll := 1; poll <= maxPolls; poll++ {
		var count *big.Int
		if err := dgf.Call(&bind.CallOpts{Context: ctx}, &[]any{&count}, "gameCount"); err != nil {
			return 0, 0, fmt.Errorf("gameCount: %w", err)
		}
		total := count.Uint64()
		// Search the most recent 10 games.
		start := uint64(0)
		if total > 10 {
			start = total - 10
		}
		for i := total; i > start; i-- {
			idx := i - 1
			var (
				gameType  uint32
				timestamp uint64
				proxy     common.Address
			)
			if err := dgf.Call(&bind.CallOpts{Context: ctx},
				&[]any{&gameType, &timestamp, &proxy},
				"gameAtIndex", new(big.Int).SetUint64(idx)); err != nil {
				continue
			}
			if gameType != respectedGameType {
				continue
			}

			game := bind.NewBoundContract(proxy, gameABI, client, client, client)
			var extraData []byte
			if err := game.Call(&bind.CallOpts{Context: ctx}, &[]any{&extraData}, "extraData"); err != nil {
				continue
			}
			// extraData is a concatenation of abi-encoded values. Scan in
			// 32-byte windows for plausible L2 block numbers (1M < val < 100M)
			// that are >= the block we need.
			for off := 0; off+32 <= len(extraData); off += 32 {
				val := new(big.Int).SetBytes(extraData[off : off+32])
				if !val.IsUint64() {
					continue
				}
				v := val.Uint64()
				if v < 1_000_000 || v > 100_000_000 {
					continue
				}
				if v >= l2Block {
					return idx, v, nil
				}
			}
		}
		logger.Info("dispute-game poll %d/%d — no covering game for l2Block %d yet (total games=%d)",
			poll, maxPolls, l2Block, total)
		select {
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return 0, 0, fmt.Errorf("timed out waiting for dispute game covering l2 block %d", l2Block)
}

// BuildWithdrawalProof builds the OutputRootProof and storage proof bytes for
// a withdrawalHash at the given L2 block. Returns (outputProof, storageProof[]).
func BuildWithdrawalProof(
	ctx context.Context,
	l2RPC string,
	withdrawalHash common.Hash,
	l2Block uint64,
) (OutputRootProof, [][]byte, error) {
	blockNum := new(big.Int).SetUint64(l2Block)
	stateRoot, blockHash, err := HeaderStateRoot(ctx, l2RPC, blockNum)
	if err != nil {
		return OutputRootProof{}, nil, fmt.Errorf("header state root: %w", err)
	}

	slot := WithdrawalHashStorageSlot(withdrawalHash)
	proof, err := GetProof(ctx, l2RPC, L2ToL1MessagePasser, slot, blockNum)
	if err != nil {
		return OutputRootProof{}, nil, fmt.Errorf("eth_getProof: %w", err)
	}
	if len(proof.StorageProof) == 0 {
		return OutputRootProof{}, nil, fmt.Errorf("eth_getProof returned no storage entries")
	}
	entry := proof.StorageProof[0]
	// Sanity: the OP MessagePasser sets sentMessages[hash] = true (=0x1).
	if entry.Value != "0x1" {
		return OutputRootProof{}, nil, fmt.Errorf("withdrawal not found in L2ToL1MessagePasser storage (value=%s)", entry.Value)
	}

	out := OutputRootProof{
		// version = bytes32(0) for OP outputV0
		StateRoot:                stateRoot,
		MessagePasserStorageRoot: proof.StorageHash,
		LatestBlockhash:          blockHash,
	}
	storageProof := make([][]byte, len(entry.Proof))
	for i, p := range entry.Proof {
		decoded, err := hexutil.Decode(p)
		if err != nil {
			return OutputRootProof{}, nil, fmt.Errorf("decode storage proof node %d: %w", i, err)
		}
		storageProof[i] = decoded
	}
	return out, storageProof, nil
}

// CallPortalView is a thin helper that reads a portal getter that returns a
// single value of the requested type. Used for finalizedWithdrawals (bool),
// numProofSubmitters (uint256), disputeGameFactory (address),
// respectedGameType (uint32), proofMaturityDelaySeconds (uint256).
func CallPortalView(
	ctx context.Context,
	rpcURL string,
	portalAddr common.Address,
	portalABI abi.ABI,
	method string,
	out any,
	args ...any,
) error {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer client.Close()

	contract := bind.NewBoundContract(portalAddr, portalABI, client, client, client)
	wrapped := []any{out}
	return contract.Call(&bind.CallOpts{Context: ctx}, &wrapped, method, args...)
}

// PackProveWithdrawal encodes ComposePortal.proveWithdrawalTransaction.
func PackProveWithdrawal(
	portalABI abi.ABI,
	wtx WithdrawalTx,
	gameIndex uint64,
	outRoot OutputRootProof,
	storageProof [][]byte,
) ([]byte, error) {
	return portalABI.Pack("proveWithdrawalTransaction",
		wtx, new(big.Int).SetUint64(gameIndex), outRoot, storageProof,
	)
}

// PackFinalizeWithdrawal encodes ComposePortal.finalizeWithdrawalTransaction.
func PackFinalizeWithdrawal(portalABI abi.ABI, wtx WithdrawalTx) ([]byte, error) {
	return portalABI.Pack("finalizeWithdrawalTransaction", wtx)
}

// PackBridgeETHToOnL2 encodes ComposeL2Bridge.bridgeETHTo(_to, _minGasLimit,
// _extraData) — same signature as the L1 bridge, but lives on the L2 side and
// emits the MessagePassed event that triggers the withdrawal flow.
func PackBridgeETHToOnL2(
	bridgeABI abi.ABI,
	to common.Address,
	minGasLimit uint32,
	extraData []byte,
) ([]byte, error) {
	return bridgeABI.Pack("bridgeETHTo", to, minGasLimit, extraData)
}

// PackBridgeERC20ToOnL2 encodes ComposeL2Bridge.bridgeERC20To(localToken,
// remoteToken, to, amount, minGasLimit, extraData) — burns the CET on L2 and
// emits the MessagePassed event.
func PackBridgeERC20ToOnL2(
	bridgeABI abi.ABI,
	localCET common.Address,
	remoteToken common.Address,
	to common.Address,
	amount *big.Int,
	minGasLimit uint32,
	extraData []byte,
) ([]byte, error) {
	return bridgeABI.Pack("bridgeERC20To", localCET, remoteToken, to, amount, minGasLimit, extraData)
}

// WaitForProofMaturity polls portal.checkWithdrawal(hash, account) in a loop
// until it stops reverting, which means the maturity window has elapsed.
func WaitForProofMaturity(
	ctx context.Context,
	rpcURL string,
	portalAddr common.Address,
	portalABI abi.ABI,
	withdrawalHash common.Hash,
	account common.Address,
	pollInterval time.Duration,
	maxPolls int,
) error {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer client.Close()
	contract := bind.NewBoundContract(portalAddr, portalABI, client, client, client)

	for poll := 1; poll <= maxPolls; poll++ {
		err := contract.Call(&bind.CallOpts{Context: ctx}, &[]any{}, "checkWithdrawal", withdrawalHash, account)
		if err == nil {
			return nil
		}
		logger.Debug("checkWithdrawal not yet ready (poll %d/%d): %v", poll, maxPolls, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
	return fmt.Errorf("checkWithdrawal never matured after %d polls", maxPolls)
}

// SubmittedTxOK convenience for verifying a portal tx receipt was successful.
func SubmittedTxOK(receipt *types.Receipt) error {
	if receipt == nil {
		return fmt.Errorf("nil receipt")
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return fmt.Errorf("tx reverted: %s", receipt.TxHash.Hex())
	}
	return nil
}

// ToSlotHex returns the 32-byte hex form of a *big.Int / uint64 / byte32
// suitable as the `slot` argument to `eth_getProof`.
func ToSlotHex(slot common.Hash) string {
	return slot.Hex()
}

// Compile-time check: keep imports needed by future call sites.
var _ = transactions.GenerateRandomSessionID
