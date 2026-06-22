// Same-L2 round-trip via ComposeL2ToL2Bridge.redeemWrappedCET.
//
// Burns a wrapper-CET back into its corresponding core-CET on the *same* L2,
// exercising the redeem path of the bridge (no XT, no cross-chain message).
//
// Setup model: redeemWrappedCET only requires
//   wrappedCET.remoteAsset() == coreCET  AND  coreCET.cetType() == CORE,
// then calls crosschainBurn(wrappedCET) + crosschainMint(coreCET). It does NOT
// check the wrapper came from CetFactory. We exploit this by deploying two
// minimal IComposableERC20-compatible test contracts (helpers.TestCET) — one
// as CORE, one as WRAPPED pointing at the core — and authorize the production
// bridge for crosschainMint/Burn on both.
//
// Test runs on a single rollup (default rollup-b; SOURCE=a flips to rollup-a).
// L1 / AA / sidecar are not required, so this passes on every configured
// network.
package test

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
	"github.com/ethera-labs/dome/internal/helpers"
	"github.com/ethera-labs/dome/internal/logger"
	"github.com/ethera-labs/dome/internal/rollup"
	"github.com/ethera-labs/dome/internal/transactions"
)

var redeemWrappedAmount = new(big.Int).Mul(big.NewInt(7), big.NewInt(1_000_000_000_000_000_000)) // 7 tokens

func TestL2_RedeemWrappedCET_RollupA(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "a", "a")
	runRedeemWrappedCET(t, TestAccountA, TestRollupA)
}

func TestL2_RedeemWrappedCET_RollupB(t *testing.T) {
	helpers.ApplyDirectionFilter(t, "b", "b")
	runRedeemWrappedCET(t, TestAccountB, TestRollupB)
}

