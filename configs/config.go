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

	ChainNameRollupA ChainName = "rollup-a"
	ChainNameRollupB ChainName = "rollup-b"

	ContractNameBridge     ContractName = "bridge"
	ContractNameToken      ContractName = "token"
	ContractNameMailbox    ContractName = "mailbox"
	ContractNameCetFactory ContractName = "cet-factory"
)

type (
	ChainName    string
	ContractName string

	App struct {
		L2 L2 `yaml:"l2"`
	}
	L2 struct {
		SidecarURL   string                          `yaml:"sidecar-url"`
		ChainConfigs map[ChainName]ChainConfig       `yaml:"chain-configs"`
		Contracts    map[ContractName]ContractConfig `yaml:"contracts"`
	}
	ChainConfig struct {
		ID     int64  `yaml:"id"`
		RPCURL string `yaml:"rpc-url"`
		PK     string `yaml:"pk"`
	}

	ContractConfig struct {
		Address common.Address `yaml:"address"`
		ABI     string         `yaml:"abi"`
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

	Values.normalizePrivateKeys()

	if err := Values.validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	bridgeABILen := len(Values.L2.Contracts[ContractNameBridge].ABI)
	tokenABILen := len(Values.L2.Contracts[ContractNameToken].ABI)
	mailboxABILen := len(Values.L2.Contracts[ContractNameMailbox].ABI)

	logger.Info("configuration loaded: sidecar=%s rollup-a=%d(%s) rollup-b=%d(%s)",
		Values.L2.SidecarURL,
		Values.L2.ChainConfigs[ChainNameRollupA].ID, Values.L2.ChainConfigs[ChainNameRollupA].RPCURL,
		Values.L2.ChainConfigs[ChainNameRollupB].ID, Values.L2.ChainConfigs[ChainNameRollupB].RPCURL)
	logger.Info("contracts: bridge=%s(%d bytes) token=%s(%d bytes) mailbox=%s(%d bytes)",
		Values.L2.Contracts[ContractNameBridge].Address.Hex(), bridgeABILen,
		Values.L2.Contracts[ContractNameToken].Address.Hex(), tokenABILen,
		Values.L2.Contracts[ContractNameMailbox].Address.Hex(), mailboxABILen)
	return nil
}

func (a *App) validate() error {
	var err error

	if a.L2.SidecarURL == "" {
		err = errors.Join(err, fmt.Errorf("field: 'sidecar-url' must be set and non-empty"))
	}

	if chainErr := a.validateChainConfig(); chainErr != nil {
		err = errors.Join(err, chainErr)
	}

	if contractsErr := a.validateContractsConfig(); contractsErr != nil {
		err = errors.Join(err, contractsErr)
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

func (a *App) validateContractsConfig() error {
	var err error
	if len(a.L2.Contracts) != 4 {
		err = errors.Join(err, fmt.Errorf("exactly four contract configs must be provided"))
	}
	if _, ok := a.L2.Contracts[ContractNameBridge]; !ok {
		err = errors.Join(err, fmt.Errorf("contract config for '%s' must be provided", ContractNameBridge))
	}
	if _, ok := a.L2.Contracts[ContractNameToken]; !ok {
		err = errors.Join(err, fmt.Errorf("contract config for '%s' must be provided", ContractNameToken))
	}
	if _, ok := a.L2.Contracts[ContractNameMailbox]; !ok {
		err = errors.Join(err, fmt.Errorf("contract config for '%s' must be provided", ContractNameMailbox))
	}
	if _, ok := a.L2.Contracts[ContractNameCetFactory]; !ok {
		err = errors.Join(err, fmt.Errorf("contract config for '%s' must be provided", ContractNameCetFactory))
	}

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

func stripHexPrefix(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
}

func (a *App) normalizePrivateKeys() {
	for name, cfg := range a.L2.ChainConfigs {
		cfg.PK = stripHexPrefix(cfg.PK)
		a.L2.ChainConfigs[name] = cfg
	}
}
