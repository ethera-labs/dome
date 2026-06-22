package test

import (
	"context"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// erc20MinABI carries balanceOf + allowance — enough for opportunistic reads
// (e.g. against a predicted-but-not-yet-deployed CET).
const erc20MinABI = `[
  {"type":"function","name":"balanceOf","inputs":[{"name":"owner","type":"address"}],"outputs":[{"type":"uint256"}],"stateMutability":"view"},
  {"type":"function","name":"allowance","inputs":[{"name":"owner","type":"address"},{"name":"spender","type":"address"}],"outputs":[{"type":"uint256"}],"stateMutability":"view"}
]`

func readERC20Balance(ctx context.Context, client *ethclient.Client, token, owner common.Address) (*big.Int, error) {
	parsed, err := abi.JSON(strings.NewReader(erc20MinABI))
	if err != nil {
		return nil, err
	}
	contract := bind.NewBoundContract(token, parsed, client, client, client)
	var bal *big.Int
	if err := contract.Call(&bind.CallOpts{Context: ctx}, &[]any{&bal}, "balanceOf", owner); err != nil {
		return nil, err
	}
	return bal, nil
}

// readERC20Allowance reads token.allowance(owner, spender).
func readERC20Allowance(ctx context.Context, rpcURL string, token, owner, spender common.Address) (*big.Int, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	parsed, err := abi.JSON(strings.NewReader(erc20MinABI))
	if err != nil {
		return nil, err
	}
	contract := bind.NewBoundContract(token, parsed, client, client, client)
	var allowance *big.Int
	if err := contract.Call(&bind.CallOpts{Context: ctx}, &[]any{&allowance}, "allowance", owner, spender); err != nil {
		return nil, err
	}
	return allowance, nil
}
