package demo

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
)

const (
	defaultStateRaceAmountWei = "1"
	defaultRaceStatusTimeout  = 5 * time.Second
	defaultRaceFinalTimeout   = 20 * time.Second

	// Minimal ERC20 ABI needed to demonstrate that normal mempool state changes
	// can invalidate an XT that simulated successfully.
	erc20AllowanceRaceABI = `[
		{
			"type":"function",
			"name":"approve",
			"stateMutability":"nonpayable",
			"inputs":[
				{"name":"spender","type":"address"},
				{"name":"amount","type":"uint256"}
			],
			"outputs":[{"name":"","type":"bool"}]
		},
		{
			"type":"function",
			"name":"transferFrom",
			"stateMutability":"nonpayable",
			"inputs":[
				{"name":"from","type":"address"},
				{"name":"to","type":"address"},
				{"name":"amount","type":"uint256"}
			],
			"outputs":[{"name":"","type":"bool"}]
		},
		{
			"type":"function",
			"name":"allowance",
			"stateMutability":"view",
			"inputs":[
				{"name":"owner","type":"address"},
				{"name":"spender","type":"address"}
			],
			"outputs":[{"name":"","type":"uint256"}]
		},
		{
			"type":"function",
			"name":"balanceOf",
			"stateMutability":"view",
			"inputs":[{"name":"account","type":"address"}],
			"outputs":[{"name":"","type":"uint256"}]
		}
	]`
)

func TestDemoStageXTInvalidatedByMempoolAllowanceRevocation(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DOME_DEMO_ENABLE_STATE_RACE")) != "1" {
		t.Skip("set DOME_DEMO_ENABLE_STATE_RACE=1 to run the allowance revocation race test")
	}

	ctx := t.Context()
	sidecarURL := envOrDefault("DOME_DEMO_SIDECAR_URL", defaultSidecarURL)
	rollupA := rollup.New(envOrDefault("DOME_DEMO_RPC_A", defaultRollupARPC), big.NewInt(defaultRollupAChainID), "rollup-a")
	rollupB := rollup.New(envOrDefault("DOME_DEMO_RPC_B", defaultRollupBRPC), big.NewInt(defaultRollupBChainID), "rollup-b")

	ownerPK := demoPrivateKey("DOME_DEMO_TOKEN_OWNER_PK")
	spenderPK := demoPrivateKey("DOME_DEMO_TOKEN_SPENDER_PK")
	if ownerPK == "" || spenderPK == "" {
		t.Skip("set DOME_DEMO_TOKEN_OWNER_PK and DOME_DEMO_TOKEN_SPENDER_PK")
	}
	pkB := demoPrivateKey("DOME_DEMO_PK_B")
	if pkB == "" {
		pkB = demoPrivateKey("DOME_DEMO_PK")
	}
	if pkB == "" {
		t.Skip("set DOME_DEMO_PK or DOME_DEMO_PK_B for the harmless rollup-b leg")
	}

	tokenAddressHex := strings.TrimSpace(os.Getenv("DOME_DEMO_TOKEN_ADDR"))
	if tokenAddressHex == "" {
		t.Skip("set DOME_DEMO_TOKEN_ADDR to an ERC20 deployed on rollup-a")
	}

	owner, err := accounts.NewRollupAccount(ownerPK, rollupA)
	require.NoError(t, err)
	t.Cleanup(owner.Close)

	spender, err := accounts.NewRollupAccount(spenderPK, rollupA)
	require.NoError(t, err)
	t.Cleanup(spender.Close)

	accountB, err := accounts.NewRollupAccount(pkB, rollupB)
	require.NoError(t, err)
	t.Cleanup(accountB.Close)

	tokenAddress := common.HexToAddress(tokenAddressHex)
	tokenABI, err := abi.JSON(strings.NewReader(erc20AllowanceRaceABI))
	require.NoError(t, err)

	amount := mustBigInt(t, envOrDefault("DOME_DEMO_STATE_RACE_AMOUNT_WEI", defaultStateRaceAmountWei))
	recipient := common.HexToAddress(envOrDefault("DOME_DEMO_STATE_RACE_RECIPIENT", spender.GetAddress().Hex()))

	ownerBalance := callERC20BigInt(t, ctx, rollupA, tokenAddress, tokenABI, "balanceOf", owner.GetAddress())
	require.GreaterOrEqual(t, ownerBalance.Cmp(amount), 0, "owner must have enough token balance for transferFrom")

	approveData, err := tokenABI.Pack("approve", spender.GetAddress(), amount)
	require.NoError(t, err)
	approveTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       120000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      approveData,
	}, owner)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, approveTx, rollupA.RPCURL())
	require.NoError(t, err)
	_, approveReceipt, err := transactions.GetTransactionDetails(ctx, approveTx.Hash(), rollupA)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, approveReceipt.Status)

	allowance := callERC20BigInt(t, ctx, rollupA, tokenAddress, tokenABI, "allowance", owner.GetAddress(), spender.GetAddress())
	require.GreaterOrEqual(t, allowance.Cmp(amount), 0, "setup allowance must be enough for XT simulation")

	transferFromData, err := tokenABI.Pack("transferFrom", owner.GetAddress(), recipient, amount)
	require.NoError(t, err)
	xtTx, signedXTBytes, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       160000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      transferFromData,
	}, spender)
	require.NoError(t, err)

	_, signedBBytes, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        accountB.GetAddress(),
		Value:     big.NewInt(1),
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      nil,
	}, accountB)
	require.NoError(t, err)

	xtResp, err := transactions.SubmitXT(ctx, sidecarURL, map[string][]string{
		rollupA.ChainID().String(): {hexutil.Encode(signedXTBytes)},
		rollupB.ChainID().String(): {hexutil.Encode(signedBBytes)},
	})
	require.NoError(t, err)

	statusCtx, cancelStatusWatch := context.WithCancel(ctx)
	defer cancelStatusWatch()
	sawVoted := make(chan struct{})
	go watchXTStatus(statusCtx, sidecarURL, xtResp.InstanceID, "voted", sawVoted)

	revokeData, err := tokenABI.Pack("approve", spender.GetAddress(), big.NewInt(0))
	require.NoError(t, err)
	revokeTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       120000,
		GasTipCap: big.NewInt(2000000000),
		GasFeeCap: big.NewInt(30000000000),
		Data:      revokeData,
	}, owner)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, revokeTx, envOrDefault("DOME_DEMO_STATE_RACE_MEMPOOL_RPC", rollupA.RPCURL()))
	require.NoError(t, err)
	_, revokeReceipt, err := transactions.GetTransactionDetails(ctx, revokeTx.Hash(), rollupA)
	require.NoError(t, err)
	require.Equal(t, types.ReceiptStatusSuccessful, revokeReceipt.Status)

	allowanceAfterRevoke := callERC20BigInt(t, ctx, rollupA, tokenAddress, tokenABI, "allowance", owner.GetAddress(), spender.GetAddress())
	require.Zero(t, allowanceAfterRevoke.Sign(), "normal mempool tx must revoke allowance before XT inclusion")

	committed, err := transactions.WaitForDecision(ctx, sidecarURL, xtResp.InstanceID, defaultRaceFinalTimeout)
	votedBeforeTerminal := channelClosed(sawVoted)
	if err == nil {
		require.True(t, votedBeforeTerminal, "XT reached a terminal state before the test observed a successful simulation vote")
		require.False(t, committed, "XT committed even though allowance was revoked by a normal mempool tx before inclusion; xt tx %s revoke tx %s", xtTx.Hash().Hex(), revokeTx.Hash().Hex())
		return
	}

	require.True(t, votedBeforeTerminal, "XT never reached voted; it likely failed initial simulation instead of execution after mempool state changed")
	t.Logf("XT did not commit before timeout after normal mempool revocation; instance=%s xt_tx=%s revoke_tx=%s err=%v", xtResp.InstanceID, xtTx.Hash().Hex(), revokeTx.Hash().Hex(), err)
}

