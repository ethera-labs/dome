// Go port of scripts/l2-to-l2-SA-existing-token-stress.ts. Two modes:
//
//   _MultiSA — N independent EOAs (deterministically derived) each control a
//              distinct SA. All N bridge their 100 tokens concurrently.
//   _SameSA  — One SA bridges 100 tokens N times sequentially.
//
// Defaults to N=10; override via BRIDGE_STRESS_ACCOUNTS.
package test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

const saStressDefaultAccounts = 10

func TestL2ToL2_SA_ExistingToken_Stress_MultiSA(t *testing.T) {
	RequireAA(t)
	RequireTSRuntime(t)
	runSAStressMulti(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func TestL2ToL2_SA_ExistingToken_Stress_SameSA(t *testing.T) {
	RequireAA(t)
	RequireTSRuntime(t)
	runSAStressSame(t, TestAccountA, TestRollupA, TestAccountB, TestRollupB)
}

func saStressNumAccounts(t *testing.T) int {
	if env := os.Getenv("BRIDGE_STRESS_ACCOUNTS"); env != "" {
		n, err := strconv.Atoi(env)
		require.NoError(t, err)
		require.Greater(t, n, 0)
		return n
	}
	return saStressDefaultAccounts
}

// runSAStressMulti spins up N derived EOAs, each with its own SA, and has all
// of them bridge concurrently.
func runSAStressMulti(t *testing.T, srcFunder *accounts.Account, srcChain *rollup.Rollup, dstFunder *accounts.Account, dstChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	bridgeAmt := helpers.ParseBridgeAmountOverride(l2ToL2TokenBridgeAmount)

	numAcc := saStressNumAccounts(t)
	tag := fmt.Sprintf("[SA STRESS multi %s->%s]", srcChain.Name(), dstChain.Name())
	logger.Info("%s N=%d", tag, numAcc)

	// Deploy token + predict CET (once, shared across all accounts).
	tokenAddr, _, err := helpers.DeployMintableToken(ctx, srcFunder, "StressSAToken", "SSAT", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	cetAddr, err := helpers.PredictCetAddress(ctx, dstChain, CetFactoryABI, tokenAddr, srcChain.ChainID())
	require.NoError(t, err)

	// Derive N EOAs from the master key.
	masterPK := configs.Values.WalletPrivateKey
	eoaSrc := deriveAccountsOnChain(t, masterPK, numAcc, srcChain)
	eoaDst := deriveAccountsOnChain(t, masterPK, numAcc, dstChain)

	// For each EOA: derive its SA address, fund EntryPoint on both chains,
	// mint 100 of the token to the SA on source.
	saAddrs := make([]common.Address, numAcc)
	for i := range eoaSrc {
		pk := privateKeyHex(eoaSrc[i])
		addr, _, err := helpers.SACreateAccount(ctx, pk,
			uint64(srcChain.ChainID().Int64()),
			[]uint64{uint64(srcChain.ChainID().Int64()), uint64(dstChain.ChainID().Int64())})
		require.NoError(t, err)
		saAddrs[i] = addr
	}

	// Top-up EntryPoint deposits + give each SA a tiny ETH cushion for gas
	// refunds (most networks let handleOps refund unused gas to the SA).
	for i := range eoaSrc {
		require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, srcFunder, saAddrs[i], helpers.MinEntryPointDeposit))
		require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, dstFunder, saAddrs[i], helpers.MinEntryPointDeposit))
	}

	// Mint to each SA on source.
	for i, sa := range saAddrs {
		mintData, err := tokenABI.Pack("mint", sa, bridgeAmt)
		require.NoError(t, err)
		mintTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
			To: tokenAddr, Value: big.NewInt(0), Gas: helpers.GasMint,
			GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: mintData,
		}, srcFunder)
		require.NoError(t, err)
		_, err = transactions.SendTransaction(ctx, mintTx, srcChain.RPCURL())
		require.NoError(t, err)
		waitReceipt(t, ctx, mintTx, srcChain)
		_ = i
	}

	// Bridge concurrently — each derived EOA + SA bridges via the TS helper.
	overrides := helpers.StandardSATokenBridgeGasOverrides(
		uint64(srcChain.ChainID().Int64()), uint64(dstChain.ChainID().Int64()),
	)
	var wg sync.WaitGroup
	errs := make([]error, numAcc)
	for i := range eoaSrc {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pk := privateKeyHex(eoaSrc[i])
			calls := buildSABridgeCalls(t, tokenAddr, cetAddr, saAddrs[i], eoaDst[i].GetAddress(),
				bridgeAddr, tokenABI, srcChain.ChainID(), dstChain.ChainID(), bridgeAmt)
			if TestXTMode == configs.XTSubmissionRPC {
				if _, err := helpers.SAComposeAndSubmit(ctx, pk, calls, overrides); err != nil {
					errs[i] = err
				}
			} else {
				if err := submitSAViaSidecar(t, ctx, pk, calls, srcChain, dstChain, srcFunder, dstFunder, overrides); err != nil {
					errs[i] = err
				}
			}
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		require.NoErrorf(t, e, "account %d (sa=%s) failed", i, saAddrs[i].Hex())
	}

	// CET must be deployed on dest after the first bridge lands.
	require.NoError(t, helpers.AssertContractDeployed(ctx, dstChain.RPCURL(), cetAddr),
		"CET contract should be deployed on dest after first bridge")

	// Assert each EOA received 100 CET on dest.
	for i := range eoaSrc {
		final, err := helpers.WaitForTokenBalanceChange(ctx, dstChain.RPCURL(),
			cetAddr, eoaDst[i].GetAddress(), tokenABI,
			helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
			func(cur *big.Int) bool { return cur.Cmp(bridgeAmt) >= 0 })
		require.NoErrorf(t, err, "account %d EOA dest CET never reached 100", i)
		require.Equalf(t, 0, final.Cmp(bridgeAmt),
			"account %d EOA dest CET mismatch: got=%s want=%s", i, final, bridgeAmt)
	}
}

