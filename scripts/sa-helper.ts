/**
 * Smart-account TS helper invoked by Go.
 *
 * Subcommands:
 *
 *   create-account
 *     Returns {smartAccountAddress, isDeployed} for the EOA + chain.
 *
 *   create-userops
 *     Given a JSON array of UserOPCall entries grouped by chainId, runs the
 *     SDK's compose flow up to the *signing* step and returns the canonical
 *     signed UserOps — without ever calling compose_buildSignedUserOpsTx or
 *     eth_sendXTransaction. Go can then build EntryPoint v0.7 handleOps txs
 *     and submit via the sidecar.
 *
 *   compose-and-submit
 *     Calls the full SDK compose+send. Returns the resulting tx hashes
 *     (one per chain). Use this on hoodi / sepolia-prod where the compose
 *     sequencer RPC handles the wire format natively.
 *
 * All subcommands take --network <hoodi|sepolia-prod|sepolia-stage>,
 * --private-key 0x.., and the calls are per-subcommand.
 */
import { ethers } from "ethers";
import {
  http,
  type Hex,
} from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { createConfig } from "@wagmi/core";
import {
  createComposeConfig,
  createSmartAccount,
  composeUnpreparedUserOps,
  type UserOPCall,
} from "@ssv-labs/ethera-sdk";

// ---------------------------------------------------------------------------
// Network config — addresses + chain ids — kept in lock-step with config.ts.
// ---------------------------------------------------------------------------
type NetworkID = "hoodi" | "sepolia-prod" | "sepolia-stage";

interface NetworkCfg {
  rollupA: {
    id: number;
    rpc: string;
    bundlerRpc?: string;
  };
  rollupB: {
    id: number;
    rpc: string;
    bundlerRpc?: string;
  };
  aa: {
    kernelImpl: Hex;
    kernelFactory: Hex;
    multichainValidator: Hex;
  };
  sidecarUrl?: string;
}

const NETWORKS: Record<NetworkID, NetworkCfg> = {
  "hoodi": {
    rollupA: { id: 11113, rpc: "https://rpc-a.testnet.compose.network/" },
    rollupB: { id: 22224, rpc: "https://rpc-b.testnet.compose.network/" },
    aa: {
      kernelImpl: "0x317A2D4564778A585BAd21376dC1ca65b75ccC6a",
      kernelFactory: "0xdEF4343958B5dE047bddEFaB5Fa8F9Ff898890e5",
      multichainValidator: "0x8aB3f6935399e1c10419cA2C93d60901a256b7e3",
    },
  },
  "sepolia-prod": {
    rollupA: { id: 555555, rpc: "https://rpc-a-altda.sepolia.ethera-labs.io/" },
    rollupB: { id: 666666, rpc: "https://rpc-b-altda.sepolia.ethera-labs.io/" },
    aa: {
      kernelImpl: "0xBAC849bB641841b44E965fB01A4Bf5F074f84b4D",
      kernelFactory: "0xaac5D4240AF87249B3f71BC8E4A2cae074A3E419",
      multichainValidator: "0x37CE732412539644b3d0E959925a4f89edd463c9",
    },
  },
  "sepolia-stage": {
    rollupA: {
      id: 100003,
      rpc: "https://op-rbuilder-a.stage.ethera-labs.io",
      bundlerRpc: "https://bundler-a.stage.ethera-labs.io",
    },
    rollupB: {
      id: 200005,
      rpc: "https://op-rbuilder-b.stage.ethera-labs.io",
      bundlerRpc: "https://bundler-b.stage.ethera-labs.io",
    },
    aa: {
      kernelImpl: "0xBAC849bB641841b44E965fB01A4Bf5F074f84b4D",
      kernelFactory: "0xaac5D4240AF87249B3f71BC8E4A2cae074A3E419",
      multichainValidator: "0x37CE732412539644b3d0E959925a4f89edd463c9",
    },
    sidecarUrl: "http://127.0.0.1:18080/xt",
  },
};

const MIN_PRIORITY_FEE = 2_000_000_000n;
const MIN_MAX_FEE = 5_000_000_000n;

function defineChain(id: number, name: string, rpc: string) {
  // Inline viem.defineChain shape — avoids version churn on viem types.
  // blockExplorers must be present because the SDK does `new URL(explorerUrl)`
  // when assembling the composeSignedUserOps build metadata.
  return {
    id,
    name,
    nativeCurrency: { name: "Ether", symbol: "ETH", decimals: 18 },
    rpcUrls: { default: { http: [rpc] } },
    blockExplorers: {
      default: { name: `${name} Explorer`, url: "https://example.invalid" },
    },
    testnet: true,
  };
}

