package test

import (
	"context"
	"math/big"
	"sync"
	"testing"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

// TestConcurrentXTsSameNonce submits two XTs with identical nonces concurrently.
// Duplicate submissions may be accepted idempotently at the API layer, but they
// must not consume more than one nonce reservation.
func TestConcurrentXTsSameNonce(t *testing.T) {
	ctx := t.Context()
	accountA, accountB := setupAccountsNoApprovals(t)
	ensureXtEndpoint(t)

	nonceA, err := accountA.GetNonce(ctx)
	if err != nil {
		t.Fatalf("failed to get nonce for account A: %v", err)
	}
	nonceB, err := accountB.GetNonce(ctx)
	if err != nil {
		t.Fatalf("failed to get nonce for account B: %v", err)
	}

	amount := big.NewInt(0)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	submit := func() {
		defer wg.Done()
		errs <- sendSimpleXtWithNonce(ctx, accountA, accountB, nonceA, nonceB, amount)
	}

	wg.Add(2)
	go submit()
	go submit()
	wg.Wait()
	close(errs)

	var success, failed int
	for err := range errs {
		if err == nil {
			success++
		} else {
			failed++
		}
	}

	if success < 1 || success > 2 || failed > 1 {
		t.Fatalf("expected duplicate submit to be accepted at least once without consuming multiple reservations, got %d success / %d failure", success, failed)
	}

	nextNonceA, err := accountA.GetNonce(ctx)
	if err != nil {
		t.Fatalf("failed to get nonce for account A after duplicate XT submit: %v", err)
	}
	nextNonceB, err := accountB.GetNonce(ctx)
	if err != nil {
		t.Fatalf("failed to get nonce for account B after duplicate XT submit: %v", err)
	}

	if nextNonceA != nonceA+1 || nextNonceB != nonceB+1 {
		t.Fatalf("duplicate XT advanced pending nonce incorrectly: A %d->%d, B %d->%d", nonceA, nextNonceA, nonceB, nextNonceB)
	}
}

// TestSequentialXtNonceReservation submits one XT and immediately builds a second XT
// using the next PendingNonce. The pending nonce should advance synchronously
// if the sidecar successfully reserves the XT in op-rbuilder before responding.
func TestSequentialXtNonceReservation(t *testing.T) {
	ctx := t.Context()
	accountA, accountB := setupAccountsNoApprovals(t)
	ensureXtEndpoint(t)

	nonceA1, err := accountA.GetNonce(ctx)
	if err != nil {
		t.Fatalf("failed to get nonce for account A: %v", err)
	}
	nonceB1, err := accountB.GetNonce(ctx)
	if err != nil {
		t.Fatalf("failed to get nonce for account B: %v", err)
	}

	amount := big.NewInt(0)
	if err := sendSimpleXtWithNonce(ctx, accountA, accountB, nonceA1, nonceB1, amount); err != nil {
		t.Fatalf("first XT submission failed: %v", err)
	}

	nonceA2, err := accountA.GetNonce(ctx)
	if err != nil {
		t.Fatalf("failed to get nonce for account A after first XT: %v", err)
	}
	nonceB2, err := accountB.GetNonce(ctx)
	if err != nil {
		t.Fatalf("failed to get nonce for account B after first XT: %v", err)
	}

	if nonceA2 != nonceA1+1 || nonceB2 != nonceB1+1 {
		t.Fatalf("pending nonce did not advance after XT submit: A %d->%d, B %d->%d", nonceA1, nonceA2, nonceB1, nonceB2)
	}

	if err := sendSimpleXtWithNonce(ctx, accountA, accountB, nonceA2, nonceB2, amount); err != nil {
		t.Fatalf("second XT submission failed: %v", err)
	}
}

func setupAccountsNoApprovals(t *testing.T) (*accounts.Account, *accounts.Account) {
	t.Helper()

	chainConfigs := configs.Values.L2.ChainConfigs
	rollupA := rollup.New(
		chainConfigs[configs.ChainNameRollupA].RPCURL,
		big.NewInt(chainConfigs[configs.ChainNameRollupA].ID),
		string(configs.ChainNameRollupA),
	)
	rollupB := rollup.New(
		chainConfigs[configs.ChainNameRollupB].RPCURL,
		big.NewInt(chainConfigs[configs.ChainNameRollupB].ID),
		string(configs.ChainNameRollupB),
	)

	accountA, err := accounts.NewRollupAccount(chainConfigs[configs.ChainNameRollupA].PK, rollupA)
	if err != nil {
		t.Fatalf("failed to create account A: %v", err)
	}
	accountB, err := accounts.NewRollupAccount(chainConfigs[configs.ChainNameRollupB].PK, rollupB)
	if err != nil {
		t.Fatalf("failed to create account B: %v", err)
	}

	return accountA, accountB
}

func sendSimpleXtWithNonce(
	ctx context.Context,
	ac1 *accounts.Account,
	ac2 *accounts.Account,
	ac1Nonce uint64,
	ac2Nonce uint64,
	amount *big.Int,
) error {
	txA, signedA, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
		To:        ac1.GetAddress(),
		Value:     amount,
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
	}, ac1, ac1Nonce)
	if err != nil {
		_ = txA
		return err
	}
	_ = txA

	txB, signedB, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
		To:        ac2.GetAddress(),
		Value:     amount,
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
	}, ac2, ac2Nonce)
	if err != nil {
		_ = txB
		return err
	}
	_ = txB

	_, err = submitSignedXT(ctx, ac1, ac2, signedA, signedB)
	return err
}