// runSAStressSame uses one SA and runs N bridges sequentially.
func runSAStressSame(t *testing.T, srcFunder *accounts.Account, srcChain *rollup.Rollup, dstFunder *accounts.Account, dstChain *rollup.Rollup) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	pk := configs.Values.WalletPrivateKey
	bridgeAmt := helpers.ParseBridgeAmountOverride(l2ToL2TokenBridgeAmount)
	numIter := saStressNumAccounts(t)
	tag := fmt.Sprintf("[SA STRESS same %s->%s]", srcChain.Name(), dstChain.Name())
	logger.Info("%s N=%d", tag, numIter)

	saAddr, _, err := helpers.SACreateAccount(ctx, pk, uint64(srcChain.ChainID().Int64()),
		[]uint64{uint64(srcChain.ChainID().Int64()), uint64(dstChain.ChainID().Int64())})
	require.NoError(t, err)

	// Top-up EntryPoint to cover N bridges worth of gas (roughly 0.05 * N).
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, srcFunder, saAddr,
		new(big.Int).Mul(big.NewInt(int64(numIter)), helpers.MinEntryPointDeposit)))
	require.NoError(t, helpers.EnsureEntryPointDeposit(ctx, dstFunder, saAddr,
		new(big.Int).Mul(big.NewInt(int64(numIter)), helpers.MinEntryPointDeposit)))

	// Deploy token, predict CET, mint N*100 to SA.
	tokenAddr, _, err := helpers.DeployMintableToken(ctx, srcFunder, "SameSAStress", "SSS", 18)
	require.NoError(t, err)
	tokenABI, err := helpers.ParseMintableTokenABI()
	require.NoError(t, err)
	cetAddr, err := helpers.PredictCetAddress(ctx, dstChain, CetFactoryABI, tokenAddr, srcChain.ChainID())
	require.NoError(t, err)
	totalMint := new(big.Int).Mul(bridgeAmt, big.NewInt(int64(numIter)))
	mintData, err := tokenABI.Pack("mint", saAddr, totalMint)
	require.NoError(t, err)
	mintTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: tokenAddr, Value: big.NewInt(0), Gas: helpers.GasMint,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: mintData,
	}, srcFunder)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, mintTx, srcChain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, mintTx, srcChain)

	eoaBefore, err := srcFunder.GetTokensBalance(ctx, cetAddr, tokenABI)
	if err != nil {
		eoaBefore = big.NewInt(0)
	}

	overrides := helpers.StandardSATokenBridgeGasOverrides(
		uint64(srcChain.ChainID().Int64()), uint64(dstChain.ChainID().Int64()),
	)
	for iter := range numIter {
		calls := buildSABridgeCalls(t, tokenAddr, cetAddr, saAddr, srcFunder.GetAddress(),
			bridgeAddr, tokenABI, srcChain.ChainID(), dstChain.ChainID(), bridgeAmt)
		if TestXTMode == configs.XTSubmissionRPC {
			_, err := helpers.SAComposeAndSubmit(ctx, pk, calls, overrides)
			require.NoErrorf(t, err, "iter %d failed", iter)
		} else {
			err := submitSAViaSidecar(t, ctx, pk, calls, srcChain, dstChain, srcFunder, dstFunder, overrides)
			require.NoErrorf(t, err, "iter %d failed", iter)
		}
		logger.Info("%s iter %d/%d done", tag, iter+1, numIter)
	}

	expected := new(big.Int).Add(eoaBefore, totalMint)
	final, err := helpers.WaitForTokenBalanceChange(ctx, dstChain.RPCURL(),
		cetAddr, srcFunder.GetAddress(), tokenABI,
		helpers.DefaultPollInterval, helpers.DefaultPollAttempts,
		func(cur *big.Int) bool { return cur.Cmp(expected) >= 0 })
	require.NoError(t, err)
	require.NoError(t, helpers.AssertContractDeployed(ctx, dstChain.RPCURL(), cetAddr),
		"CET contract should be deployed on dest after first bridge")
	require.Equalf(t, 0, final.Cmp(expected), "EOA dest CET mismatch: got=%s want=%s", final, expected)
}

