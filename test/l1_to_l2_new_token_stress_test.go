// Go port of scripts/l1-to-l2-new-token-stress.ts. Deploys a single L1 token,
// mints 100 to each of N accounts, then bridges. The first account uses the
// CET-deploy gas budget; the rest reuse the deployed CET with normal gas.
package test

import (
	"fmt"
	"math/big"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

func TestL1ToL2_NewToken_Stress_RollupA(t *testing.T) {
	RequireL1(t)
	runL1ToL2NewTokenStress(t, configs.ChainNameRollupA, TestRollupA)
}

func TestL1ToL2_NewToken_Stress_RollupB(t *testing.T) {
	RequireL1(t)
	runL1ToL2NewTokenStress(t, configs.ChainNameRollupB, TestRollupB)
}

func runL1ToL2NewTokenStress(t *testing.T, destRollup configs.ChainName, destChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()
	bridgeAmt := helpers.ParseBridgeAmountOverride(l1ToL2TokenBridgeAmount)

	numAcc := l1StressDefaultAccounts
	if env := os.Getenv("BRIDGE_STRESS_ACCOUNTS"); env != "" {
		n, err := strconv.Atoi(env)
		require.NoError(t, err)
		require.Greater(t, n, 0)
		numAcc = n
	}

	bridgeAddr, err := helpers.L1BridgeAddressFor(destRollup)
	require.NoError(t, err)

	tag := fmt.Sprintf("[STRESS L1->%s NewToken]", destChain.Name())
	logger.Info("%s accounts=%d", tag, numAcc)

	// 1. Deploy a single StressToken on L1 from the funder. All accounts use it.
	tokenAddr, _, err := helpers.DeployMintableToken(ctx, TestL1Account, "StressToken", "STRS", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	logger.Info("%s token deployed at %s", tag, tokenAddr.Hex())

	// 2. Predict CET on L2.
	cetAddr, err := helpers.PredictCetAddress(ctx, destChain, CetFactoryABI, tokenAddr, TestL1.ChainID())
	require.NoError(t, err)
	logger.Info("%s predicted CET on %s = %s", tag, destChain.Name(), cetAddr.Hex())

	extraData, err := helpers.EncodeERC20ExtraData("StressToken", "STRS", 18)
	require.NoError(t, err)

	// 3. Derive accounts + fund.
	accountsOnL1 := deriveL1Accounts(t, configs.Values.WalletPrivateKey, numAcc)

	buf, _ := new(big.Int).SetString(l1StressFundBuffer, 10)
	minRequired := buf
	fundAmount := new(big.Int).Mul(buf, big.NewInt(2)) // 0.1 ETH each
	var needFunding []*accounts.Account
	for _, ac := range accountsOnL1 {
		bal, err := ac.GetBalance(ctx)
		require.NoError(t, err)
		if bal.Cmp(minRequired) < 0 {
			needFunding = append(needFunding, ac)
		}
	}
	if len(needFunding) > 0 {
		logger.Info("%s funding %d accounts with %s wei each", tag, len(needFunding), fundAmount)
		require.NoError(t, transactions.DistributeEth(ctx, TestL1Account, needFunding, fundAmount))
	}

	// 4. From the funder, mint 100 to each derived account on L1. Sequential
	//    so we don't blow nonces.
	for i, ac := range accountsOnL1 {
		mintData, err := tokenABI.Pack("mint", ac.GetAddress(), bridgeAmt)
		require.NoError(t, err)
		_, _, err = helpers.SendL1Tx(ctx, TestL1Account, tokenAddr, big.NewInt(0), helpers.GasMint, mintData)
		require.NoError(t, err)
		logger.Info("%s minted 100 to account %d (%s)", tag, i, ac.GetAddress().Hex())
	}

	// 5. Each account approves the bridge — concurrent OK, different nonces.
	var approveWg sync.WaitGroup
	for i, ac := range accountsOnL1 {
		approveWg.Add(1)
		go func(i int, ac *accounts.Account) {
			defer approveWg.Done()
			approveData, err := tokenABI.Pack("approve", bridgeAddr, bridgeAmt)
			require.NoError(t, err)
			_, _, err = helpers.SendL1Tx(ctx, ac, tokenAddr, big.NewInt(0), helpers.GasApprove, approveData)
			require.NoError(t, err)
		}(i, ac)
	}
	approveWg.Wait()

	// 6. Bridge: account 0 first (CET-deploy gas), then the rest concurrent.
	bridgeFunc := func(ac *accounts.Account, minGas uint32) error {
		data, err := helpers.PackBridgeERC20ToL1(ComposeL1BridgeABI,
			tokenAddr, cetAddr, ac.GetAddress(),
			bridgeAmt, minGas, extraData)
		if err != nil {
			return err
		}
		_, _, err = helpers.SendL1Tx(ctx, ac, bridgeAddr, big.NewInt(0), helpers.L1BridgeGasLimit, data)
		return err
	}

	logger.Info("%s account 0 bridging (CET-deploy gas)…", tag)
	require.NoError(t, bridgeFunc(accountsOnL1[0], uint32(helpers.L1MinGasLimitNewToken)))

	// Wait for CET on L2 — sanity, also avoids the rest racing the deploy.
	_, err = helpers.WaitForTokenBalanceChange(ctx, destChain.RPCURL(),
		cetAddr, accountsOnL1[0].GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(big.NewInt(0)) > 0 })
	require.NoError(t, err, "account 0 CET never deployed on L2")
	require.NoError(t, helpers.AssertContractDeployed(ctx, destChain.RPCURL(), cetAddr),
		"CET contract should be deployed on L2 after account 0's bridge")

	logger.Info("%s account 0 received CET; bridging remaining %d concurrently", tag, numAcc-1)
	const maxRetries = 5
	const retryDelay = 15 * time.Second
	succeeded := make([]bool, numAcc)
	succeeded[0] = true
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var wg sync.WaitGroup
		for i := 1; i < numAcc; i++ {
			if succeeded[i] {
				continue
			}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if err := bridgeFunc(accountsOnL1[i], uint32(helpers.L1MinGasLimitERC20)); err != nil {
					logger.Debug("%s acct %d attempt %d failed: %v", tag, i, attempt, err)
					return
				}
				succeeded[i] = true
			}(i)
		}
		wg.Wait()

		done := 0
		for _, s := range succeeded {
			if s {
				done++
			}
		}
		if done == numAcc {
			break
		}
		logger.Info("%s attempt %d: %d/%d succeeded", tag, attempt, done, numAcc)
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}
	for i, s := range succeeded {
		require.Truef(t, s, "account %d (%s) failed bridge after retries",
			i, accountsOnL1[i].GetAddress().Hex())
	}

	// 7. Assert all L1 balances are 0 and all L2 CET balances are 100.
	for i, ac := range accountsOnL1 {
		l1Bal, err := ac.GetTokensBalance(ctx, tokenAddr, tokenABI)
		require.NoError(t, err)
		require.Equalf(t, 0, l1Bal.Sign(), "account %d (%s) L1 balance should be 0 after bridge, got=%s",
			i, ac.GetAddress().Hex(), l1Bal)

		cetFinal, err := helpers.WaitForTokenBalanceChange(ctx, destChain.RPCURL(),
			cetAddr, ac.GetAddress(), tokenABI,
			helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
			func(cur *big.Int) bool { return cur.Cmp(bridgeAmt) >= 0 })
		require.NoErrorf(t, err, "account %d (%s) CET never reached 100", i, ac.GetAddress().Hex())
		require.Equalf(t, 0, cetFinal.Cmp(bridgeAmt),
			"account %d (%s) CET mismatch: got=%s want=%s",
			i, ac.GetAddress().Hex(), cetFinal, bridgeAmt)
	}
}

