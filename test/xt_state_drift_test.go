package test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/compose-network/dome/configs"
	"github.com/compose-network/dome/internal/accounts"
	"github.com/compose-network/dome/internal/helpers"
	"github.com/compose-network/dome/internal/logger"
	"github.com/compose-network/dome/internal/rollup"
	"github.com/compose-network/dome/internal/transactions"
	"github.com/compose-network/dome/pkg/rollupv1"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type sidecarXtSubmitRequest struct {
	Transactions map[uint64][]string `json:"transactions"`
}

type sidecarXtSubmitResponse struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
}

type sidecarXtStatusResponse struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
	Decision   *bool  `json:"decision,omitempty"`
}

const staleStandaloneInstanceError = "already exists with different transactions"

// TestCommittedXtCanStillFailAfterDestinationStateDrift documents the current
// architecture gap:
//  1. a destination-chain XT tx simulates successfully against latest canonical state,
//  2. a queued normal mempool tx later mutates that state before block execution,
//  3. sidecar has already decided "committed", but the destination XT tx never lands.
//
// The test intentionally demonstrates that nonce reservation is not equivalent
// to state locking. If the architecture is fixed later, this expectation should
// be inverted.
func TestCommittedXtCanStillFailAfterDestinationStateDrift(t *testing.T) {
	ctx := t.Context()
	ensureXtEndpoint(t)

	const (
		waitCommittedTimeout = 20 * time.Second
		pollInterval         = 100 * time.Millisecond
	)

	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	driftAmount := new(big.Int).Mul(transferredAmount, big.NewInt(2))
	xtSourceA, xtDestinationB := newFundedAccountPair(t)

	ownerB := newFundedAccountOnRollup(t, TestRollupB, TestAccountB)
	_, _, err := helpers.SendMintTx(t, ownerB, new(big.Int).Set(driftAmount), TokenABI)
	require.NoError(t, err)
	_, _, err = helpers.ApproveTokens(t, ownerB, xtDestinationB.GetAddress(), TokenABI)
	require.NoError(t, err)

	allowanceBefore := getTokenAllowance(t, ctx, ownerB.GetRollup(), tokenAddress, ownerB.GetAddress(), xtDestinationB.GetAddress())
	require.True(t, allowanceBefore.Sign() > 0, "owner must approve spender before XT simulation")

	ownerNonce, err := ownerB.GetNonce(ctx)
	require.NoError(t, err)

	revokeQueuedTx := mustCreateApproveTxWithNonce(t, ctx, ownerB, xtDestinationB.GetAddress(), big.NewInt(0), ownerNonce+1)
	_, err = transactions.SendTransaction(ctx, revokeQueuedTx, ownerB.GetRollup().RPCURL())
	require.NoError(t, err)
	logger.Info("Queued destination-side revoke tx with future nonce: %s", revokeQueuedTx.Hash())

	originTx, signedOriginTx, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        xtSourceA.GetAddress(),
		Value:     big.NewInt(0),
		Gas:       21000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
	}, xtSourceA)
	require.NoError(t, err)

	destinationCalldata, err := TokenABI.Pack(
		"transferFrom",
		ownerB.GetAddress(),
		xtDestinationB.GetAddress(),
		new(big.Int).Set(transferredAmount),
	)
	require.NoError(t, err)

	destinationTx, signedDestinationTx, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      destinationCalldata,
	}, xtDestinationB)
	require.NoError(t, err)

	encodedPayload, err := transactions.CreateCrossTxRequestMsg(ctx, xtSourceA, xtDestinationB, signedOriginTx, signedDestinationTx)
	require.NoError(t, err)

	submitResp, err := submitXtViaSidecar(ctx, encodedPayload)
	require.NoError(t, err)
	require.NotEmpty(t, submitResp.InstanceID)
	require.Equal(t, "submitted", submitResp.Status)

	statusResp := waitForXtStatus(t, ctx, submitResp.InstanceID, waitCommittedTimeout, pollInterval)
	require.Equal(t, "committed", statusResp.Status, "XT must have simulated and decided commit before we unlock the revoke tx")
	require.NotNil(t, statusResp.Decision)
	require.True(t, *statusResp.Decision)

	// Unlock the queued revoke only after sidecar already committed the XT. The
	// revoke itself was invisible to simulation because sidecar simulates against
	// latest canonical state, not pending mempool state.
	unlockTx := mustCreateSelfTransferWithNonce(t, ctx, ownerB, ownerNonce, big.NewInt(1))
	_, err = transactions.SendTransaction(ctx, unlockTx, ownerB.GetRollup().RPCURL())
	require.NoError(t, err)

	requireSuccessfulReceipt(t, unlockTx.Hash(), ownerB.GetRollup())
	requireSuccessfulReceipt(t, revokeQueuedTx.Hash(), ownerB.GetRollup())

	allowanceAfter := getTokenAllowance(t, ctx, ownerB.GetRollup(), tokenAddress, ownerB.GetAddress(), xtDestinationB.GetAddress())
	require.Zero(t, allowanceAfter.Sign(), "queued revoke must execute before XT block execution")

	originReceipt := requireSuccessfulReceipt(t, originTx.Hash(), xtSourceA.GetRollup())
	requireNoReceipt(t, destinationTx.Hash(), TestRollupB)
	t.Logf("origin XT tx was included in block %d while destination XT tx never landed", originReceipt.BlockNumber.Uint64())
}