func deriveAccountsOnChain(t *testing.T, masterPK string, count int, chain *rollup.Rollup) []*accounts.Account {
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
		ac, err := accounts.NewRollupAccount(hex.EncodeToString(derived), chain)
		require.NoError(t, err)
		out[i] = ac
	}
	return out
}

func privateKeyHex(ac *accounts.Account) string {
	return hex.EncodeToString(crypto.FromECDSA(ac.GetPrivateKey()))
}

func buildSABridgeCalls(
	t *testing.T,
	tokenAddr, cetAddr, saAddr, eoaRecipient, bridgeAddr common.Address,
	tokenABI interface {
		Pack(name string, args ...any) ([]byte, error)
	},
	srcChainID, dstChainID *big.Int,
	amount *big.Int,
) []helpers.UserOpCall {
	t.Helper()
	sessionID := transactions.GenerateRandomSessionID()

	approveData, err := tokenABI.Pack("approve", bridgeAddr, amount)
	require.NoError(t, err)
	bridgeData, err := helpers.PackBridgeERC20To(BridgeABI, dstChainID,
		tokenAddr, amount, saAddr, sessionID)
	require.NoError(t, err)
	receiveData, err := helpers.PackBridgeReceiveTokens(BridgeABI,
		srcChainID, dstChainID, bridgeAddr, saAddr, sessionID)
	require.NoError(t, err)
	transferData, err := tokenABI.Pack("transfer", eoaRecipient, amount)
	require.NoError(t, err)

	return []helpers.UserOpCall{
		{ChainID: srcChainID.Uint64(), To: tokenAddr, Value: "0", Data: hexutil.Encode(approveData)},
		{ChainID: srcChainID.Uint64(), To: bridgeAddr, Value: "0", Data: hexutil.Encode(bridgeData)},
		{ChainID: dstChainID.Uint64(), To: bridgeAddr, Value: "0", Data: hexutil.Encode(receiveData)},
		{ChainID: dstChainID.Uint64(), To: cetAddr, Value: "0", Data: hexutil.Encode(transferData)},
	}
}
