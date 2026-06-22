package helpers

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// ProofResult mirrors the shape returned by `eth_getProof`.
type ProofResult struct {
	Address      common.Address  `json:"address"`
	AccountProof []string        `json:"accountProof"`
	Balance      *hexutil.Big    `json:"balance"`
	CodeHash     common.Hash     `json:"codeHash"`
	Nonce        hexutil.Uint64  `json:"nonce"`
	StorageHash  common.Hash     `json:"storageHash"`
	StorageProof []StorageEntry  `json:"storageProof"`
}

// StorageEntry is one `(key, value, proof)` triple from the storageProof array.
type StorageEntry struct {
	Key   string   `json:"key"`
	Value string   `json:"value"`
	Proof []string `json:"proof"`
}

// GetProof calls `eth_getProof(address, [slot], block)` against the given RPC.
// `slot` is a 32-byte storage key (hex string). `blockNumber` is the L2 block
// the proof should be anchored to.
func GetProof(
	ctx context.Context,
	rpcURL string,
	address common.Address,
	slot string,
	blockNumber *big.Int,
) (*ProofResult, error) {
	client, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", rpcURL, err)
	}
	defer client.Close()

	blockArg := hexutil.EncodeBig(blockNumber)
	var result ProofResult
	if err := client.CallContext(ctx, &result, "eth_getProof",
		address.Hex(), []string{slot}, blockArg,
	); err != nil {
		return nil, fmt.Errorf("eth_getProof: %w", err)
	}
	return &result, nil
}

// HeaderStateRoot returns just the stateRoot of the given L2 block by calling
// `eth_getBlockByNumber(blockNumber, false)`.
func HeaderStateRoot(
	ctx context.Context,
	rpcURL string,
	blockNumber *big.Int,
) (common.Hash, common.Hash, error) {
	client, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		return common.Hash{}, common.Hash{}, fmt.Errorf("dial: %w", err)
	}
	defer client.Close()

	var raw struct {
		Hash      common.Hash `json:"hash"`
		StateRoot common.Hash `json:"stateRoot"`
	}
	if err := client.CallContext(ctx, &raw, "eth_getBlockByNumber",
		hexutil.EncodeBig(blockNumber), false,
	); err != nil {
		return common.Hash{}, common.Hash{}, fmt.Errorf("eth_getBlockByNumber: %w", err)
	}
	return raw.StateRoot, raw.Hash, nil
}
