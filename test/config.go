package test

import (
	"context"
	"math/big"
	"os"
	"strings"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

// Global test variables
var (
	TestRollupA  *rollup.Rollup
	TestRollupB  *rollup.Rollup
	TestAccountA *accounts.Account
	TestAccountB *accounts.Account
	BridgeABI abi.ABI
	TokenABI  abi.ABI
)

func setup(ctx context.Context) {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "INFO"
	}
	logger.SetLogLevelFromString(logLevel)

	var (
		err             error
		chainConfigs    = configs.Values.L2.ChainConfigs
		contractConfigs = configs.Values.L2.Contracts
	)

	TestRollupA = rollup.New(chainConfigs[configs.ChainNameRollupA].RPCURL, big.NewInt(chainConfigs[configs.ChainNameRollupA].ID), string(configs.ChainNameRollupA))
	TestRollupB = rollup.New(chainConfigs[configs.ChainNameRollupB].RPCURL, big.NewInt(chainConfigs[configs.ChainNameRollupB].ID), string(configs.ChainNameRollupB))

	TestAccountA, err = accounts.NewRollupAccount(chainConfigs[configs.ChainNameRollupA].PK, TestRollupA)
	if err != nil {
		panic("Failed to create account A: " + err.Error())
	}

	TestAccountB, err = accounts.NewRollupAccount(chainConfigs[configs.ChainNameRollupB].PK, TestRollupB)
	if err != nil {
		panic("Failed to create account B: " + err.Error())
	}

	BridgeABI, err = abi.JSON(strings.NewReader(contractConfigs[configs.ContractNameBridge].ABI))
	if err != nil {
		panic("Failed to parse ABI: " + err.Error())
	}

	TokenABI, err = abi.JSON(strings.NewReader(contractConfigs[configs.ContractNameToken].ABI))
	if err != nil {
		panic("Failed to parse ABI: " + err.Error())
	}

	// approve tokens for the main accounts
	_, _, err = helpers.ApproveTokensCtx(context.Background(), TestAccountA, configs.Values.L2.Contracts[configs.ContractNameBridge].Address, TokenABI)
	if err != nil {
		panic("Failed to approve tokens for TestAccountA: " + err.Error())
	}
	_, _, err = helpers.ApproveTokensCtx(context.Background(), TestAccountB, configs.Values.L2.Contracts[configs.ContractNameBridge].Address, TokenABI)
	if err != nil {
		panic("Failed to approve tokens for TestAccountB: " + err.Error())
	}
}