func runRedeemWrappedCET(
	t *testing.T,
	user *accounts.Account,
	chain *rollup.Rollup,
) {
	t.Helper()
	ctx := t.Context()
	bridgeAddr := configs.Values.L2.Contracts[configs.ContractNameBridge].Address
	amount := helpers.ParseBridgeAmountOverride(redeemWrappedAmount)

	logger.Info("[redeemWrappedCET] chain=%s user=%s bridge=%s amount=%s",
		chain.Name(), user.GetAddress().Hex(), bridgeAddr.Hex(), amount)

	// 1. Deploy CORE TestCET. Constructor with cetType=0 forces
	// remoteAsset=address(this), remoteChainID=block.chainid — i.e. CORE.
	coreAddr, _, err := helpers.DeployTestCET(
		ctx, user, "RedeemCore", "RC",
		common.Address{}, nil,
		helpers.TestCETTypeCORE, bridgeAddr,
	)
	require.NoError(t, err, "deploy CORE TestCET")
	logger.Info("[redeemWrappedCET] core=%s", coreAddr.Hex())

	helpers.LogAssertOK("core.cetType()==0 (CORE) addr=%s", coreAddr.Hex())
	requireCetType(ctx, t, chain, coreAddr, 0)
	helpers.LogAssertOK("core.remoteAsset()==self addr=%s", coreAddr.Hex())
	requireRemoteAsset(ctx, t, chain, coreAddr, coreAddr)

	// 2. Deploy WRAPPED TestCET pointing at the core. remoteChainID for a
	// wrapper would normally be the source-chain's chainID; the redeem path
	// doesn't read it, but use chain.ChainID() so the contract is internally
	// consistent.
	wrappedAddr, _, err := helpers.DeployTestCET(
		ctx, user, "WrappedRedeemCore", "wRC",
		coreAddr, chain.ChainID(),
		helpers.TestCETTypeWRAPPED, bridgeAddr,
	)
	require.NoError(t, err, "deploy WRAPPED TestCET")
	logger.Info("[redeemWrappedCET] wrapped=%s", wrappedAddr.Hex())

	helpers.LogAssertOK("wrapped.cetType()==1 (WRAPPED) addr=%s", wrappedAddr.Hex())
	requireCetType(ctx, t, chain, wrappedAddr, 1)
	helpers.LogAssertOK("wrapped.remoteAsset()==core: wrapped=%s core=%s", wrappedAddr.Hex(), coreAddr.Hex())
	requireRemoteAsset(ctx, t, chain, wrappedAddr, coreAddr)

	// 3. Mint `amount` wrapper tokens to the user via the public mint().
	cetABI, err := helpers.ParseTestCETABI()
	require.NoError(t, err)

	mintCalldata, err := cetABI.Pack("mint", user.GetAddress(), amount)
	require.NoError(t, err)
	mintTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: wrappedAddr, Value: big.NewInt(0), Gas: helpers.GasMint,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: mintCalldata,
	}, user)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, mintTx, chain.RPCURL())
	require.NoError(t, err)
	waitReceipt(t, ctx, mintTx, chain)
	logger.Info("[redeemWrappedCET] minted %s wrapper tokens to %s (tx=%s)",
		amount, user.GetAddress().Hex(), mintTx.Hash().Hex())

	// 4. Snapshot balances/supplies BEFORE redeem.
	client, err := ethclient.DialContext(ctx, chain.RPCURL())
	require.NoError(t, err)
	defer client.Close()

	wrappedBalBefore := readCETBalance(ctx, t, client, wrappedAddr, user.GetAddress())
	coreBalBefore := readCETBalance(ctx, t, client, coreAddr, user.GetAddress())
	wrappedSupplyBefore := readCETTotalSupply(ctx, t, client, wrappedAddr)
	coreSupplyBefore := readCETTotalSupply(ctx, t, client, coreAddr)

	helpers.LogAssertOK("wrapped.balanceOf(user)==amount before redeem: got=%s want=%s",
		wrappedBalBefore, amount)
	require.Zero(t, wrappedBalBefore.Cmp(amount),
		"wrapped balance before redeem should equal minted amount: got=%s want=%s",
		wrappedBalBefore, amount)
	helpers.LogAssertOK("core.balanceOf(user)==0 before redeem: got=%s", coreBalBefore)
	require.Zero(t, coreBalBefore.Sign(),
		"core balance before redeem should be zero: got=%s", coreBalBefore)

	// 5. Call ComposeL2ToL2Bridge.redeemWrappedCET(wrappedCET, coreCET, amount).
	redeemCalldata, err := BridgeABI.Pack("redeemWrappedCET", wrappedAddr, coreAddr, amount)
	require.NoError(t, err)
	redeemTx, _, err := transactions.CreateTransaction(ctx, transactions.TransactionDetails{
		To: bridgeAddr, Value: big.NewInt(0), Gas: 400_000,
		GasTipCap: helpers.GasTipCap, GasFeeCap: helpers.GasFeeCap, Data: redeemCalldata,
	}, user)
	require.NoError(t, err)
	_, err = transactions.SendTransaction(ctx, redeemTx, chain.RPCURL())
	require.NoError(t, err)
	_, redeemReceipt, err := transactions.GetTransactionDetails(ctx, redeemTx.Hash(), chain)
	require.NoError(t, err)

	helpers.LogAssertOK("redeemWrappedCET tx successful: tx=%s status=%d",
		redeemTx.Hash().Hex(), redeemReceipt.Status)
	require.Equal(t, types.ReceiptStatusSuccessful, redeemReceipt.Status,
		"redeemWrappedCET reverted: tx=%s", redeemTx.Hash().Hex())

	// 6. Verify balances + totalSupplies moved exactly by `amount`.
	wrappedBalAfter := readCETBalance(ctx, t, client, wrappedAddr, user.GetAddress())
	coreBalAfter := readCETBalance(ctx, t, client, coreAddr, user.GetAddress())
	wrappedSupplyAfter := readCETTotalSupply(ctx, t, client, wrappedAddr)
	coreSupplyAfter := readCETTotalSupply(ctx, t, client, coreAddr)

	helpers.LogAssertOK("wrapped.balanceOf(user) decreased by exactly amount: before=%s after=%s amount=%s",
		wrappedBalBefore, wrappedBalAfter, amount)
	require.Zero(t,
		new(big.Int).Sub(wrappedBalBefore, wrappedBalAfter).Cmp(amount),
		"wrapped balance delta should equal amount: before=%s after=%s amount=%s",
		wrappedBalBefore, wrappedBalAfter, amount)

	helpers.LogAssertOK("core.balanceOf(user) increased by exactly amount: before=%s after=%s amount=%s",
		coreBalBefore, coreBalAfter, amount)
	require.Zero(t,
		new(big.Int).Sub(coreBalAfter, coreBalBefore).Cmp(amount),
		"core balance delta should equal amount: before=%s after=%s amount=%s",
		coreBalBefore, coreBalAfter, amount)

	helpers.LogAssertOK("wrapped.totalSupply decreased by exactly amount: before=%s after=%s amount=%s",
		wrappedSupplyBefore, wrappedSupplyAfter, amount)
	require.Zero(t,
		new(big.Int).Sub(wrappedSupplyBefore, wrappedSupplyAfter).Cmp(amount),
		"wrapped totalSupply delta should equal amount: before=%s after=%s amount=%s",
		wrappedSupplyBefore, wrappedSupplyAfter, amount)

	helpers.LogAssertOK("core.totalSupply increased by exactly amount: before=%s after=%s amount=%s",
		coreSupplyBefore, coreSupplyAfter, amount)
	require.Zero(t,
		new(big.Int).Sub(coreSupplyAfter, coreSupplyBefore).Cmp(amount),
		"core totalSupply delta should equal amount: before=%s after=%s amount=%s",
		coreSupplyBefore, coreSupplyAfter, amount)

	// 7. Verify WrappedCETRedeemed event in the redeem receipt.
	requireWrappedCETRedeemed(t, redeemReceipt, bridgeAddr, wrappedAddr, coreAddr, user.GetAddress(), amount)
}

