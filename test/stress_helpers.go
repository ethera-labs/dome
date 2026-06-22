package test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
)

// runConcurrentTxRounds submits one tx per account in parallel, waits for each
// receipt with a generous budget, and re-runs buildTx for any account whose tx
// reverted on-chain (typical for OP-portal bridge calls when the per-L1-block
// deposit gas cap is hit). Accounts that succeed in any round are not retried.
//
// IMPORTANT: do NOT retry on "receipt not found" — that just means the tx is
// still in the mempool, and resubmitting a fresh tx while the old one is
// pending races against the account's reserved-balance check and produces
// self-inflicted "insufficient funds" errors. Only on-chain reverts trigger a
// retry; transient submission errors get a short inner retry loop.
//
// buildTx is called once per pending account per round; it must return a
// fully-signed tx ready to send (use transactions.CreateTransaction). The
// helper writes the confirmed tx into the returned slice at the account's
// index, or the last error if the account never confirmed.
func runConcurrentTxRounds(
	ctx context.Context,
	accountsForItems []*accounts.Account,
	buildTx func(i int, ac *accounts.Account) (*types.Transaction, error),
	tag string,
	maxRounds int,
) ([]*types.Transaction, []error) {
	const (
		sendRetries    = 3
		sendRetryDelay = 5 * time.Second
		roundDelay     = 15 * time.Second // ≥ 1 L1 block, spreads submissions
		receiptRetries = 500              // 500 × 600ms = 5min per tx
	)

	n := len(accountsForItems)
	txs := make([]*types.Transaction, n)
	errs := make([]error, n)

	for round := 0; round <= maxRounds; round++ {
		var wg sync.WaitGroup
		for i, ac := range accountsForItems {
			if txs[i] != nil {
				continue // already confirmed in a previous round
			}
			wg.Add(1)
			go func(i int, ac *accounts.Account) {
				defer wg.Done()

				tx, err := buildTx(i, ac)
				if err != nil {
					errs[i] = fmt.Errorf("build: %w", err)
					return
				}

				var sendErr error
				for r := 0; r < sendRetries; r++ {
					if _, sendErr = transactions.SendTransaction(ctx, tx, ac.GetRollup().RPCURL()); sendErr == nil {
						break
					}
					if r < sendRetries-1 {
						time.Sleep(sendRetryDelay)
					}
				}
				if sendErr != nil {
					errs[i] = fmt.Errorf("send: %w", sendErr)
					return
				}

				_, receipt, err := transactions.GetTransactionDetailsWithRetries(ctx, tx.Hash(), ac.GetRollup(), receiptRetries)
				if err != nil {
					errs[i] = fmt.Errorf("receipt: %w", err)
					return
				}
				if receipt.Status != types.ReceiptStatusSuccessful {
					errs[i] = fmt.Errorf("reverted: %s (round %d)", tx.Hash().Hex(), round)
					return
				}

				txs[i] = tx
				errs[i] = nil // clear stale error from a previous round
			}(i, ac)
		}
		wg.Wait()

		confirmed := countNonNilTx(txs)
		remaining := n - confirmed
		logger.Info("%s round %d: %d/%d confirmed (%d remaining)", tag, round, confirmed, n, remaining)
		if confirmed == n {
			break
		}
		if round < maxRounds {
			time.Sleep(roundDelay)
		}
	}

	return txs, errs
}

func countNonNilTx(txs []*types.Transaction) int {
	n := 0
	for _, t := range txs {
		if t != nil {
			n++
		}
	}
	return n
}
