// Smart-account helpers. The TS helper at scripts/sa-helper.ts handles the
// ZeroDev multichain-ECDSA signing and gives us signed canonical UserOps;
// here in Go we pack them into EntryPoint v0.7 PackedUserOperation, ABI-encode
// handleOps, sign the outer EIP-1559 tx, and submit via the existing
// sidecar/rpc helpers.
package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/ethera-labs/dome/configs"
	"github.com/ethera-labs/dome/internal/accounts"
)

// EntryPointV07Address is the canonical ERC-4337 v0.7 EntryPoint.
var EntryPointV07Address = common.HexToAddress("0x0000000071727De22E5E9d8BAf0edAc6f37da032")

// EntryPointABI exposes the few view + state-changing methods the test helpers
// touch directly (balanceOf + depositTo). For handleOps we use HandleOpsABI.
const EntryPointABI = `[
  {"type":"function","name":"balanceOf","stateMutability":"view","inputs":[{"name":"account","type":"address"}],"outputs":[{"type":"uint256"}]},
  {"type":"function","name":"depositTo","stateMutability":"payable","inputs":[{"name":"account","type":"address"}],"outputs":[]}
]`

// MinEntryPointDeposit is the floor we top SA deposits up to. Mirrors the TS
// scripts' 0.05 ETH constant.
var MinEntryPointDeposit = new(big.Int).Mul(big.NewInt(5), big.NewInt(10_000_000_000_000_000)) // 0.05 ETH

// UserOperationEventTopic is the topic0 of EntryPoint v0.7's UserOperationEvent.
// Data layout: (uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed).
//   keccak256("UserOperationEvent(bytes32,address,address,uint256,bool,uint256,uint256)")
var UserOperationEventTopic = common.HexToHash("0x49628fd1471006c1482da88028e9ce4dbb080b815c9b0344d39e5a8e6ec1419f")

// CheckUserOpSuccess scans the receipt for an EntryPoint UserOperationEvent
// and returns success/actualGasUsed. If no event is present, returns
// (false, 0, error).
func CheckUserOpSuccess(receipt *types.Receipt) (success bool, actualGasUsed *big.Int, err error) {
	for _, log := range receipt.Logs {
		if log.Address != EntryPointV07Address {
			continue
		}
		if len(log.Topics) == 0 || log.Topics[0] != UserOperationEventTopic {
			continue
		}
		// Non-indexed data: nonce, success, actualGasCost, actualGasUsed (all uint256/bool).
		if len(log.Data) < 4*32 {
			return false, nil, fmt.Errorf("UserOperationEvent data too short: %d", len(log.Data))
		}
		// success is bytes 32-64.
		successWord := log.Data[32:64]
		success = successWord[31] == 1
		actualGasUsed = new(big.Int).SetBytes(log.Data[96:128])
		return success, actualGasUsed, nil
	}
	return false, nil, fmt.Errorf("no UserOperationEvent in receipt")
}

// CanonicalUserOp mirrors what scripts/sa-helper.ts writes to stdout under
// `userOps`. All numbers are decimal strings so we don't lose precision.
type CanonicalUserOp struct {
	ChainID              uint64 `json:"chainId"`
	Sender               string `json:"sender"`
	Nonce                string `json:"nonce"`
	InitCode             string `json:"initCode"`
	CallData             string `json:"callData"`
	CallGasLimit         string `json:"callGasLimit"`
	VerificationGasLimit string `json:"verificationGasLimit"`
	PreVerificationGas   string `json:"preVerificationGas"`
	MaxFeePerGas         string `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas"`
	PaymasterAndData     string `json:"paymasterAndData"`
	Signature            string `json:"signature"`
}

// UserOpCall is the Go-side mirror of the TS UserOPCall type used by the
// helper's --calls argument.
type UserOpCall struct {
	ChainID uint64         `json:"chainId"`
	To      common.Address `json:"to"`
	Value   string         `json:"value"` // decimal wei string
	Data    string         `json:"data"`  // hex with 0x prefix
}