function getArg(flag: string): string | undefined {
  const idx = process.argv.indexOf(flag);
  if (idx !== -1 && idx + 1 < process.argv.length) {
    return process.argv[idx + 1];
  }
  return undefined;
}

function fail(msg: string, err?: any): never {
  console.error(`[sa-helper] ${msg}`);
  if (err && err.stack) console.error(err.stack);
  process.exit(1);
  throw new Error(msg); // keep TS happy if process is untyped
}

function getNetwork(): { id: NetworkID; cfg: NetworkCfg } {
  const id = getArg("--network") as NetworkID | undefined;
  if (!id || !NETWORKS[id]) fail("missing/invalid --network");
  return { id: id!, cfg: NETWORKS[id!] };
}

function getPrivateKey(): Hex {
  const raw = getArg("--private-key");
  if (!raw) fail("missing --private-key");
  return (raw!.startsWith("0x") ? raw! : `0x${raw!}`) as Hex;
}

function buildComposeConfig(cfg: NetworkCfg) {
  const rollupA = defineChain(cfg.rollupA.id, "Rollup A", cfg.rollupA.rpc);
  const rollupB = defineChain(cfg.rollupB.id, "Rollup B", cfg.rollupB.rpc);

  const wagmi = (createConfig as any)({
    chains: [rollupA, rollupB],
    transports: {
      [rollupA.id]: http(rollupA.rpcUrls.default.http[0]),
      [rollupB.id]: http(rollupB.rpcUrls.default.http[0]),
    },
  });

  const compose = createComposeConfig({
    wagmi,
    accountAbstractionContracts: {
      [rollupA.id]: cfg.aa,
      [rollupB.id]: cfg.aa,
    },
  });

  return { rollupA, rollupB, wagmi, compose };
}

// ---------------------------------------------------------------------------
// create-account
// ---------------------------------------------------------------------------
async function createAccount() {
  const { cfg } = getNetwork();
  const pk = getPrivateKey();
  const chainIdStr = getArg("--chain-id");
  if (!chainIdStr) fail("missing --chain-id");
  const chainId = parseInt(chainIdStr!, 10);

  const multiStr = getArg("--multi-chain-ids");
  if (!multiStr) fail("missing --multi-chain-ids (comma-separated)");
  const multiChainIds = multiStr!.split(",").map((s) => parseInt(s.trim(), 10));

  const { compose, rollupA, rollupB } = buildComposeConfig(cfg);
  const signer = privateKeyToAccount(pk);

  const sa = await createSmartAccount(
    { signer, chainId, multiChainIds },
    compose as any,
  );

  // isDeployed: best-effort via getCode.
  const rpc = chainId === rollupA.id ? cfg.rollupA.rpc : cfg.rollupB.rpc;
  const provider = new ethers.JsonRpcProvider(rpc);
  const code = await provider.getCode(sa.account.address);
  const isDeployed = code !== "0x" && code !== "0x0";

  process.stdout.write(JSON.stringify({
    smartAccountAddress: sa.account.address,
    isDeployed,
  }) + "\n");
}

// ---------------------------------------------------------------------------
// Shared: prepare and sign UserOps for a list of (chainId, calls).
// ---------------------------------------------------------------------------
interface ChainCalls {
  chainId: number;
  calls: UserOPCall[];
}

interface CapturedUserOps {
  capturedByChain: Record<number, any[]>;
}

interface GasOverride {
  chainId: number;
  callGasLimit?: string;
  verificationGasLimit?: string;
  preVerificationGas?: string;
}

function parseGasOverrides(): Map<number, GasOverride> {
  const raw = getArg("--gas-overrides");
  const out = new Map<number, GasOverride>();
  if (!raw) return out;
  try {
    const arr = JSON.parse(raw) as GasOverride[];
    for (const o of arr) out.set(o.chainId, o);
  } catch (e: any) {
    fail(`invalid --gas-overrides JSON: ${e.message || e}`);
  }
  return out;
}

