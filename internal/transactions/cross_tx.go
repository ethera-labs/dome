package transactions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethera-labs/dome/internal/logger"
)

const (
	xtPollInterval                 = 100 * time.Millisecond
	xtStatusCommitted              = "committed"
	xtStatusAborted                = "aborted"
	sidecarEndpointEnv             = "SIDECAR_XT_ENDPOINT"
	staleStandaloneInstanceError   = "already exists with different transactions"
	defaultXTSubmitRetryDelay      = 500 * time.Millisecond
	defaultXTSubmitRetryMaxAttempt = 20
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// XTSubmitRequest is the JSON body sent to POST /xt on the sidecar.
type XTSubmitRequest struct {
	Transactions map[string][]string `json:"transactions"` // chainId -> list of raw tx hex
}

// XTResponse is returned by POST /xt.
type XTResponse struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
}

// XTStatus is returned by GET /xt/:id.
type XTStatus struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
	Decision   *bool  `json:"decision,omitempty"`
}

// SubmitXT posts a cross-chain transaction to the sidecar's /xt endpoint.
// transactions maps chain ID (as string) to a list of 0x-prefixed raw transaction hex strings.
func SubmitXT(ctx context.Context, sidecarURL string, transactions map[string][]string) (*XTResponse, error) {
	sidecarBaseURL := resolveSidecarBaseURL(sidecarURL)

	body, err := json.Marshal(XTSubmitRequest{Transactions: transactions})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal XT request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= defaultXTSubmitRetryMaxAttempt; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarBaseURL+"/xt", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to submit XT: %w", err)
		} else {
			respBody, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("failed to read response: %w", readErr)
			}

			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
				var xtResp XTResponse
				if err := json.Unmarshal(respBody, &xtResp); err != nil {
					return nil, fmt.Errorf("failed to unmarshal XT response: %w", err)
				}

				logger.Info("XT submitted successfully, instance_id: %s", xtResp.InstanceID)
				return &xtResp, nil
			}

			lastErr = fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, string(respBody))
		}

		if !isRetryableStaleInstanceError(lastErr) || attempt == defaultXTSubmitRetryMaxAttempt {
			return nil, lastErr
		}

		logger.Info(
			"Retrying XT submission after stale standalone instance collision (attempt %d/%d): %v",
			attempt,
			defaultXTSubmitRetryMaxAttempt,
			lastErr,
		)
		time.Sleep(defaultXTSubmitRetryDelay)
	}

	return nil, lastErr
}

// GetXTStatus retrieves the status of a previously submitted XT.
func GetXTStatus(ctx context.Context, sidecarURL string, instanceID string) (*XTStatus, error) {
	sidecarBaseURL := resolveSidecarBaseURL(sidecarURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarBaseURL+"/xt/"+instanceID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get XT status: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar returned %d: %s", resp.StatusCode, string(respBody))
	}

	var status XTStatus
	if err := json.Unmarshal(respBody, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal XT status: %w", err)
	}

	return &status, nil
}

// WaitForDecision polls the sidecar until the XT reaches a terminal state (committed or aborted).
// Returns true if committed, false if aborted.
func WaitForDecision(ctx context.Context, sidecarURL string, instanceID string, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		status, err := GetXTStatus(ctx, sidecarURL, instanceID)
		if err != nil {
			logger.Debug("XT status poll failed (will retry): %v", err)
		} else {
			switch status.Status {
			case xtStatusCommitted:
				return true, nil
			case xtStatusAborted:
				return false, nil
			}
		}

		select {
		case <-ctx.Done():
			return false, fmt.Errorf("context cancelled while waiting for XT decision")
		case <-time.After(xtPollInterval):
		}
	}

	return false, fmt.Errorf("timeout waiting for XT decision after %s", timeout)
}

func resolveSidecarBaseURL(fallback string) string {
	if endpoint := strings.TrimSpace(os.Getenv(sidecarEndpointEnv)); endpoint != "" {
		return normalizeSidecarBaseURL(endpoint)
	}
	return normalizeSidecarBaseURL(fallback)
}

func normalizeSidecarBaseURL(url string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(url), "/")
	return strings.TrimSuffix(trimmed, "/xt")
}

func isRetryableStaleInstanceError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), staleStandaloneInstanceError)
}