func newFundedAccountPair(t *testing.T) (*accounts.Account, *accounts.Account) {
	t.Helper()

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	privateKeyHex := hex.EncodeToString(crypto.FromECDSA(privateKey))
	accountA, err := accounts.NewRollupAccount(privateKeyHex, TestRollupA)
	require.NoError(t, err)
	accountB, err := accounts.NewRollupAccount(privateKeyHex, TestRollupB)
	require.NoError(t, err)

	t.Cleanup(accountA.Close)
	t.Cleanup(accountB.Close)

	err = transactions.DistributeEth(t.Context(), TestAccountA, []*accounts.Account{accountA}, gasFundingAmount)
	require.NoError(t, err)
	err = transactions.DistributeEth(t.Context(), TestAccountB, []*accounts.Account{accountB}, gasFundingAmount)
	require.NoError(t, err)

	return accountA, accountB
}

func newFundedAccountOnRollup(
	t *testing.T,
	onRollup *rollup.Rollup,
	sponsor *accounts.Account,
) *accounts.Account {
	t.Helper()

	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)

	privateKeyHex := hex.EncodeToString(crypto.FromECDSA(privateKey))
	account, err := accounts.NewRollupAccount(privateKeyHex, onRollup)
	require.NoError(t, err)

	t.Cleanup(account.Close)

	err = transactions.DistributeEth(t.Context(), sponsor, []*accounts.Account{account}, gasFundingAmount)
	require.NoError(t, err)

	return account
}

func mustCreateApproveTxWithNonce(
	t *testing.T,
	ctx context.Context,
	ac *accounts.Account,
	spender common.Address,
	amount *big.Int,
	nonce uint64,
) *types.Transaction {
	t.Helper()

	tokenAddress := configs.Values.L2.Contracts[configs.ContractNameToken].Address
	calldata, err := TokenABI.Pack("approve", spender, amount)
	require.NoError(t, err)

	tx, _, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
		To:        tokenAddress,
		Value:     big.NewInt(0),
		Gas:       900000,
		GasTipCap: big.NewInt(1000000000),
		GasFeeCap: big.NewInt(20000000000),
		Data:      calldata,
	}, ac, nonce)
	require.NoError(t, err)
	return tx
}

func mustCreateSelfTransferWithNonce(
	t *testing.T,
	ctx context.Context,
	ac *accounts.Account,
	nonce uint64,
	amount *big.Int,
) *types.Transaction {
	t.Helper()

	tx, _, err := transactions.CreateTransactionWithNonce(ctx, transactions.TransactionDetails{
		To:        ac.GetAddress(),
		Value:     amount,
		Gas:       25000,
		GasTipCap: big.NewInt(1000000),
		GasFeeCap: big.NewInt(2000000),
	}, ac, nonce)
	require.NoError(t, err)
	return tx
}

