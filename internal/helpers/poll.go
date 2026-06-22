package helpers

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/ethera-labs/dome/internal/logger"
)

// Default polling cadence, matching the 60 polls @ 10s used across the TS scripts.
const (
	DefaultPollInterval = 10 * time.Second
	DefaultPollAttempts = 60
)

// PollUntil calls `fn` every `interval` until it returns (true, nil) or the
// total attempts run out. It also respects ctx cancellation. Final error
// includes the last error returned by `fn` if any.
func PollUntil(ctx context.Context, interval time.Duration, attempts int, fn func() (bool, error)) error {
	var lastErr error
	for i := 1; i <= attempts; i++ {
		done, err := fn()
		if err != nil {
			lastErr = err
		}
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while polling: %w", ctx.Err())
		case <-time.After(interval):
		}
		_ = i
	}
	if lastErr != nil {
		return fmt.Errorf("polling timed out after %d attempts: last error: %w", attempts, lastErr)
	}
	return fmt.Errorf("polling timed out after %d attempts", attempts)
}

// WaitForETHBalanceChange polls the given address on rpcURL until predicate
// (called with the current balance) returns true.
func WaitForETHBalanceChange(
	ctx context.Context,
	rpcURL string,
	address common.Address,
	interval time.Duration,
	attempts int,
	predicate func(current *big.Int) bool,
) (*big.Int, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", rpcURL, err)
	}
	defer client.Close()

	var latest *big.Int
	err = PollUntil(ctx, interval, attempts, func() (bool, error) {
		bal, err := client.BalanceAt(ctx, address, nil)
		if err != nil {
			return false, err
		}
		latest = bal
		return predicate(bal), nil
	})
	if err != nil {
		return latest, err
	}
	return latest, nil
}

// AssertContractDeployed reads eth_getCode at `addr` on `rpcURL` and returns
// nil if there is non-empty bytecode, else an error. Tests call this to
// distinguish "bridge silently failed to deploy CET" from "bridge moved the
// wrong amount".
func AssertContractDeployed(ctx context.Context, rpcURL string, addr common.Address) error {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("dial %s: %w", rpcURL, err)
	}
	defer client.Close()
	code, err := client.CodeAt(ctx, addr, nil)
	if err != nil {
		return fmt.Errorf("getCode(%s): %w", addr.Hex(), err)
	}
	if len(code) == 0 {
		return fmt.Errorf("no code deployed at %s", addr.Hex())
	}
	return nil
}

// WaitForTokenBalanceChange polls an ERC-20 balance via the given ABI's
// balanceOf method. `tokenABI` must contain a balanceOf(address) entry.
func WaitForTokenBalanceChange(
	ctx context.Context,
	rpcURL string,
	token common.Address,
	owner common.Address,
	tokenABI abi.ABI,
	interval time.Duration,
	attempts int,
	predicate func(current *big.Int) bool,
) (*big.Int, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", rpcURL, err)
	}
	defer client.Close()

	contract := bind.NewBoundContract(token, tokenABI, client, client, client)
	var latest *big.Int
	err = PollUntil(ctx, interval, attempts, func() (bool, error) {
		var bal *big.Int
		if err := contract.Call(&bind.CallOpts{Context: ctx}, &[]any{&bal}, "balanceOf", owner); err != nil {
			// The CET contract may not be deployed yet — treat as zero balance.
			logger.Debug("balanceOf(%s) failed (assuming 0): %v", owner.Hex(), err)
			bal = big.NewInt(0)
		}
		latest = bal
		return predicate(bal), nil
	})
	if err != nil {
		return latest, err
	}
	return latest, nil
}