// --- helpers (test-local) -------------------------------------------------

func readCETBalance(ctx context.Context, t *testing.T, client *ethclient.Client, token, owner common.Address) *big.Int {
	t.Helper()
	cetABI, err := helpers.ParseTestCETABI()
	require.NoError(t, err)
	contract := bind.NewBoundContract(token, cetABI, client, client, client)
	var bal *big.Int
	require.NoError(t,
		contract.Call(&bind.CallOpts{Context: ctx}, &[]any{&bal}, "balanceOf", owner),
		"balanceOf(%s) on %s", owner.Hex(), token.Hex())
	return bal
}

func readCETTotalSupply(ctx context.Context, t *testing.T, client *ethclient.Client, token common.Address) *big.Int {
	t.Helper()
	cetABI, err := helpers.ParseTestCETABI()
	require.NoError(t, err)
	contract := bind.NewBoundContract(token, cetABI, client, client, client)
	var supply *big.Int
	require.NoError(t,
		contract.Call(&bind.CallOpts{Context: ctx}, &[]any{&supply}, "totalSupply"),
		"totalSupply on %s", token.Hex())
	return supply
}

func requireCetType(ctx context.Context, t *testing.T, chain *rollup.Rollup, token common.Address, want uint8) {
	t.Helper()
	client, err := ethclient.DialContext(ctx, chain.RPCURL())
	require.NoError(t, err)
	defer client.Close()
	cetABI, err := helpers.ParseTestCETABI()
	require.NoError(t, err)
	contract := bind.NewBoundContract(token, cetABI, client, client, client)
	var got uint8
	require.NoError(t,
		contract.Call(&bind.CallOpts{Context: ctx}, &[]any{&got}, "cetType"),
		"cetType() on %s", token.Hex())
	require.Equal(t, want, got, "cetType mismatch on %s: got=%d want=%d", token.Hex(), got, want)
}

func requireRemoteAsset(ctx context.Context, t *testing.T, chain *rollup.Rollup, token, want common.Address) {
	t.Helper()
	client, err := ethclient.DialContext(ctx, chain.RPCURL())
	require.NoError(t, err)
	defer client.Close()
	cetABI, err := helpers.ParseTestCETABI()
	require.NoError(t, err)
	contract := bind.NewBoundContract(token, cetABI, client, client, client)
	var got common.Address
	require.NoError(t,
		contract.Call(&bind.CallOpts{Context: ctx}, &[]any{&got}, "remoteAsset"),
		"remoteAsset() on %s", token.Hex())
	require.Equal(t, want, got, "remoteAsset mismatch on %s: got=%s want=%s",
		token.Hex(), got.Hex(), want.Hex())
}

// requireWrappedCETRedeemed scans the receipt for the bridge's
// WrappedCETRedeemed(address wrappedCET, address coreCET, address caller, uint256 amount)
// event and asserts the indexed topics + amount match.
func requireWrappedCETRedeemed(
	t *testing.T,
	receipt *types.Receipt,
	bridge, wrapped, core, caller common.Address,
	amount *big.Int,
) {
	t.Helper()
	eventTopic := crypto.Keccak256Hash([]byte("WrappedCETRedeemed(address,address,address,uint256)"))
	for _, log := range receipt.Logs {
		if log.Address != bridge {
			continue
		}
		if len(log.Topics) < 4 || log.Topics[0] != eventTopic {
			continue
		}
		gotWrapped := common.BytesToAddress(log.Topics[1].Bytes())
		gotCore := common.BytesToAddress(log.Topics[2].Bytes())
		gotCaller := common.BytesToAddress(log.Topics[3].Bytes())

		evt, ok := BridgeABI.Events["WrappedCETRedeemed"]
		require.True(t, ok, "WrappedCETRedeemed event not in BridgeABI")
		data, err := evt.Inputs.NonIndexed().Unpack(log.Data)
		require.NoError(t, err, "decode WrappedCETRedeemed data")
		require.Len(t, data, 1, "WrappedCETRedeemed should have one non-indexed arg")
		gotAmount, _ := data[0].(*big.Int)
		require.NotNil(t, gotAmount, "amount decode failed")

		helpers.LogAssertOK("WrappedCETRedeemed event matches: wrapped=%s core=%s caller=%s amount=%s",
			gotWrapped.Hex(), gotCore.Hex(), gotCaller.Hex(), gotAmount)
		require.Equal(t, wrapped, gotWrapped, "WrappedCETRedeemed.wrappedCET mismatch")
		require.Equal(t, core, gotCore, "WrappedCETRedeemed.coreCET mismatch")
		require.Equal(t, caller, gotCaller, "WrappedCETRedeemed.caller mismatch")
		require.Zero(t, gotAmount.Cmp(amount), "WrappedCETRedeemed.amount mismatch: got=%s want=%s", gotAmount, amount)
		return
	}
	t.Fatalf("WrappedCETRedeemed event not found in receipt tx=%s", receipt.TxHash.Hex())
}
