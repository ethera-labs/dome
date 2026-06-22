package helpers

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/transactions"
)

// Gas constants matching the TS scripts for L1->L2 bridging.
const (
	// portal minimum for 0-byte calldata
	L1MinGasLimitETH uint64 = 21_000
	// covers first-call CET deployment on L2
	L1MinGasLimitNewToken uint64 = 2_500_000
	// post-CET-deploy ERC20 bridge gas
	L1MinGasLimitERC20 uint64 = 200_000
	// L1 tx gas (the deposit-burn portal call)
	L1BridgeGasLimit uint64 = 5_000_000
)

// PackBridgeETHTo encodes ComposeL1Bridge.bridgeETHTo(_to, _minGasLimit, _extraData).
func PackBridgeETHTo(
	bridgeABI abi.ABI,
	to common.Address,
	minGasLimit uint32,
	extraData []byte,
) ([]byte, error) {
	return bridgeABI.Pack("bridgeETHTo", to, minGasLimit, extraData)
}

// PackBridgeERC20ToL1 encodes ComposeL1Bridge.bridgeERC20To
//
//	(_localToken, _remoteToken, _to, _amount, _minGasLimit, _extraData)
func PackBridgeERC20ToL1(
	bridgeABI abi.ABI,
	localToken common.Address,
	remoteToken common.Address,
	to common.Address,
	amount *big.Int,
	minGasLimit uint32,
	extraData []byte,
) ([]byte, error) {
	return bridgeABI.Pack("bridgeERC20To", localToken, remoteToken, to, amount, minGasLimit, extraData)
}

// EncodeERC20ExtraData ABI-encodes the (name, symbol, decimals, bytes) tuple
// the L1 bridge stamps onto the deposit so the L2 side can deploy a CET with
// matching metadata. Mirrors the TS extraData encoding.
func EncodeERC20ExtraData(name, symbol string, decimals uint8) ([]byte, error) {
	tuple := abi.Arguments{
		{Name: "name", Type: mustABIType("string")},
		{Name: "symbol", Type: mustABIType("string")},
		{Name: "decimals", Type: mustABIType("uint8")},
		{Name: "extra", Type: mustABIType("bytes")},
	}
	return tuple.Pack(name, symbol, decimals, []byte{})
}

func mustABIType(t string) abi.Type {
	parsed, err := abi.NewType(t, "", nil)
	if err != nil {
		panic("abi.NewType " + t + ": " + err.Error())
	}
	return parsed
}

// L1BridgeAddressFor returns the ComposeL1Bridge address configured for the
// given destination rollup ("rollup-a" or "rollup-b"). Returns (zero, error)
// if the L1 section or the entry is missing.
func L1BridgeAddressFor(destRollup configs.ChainName) (common.Address, error) {
	if !configs.Values.HasL1() {
		return common.Address{}, fmt.Errorf("no l1 section in config")
	}
	var key configs.ContractName
	switch destRollup {
	case configs.ChainNameRollupA:
		key = configs.ContractNameL1BridgeA
	case configs.ChainNameRollupB:
		key = configs.ContractNameL1BridgeB
	default:
		return common.Address{}, fmt.Errorf("unknown rollup %s", destRollup)
	}
	cfg, ok := configs.Values.L1.Contracts[key]
	if !ok {
		return common.Address{}, fmt.Errorf("no contract config for %s", key)
	}
	return cfg.Address, nil
}

// SendL1Tx signs+broadcasts an L1 tx using `ac` (which must be on the L1
// rollup), waits for the receipt and asserts status==1.
func SendL1Tx(
	ctx context.Context,
	ac *accounts.Account,
	to common.Address,
	value *big.Int,
	gas uint64,
	data []byte,
) (*types.Transaction, *types.Receipt, error) {
	tx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        to,
		Value:     value,
		Gas:       gas,
		GasTipCap: GasTipCap,
		GasFeeCap: GasFeeCap,
		Data:      data,
	}, ac)
	if err != nil {
		return nil, nil, fmt.Errorf("sign l1 tx: %w", err)
	}
	if _, err := transactions.SendTransaction(ctx, tx, ac.GetRollup().RPCURL()); err != nil {
		return nil, nil, fmt.Errorf("send l1 tx: %w", err)
	}
	_, receipt, err := transactions.GetTransactionDetails(ctx, tx.Hash(), ac.GetRollup())
	if err != nil {
		return tx, nil, fmt.Errorf("receipt l1 tx: %w", err)
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return tx, receipt, fmt.Errorf("l1 tx reverted: %s", tx.Hash().Hex())
	}
	return tx, receipt, nil
}
