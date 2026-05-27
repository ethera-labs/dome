package helpers

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/rollup"
)

// PredictCetAddress returns the destination-chain wrapper-CET address that
// `ComposeL2ToL2Bridge.receiveTokens` mints into when bridging `remoteAsset`
// from `remoteChainID`. The CET address is deterministic via CREATE2 and is
// queried from the local-chain `CetFactory.predictAddress`.
func PredictCetAddress(
	ctx context.Context,
	localChain *rollup.Rollup,
	cetFactoryABI abi.ABI,
	remoteAsset common.Address,
	remoteChainID *big.Int,
) (common.Address, error) {
	client, err := ethclient.DialContext(ctx, localChain.RPCURL())
	if err != nil {
		return common.Address{}, fmt.Errorf("dial %s: %w", localChain.RPCURL(), err)
	}
	defer client.Close()

	contract := bind.NewBoundContract(
		configs.Values.L2.Contracts[configs.ContractNameCetFactory].Address,
		cetFactoryABI,
		client,
		client,
		client,
	)
	var cet common.Address
	if err := contract.Call(&bind.CallOpts{Context: ctx}, &[]any{&cet}, "predictAddress", remoteAsset, remoteChainID); err != nil {
		return common.Address{}, fmt.Errorf("predictAddress(%s, %s): %w", remoteAsset.Hex(), remoteChainID, err)
	}
	return cet, nil
}
