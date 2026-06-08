package test

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethereum/go-ethereum/accounts/abi"
)

// Global test variables. Populated by setup() based on what the active config
// supports. On configs that omit a section (e.g. l1 missing on local-testnet)
// the corresponding globals stay nil/empty and the tests that need them call
// the Require* helpers below to skip cleanly.
var (
	// L2 rollups + accounts (always present).
	TestRollupA  *rollup.Rollup
	TestRollupB  *rollup.Rollup
	TestAccountA *accounts.Account
	TestAccountB *accounts.Account

	// L1 (optional).
	TestL1        *rollup.Rollup
	TestL1Account *accounts.Account

	// Active-config snapshot.
	TestNetwork string
	TestXTMode  string

	// L2 ABIs.
	BridgeABI          abi.ABI // ComposeL2ToL2Bridge
	TokenABI           abi.ABI // MockL2ERC20 (local only)
	CetFactoryABI      abi.ABI
	MailboxABI         abi.ABI
	ComposeL2BridgeABI abi.ABI // ComposeL2Bridge — used for L2->L1
	ETHLiquidityABI    abi.ABI

	// L1 ABIs.
	ComposeL1BridgeABI abi.ABI
	ComposePortalABI   abi.ABI
	DisputeGameABI     abi.ABI

	// Set to true when TokenABI parsed (local-testnet). Drives the legacy
	// mint+approve in setup().
	tokenABIPresent bool
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

	TestNetwork = configs.Values.Network
	TestXTMode = configs.Values.XTSubmission

	chainConfigs := configs.Values.L2.ChainConfigs
	TestRollupA = rollup.New(chainConfigs[configs.ChainNameRollupA].RPCURL, big.NewInt(chainConfigs[configs.ChainNameRollupA].ID), string(configs.ChainNameRollupA))
	TestRollupB = rollup.New(chainConfigs[configs.ChainNameRollupB].RPCURL, big.NewInt(chainConfigs[configs.ChainNameRollupB].ID), string(configs.ChainNameRollupB))

	var err error
	TestAccountA, err = accounts.NewRollupAccount(chainConfigs[configs.ChainNameRollupA].PK, TestRollupA)
	if err != nil {
		panic("create account A: " + err.Error())
	}
	TestAccountB, err = accounts.NewRollupAccount(chainConfigs[configs.ChainNameRollupB].PK, TestRollupB)
	if err != nil {
		panic("create account B: " + err.Error())
	}

	parseL2ABIs()
	if configs.Values.HasL1() {
		setupL1()
	}

	// Mint + approve is only meaningful when the configured `token` is the
	// MockL2ERC20 the local-testnet deploys with a mint() entrypoint. On other
	// networks the same address (if any) is just a regular ERC-20, so calling
	// mint() would revert.
	if configs.Values.IsLocal() && tokenABIPresent {
		bridge := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
		for label, ac := range map[string]*accounts.Account{"A": TestAccountA, "B": TestAccountB} {
			if err := helpers.MintAndApproveCtx(context.Background(), ac, bridge, setupMintAmount, TokenABI); err != nil {
				panic("setup mint+approve for TestAccount" + label + ": " + err.Error())
			}
		}
	}
}

func parseL2ABIs() {
	contracts := configs.Values.L2.Contracts

	// Required contracts.
	BridgeABI = mustParseABI("bridge", contracts[configs.ContractNameBridge].ABI)
	MailboxABI = mustParseABI("mailbox", contracts[configs.ContractNameMailbox].ABI)
	CetFactoryABI = mustParseABI("cet-factory", contracts[configs.ContractNameCetFactory].ABI)

	// Optional contracts.
	if cfg, ok := contracts[configs.ContractNameToken]; ok && cfg.ABI != "" {
		TokenABI = mustParseABI("token", cfg.ABI)
		tokenABIPresent = true
	}
	// ComposeL2Bridge (per-rollup) lives at two map keys with the same ABI; parse
	// once for either side.
	for _, key := range []configs.ContractName{configs.ContractNameL2BridgeA, configs.ContractNameL2BridgeB} {
		if cfg, ok := contracts[key]; ok && cfg.ABI != "" {
			ComposeL2BridgeABI = mustParseABI(string(key), cfg.ABI)
			break
		}
	}
	if cfg, ok := contracts[configs.ContractNameETHLiquidity]; ok && cfg.ABI != "" {
		ETHLiquidityABI = mustParseABI("eth-liquidity", cfg.ABI)
	}
}

