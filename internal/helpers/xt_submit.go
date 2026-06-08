package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/transactions"
)

// XTEntry is one (chainId, signedRawTx) pair in an XT batch.
type XTEntry struct {
	ChainID *uint64 // big-enough to hold all known chain ids
	RawTx   []byte
}

// SubmitXTPair is a convenience wrapper for the common case of one source tx +
// one destination tx, where each tx is already signed. Routes to the sidecar
// or to eth_sendXTransaction based on configs.Values.XTSubmission.
//
// Returns the sidecar instance ID (empty in rpc mode) so callers can call
// WaitForXTDecision when in sidecar mode.
func SubmitXTPair(
	ctx context.Context,
	srcChainID uint64,
	srcSigned []byte,
	dstChainID uint64,
	dstSigned []byte,
) (instanceID string, err error) {
	return SubmitXTRaw(ctx, map[uint64][][]byte{
		srcChainID: {srcSigned},
		dstChainID: {dstSigned},
	}, srcChainID)
}

// SubmitXTRaw is the general entrypoint. `txs` maps chainID -> list of signed
// raw tx bytes. `sourceChainID` is only used by the RPC mode to pick which
// rollup RPC to call eth_sendXTransaction on; in sidecar mode it's ignored.
func SubmitXTRaw(
	ctx context.Context,
	txs map[uint64][][]byte,
	sourceChainID uint64,
) (string, error) {
	switch configs.Values.XTSubmission {
	case configs.XTSubmissionSidecar:
		return submitViaSidecar(ctx, txs)
	case configs.XTSubmissionRPC:
		return "", submitViaRpc(ctx, txs, sourceChainID)
	default:
		return "", fmt.Errorf("unknown xt-submission mode: %s", configs.Values.XTSubmission)
	}
}

// SubmitXTWaitCommitted submits an XT and, in sidecar mode, waits for the
// committed/aborted decision. In RPC mode there's no decision-polling endpoint;
// callers must verify via tx receipts. Returns (instanceID, committed, error).
func SubmitXTWaitCommitted(
	ctx context.Context,
	txs map[uint64][][]byte,
	sourceChainID uint64,
	timeout time.Duration,
) (string, bool, error) {
	id, err := SubmitXTRaw(ctx, txs, sourceChainID)
	if err != nil {
		return "", false, err
	}
	if configs.Values.XTSubmission != configs.XTSubmissionSidecar {
		return id, true, nil
	}
	committed, err := transactions.WaitForDecision(ctx, configs.Values.L2.SidecarURL, id, timeout)
	return id, committed, err
}

// ---------------------------------------------------------------------------
// sidecar mode
// ---------------------------------------------------------------------------

func submitViaSidecar(ctx context.Context, txs map[uint64][][]byte) (string, error) {
	logger.Info("XT submission path: SIDECAR — POST %s/xt", configs.Values.L2.SidecarURL)
	mapped := make(map[string][]string, len(txs))
	for chainID, list := range txs {
		hexes := make([]string, len(list))
		for i, raw := range list {
			hexes[i] = hexutil.Encode(raw)
		}
		mapped[fmt.Sprintf("%d", chainID)] = hexes
	}
	resp, err := transactions.SubmitXT(ctx, configs.Values.L2.SidecarURL, mapped)
	if err != nil {
		return "", err
	}
	return resp.InstanceID, nil
}

// ---------------------------------------------------------------------------
// rpc mode (eth_sendXTransaction)
// ---------------------------------------------------------------------------

type xtPayload struct {
	Payload string `json:"payload"`
}

