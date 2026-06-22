/**
 * Helper for composing Smart Account UserOps.
 *
 * Two modes:
 * 1. Standard (hoodi/sepolia-prod): SDK's composeUnpreparedUserOps + composed.send() directly.
 *    The rollup RPCs support compose_buildSignedUserOpsTx and eth_sendXTransaction natively.
 *
 * 2. Sidecar + manual build (sepolia-stage): SDK signs the UserOps (multichain ECDSA),
 *    then we intercept at the transport layer to:
 *    - Manually build EntryPoint handleOps txs from the signed canonical UserOps
 *    - Collect the raw signed txs
 *    - Submit them atomically via the sidecar (POST /xt)
 */
import { ethers } from "ethers";
import { type Hex } from "viem";
import {
  composeUnpreparedUserOps,
  type ComposeUserOpsOptions,
} from "@ssv-labs/ethera-sdk";
import {
  SIDECAR_URL,
  BUNDLER_ROLLUP_A_RPC,
  BUNDLER_ROLLUP_B_RPC,
  ROLLUP_A_CHAIN_ID,
  ROLLUP_B_CHAIN_ID,
  L1_Rollup_1_RPC,
  L1_Rollup_2_RPC,
  wallet_private_key,
} from "../config";
import { submitXt } from "./xt-submit";

interface UserOpData {
  account: any;
  signer: any;
  chainId: number;
  publicClient: any;
  userOp: any;
}

interface ComposeResult {
  hashes: string[];
  wait: () => Promise<any[]>;
}

// Map chainId → bundler URL (used to detect bundler mode)
const BUNDLER_URLS: Record<number, string> = {};
if (BUNDLER_ROLLUP_A_RPC) BUNDLER_URLS[ROLLUP_A_CHAIN_ID] = BUNDLER_ROLLUP_A_RPC;
if (BUNDLER_ROLLUP_B_RPC) BUNDLER_URLS[ROLLUP_B_CHAIN_ID] = BUNDLER_ROLLUP_B_RPC;

const hasBundlers = Object.keys(BUNDLER_URLS).length > 0;
const useSidecar = !!SIDECAR_URL && hasBundlers;

// Bundler requires higher gas fees than the rollup RPC reports.
// Applied BEFORE signing so the UserOp hash includes them.
const MIN_PRIORITY_FEE = 2_000_000_000n;  // 2 Gwei
const MIN_MAX_FEE = 5_000_000_000n;       // 5 Gwei

// EntryPoint v0.7 address
const ENTRYPOINT_ADDRESS = "0x0000000071727De22E5E9d8BAf0edAc6f37da032";

// EntryPoint v0.7 handleOps ABI (PackedUserOperation)
const ENTRYPOINT_IFACE = new ethers.Interface([{
  type: "function",
  name: "handleOps",
  inputs: [
    {
      name: "ops",
      type: "tuple[]",
      components: [
        { name: "sender", type: "address" },
        { name: "nonce", type: "uint256" },
        { name: "initCode", type: "bytes" },
        { name: "callData", type: "bytes" },
        { name: "accountGasLimits", type: "bytes32" },
        { name: "preVerificationGas", type: "uint256" },
        { name: "gasFees", type: "bytes32" },
        { name: "paymasterAndData", type: "bytes" },
        { name: "signature", type: "bytes" },
      ],
    },
    { name: "beneficiary", type: "address" },
  ],
  outputs: [],
  stateMutability: "nonpayable",
}]);

/**
 * Pack two 128-bit values into a single bytes32 (left = high, right = low).
 */
function packUint128Pair(high: bigint, low: bigint): string {
  const h = high.toString(16).padStart(32, "0");
  const l = low.toString(16).padStart(32, "0");
  return "0x" + h + l;
}

/**
 * Convert a signed canonical (RPC-format) UserOp into an EntryPoint v0.7 packed struct
 * and build a signed handleOps transaction.
 */
