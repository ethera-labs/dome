// Go port of scripts/l1-to-l2-eth-stress.ts. Spawns N deterministic accounts,
// funds each on L1 (skipping accounts that already have enough), then has
// every account bridge 0.01 ETH to the configured destination rollup. Retries
// failed bridge txs in subsequent rounds (the portal's gas metering caps
// deposits per block at ~32).
//
// Override the account count with `BRIDGE_STRESS_ACCOUNTS=<N>` (default 25).
package test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

const (
	l1StressDefaultAccounts = 25
	// Buffer over the bridge value to cover gas on the L1 portal call.
	l1StressFundBuffer = "50000000000000000" // 0.05 ETH in wei
)

var l1StressBridgeAmount = big.NewInt(10_000_000_000_000_000) // 0.01 ETH

func TestL1ToL2_ETH_Stress_RollupA(t *testing.T) {
	RequireL1(t)
	runL1ToL2ETHStress(t, configs.ChainNameRollupA, TestRollupA)
}

func TestL1ToL2_ETH_Stress_RollupB(t *testing.T) {
	RequireL1(t)
	runL1ToL2ETHStress(t, configs.ChainNameRollupB, TestRollupB)
}

func runL1ToL2ETHStress(t *testing.T, destRollup configs.ChainName, destChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()

	numAcc := l1StressDefaultAccounts
	if env := os.Getenv("BRIDGE_STRESS_ACCOUNTS"); env != "" {
		n, err := strconv.Atoi(env)
		require.NoError(t, err)
		require.Greater(t, n, 0)
		numAcc = n
	}

	bridgeAddr, err := helpers.L1BridgeAddressFor(destRollup)
	require.NoError(t, err)

	tag := fmt.Sprintf("[STRESS L1->%s ETH]", destChain.Name())
	logger.Info("%s accounts=%d bridge=%s wei each", tag, numAcc, l1StressBridgeAmount)

	// 1. Derive N accounts deterministically from the master key.
	masterPK := configs.Values.WalletPrivateKey
	accountsOnL1 := deriveL1Accounts(t, masterPK, numAcc)
	accountsOnL2 := make([]*accounts.Account, numAcc)
	for i, ac := range accountsOnL1 {
		// Same private key, dial against L2 RPC for balance reads.
		pkHex := hex.EncodeToString(crypto.FromECDSA(ac.GetPrivateKey()))
		l2Acc, err := accounts.NewRollupAccount(pkHex, destChain)
		require.NoError(t, err)
		accountsOnL2[i] = l2Acc
		_ = i
	}

	// 2. Fund accounts on L1 that don't have enough yet.
	buf, _ := new(big.Int).SetString(l1StressFundBuffer, 10)
	minRequired := new(big.Int).Add(l1StressBridgeAmount, buf)
	fundAmount := new(big.Int).Mul(l1StressBridgeAmount, big.NewInt(10)) // 0.1 ETH
	var needFunding []*accounts.Account
	for _, ac := range accountsOnL1 {
		bal, err := ac.GetBalance(ctx)
		require.NoError(t, err)
		if bal.Cmp(minRequired) < 0 {
			needFunding = append(needFunding, ac)
		}
	}
	if len(needFunding) > 0 {
		logger.Info("%s funding %d/%d accounts with %s wei each", tag, len(needFunding), numAcc, fundAmount)
		require.NoError(t, transactions.DistributeEth(ctx, TestL1Account, needFunding, fundAmount))
	} else {
		logger.Info("%s all %d accounts already funded", tag, numAcc)
	}

	// 3. Snapshot L2 balances.
	l2BalsBefore := make([]*big.Int, numAcc)
	for i, ac := range accountsOnL2 {
		bal, err := ac.GetBalance(ctx)
		require.NoError(t, err)
		l2BalsBefore[i] = bal
	}

	// 4. Bridge from each account on L1. Submit in parallel, then retry the
	//    ones that fail (portal gas metering caps per-block deposits).
	const maxRetries = 10
	const retryDelay = 15 * time.Second
	bridgeTxs := make([]*types.Transaction, numAcc)
	succeeded := make([]bool, numAcc)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var wg sync.WaitGroup
		for i, ac := range accountsOnL1 {
			if succeeded[i] {
				continue
			}
			wg.Add(1)
			go func(i int, ac *accounts.Account) {
				defer wg.Done()
				calldata, err := helpers.PackBridgeETHTo(ComposeL1BridgeABI,
					ac.GetAddress(), uint32(helpers.L1MinGasLimitETH), []byte{})
				if err != nil {
					logger.Error("%s acct %d pack: %v", tag, i, err)
					return
				}
				tx, _, err := helpers.SendL1Tx(ctx, ac, bridgeAddr, l1StressBridgeAmount, helpers.L1BridgeGasLimit, calldata)
				if err != nil {
					logger.Debug("%s acct %d attempt %d failed: %v", tag, i, attempt, err)
					return
				}
				bridgeTxs[i] = tx
				succeeded[i] = true
			}(i, ac)
		}
		wg.Wait()

		successCount := 0
		for _, s := range succeeded {
			if s {
				successCount++
			}
		}
		logger.Info("%s attempt %d: %d/%d succeeded", tag, attempt, successCount, numAcc)
		if successCount == numAcc {
			break
		}
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}
	for i, s := range succeeded {
		require.Truef(t, s, "account %d (%s) failed all bridge retries",
			i, accountsOnL1[i].GetAddress().Hex())
	}

	// 5. Poll L2 — each account's balance should increase by exactly l1StressBridgeAmount.
	for i, ac := range accountsOnL2 {
		final, err := helpers.WaitForETHBalanceChange(ctx, destChain.RPCURL(), ac.GetAddress(),
			helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
			func(cur *big.Int) bool { return new(big.Int).Sub(cur, l2BalsBefore[i]).Cmp(l1StressBridgeAmount) >= 0 })
		require.NoErrorf(t, err, "account %d (%s): L2 balance never reached +%s",
			i, ac.GetAddress().Hex(), l1StressBridgeAmount)
		got := new(big.Int).Sub(final, l2BalsBefore[i])
		require.Equalf(t, 0, got.Cmp(l1StressBridgeAmount),
			"account %d (%s): got=%s want=%s", i, ac.GetAddress().Hex(), got, l1StressBridgeAmount)
	}
}

// deriveL1Accounts mirrors the TS deriveAccounts: keccak256(masterPK ++ i)
// gives a deterministic private key per account.
func deriveL1Accounts(t *testing.T, masterPK string, count int) []*accounts.Account {
	t.Helper()
	master, err := hex.DecodeString(masterPK)
	require.NoError(t, err)

	out := make([]*accounts.Account, count)
	for i := range count {
		seed := make([]byte, 32+32)
		copy(seed[:32], master)
		idx := new(big.Int).SetInt64(int64(i)).Bytes()
		copy(seed[64-len(idx):], idx)
		derived := crypto.Keccak256(seed)

		ac, err := accounts.NewRollupAccount(hex.EncodeToString(derived), TestL1)
		require.NoError(t, err)
		out[i] = ac
	}
	return out
}

// _ keeps the common import used by tests below alive.
var _ = common.Address{}
