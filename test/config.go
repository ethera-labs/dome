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
	TestRollupA   *rollup.Rollup
	TestRollupB   *rollup.Rollup
	TestAccountA  *accounts.Account
	TestAccountB  *accounts.Account
	BridgeABI     abi.ABI
	TokenABI      abi.ABI
	CetFactoryABI abi.ABI
)

// Initial mint amount for each main test account. Sized generously so the
// stress tests' worst case (25 bridge XTs * 1 token each, plus headroom for
// repeated runs) does not deplete the source-side ERC-20 balance.
var setupMintAmount = new(big.Int).Mul(big.NewInt(1000), big.NewInt(1_000_000_000_000_000_000)) // 1000 tokens

func setup(ctx context.Context) {
	_ = ctx
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
		panic("create account A: " + err.Error())
	}
	TestAccountB, err = accounts.NewRollupAccount(chainConfigs[configs.ChainNameRollupB].PK, TestRollupB)
	if err != nil {
		panic("create account B: " + err.Error())
	}

	for name, dst := range map[configs.ContractName]*abi.ABI{
		configs.ContractNameBridge:     &BridgeABI,
		configs.ContractNameToken:      &TokenABI,
		configs.ContractNameCetFactory: &CetFactoryABI,
	} {
		parsed, err := abi.JSON(strings.NewReader(contractConfigs[name].ABI))
		if err != nil {
			panic("parse " + string(name) + " ABI: " + err.Error())
		}
		*dst = parsed
	}

	bridge := contractConfigs[configs.ContractNameBridge].Address
	for label, ac := range map[string]*accounts.Account{"A": TestAccountA, "B": TestAccountB} {
		if err := helpers.MintAndApproveCtx(context.Background(), ac, bridge, setupMintAmount, TokenABI); err != nil {
			panic("setup mint+approve for TestAccount" + label + ": " + err.Error())
		}
	}
}