func getTokenAllowance(
	t *testing.T,
	ctx context.Context,
	onRollup *rollup.Rollup,
	tokenAddress common.Address,
	owner common.Address,
	spender common.Address,
) *big.Int {
	t.Helper()

	client, err := ethclient.DialContext(ctx, onRollup.RPCURL())
	require.NoError(t, err)
	defer client.Close()

	contract := bind.NewBoundContract(tokenAddress, TokenABI, client, client, client)
	call := &bind.CallOpts{Context: ctx}
	var allowance *big.Int
	err = contract.Call(call, &[]interface{}{&allowance}, "allowance", owner, spender)
	require.NoError(t, err)
	return allowance
}

func submitXtViaSidecar(
	ctx context.Context,
	encodedPayload []byte,
) (*sidecarXtSubmitResponse, error) {
	const (
		maxSubmitAttempts = 20
		retryDelay        = 500 * time.Millisecond
		requestTimeout    = 15 * time.Second
	)

	var msg rollupv1.Message
	if err := proto.Unmarshal(encodedPayload, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal XT message: %w", err)
	}

	xt := msg.GetXtRequest()
	if xt == nil {
		return nil, fmt.Errorf("XT request not found in message payload")
	}

	req := sidecarXtSubmitRequest{Transactions: map[uint64][]string{}}
	for _, txReq := range xt.GetTransactions() {
		if txReq == nil {
			continue
		}
		chainID := new(big.Int).SetBytes(txReq.GetChainId()).Uint64()
		if chainID == 0 {
			return nil, fmt.Errorf("invalid chain_id in XT request")
		}
		for _, raw := range txReq.GetTransaction() {
			req.Transactions[chainID] = append(req.Transactions[chainID], "0x"+hex.EncodeToString(raw))
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal XT request: %w", err)
	}

	client := &http.Client{Timeout: requestTimeout}
	var lastErr error

	for attempt := 1; attempt <= maxSubmitAttempts; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarXtBaseURL(), bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("failed to build XT request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("XT request failed: %w", err)
		} else {
			var bodyBytes []byte
			if resp.Body != nil {
				bodyBytes, _ = io.ReadAll(resp.Body)
			}
			_ = resp.Body.Close()

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				var submitResp sidecarXtSubmitResponse
				if err := json.Unmarshal(bodyBytes, &submitResp); err != nil {
					return nil, fmt.Errorf("failed to decode XT submit response: %w", err)
				}
				return &submitResp, nil
			}

			lastErr = fmt.Errorf(
				"XT request failed: status=%d body=%s",
				resp.StatusCode,
				strings.TrimSpace(string(bodyBytes)),
			)
		}

		if !isRetryableStandaloneInstanceCollision(lastErr) || attempt == maxSubmitAttempts {
			return nil, lastErr
		}

		logger.Info(
			"Retrying XT submission after stale standalone instance collision (attempt %d/%d): %v",
			attempt,
			maxSubmitAttempts,
			lastErr,
		)
		time.Sleep(retryDelay)
	}

	return nil, lastErr
}

func isRetryableStandaloneInstanceCollision(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), staleStandaloneInstanceError)
}

func waitForXtStatus(
	t *testing.T,
	ctx context.Context,
	instanceID string,
	timeout time.Duration,
	interval time.Duration,
) *sidecarXtStatusResponse {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		resp, err := getXtStatus(ctx, instanceID)
		if err == nil {
			switch resp.Status {
			case "committed", "aborted":
				return resp
			}
		}

		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("timed out waiting for XT status for %s: %v", instanceID, err)
			}
			t.Fatalf("timed out waiting for XT status for %s, last status=%s", instanceID, resp.Status)
		}

		time.Sleep(interval)
	}
}

func getXtStatus(ctx context.Context, instanceID string) (*sidecarXtStatusResponse, error) {
	url := strings.TrimRight(sidecarXtBaseURL(), "/") + "/" + instanceID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status request failed: %d", resp.StatusCode)
	}

	var statusResp sidecarXtStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, err
	}
	return &statusResp, nil
}

func sidecarXtBaseURL() string {
	endpoint := strings.TrimSpace(httpURLFromEnv())
	if !strings.HasSuffix(endpoint, "/xt") {
		endpoint = strings.TrimRight(endpoint, "/") + "/xt"
	}
	return strings.TrimRight(endpoint, "/")
}

func httpURLFromEnv() string {
	return strings.TrimSpace(os.Getenv("SIDECAR_XT_ENDPOINT"))
}
