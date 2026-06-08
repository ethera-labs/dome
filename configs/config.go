package configs

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

var (
	//go:embed config.yaml
	embeddedConfig []byte
	Values         App
)

const (
	configPathEnvVar = "CONFIG_PATH"

	NetworkLocal        = "local"
	NetworkHoodi        = "hoodi"
	NetworkSepoliaProd  = "sepolia-prod"
	NetworkSepoliaStage = "sepolia-stage"

	XTSubmissionSidecar = "sidecar"
	XTSubmissionRPC     = "rpc"

	ChainNameRollupA ChainName = "rollup-a"
	ChainNameRollupB ChainName = "rollup-b"

	// L2 contracts.
	ContractNameBridge        ContractName = "bridge"      // ComposeL2ToL2Bridge
	ContractNameToken         ContractName = "token"       // MockL2ERC20 (local only)
	ContractNameMailbox       ContractName = "mailbox"     // UniversalBridgeMailbox
	ContractNameCetFactory    ContractName = "cet-factory" // CetFactory
	ContractNameL2BridgeA     ContractName = "compose-l2-bridge-rollup-a"
	ContractNameL2BridgeB     ContractName = "compose-l2-bridge-rollup-b"
	ContractNameETHLiquidity  ContractName = "eth-liquidity"

	// L1 contracts.
	ContractNameL1BridgeA          ContractName = "compose-l1-bridge-rollup-a"
	ContractNameL1BridgeB          ContractName = "compose-l1-bridge-rollup-b"
	ContractNamePortalA            ContractName = "compose-portal-rollup-a"
	ContractNamePortalB            ContractName = "compose-portal-rollup-b"
	ContractNameDisputeGameFactory ContractName = "dispute-game-factory"
	ContractNameETHLockbox         ContractName = "eth-lockbox"
	ContractNameERC20Lockbox       ContractName = "erc20-lockbox"
)

type (
	ChainName    string
	ContractName string

	App struct {
		Network          string `yaml:"network"`
		XTSubmission     string `yaml:"xt-submission"`
		WalletPrivateKey string `yaml:"wallet-private-key"`
		L1               *L1    `yaml:"l1,omitempty"`
		L2               L2     `yaml:"l2"`
	}

	L1 struct {
		RPCURL    string                          `yaml:"rpc-url"`
		ChainID   int64                           `yaml:"chain-id"`
		Contracts map[ContractName]ContractConfig `yaml:"contracts"`
	}

	L2 struct {
		SidecarURL   string                          `yaml:"sidecar-url"`
		ChainConfigs map[ChainName]ChainConfig       `yaml:"chain-configs"`
		Contracts    map[ContractName]ContractConfig `yaml:"contracts"`
		AA           *AAContracts                    `yaml:"aa,omitempty"`
	}

	ChainConfig struct {
		ID         int64  `yaml:"id"`
		RPCURL     string `yaml:"rpc-url"`
		PK         string `yaml:"pk"`
		BundlerURL string `yaml:"bundler-url,omitempty"`
	}

	ContractConfig struct {
		Address common.Address `yaml:"address"`
		ABI     string         `yaml:"abi"`
	}

	AAContracts struct {
		KernelImpl          common.Address `yaml:"kernel-impl"`
		KernelFactory       common.Address `yaml:"kernel-factory"`
		MultichainValidator common.Address `yaml:"multichain-validator"`
	}
)

func init() {
	configPath, isSet := os.LookupEnv(configPathEnvVar)
	if !isSet {
		logger.Info("%s was not set, will use configuration values from embedded config.yaml", configPathEnvVar)
		if err := loadConfig(embeddedConfig); err != nil {
			panic(err.Error())
		}
		return
	}

	logger.Info("%s environment variable set to: %s. Loading configuration", configPathEnvVar, configPath)
	data, err := os.ReadFile(configPath)
	if err != nil {
		panic(fmt.Errorf("failed to read config file %s: %w", configPath, err))
	}

	if err := loadConfig(data); err != nil {
		logger.Info("failed to load external config (%v), falling back to embedded config", err)
		panic(err.Error())
	}
}