type rpcRequest struct {
	JsonRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

type rpcResponse struct {
	JsonRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	ID int `json:"id"`
}

func submitViaRpc(ctx context.Context, txs map[uint64][][]byte, sourceChainID uint64) error {
	logger.Info("XT submission path: RPC — eth_sendXTransaction on source rollup chainId=%d", sourceChainID)
	// Flatten to encode-xt.ts entries.
	type entry struct {
		ChainID uint64 `json:"chainId"`
		RawTx   string `json:"rawTx"`
	}
	var entries []entry
	for chainID, list := range txs {
		for _, raw := range list {
			entries = append(entries, entry{ChainID: chainID, RawTx: hexutil.Encode(raw)})
		}
	}

	entriesJSON, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal entries: %w", err)
	}

	payload, err := encodeXTPayload(ctx, entriesJSON)
	if err != nil {
		return fmt.Errorf("encode xt payload: %w", err)
	}

	// POST eth_sendXTransaction to the source rollup RPC.
	rpcURL := rpcURLForChain(sourceChainID)
	if rpcURL == "" {
		return fmt.Errorf("no rpc-url configured for source chain %d", sourceChainID)
	}

	reqBody, err := json.Marshal(rpcRequest{
		JsonRPC: "2.0",
		Method:  "eth_sendXTransaction",
		Params:  []any{payload},
		ID:      1,
	})
	if err != nil {
		return fmt.Errorf("marshal rpc request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("build rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post eth_sendXTransaction: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("unmarshal rpc response (status=%d body=%s): %w", resp.StatusCode, string(body), err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("eth_sendXTransaction failed: code=%d %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	logger.Info("XT submitted via eth_sendXTransaction: result=%s", string(rpcResp.Result))
	return nil
}

// encodeXTPayload shells out to scripts/encode-xt.ts.
//
// The helper exists because the wire format (encodeXtMessage in the SDK) is
// non-trivial to re-implement in Go. Cost is a single subprocess per XT —
// acceptable for an E2E test.
func encodeXTPayload(ctx context.Context, entriesJSON []byte) (string, error) {
	scriptPath, err := encodeXTScriptPath()
	if err != nil {
		return "", err
	}
	scriptsDir := filepath.Dir(scriptPath)
	if _, err := os.Stat(filepath.Join(scriptsDir, "node_modules")); err != nil {
		return "", fmt.Errorf(
			"node_modules missing in %s — run `make scripts-install` (or `cd %s && npm install`) once before using xt-submission=rpc",
			scriptsDir, scriptsDir,
		)
	}

	// --transpile-only skips type-checking. We still get runtime errors if a
	// required package is missing, but transient type-errors (e.g. missing
	// @types/node) don't block the actual encodeXtMessage call.
	cmd := exec.CommandContext(ctx, "npx", "ts-node", "--transpile-only", scriptPath, "--entries", string(entriesJSON))
	cmd.Dir = scriptsDir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("run encode-xt.ts: %w", err)
	}

	var p xtPayload
	if err := json.Unmarshal(out, &p); err != nil {
		return "", fmt.Errorf("parse encode-xt.ts output %q: %w", string(out), err)
	}
	if p.Payload == "" {
		return "", fmt.Errorf("encode-xt.ts returned empty payload")
	}
	return p.Payload, nil
}

// encodeXTScriptPath looks up scripts/encode-xt.ts relative to the repo root.
// First tries the path of this source file at compile time (works when running
// `go test` inside the repo), then falls back to an env var override.
func encodeXTScriptPath() (string, error) {
	if env := os.Getenv("DOME_ENCODE_XT_SCRIPT"); env != "" {
		return env, nil
	}
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile = <repo>/internal/helpers/xt_submit.go
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	path := filepath.Join(repoRoot, "scripts", "encode-xt.ts")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("scripts/encode-xt.ts not found (tried %s); set DOME_ENCODE_XT_SCRIPT to override", path)
}

// HasTSRuntime probes for `npx` so RPC-mode and SA tests can skip cleanly when
// Node isn't installed.
func HasTSRuntime() bool {
	_, err := exec.LookPath("npx")
	return err == nil
}

// SubmittedTxHash extracts the hash of a tx after we sign it locally — useful
// in rpc mode where the XT submit doesn't return per-tx hashes.
func SubmittedTxHash(tx *types.Transaction) string {
	return tx.Hash().Hex()
}

func rpcURLForChain(chainID uint64) string {
	for _, cfg := range configs.Values.L2.ChainConfigs {
		if uint64(cfg.ID) == chainID {
			return cfg.RPCURL
		}
	}
	return ""
}