func setupL1() {
	l1 := configs.Values.L1
	TestL1 = rollup.New(l1.RPCURL, big.NewInt(l1.ChainID), "l1")

	pk := configs.Values.WalletPrivateKey
	var err error
	TestL1Account, err = accounts.NewRollupAccount(pk, TestL1)
	if err != nil {
		panic("create L1 account: " + err.Error())
	}

	if cfg, ok := l1.Contracts[configs.ContractNameL1BridgeA]; ok && cfg.ABI != "" {
		ComposeL1BridgeABI = mustParseABI("compose-l1-bridge", cfg.ABI)
	}
	if cfg, ok := l1.Contracts[configs.ContractNamePortalA]; ok && cfg.ABI != "" {
		ComposePortalABI = mustParseABI("compose-portal", cfg.ABI)
	}
	if cfg, ok := l1.Contracts[configs.ContractNameDisputeGameFactory]; ok && cfg.ABI != "" {
		DisputeGameABI = mustParseABI("dispute-game-factory", cfg.ABI)
	}
}

func mustParseABI(label, source string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(source))
	if err != nil {
		panic("parse " + label + " ABI: " + err.Error())
	}
	return parsed
}

// RequireL1 fails the test if the active config has no L1 section.
func RequireL1(t *testing.T) {
	t.Helper()
	if !configs.Values.HasL1() {
		t.Fatalf("test requires l1 section in config (active network: %s)", TestNetwork)
	}
}

// RequireAA fails the test if the active config has no AA section.
func RequireAA(t *testing.T) {
	t.Helper()
	if !configs.Values.HasAA() {
		t.Fatalf("test requires l2.aa section in config (active network: %s)", TestNetwork)
	}
}

// RequireLocal fails the test if the active network is not the local-testnet.
// Used by tests that depend on MockL2ERC20 (the only mintable token).
func RequireLocal(t *testing.T) {
	t.Helper()
	if !configs.Values.IsLocal() {
		t.Fatalf("test requires network=local (active: %s)", TestNetwork)
	}
}

// RequireL2BridgePerRollup fails the test if compose-l2-bridge-rollup-{a,b}
// are missing (needed for L2->L1 withdrawals).
func RequireL2BridgePerRollup(t *testing.T) {
	t.Helper()
	if !configs.Values.HasL2BridgePerRollup() {
		t.Fatalf("test requires compose-l2-bridge-rollup-a/b in config (active network: %s)", TestNetwork)
	}
}

// RequireTSRuntime fails the test if `npx`/Node aren't on PATH. Tests that
// shell out to scripts/encode-xt.ts (rpc mode) or scripts/sa-helper.ts (SA
// flows) call this.
func RequireTSRuntime(t *testing.T) {
	t.Helper()
	if !helpers.HasTSRuntime() {
		t.Fatal("test requires npx/Node on PATH (used to invoke scripts/*.ts) — run `make scripts-install`")
	}
}

// RequireRPCMode fails the test if the active config is in sidecar mode.
func RequireRPCMode(t *testing.T) {
	t.Helper()
	if TestXTMode != configs.XTSubmissionRPC {
		t.Fatalf("test requires xt-submission=rpc (active: %s)", TestXTMode)
	}
}

// RequireSidecarMode fails the test if the active config is in rpc mode.
func RequireSidecarMode(t *testing.T) {
	t.Helper()
	if TestXTMode != configs.XTSubmissionSidecar {
		t.Fatalf("test requires xt-submission=sidecar (active: %s)", TestXTMode)
	}
}