func loadConfig(data []byte) error {
	if err := yaml.Unmarshal(data, &Values); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	Values.applyDefaults()
	Values.normalizePrivateKeys()

	if err := Values.validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	logger.Info("configuration loaded: network=%s xt-submission=%s sidecar=%s rollup-a=%d(%s) rollup-b=%d(%s) l1=%v aa=%v",
		Values.Network, Values.XTSubmission, Values.L2.SidecarURL,
		Values.L2.ChainConfigs[ChainNameRollupA].ID, Values.L2.ChainConfigs[ChainNameRollupA].RPCURL,
		Values.L2.ChainConfigs[ChainNameRollupB].ID, Values.L2.ChainConfigs[ChainNameRollupB].RPCURL,
		Values.HasL1(), Values.HasAA())
	return nil
}

// applyDefaults fills in fields the local-testnet YAML omits so a config from
// before the multi-network rewrite still loads.
func (a *App) applyDefaults() {
	if a.Network == "" {
		a.Network = NetworkLocal
	}
	if a.XTSubmission == "" {
		// Local-testnet always used the sidecar; preserve that on configs that
		// don't set the field.
		a.XTSubmission = XTSubmissionSidecar
	}
	if a.WalletPrivateKey == "" {
		// Back-compat: pre-rewrite configs put the PK under l2.chain-configs.
		if cfg, ok := a.L2.ChainConfigs[ChainNameRollupA]; ok && cfg.PK != "" {
			a.WalletPrivateKey = cfg.PK
		}
	}
	// Propagate the top-level wallet PK to chain configs that don't set their own.
	for name, cfg := range a.L2.ChainConfigs {
		if cfg.PK == "" {
			cfg.PK = a.WalletPrivateKey
			a.L2.ChainConfigs[name] = cfg
		}
	}
}

func (a *App) validate() error {
	var err error

	switch a.Network {
	case NetworkLocal, NetworkHoodi, NetworkSepoliaProd, NetworkSepoliaStage:
	default:
		err = errors.Join(err, fmt.Errorf("field: 'network' must be one of local|hoodi|sepolia-prod|sepolia-stage, got %q", a.Network))
	}

	switch a.XTSubmission {
	case XTSubmissionSidecar:
		if a.L2.SidecarURL == "" {
			err = errors.Join(err, fmt.Errorf("xt-submission=sidecar requires l2.sidecar-url to be set"))
		}
	case XTSubmissionRPC:
		// RPC mode submits via the source rollup's eth_sendXTransaction.
		// sidecar-url is allowed to be empty.
	default:
		err = errors.Join(err, fmt.Errorf("field: 'xt-submission' must be 'sidecar' or 'rpc', got %q", a.XTSubmission))
	}

	if a.WalletPrivateKey == "" {
		err = errors.Join(err, fmt.Errorf("field: 'wallet-private-key' must be set (or l2.chain-configs.rollup-a.pk for back-compat)"))
	}

	if chainErr := a.validateChainConfig(); chainErr != nil {
		err = errors.Join(err, chainErr)
	}

	if contractsErr := a.validateL2Contracts(); contractsErr != nil {
		err = errors.Join(err, contractsErr)
	}

	if a.L1 != nil {
		if l1Err := a.validateL1(); l1Err != nil {
			err = errors.Join(err, l1Err)
		}
	}

	if a.L2.AA != nil {
		if aaErr := a.validateAA(); aaErr != nil {
			err = errors.Join(err, aaErr)
		}
	}

	return err
}

func (a *App) validateChainConfig() error {
	var err error
	if len(a.L2.ChainConfigs) != 2 {
		err = errors.Join(err, fmt.Errorf("exactly two chain configs must be provided"))
	}
	if _, ok := a.L2.ChainConfigs[ChainNameRollupA]; !ok {
		err = errors.Join(err, fmt.Errorf("chain config for '%s' must be provided", ChainNameRollupA))
	}
	if _, ok := a.L2.ChainConfigs[ChainNameRollupB]; !ok {
		err = errors.Join(err, fmt.Errorf("chain config for '%s' must be provided", ChainNameRollupB))
	}

	for name, cfg := range a.L2.ChainConfigs {
		if cfg.ID == 0 {
			err = errors.Join(err, fmt.Errorf("field: 'id', chain: '%s', must be set and non-zero", name))
		}
		if cfg.RPCURL == "" {
			err = errors.Join(err, fmt.Errorf("field: 'rpc-url', chain: '%s', must be set and non-zero", name))
		}
		if cfg.PK == "" {
			err = errors.Join(err, fmt.Errorf("field: 'pk', chain: '%s', must be set and non-zero", name))
		}
	}

	return err
}

