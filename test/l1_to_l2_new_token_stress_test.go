// Go port of scripts/l1-to-l2-new-token-stress.ts. Deploys a single L1 token,
// mints 100 to each of N accounts, then bridges. The first account uses the
// CET-deploy gas budget; the rest reuse the deployed CET with normal gas.
package test

import (
	"fmt"
	"math/big"
	"os"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
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

	// 4. From the funder, mint to each derived account on L1. Submit all in
	//    parallel with sequential nonces from the funder, then wait on the
	//    last receipt — same DistributeEth pattern. The old sequential
	//    SendL1Tx-per-mint loop was waiting one L1 block per mint (~12s),
	//    which at N=100 burned 20+ minutes for the mint phase alone.
	masterNonce, err := TestL1Account.GetNonce(ctx)
	require.NoError(t, err)
	var lastMint *types.Transaction
	for i, ac := range accountsOnL1 {
		mintData, err := tokenABI.Pack("mint", ac.GetAddress(), bridgeAmt)
		require.NoError(t, err)
		tx, _, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
			To:        tokenAddr,
			Value:     big.NewInt(0),
			Gas:       helpers.GasMint,
			GasTipCap: helpers.GasTipCap,
			GasFeeCap: helpers.GasFeeCap,
			Data:      mintData,
		}, TestL1Account, masterNonce+uint64(i))
		require.NoError(t, err)
		_, err = transactions.SendTransaction(ctx, tx, TestL1Account.GetRollup().RPCURL())
		require.NoError(t, err)
		lastMint = tx
	}
	mintReceiptRetries := 4 * numAcc
	if mintReceiptRetries < 30 {
		mintReceiptRetries = 30
	}
	_, _, err = transactions.GetTransactionDetailsWithRetries(ctx, lastMint.Hash(), TestL1Account.GetRollup(), mintReceiptRetries)
	require.NoError(t, err, "wait for last mint receipt")
	logger.Info("%s minted to %d accounts (last hash %s)", tag, numAcc, lastMint.Hash().Hex())

	// 5. Each account approves the bridge in parallel. Same round-retry
	//    helper as the bridge step — approves are cheap, but at large N the
	//    18s receipt poll in SendL1Tx isn't always enough and we don't want
	//    to crash a goroutine with require.* on a transient timeout.
	const approveMaxRounds = 5
	approveTxs, approveErrs := runConcurrentTxRounds(ctx, accountsOnL1,
		func(_ int, ac *accounts.Account) (*types.Transaction, error) {
			approveData, err := tokenABI.Pack("approve", bridgeAddr, bridgeAmt)
			if err != nil {
				return nil, err
			}
			tx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
				To:        tokenAddr,
				Value:     big.NewInt(0),
				Gas:       helpers.GasApprove,
				GasTipCap: helpers.GasTipCap,
				GasFeeCap: helpers.GasFeeCap,
				Data:      approveData,
			}, ac)
			return tx, err
		}, tag+" approve", approveMaxRounds)
	for i, err := range approveErrs {
		if approveTxs[i] != nil {
			continue
		}
		assert.NoErrorf(t, err, "account %d (%s) approve: %v",
			i, accountsOnL1[i].GetAddress().Hex(), err)
	}
	require.Equalf(t, numAcc, countNonNilTx(approveTxs),
		"only %d/%d approves confirmed", countNonNilTx(approveTxs), numAcc)

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
	// Use runConcurrentTxRounds to retry only on actual on-chain reverts
	// (= OP portal's per-L1-block deposit gas cap was hit). See the helper's
	// doc for why receipt-poll timeouts are NOT a retry signal here.
	const maxRounds = 10
	bridgeTxs, bridgeErrs := runConcurrentTxRounds(ctx, accountsOnL1[1:],
		func(_ int, ac *accounts.Account) (*types.Transaction, error) {
			data, err := helpers.PackBridgeERC20ToL1(ComposeL1BridgeABI,
				tokenAddr, cetAddr, ac.GetAddress(),
				bridgeAmt, uint32(helpers.L1MinGasLimitERC20), extraData)
			if err != nil {
				return nil, err
			}
			tx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
				To:        bridgeAddr,
				Value:     big.NewInt(0),
				Gas:       helpers.L1BridgeGasLimit,
				GasTipCap: helpers.GasTipCap,
				GasFeeCap: helpers.GasFeeCap,
				Data:      data,
			}, ac)
			return tx, err
		}, tag, maxRounds)

	confirmed := countNonNilTx(bridgeTxs) + 1 // +1 for account 0 above
	for i, err := range bridgeErrs {
		if bridgeTxs[i] != nil {
			continue
		}
		// i is relative to accountsOnL1[1:], so the real account index is i+1.
		assert.NoErrorf(t, err, "account %d (%s) bridge: %v",
			i+1, accountsOnL1[i+1].GetAddress().Hex(), err)
	}
	require.Equalf(t, numAcc, confirmed,
		"only %d/%d bridges confirmed after %d rounds", confirmed, numAcc, maxRounds+1)

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