func callERC20BigInt(
	t *testing.T,
	ctx context.Context,
	chain *rollup.Rollup,
	token common.Address,
	tokenABI abi.ABI,
	method string,
	args ...interface{},
) *big.Int {
	t.Helper()

	ethClient, err := ethClientForRollup(ctx, chain)
	require.NoError(t, err)
	defer ethClient.Close()

	contract := bind.NewBoundContract(token, tokenABI, ethClient, ethClient, ethClient)
	var result *big.Int
	err = contract.Call(&bind.CallOpts{Context: ctx}, &[]interface{}{&result}, method, args...)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func ethClientForRollup(ctx context.Context, chain *rollup.Rollup) (*ethclient.Client, error) {
	return ethclient.DialContext(ctx, chain.RPCURL())
}

func waitForXTStatus(
	ctx context.Context,
	sidecarURL string,
	instanceID string,
	targets map[string]bool,
	timeout time.Duration,
) (*transactions.XTStatus, error) {
	deadline := time.Now().Add(timeout)
	var last *transactions.XTStatus
	for time.Now().Before(deadline) {
		status, err := transactions.GetXTStatus(ctx, sidecarURL, instanceID)
		if err == nil {
			last = status
			if targets[status.Status] {
				return status, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}

	if last != nil {
		return last, fmt.Errorf("timeout waiting for XT status targets after %s; last status=%s", timeout, last.Status)
	}
	return nil, fmt.Errorf("timeout waiting for XT status targets after %s", timeout)
}

func watchXTStatus(
	ctx context.Context,
	sidecarURL string,
	instanceID string,
	target string,
	seen chan<- struct{},
) {
	defer close(seen)
	for {
		status, err := transactions.GetXTStatus(ctx, sidecarURL, instanceID)
		if err == nil && status.Status == target {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func channelClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
