package accounts

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Account struct {
	privateKey *ecdsa.PrivateKey
	address    common.Address
	onRollup   *rollup.Rollup
	client     *ethclient.Client
}

// NewRollupAccount creates a new blockchain account
func NewRollupAccount(privateKeyHex string, onRollup *rollup.Rollup) (*Account, error) {
	client, err := ethclient.Dial(onRollup.RPCURL())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to blockchain: %w", err)
	}

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	address := crypto.PubkeyToAddress(privateKey.PublicKey)

	return &Account{
		privateKey: privateKey,
		address:    address,
		onRollup:   onRollup,
		client:     client,
	}, nil
}

// GetAddress returns the address derived from the private key
func (ac *Account) GetAddress() common.Address {
	return ac.address
}

// GetRollup returns the rollup associated with this account
func (ac *Account) GetRollup() *rollup.Rollup {
	return ac.onRollup
}

// GetBalance returns the balance of the account
func (ac *Account) GetBalance(ctx context.Context) (*big.Int, error) {
	address := ac.GetAddress()
	balance, err := ac.client.BalanceAt(ctx, address, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}
	return balance, nil
}

// GetNonce returns the nonce for the next transaction
func (ac *Account) GetNonce(ctx context.Context) (uint64, error) {
	address := ac.GetAddress()
	nonce, err := ac.client.PendingNonceAt(ctx, address)
	if err != nil {
		return 0, fmt.Errorf("failed to get nonce: %w", err)
	}
	logger.Info("Nonce loaded successfully for account: %s with nonce: %d", address.Hex(), nonce)
	return nonce, nil
}

func (ac *Account) GetPrivateKey() *ecdsa.PrivateKey {
	return ac.privateKey
}

// Close closes the blockchain client connection
func (ac *Account) Close() {
	if ac.client != nil {
		ac.client.Close()
	}
}

func (ac *Account) GetTokensBalance(ctx context.Context, contractAddress common.Address, contractABI abi.ABI) (*big.Int, error) {
	ownerAddr := ac.GetAddress()
	contract := bind.NewBoundContract(contractAddress, contractABI, ac.client, ac.client, ac.client)
	call := &bind.CallOpts{Context: ctx}

	var balance *big.Int
	if err := contract.Call(call, &[]interface{}{&balance}, "balanceOf", ownerAddr); err != nil {
		logger.Error("failed to get tokens balance on %s for account: %s: %w", ac.onRollup.Name(), ownerAddr.Hex(), err)
		return nil, err
	}
	logger.Info("Tokens balance loaded successfully on %s for account: %s with balance: %d", ac.onRollup.Name(), ownerAddr.Hex(), balance)

	return balance, nil
}
