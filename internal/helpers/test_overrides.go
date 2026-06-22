package helpers

import (
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethera-labs/dome/internal/logger"
)

// LogAssertOK prints a structured `ASSERT: <check>` line. Call BEFORE the
// corresponding `require.*` so on failure the log clearly shows which
// assertion the test was on. Include the actual values in the format string
// so the line is fully self-describing.
func LogAssertOK(format string, args ...any) {
	logger.Info("ASSERT: " + fmt.Sprintf(format, args...))
}

// LogAssertXTOK emits a mode-aware "XT submitted" ASSERT line. In sidecar
// mode the sidecar has returned a `committed` decision; in RPC mode there is
// no decision endpoint, so we only assert "submitted". Each chain's on-chain
// receipt is checked separately via the receipt-status asserts that follow.
func LogAssertXTOK(srcHash, dstHash, instanceID string) {
	if instanceID != "" {
		LogAssertOK("XT submitted and sidecar-committed (instance=%s src=%s dst=%s)",
			instanceID, srcHash, dstHash)
	} else {
		LogAssertOK("XT submitted via eth_sendXTransaction — sidecar decision N/A in RPC mode (src=%s dst=%s); see per-chain receipt asserts below",
			srcHash, dstHash)
	}
}

// ApplyDirectionFilter consults BRIDGE_SOURCE / BRIDGE_DEST env vars. Each
// test variant calls this with the source/dest pair it represents (use "a",
// "b", or "l1"). If either env var is set and doesn't match the variant, the
// test is skipped — so passing SOURCE=a DEST=b to `make` runs only the A->B
// variant out of a file that defines both directions.
//
// When neither env var is set, all variants run as before.
func ApplyDirectionFilter(t *testing.T, testSrc, testDst string) {
	t.Helper()
	src := strings.ToLower(os.Getenv("BRIDGE_SOURCE"))
	dst := strings.ToLower(os.Getenv("BRIDGE_DEST"))
	if src != "" && src != testSrc {
		t.Skipf("skipped by BRIDGE_SOURCE=%s (this test uses source=%s)", src, testSrc)
	}
	if dst != "" && dst != testDst {
		t.Skipf("skipped by BRIDGE_DEST=%s (this test uses dest=%s)", dst, testDst)
	}
}

// ParseBridgeAmountOverride returns the bridge amount in wei. Priority:
//  1. BRIDGE_AMOUNT_WEI — decimal wei.
//  2. BRIDGE_AMOUNT — decimal ETH (e.g. "0.01").
//  3. defaultWei.
func ParseBridgeAmountOverride(defaultWei *big.Int) *big.Int {
	if s := os.Getenv("BRIDGE_AMOUNT_WEI"); s != "" {
		if v, ok := new(big.Int).SetString(s, 10); ok {
			return v
		}
	}
	if s := os.Getenv("BRIDGE_AMOUNT"); s != "" {
		// Decimal ETH → wei. Use big.Float to handle e.g. "0.01".
		f, _, err := big.ParseFloat(s, 10, 256, big.ToNearestEven)
		if err == nil {
			wei := new(big.Float).Mul(f, new(big.Float).SetInt64(1_000_000_000_000_000_000))
			out, _ := wei.Int(nil)
			return out
		}
	}
	return defaultWei
}