// SAGasOverride forces specific UserOp gas fields on a chain. The default
// estimator undershoots for cross-chain calls (each tx fails when simulated
// in isolation), so callers can override per the working TS scripts:
//
//	src: callGasLimit=3M; dst: callGasLimit=5M, verificationGasLimit=3.5M
type SAGasOverride struct {
	ChainID              uint64 `json:"chainId"`
	CallGasLimit         string `json:"callGasLimit,omitempty"`
	VerificationGasLimit string `json:"verificationGasLimit,omitempty"`
	PreVerificationGas   string `json:"preVerificationGas,omitempty"`
}

// StandardSATokenBridgeGasOverrides returns the gas overrides the existing TS
// SA-token scripts use: src callGas 3M, dst callGas 5M, dst verifGas 3.5M.
func StandardSATokenBridgeGasOverrides(srcChainID, dstChainID uint64) []SAGasOverride {
	return []SAGasOverride{
		{ChainID: srcChainID, CallGasLimit: "3000000"},
		{ChainID: dstChainID, CallGasLimit: "5000000", VerificationGasLimit: "3500000"},
	}
}

// ---------------------------------------------------------------------------
// TS helper wrappers
// ---------------------------------------------------------------------------

// SACreateAccount returns the smart-account address for (pk, chainID) on the
// active network.
func SACreateAccount(
	ctx context.Context,
	pk string,
	chainID uint64,
	multiChainIDs []uint64,
) (common.Address, bool, error) {
	scriptPath, err := saHelperPath()
	if err != nil {
		return common.Address{}, false, err
	}
	if err := checkScriptsNodeModules(filepath.Dir(scriptPath)); err != nil {
		return common.Address{}, false, err
	}

	pkArg := pk
	if len(pkArg) > 0 && pkArg[:2] != "0x" {
		pkArg = "0x" + pkArg
	}

	multiStr := joinUint64s(multiChainIDs)
	cmd := exec.CommandContext(ctx, "npx", "ts-node", "--transpile-only",
		scriptPath, "create-account",
		"--network", configs.Values.Network,
		"--private-key", pkArg,
		"--chain-id", fmt.Sprintf("%d", chainID),
		"--multi-chain-ids", multiStr,
	)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return common.Address{}, false, fmt.Errorf("create-account: %w", err)
	}
	var resp struct {
		SmartAccountAddress common.Address `json:"smartAccountAddress"`
		IsDeployed          bool           `json:"isDeployed"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return common.Address{}, false, fmt.Errorf("decode create-account output %q: %w", string(out), err)
	}
	return resp.SmartAccountAddress, resp.IsDeployed, nil
}

// SACreateUserOps invokes the TS helper to sign canonical UserOps for the
// given calls. Use when xt-submission=sidecar (Go will build the handleOps txs).
// `overrides` may be nil for default gas estimation.
func SACreateUserOps(
	ctx context.Context,
	pk string,
	calls []UserOpCall,
	overrides []SAGasOverride,
) ([]CanonicalUserOp, error) {
	scriptPath, err := saHelperPath()
	if err != nil {
		return nil, err
	}
	if err := checkScriptsNodeModules(filepath.Dir(scriptPath)); err != nil {
		return nil, err
	}

	pkArg := pk
	if len(pkArg) > 0 && pkArg[:2] != "0x" {
		pkArg = "0x" + pkArg
	}
	callsJSON, err := json.Marshal(calls)
	if err != nil {
		return nil, err
	}

	args := []string{"ts-node", "--transpile-only",
		scriptPath, "create-userops",
		"--network", configs.Values.Network,
		"--private-key", pkArg,
		"--calls", string(callsJSON),
	}
	if len(overrides) > 0 {
		ovJSON, err := json.Marshal(overrides)
		if err != nil {
			return nil, err
		}
		args = append(args, "--gas-overrides", string(ovJSON))
	}

	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("create-userops: %w", err)
	}
	var resp struct {
		UserOps []CanonicalUserOp `json:"userOps"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode create-userops output: %w", err)
	}
	return resp.UserOps, nil
}

// SAComposedTx pairs a tx hash with the chain it landed on. The TS helper
// returns one entry per UserOp passed to the SDK, in submission order.
type SAComposedTx struct {
	Hash    common.Hash
	ChainID uint64
}