func (a *App) validateL2Contracts() error {
	var err error
	// `bridge`, `mailbox`, and `cet-factory` are required on every network.
	// `token` is only present on local-testnet. The other entries (compose-l2-
	// bridge-rollup-{a,b}, eth-liquidity) are required only when L2->L1 / SA
	// tests run; those tests skip if the entry is missing.
	required := []ContractName{ContractNameBridge, ContractNameMailbox, ContractNameCetFactory}
	for _, name := range required {
		cfg, ok := a.L2.Contracts[name]
		if !ok {
			err = errors.Join(err, fmt.Errorf("contract config for '%s' must be provided", name))
			continue
		}
		if cfg.Address == (common.Address{}) {
			err = errors.Join(err, fmt.Errorf("field: 'address', contract: '%s', must be set and non-zero", name))
		}
		if cfg.ABI == "" {
			err = errors.Join(err, fmt.Errorf("field: 'abi', contract: '%s', must be set and non-empty", name))
		}
	}

	// Any other contracts present in the map must have address + ABI populated.
	for name, cfg := range a.L2.Contracts {
		if cfg.Address == (common.Address{}) {
			err = errors.Join(err, fmt.Errorf("field: 'address', contract: '%s', must be set and non-zero", name))
		}
		if cfg.ABI == "" {
			err = errors.Join(err, fmt.Errorf("field: 'abi', contract: '%s', must be set and non-empty", name))
		}
	}

	return err
}

func (a *App) validateL1() error {
	var err error
	if a.L1.RPCURL == "" {
		err = errors.Join(err, fmt.Errorf("field: 'l1.rpc-url' must be set"))
	}
	if a.L1.ChainID == 0 {
		err = errors.Join(err, fmt.Errorf("field: 'l1.chain-id' must be set and non-zero"))
	}
	for name, cfg := range a.L1.Contracts {
		if cfg.Address == (common.Address{}) {
			err = errors.Join(err, fmt.Errorf("field: 'address', l1 contract: '%s', must be set and non-zero", name))
		}
		if cfg.ABI == "" {
			err = errors.Join(err, fmt.Errorf("field: 'abi', l1 contract: '%s', must be set and non-empty", name))
		}
	}
	return err
}

func (a *App) validateAA() error {
	var err error
	if a.L2.AA.KernelImpl == (common.Address{}) {
		err = errors.Join(err, fmt.Errorf("field: 'l2.aa.kernel-impl' must be set"))
	}
	if a.L2.AA.KernelFactory == (common.Address{}) {
		err = errors.Join(err, fmt.Errorf("field: 'l2.aa.kernel-factory' must be set"))
	}
	if a.L2.AA.MultichainValidator == (common.Address{}) {
		err = errors.Join(err, fmt.Errorf("field: 'l2.aa.multichain-validator' must be set"))
	}
	return err
}

// IsLocal reports whether the active config targets the local-testnet.
func (a *App) IsLocal() bool { return a.Network == NetworkLocal }

// HasL1 reports whether L1 RPC + contract config is available.
func (a *App) HasL1() bool { return a.L1 != nil && a.L1.RPCURL != "" }

// HasAA reports whether account-abstraction config is available.
func (a *App) HasAA() bool { return a.L2.AA != nil }

// HasL2BridgePerRollup reports whether the per-rollup ComposeL2Bridge contracts
// (needed for L2->L1 withdrawals) are present.
func (a *App) HasL2BridgePerRollup() bool {
	_, okA := a.L2.Contracts[ContractNameL2BridgeA]
	_, okB := a.L2.Contracts[ContractNameL2BridgeB]
	return okA && okB
}

// HasBundlers reports whether per-rollup bundler URLs are configured (sepolia-stage).
func (a *App) HasBundlers() bool {
	a1, okA := a.L2.ChainConfigs[ChainNameRollupA]
	a2, okB := a.L2.ChainConfigs[ChainNameRollupB]
	return okA && okB && a1.BundlerURL != "" && a2.BundlerURL != ""
}

func stripHexPrefix(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
}

func (a *App) normalizePrivateKeys() {
	a.WalletPrivateKey = stripHexPrefix(a.WalletPrivateKey)
	for name, cfg := range a.L2.ChainConfigs {
		cfg.PK = stripHexPrefix(cfg.PK)
		a.L2.ChainConfigs[name] = cfg
	}
}