async function buildHandleOpsTx(
  canonicalOps: any[],
  chainId: number,
): Promise<{ hash: string; raw: string }> {
  // Use the rollup RPC (not bundler) for nonce/fee queries
  const rpcUrl = chainId === ROLLUP_A_CHAIN_ID ? L1_Rollup_1_RPC : L1_Rollup_2_RPC;
  const provider = new ethers.JsonRpcProvider(rpcUrl);
  const wallet = new ethers.Wallet(wallet_private_key, provider);

  // Pack each canonical UserOp into v0.7 format
  const packedOps = canonicalOps.map((op: any) => {
    const initCode = op.initCode || "0x";
    const accountGasLimits = packUint128Pair(
      BigInt(op.verificationGasLimit),
      BigInt(op.callGasLimit),
    );
    const gasFees = packUint128Pair(
      BigInt(op.maxPriorityFeePerGas),
      BigInt(op.maxFeePerGas),
    );

    return {
      sender: op.sender,
      nonce: BigInt(op.nonce),
      initCode,
      callData: op.callData,
      accountGasLimits,
      preVerificationGas: BigInt(op.preVerificationGas),
      gasFees,
      paymasterAndData: "0x",
      signature: op.signature,
    };
  });

  const handleOpsData = ENTRYPOINT_IFACE.encodeFunctionData("handleOps", [
    packedOps,
    wallet.address, // beneficiary
  ]);

  // Compute total gas from all ops
  const totalGas = canonicalOps.reduce((sum: bigint, op: any) => {
    return sum
      + BigInt(op.callGasLimit)
      + BigInt(op.verificationGasLimit)
      + BigInt(op.preVerificationGas);
  }, 0n) + 100_000n; // overhead

  const nonce = await provider.getTransactionCount(wallet.address, "pending");
  const feeData = await provider.getFeeData();

  const tx = {
    to: ENTRYPOINT_ADDRESS,
    data: handleOpsData,
    nonce,
    gasLimit: totalGas,
    maxFeePerGas: feeData.maxFeePerGas!,
    maxPriorityFeePerGas: feeData.maxPriorityFeePerGas!,
    chainId,
    type: 2,
  };

  const signedTx = await wallet.signTransaction(tx);
  const txHash = ethers.keccak256(signedTx);

  return { hash: txHash, raw: signedTx };
}

// Collect raw txs across all chains during the compose step
let pendingRawTxs: Array<{ chainId: number; rawTx: string; hash: string }> = [];

/**
 * Wrap a viem publicClient to intercept compose RPC methods.
 *
 * Sidecar mode: manually build handleOps from signed UserOps, collect raw txs,
 * then submit atomically via sidecar in eth_sendXTransaction.
 */
function wrapClientForSidecar(client: any, chainId: number): any {
  const originalRequest = client.request.bind(client);

  return new Proxy(client, {
    get(target: any, prop: string | symbol) {
      if (prop === "request") {
        return async (args: { method: string; params?: any }) => {
          if (args.method === "compose_buildSignedUserOpsTx") {
            // Build handleOps tx from the SDK's signed canonical UserOps
            const canonicalOps = args.params[0]; // array of signed canonical UserOps
            const result = await buildHandleOpsTx(canonicalOps, chainId);

            // Collect for sidecar submission
            pendingRawTxs.push({ chainId, rawTx: result.raw, hash: result.hash });
            console.log(`  [sidecar] Built handleOps for chain ${chainId} (hash: ${result.hash.slice(0, 18)}...)`);

            // Return {hash, raw} — SDK will use these for XT message encoding
            return result;
          }
          if (args.method === "eth_sendXTransaction") {
            // Submit all collected raw txs via sidecar
            console.log(`  [sidecar] Submitting ${pendingRawTxs.length} txs via sidecar...`);
            const entries = pendingRawTxs.map((e) => ({
              chainId: e.chainId,
              rawTx: e.rawTx,
            }));
            const xtResult = await submitXt(entries, "");
            console.log(`  [sidecar] Submitted:`, JSON.stringify(xtResult.result));

            // Clear pending txs
            const hashes = pendingRawTxs.map((e) => e.hash);
            pendingRawTxs = [];

            return "0x0"; // SDK ignores this return value
          }
          return originalRequest(args);
        };
      }
      return target[prop];
    },
  });
}

/**
 * Wrap operations for sidecar/bundler compatibility if needed.
 * - Bumps UserOp gas fees to meet bundler minimums BEFORE signing
 * - Wraps publicClient to intercept compose RPC methods
 */
function maybeWrapOps(operations: UserOpData[]): UserOpData[] {
  if (!useSidecar) return operations;

  // Reset pending txs for this compose batch
  pendingRawTxs = [];

  return operations.map((op) => {
    // Bump fees before signing — prepareUserOperation preserves existing bigint fees
    if (typeof op.userOp?.maxPriorityFeePerGas === "bigint" && op.userOp.maxPriorityFeePerGas < MIN_PRIORITY_FEE) {
      op.userOp.maxPriorityFeePerGas = MIN_PRIORITY_FEE;
    }
    if (typeof op.userOp?.maxFeePerGas === "bigint" && op.userOp.maxFeePerGas < MIN_MAX_FEE) {
      op.userOp.maxFeePerGas = MIN_MAX_FEE;
    }
    return { ...op, publicClient: wrapClientForSidecar(op.publicClient, op.chainId) };
  });
}

/**
 * Drop-in replacement for composeUnpreparedUserOps that handles sidecar submission.
 * Use this instead of importing composeUnpreparedUserOps directly.
 */
export async function composeOps(
  operations: UserOpData[],
  options?: ComposeUserOpsOptions
) {
  return composeUnpreparedUserOps(maybeWrapOps(operations), options);
}

/**
 * Compose, send, and return {hashes, wait}.
 */
export async function composeAndSubmit(
  operations: UserOpData[],
  options?: ComposeUserOpsOptions
): Promise<ComposeResult> {
  const composed = await composeOps(operations, options);
  const sendResult = await composed.send();
  return sendResult;
}