// SAComposeAndSubmit calls the SDK's full compose+send+wait flow via TS.
// Returns the per-chain tx hashes paired with their chain IDs. Use when
// xt-submission=rpc (hoodi/sepolia-prod); the SDK handles encodeXtMessage and
// eth_sendXTransaction internally and the TS helper blocks on receipts so
// callers can immediately query them.
func SAComposeAndSubmit(
	ctx context.Context,
	pk string,
	calls []UserOpCall,
	overrides []SAGasOverride,
) ([]SAComposedTx, error) {
	scriptPath, err := saHelperPath()
	if err != nil {
		return nil, err
	}
	if err := checkScriptsNodeModules(filepath.Dir(scriptPath)); err != nil {
		return nil, err
	}

	pkArg := pk
	if len(pkArg) > 0 && pkArg[:2] != "0x" {
		pkArg = "0x" + pkArg
	}
	callsJSON, err := json.Marshal(calls)
	if err != nil {
		return nil, err
	}

	args := []string{"ts-node", "--transpile-only",
		scriptPath, "compose-and-submit",
		"--network", configs.Values.Network,
		"--private-key", pkArg,
		"--calls", string(callsJSON),
	}
	if len(overrides) > 0 {
		ovJSON, err := json.Marshal(overrides)
		if err != nil {
			return nil, err
		}
		args = append(args, "--gas-overrides", string(ovJSON))
	}

	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("compose-and-submit: %w", err)
	}
	var resp struct {
		Hashes   []string `json:"hashes"`
		ChainIDs []uint64 `json:"chainIds"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode compose-and-submit output: %w", err)
	}
	if len(resp.ChainIDs) != len(resp.Hashes) {
		return nil, fmt.Errorf("compose-and-submit: hashes(%d) != chainIds(%d)",
			len(resp.Hashes), len(resp.ChainIDs))
	}
	out2 := make([]SAComposedTx, len(resp.Hashes))
	for i := range resp.Hashes {
		out2[i] = SAComposedTx{
			Hash:    common.HexToHash(resp.Hashes[i]),
			ChainID: resp.ChainIDs[i],
		}
	}
	return out2, nil
}

// ---------------------------------------------------------------------------
// EntryPoint v0.7 handleOps building (Go-side, used in sidecar mode)
// ---------------------------------------------------------------------------

// PackedUserOperation matches the EntryPoint v0.7 PackedUserOperation struct.
type PackedUserOperation struct {
	Sender             common.Address `abi:"sender"`
	Nonce              *big.Int       `abi:"nonce"`
	InitCode           []byte         `abi:"initCode"`
	CallData           []byte         `abi:"callData"`
	AccountGasLimits   [32]byte       `abi:"accountGasLimits"`
	PreVerificationGas *big.Int       `abi:"preVerificationGas"`
	GasFees            [32]byte       `abi:"gasFees"`
	PaymasterAndData   []byte         `abi:"paymasterAndData"`
	Signature          []byte         `abi:"signature"`
}

// HandleOpsABI is the minimal ABI for EntryPoint.handleOps(PackedUserOperation[], address).
const HandleOpsABI = `[{
  "type":"function","name":"handleOps","stateMutability":"nonpayable","outputs":[],
  "inputs":[
    {"name":"ops","type":"tuple[]","components":[
      {"name":"sender","type":"address"},
      {"name":"nonce","type":"uint256"},
      {"name":"initCode","type":"bytes"},
      {"name":"callData","type":"bytes"},
      {"name":"accountGasLimits","type":"bytes32"},
      {"name":"preVerificationGas","type":"uint256"},
      {"name":"gasFees","type":"bytes32"},
      {"name":"paymasterAndData","type":"bytes"},
      {"name":"signature","type":"bytes"}
    ]},
    {"name":"beneficiary","type":"address"}
  ]
}]`

// PackHandleOps encodes handleOps(PackedUserOperation[], beneficiary).
func PackHandleOps(ops []PackedUserOperation, beneficiary common.Address) ([]byte, error) {
	parsed, err := abi.JSON(strings.NewReader(HandleOpsABI))
	if err != nil {
		return nil, fmt.Errorf("parse handleOps abi: %w", err)
	}
	return parsed.Pack("handleOps", ops, beneficiary)
}

// EnsureEntryPointDeposit tops up the EntryPoint deposit for `saAddress` to at
// least `min` by calling depositTo from `funder`. Skips if the deposit is
// already sufficient.
func EnsureEntryPointDeposit(
	ctx context.Context,
	funder *accounts.Account,
	saAddress common.Address,
	min *big.Int,
) error {
	parsedABI, err := abi.JSON(strings.NewReader(EntryPointABI))
	if err != nil {
		return fmt.Errorf("parse entrypoint abi: %w", err)
	}

	// Read current deposit via bound contract.
	client, err := ethclient.DialContext(ctx, funder.GetRollup().RPCURL())
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer client.Close()
	bound := bind.NewBoundContract(EntryPointV07Address, parsedABI, client, client, client)
	var current *big.Int
	if err := bound.Call(&bind.CallOpts{Context: ctx}, &[]any{&current}, "balanceOf", saAddress); err != nil {
		return fmt.Errorf("balanceOf: %w", err)
	}
	if current.Cmp(min) >= 0 {
		return nil
	}
	needed := new(big.Int).Sub(min, current)

	depositData, err := parsedABI.Pack("depositTo", saAddress)
	if err != nil {
		return fmt.Errorf("pack depositTo: %w", err)
	}
	_, _, err = SendL1Tx(ctx, funder, EntryPointV07Address, needed, 100_000, depositData)
	if err != nil {
		return fmt.Errorf("depositTo: %w", err)
	}
	return nil
}

// PackCanonicalAsV07 converts a signed canonical UserOp from the TS helper into
// the on-chain PackedUserOperation format that handleOps expects.
func PackCanonicalAsV07(op CanonicalUserOp) (PackedUserOperation, error) {
	nonce, ok := new(big.Int).SetString(op.Nonce, 10)
	if !ok {
		return PackedUserOperation{}, fmt.Errorf("nonce %q not decimal", op.Nonce)
	}
	verifGas, ok := new(big.Int).SetString(op.VerificationGasLimit, 10)
	if !ok {
		return PackedUserOperation{}, fmt.Errorf("verificationGasLimit %q not decimal", op.VerificationGasLimit)
	}
	callGas, ok := new(big.Int).SetString(op.CallGasLimit, 10)
	if !ok {
		return PackedUserOperation{}, fmt.Errorf("callGasLimit %q not decimal", op.CallGasLimit)
	}
	preVerifGas, ok := new(big.Int).SetString(op.PreVerificationGas, 10)
	if !ok {
		return PackedUserOperation{}, fmt.Errorf("preVerificationGas %q not decimal", op.PreVerificationGas)
	}
	maxFee, ok := new(big.Int).SetString(op.MaxFeePerGas, 10)
	if !ok {
		return PackedUserOperation{}, fmt.Errorf("maxFeePerGas %q not decimal", op.MaxFeePerGas)
	}
	maxPriority, ok := new(big.Int).SetString(op.MaxPriorityFeePerGas, 10)
	if !ok {
		return PackedUserOperation{}, fmt.Errorf("maxPriorityFeePerGas %q not decimal", op.MaxPriorityFeePerGas)
	}

	initCode, err := hexDecode(op.InitCode)
	if err != nil {
		return PackedUserOperation{}, fmt.Errorf("initCode: %w", err)
	}
	callData, err := hexDecode(op.CallData)
	if err != nil {
		return PackedUserOperation{}, fmt.Errorf("callData: %w", err)
	}
	paymasterAndData, err := hexDecode(op.PaymasterAndData)
	if err != nil {
		return PackedUserOperation{}, fmt.Errorf("paymasterAndData: %w", err)
	}
	signature, err := hexDecode(op.Signature)
	if err != nil {
		return PackedUserOperation{}, fmt.Errorf("signature: %w", err)
	}

	return PackedUserOperation{
		Sender:             common.HexToAddress(op.Sender),
		Nonce:              nonce,
		InitCode:           initCode,
		CallData:           callData,
		AccountGasLimits:   packUint128Pair(verifGas, callGas),
		PreVerificationGas: preVerifGas,
		GasFees:            packUint128Pair(maxPriority, maxFee),
		PaymasterAndData:   paymasterAndData,
		Signature:          signature,
	}, nil
}

// BuildHandleOpsRawTx packs `ops` into handleOps calldata, builds an EIP-1559
// transaction targeting the v0.7 EntryPoint with the funder as beneficiary,
// signs with `ac`, and returns the signed raw tx bytes ready for sidecar
// submission. `ops` should all be from the same chain — pass the chainID
// explicitly to avoid sniffing from ac (in case the funder is on L1).
func BuildHandleOpsRawTx(
	ctx context.Context,
	ac *accounts.Account,
	ops []CanonicalUserOp,
) (*types.Transaction, []byte, error) {
	if len(ops) == 0 {
		return nil, nil, fmt.Errorf("no ops")
	}
	packed := make([]PackedUserOperation, len(ops))
	for i, op := range ops {
		p, err := PackCanonicalAsV07(op)
		if err != nil {
			return nil, nil, fmt.Errorf("op %d: %w", i, err)
		}
		packed[i] = p
	}

	data, err := PackHandleOps(packed, ac.GetAddress())
	if err != nil {
		return nil, nil, fmt.Errorf("pack handleOps: %w", err)
	}

	// Outer tx gas = sum of (callGas + verifGas + preVerifGas) + 100K overhead.
	totalGas := uint64(100_000)
	for _, op := range ops {
		cg, _ := new(big.Int).SetString(op.CallGasLimit, 10)
		vg, _ := new(big.Int).SetString(op.VerificationGasLimit, 10)
		pvg, _ := new(big.Int).SetString(op.PreVerificationGas, 10)
		totalGas += cg.Uint64() + vg.Uint64() + pvg.Uint64()
	}

	// Use the bumped-fee floor for sepolia-stage; otherwise the ops fees.
	maxPriority, _ := new(big.Int).SetString(ops[0].MaxPriorityFeePerGas, 10)
	maxFee, _ := new(big.Int).SetString(ops[0].MaxFeePerGas, 10)

	nonce, err := ac.GetNonce(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("nonce: %w", err)
	}
	chainID := ac.GetRollup().ChainID()
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		To:        &EntryPointV07Address,
		Gas:       totalGas,
		GasTipCap: maxPriority,
		GasFeeCap: maxFee,
		Value:     big.NewInt(0),
		Data:      data,
	})
	signed, err := types.SignTx(tx, types.NewLondonSigner(chainID), ac.GetPrivateKey())
	if err != nil {
		return nil, nil, fmt.Errorf("sign handleOps tx: %w", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, nil, fmt.Errorf("marshal handleOps tx: %w", err)
	}
	return signed, raw, nil
}

// ---------------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------------

func packUint128Pair(high, low *big.Int) [32]byte {
	var out [32]byte
	highBytes := high.Bytes()
	lowBytes := low.Bytes()
	copy(out[16-len(highBytes):16], highBytes)
	copy(out[32-len(lowBytes):32], lowBytes)
	return out
}

func hexDecode(s string) ([]byte, error) {
	if s == "" || s == "0x" {
		return []byte{}, nil
	}
	return hexutil.Decode(s)
}

func joinUint64s(xs []uint64) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%d", x)
	}
	return strings.Join(parts, ",")
}

func saHelperPath() (string, error) {
	if env := os.Getenv("DOME_SA_HELPER_SCRIPT"); env != "" {
		return env, nil
	}
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	path := filepath.Join(repoRoot, "scripts", "sa-helper.ts")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("scripts/sa-helper.ts not found at %s; set DOME_SA_HELPER_SCRIPT to override", path)
}

func checkScriptsNodeModules(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		return fmt.Errorf(
			"node_modules missing in %s — run `make scripts-install` (or `cd %s && npm install --legacy-peer-deps`) once",
			dir, dir,
		)
	}
	return nil
}