async function prepareAndSignUserOps(
  cfg: NetworkCfg,
  pk: Hex,
  groups: ChainCalls[],
  gasOverrides?: Map<number, GasOverride>,
): Promise<{ capturedOps: CapturedUserOps; eoa: string }> {
  const { compose, rollupA, rollupB } = buildComposeConfig(cfg);
  const signer = privateKeyToAccount(pk);
  const multiChainIds = [rollupA.id, rollupB.id];

  // Build operations array (one per chain group).
  const operations = [];
  for (const g of groups) {
    const sa = await createSmartAccount(
      { signer, chainId: g.chainId, multiChainIds },
      compose as any,
    );
    // Suppress warning noise from gas estimation in cross-chain calls.
    const origWarn = console.warn;
    console.warn = () => {};
    const userOp = await sa.account.createUserOp(g.calls);
    console.warn = origWarn;

    operations.push(userOp);
  }

  // Bump fees if needed before signing — sepolia-stage bundler requires it.
  for (const op of operations) {
    if (typeof op?.userOp?.maxPriorityFeePerGas === "bigint" &&
        op.userOp.maxPriorityFeePerGas < MIN_PRIORITY_FEE) {
      op.userOp.maxPriorityFeePerGas = MIN_PRIORITY_FEE;
    }
    if (typeof op?.userOp?.maxFeePerGas === "bigint" &&
        op.userOp.maxFeePerGas < MIN_MAX_FEE) {
      op.userOp.maxFeePerGas = MIN_MAX_FEE;
    }
  }

  // Apply per-chain gas overrides (callGasLimit etc.) — needed for cross-chain
  // calls where the default estimator undershoots because it simulates each
  // tx in isolation.
  if (gasOverrides && gasOverrides.size > 0) {
    for (const op of operations) {
      const ov = gasOverrides.get(op.chainId);
      if (!ov) continue;
      if (ov.callGasLimit) op.userOp.callGasLimit = BigInt(ov.callGasLimit);
      if (ov.verificationGasLimit) op.userOp.verificationGasLimit = BigInt(ov.verificationGasLimit);
      if (ov.preVerificationGas) op.userOp.preVerificationGas = BigInt(ov.preVerificationGas);
    }
  }

  // Intercept compose_buildSignedUserOpsTx + eth_sendXTransaction on each
  // chain's publicClient so the SDK signs UserOps without trying to submit.
  const capturedByChain: Record<number, any[]> = {};

  for (const op of operations) {
    const original = op.publicClient.request.bind(op.publicClient);
    op.publicClient = new Proxy(op.publicClient, {
      get(target: any, prop: string | symbol) {
        if (prop === "request") {
          return async (args: { method: string; params?: any }) => {
            if (args.method === "compose_buildSignedUserOpsTx") {
              const canonicalOps = args.params?.[0] ?? [];
              capturedByChain[op.chainId] = canonicalOps;
              // Return a dummy {hash, raw} — the SDK uses this for XT
              // encoding which we never actually submit.
              return {
                hash: "0x0000000000000000000000000000000000000000000000000000000000000000",
                raw: "0x",
              };
            }
            if (args.method === "eth_sendXTransaction") {
              // Silently swallow — we just wanted the signed UserOps.
              return "0x0";
            }
            return original(args);
          };
        }
        return target[prop];
      },
    });
  }

  await composeUnpreparedUserOps(operations);

  return { capturedOps: { capturedByChain }, eoa: signer.address };
}

// ---------------------------------------------------------------------------
// create-userops
// ---------------------------------------------------------------------------
async function createUserOps() {
  const { cfg } = getNetwork();
  const pk = getPrivateKey();
  const callsArg = getArg("--calls");
  if (!callsArg) fail("missing --calls (JSON)");
  const raw = JSON.parse(callsArg!) as Array<{
    chainId: number;
    to: Hex;
    value: string;
    data: Hex;
  }>;

  // Group calls by chainId, casting value strings to BigInt.
  const grouped: Map<number, UserOPCall[]> = new Map();
  for (const c of raw) {
    const arr = grouped.get(c.chainId) ?? [];
    arr.push({ to: c.to, value: BigInt(c.value), data: c.data });
    grouped.set(c.chainId, arr);
  }

  const groups: ChainCalls[] = Array.from(grouped.entries()).map(
    ([chainId, calls]) => ({ chainId, calls }),
  );

  const overrides = parseGasOverrides();
  const { capturedOps } = await prepareAndSignUserOps(cfg, pk, groups, overrides);

  // Emit one record per chain. We serialize bigints as decimal strings —
  // Go will parse them back.
  const out: any[] = [];
  for (const [chainId, ops] of Object.entries(capturedOps.capturedByChain)) {
    for (const op of ops as any[]) {
      out.push({
        chainId: Number(chainId),
        sender: op.sender,
        nonce: String(BigInt(op.nonce)),
        initCode: op.initCode ?? "0x",
        callData: op.callData,
        callGasLimit: String(BigInt(op.callGasLimit)),
        verificationGasLimit: String(BigInt(op.verificationGasLimit)),
        preVerificationGas: String(BigInt(op.preVerificationGas)),
        maxFeePerGas: String(BigInt(op.maxFeePerGas)),
        maxPriorityFeePerGas: String(BigInt(op.maxPriorityFeePerGas)),
        paymasterAndData: op.paymasterAndData ?? "0x",
        signature: op.signature,
      });
    }
  }

  process.stdout.write(JSON.stringify({ userOps: out }) + "\n");
}

