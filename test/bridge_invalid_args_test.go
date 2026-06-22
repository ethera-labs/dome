// Wrong-destination / wrong-receiver scenarios for the L2 bridge. Each test
// here is a pure eth_call simulation — no on-chain writes, no setup beyond a
// configured bridge contract, no gas cost. Asserts that bad inputs surface as
// specific named errors from the live ABIs (BridgeABI or MailboxABI), not as
// generic reverts or out-of-gas.
//
// Covered today:
//   1. bridgeEthTo with msg.value=0                → expect NoETHSent (bridge guard)
//   2. receiveETH with chainDest != current chain  → expect WrongDestinationChain (bridge guard)
//   3. bridgeEthTo with receiver=address(0)        → expect MessageNotFound (mailbox guard)
//      Surprising at first — the bridge does have a ZeroAddress check, but a
//      naked eth_call has no pre-existing inbox slot, so the mailbox-layer
//      guard fires first on sidecar-mediated envs. Documents the real
//      contract-stack behaviour rather than the cleaned-up source-code
//      reading.
//
// Test catalogue: item #24 ("Wrong-destination / wrong-receiver scenarios").
package test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

// simulateBridgeRevert is a value-aware sibling of simulateRevertSelector
// (defined in replay_attack_test.go). bridgeEthTo is payable, so the
// msg.value is part of the test surface — without it we couldn't reproduce
// NoETHSent vs. ZeroAddress vs. WrongDestinationChain distinctly.
func simulateBridgeRevert(ctx context.Context, rpcURL string, from, to common.Address, data []byte, value *big.Int) ([]byte, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if _, err := client.CallContract(ctx, ethereum.CallMsg{
		From:  from,
		To:    &to,
		Data:  data,
		Value: value,
	}, nil); err == nil {
		return nil, errors.New("eth_call returned successfully — expected a revert")
	} else {
		return extractRevertSelector(err)
	}
}

// expectedNamedErrorSelector returns the 4-byte selector for a named error
// declared in either BridgeABI or MailboxABI. Searches the bridge first.
// Fails the test if neither ABI declares the error — meaning the expected
// name is wrong or both ABIs drifted.
func expectedNamedErrorSelector(t *testing.T, errName string) []byte {
	t.Helper()
	if e, ok := BridgeABI.Errors[errName]; ok {
		return e.ID.Bytes()[:4]
	}
	if e, ok := MailboxABI.Errors[errName]; ok {
		return e.ID.Bytes()[:4]
	}
	require.Failf(t, "named error not found",
		"no error named %q in BridgeABI or MailboxABI", errName)
	return nil
}

// runBridgeInvalidArgsCase: build the calldata + msg.value via buildCall,
// eth_call it against `chain`'s bridge from `from`, assert it reverts with
// the named error `expectedErrorName`.
func runBridgeInvalidArgsCase(
	t *testing.T,
	chain *rollup.Rollup,
	from common.Address,
	buildCall func() ([]byte, *big.Int),
	expectedErrorName string,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	data, value := buildCall()

	expected := expectedNamedErrorSelector(t, expectedErrorName)
	helpers.LogAssertOK("expect eth_call on %s bridge to revert with %s (selector=%s)",
		chain.Name(), expectedErrorName, hexutil.Encode(expected))

	got, err := simulateBridgeRevert(ctx, chain.RPCURL(), from, bridgeAddr, data, value)
	require.NoErrorf(t, err, "simulation should return a revert selector, not a transport error")
	require.Equalf(t, hexutil.Encode(expected), hexutil.Encode(got),
		"eth_call should revert with %s; got selector=%s", expectedErrorName, hexutil.Encode(got))
}

// 1. bridgeEthTo(_, B, validReceiver) with value=0 → NoETHSent (bridge).
func TestBridge_InvalidArgs_NoETHSent_AtoB(t *testing.T) {
	runBridgeInvalidArgsCase(t, TestRollupA, TestAccountA.GetAddress(),
		func() ([]byte, *big.Int) {
			data, err := helpers.PackBridgeEthTo(BridgeABI,
				transactions.GenerateRandomSessionID(),
				TestRollupB.ChainID(),
				TestAccountB.GetAddress(),
			)
			require.NoError(t, err)
			return data, big.NewInt(0)
		},
		"NoETHSent",
	)
}

// 2. bridgeEthTo(_, B, address(0)) with value > 0 → MessageNotFound (mailbox).
//    The bridge has its own ZeroAddress check, but on sidecar-mediated envs
//    a naked eth_call has no pre-existing inbox slot — the mailbox guard
//    fires first, which is the more interesting integration assertion.
func TestBridge_InvalidArgs_ZeroReceiver_ETH_AtoB(t *testing.T) {
	runBridgeInvalidArgsCase(t, TestRollupA, TestAccountA.GetAddress(),
		func() ([]byte, *big.Int) {
			data, err := helpers.PackBridgeEthTo(BridgeABI,
				transactions.GenerateRandomSessionID(),
				TestRollupB.ChainID(),
				common.Address{}, // receiver = address(0)
			)
			require.NoError(t, err)
			return data, big.NewInt(1_000_000_000_000_000) // 0.001 ETH
		},
		"MessageNotFound",
	)
}

// 3. receiveETH on rollup B with a MessageHeader whose chainDest is bogus
//    (i.e. not B's chainID) → WrongDestinationChain (bridge).
func TestBridge_InvalidArgs_WrongDestinationChain_Receive_OnB(t *testing.T) {
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	runBridgeInvalidArgsCase(t, TestRollupB, TestAccountB.GetAddress(),
		func() ([]byte, *big.Int) {
			data, err := helpers.PackReceiveETH(BridgeABI,
				TestRollupA.ChainID(),        // chainSrc
				big.NewInt(0xDEADBEEF),       // chainDest — NOT rollup-b's id
				bridgeAddr,                   // source-side bridge (placeholder)
				TestAccountB.GetAddress(),    // receiver
				transactions.GenerateRandomSessionID(),
			)
			require.NoError(t, err)
			return data, big.NewInt(0)
		},
		"WrongDestinationChain",
	)
}