// ---------------------------------------------------------------------------
// compose-and-submit — full end-to-end, returns tx hashes.
// ---------------------------------------------------------------------------
async function composeAndSubmit() {
  const { cfg } = getNetwork();
  const pk = getPrivateKey();
  const callsArg = getArg("--calls");
  if (!callsArg) fail("missing --calls");

  const raw = JSON.parse(callsArg!) as Array<{
    chainId: number;
    to: Hex;
    value: string;
    data: Hex;
  }>;

  const grouped: Map<number, UserOPCall[]> = new Map();
  for (const c of raw) {
    const arr = grouped.get(c.chainId) ?? [];
    arr.push({ to: c.to, value: BigInt(c.value), data: c.data });
    grouped.set(c.chainId, arr);
  }

  const { compose, rollupA, rollupB } = buildComposeConfig(cfg);
  const signer = privateKeyToAccount(pk);
  const multiChainIds = [rollupA.id, rollupB.id];
  const overrides = parseGasOverrides();

  const operations = [];
  for (const [chainId, calls] of grouped.entries()) {
    const sa = await createSmartAccount(
      { signer, chainId, multiChainIds },
      compose as any,
    );
    const origWarn = console.warn;
    console.warn = () => {};
    const userOp = await sa.account.createUserOp(calls);
    console.warn = origWarn;
    operations.push(userOp);
  }

  // Fee floor + per-chain gas overrides — same logic as create-userops.
  for (const op of operations) {
    if (typeof op?.userOp?.maxPriorityFeePerGas === "bigint" &&
        op.userOp.maxPriorityFeePerGas < MIN_PRIORITY_FEE) {
      op.userOp.maxPriorityFeePerGas = MIN_PRIORITY_FEE;
    }
    if (typeof op?.userOp?.maxFeePerGas === "bigint" &&
        op.userOp.maxFeePerGas < MIN_MAX_FEE) {
      op.userOp.maxFeePerGas = MIN_MAX_FEE;
    }
    const ov = overrides.get(op.chainId);
    if (ov) {
      if (ov.callGasLimit) op.userOp.callGasLimit = BigInt(ov.callGasLimit);
      if (ov.verificationGasLimit) op.userOp.verificationGasLimit = BigInt(ov.verificationGasLimit);
      if (ov.preVerificationGas) op.userOp.preVerificationGas = BigInt(ov.preVerificationGas);
    }
  }

  const composed = await composeUnpreparedUserOps(operations);
  const send = await composed.send();

  // Wait for receipts here so Go can immediately poll getTransactionReceipt
  // without racing the compose sequencer. We also pair each hash with the
  // chainId it belongs to (the order of `operations` matches the order of
  // `send.hashes`).
  try {
    await send.wait();
  } catch (e) {
    // Surface in stderr but still return hashes so Go can investigate.
    console.error(`[sa-helper] send.wait() error: ${(e as any)?.message || e}`);
  }

  const chainIds = operations.map((o) => o.chainId);
  process.stdout.write(JSON.stringify({
    hashes: send.hashes,
    chainIds,
  }) + "\n");
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------
async function main() {
  const sub = process.argv[2];
  switch (sub) {
    case "create-account":
      return createAccount();
    case "create-userops":
      return createUserOps();
    case "compose-and-submit":
      return composeAndSubmit();
    default:
      fail(`unknown subcommand ${sub} (expected create-account|create-userops|compose-and-submit)`);
  }
}

main().catch((err: any) => fail(err?.message || String(err), err));
