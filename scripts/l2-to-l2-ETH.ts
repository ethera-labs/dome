import { ethers } from "ethers";
import {
  createPublicClient,
  createWalletClient,
  http,
  encodeFunctionData,
  defineChain,
  type Hex,
} from "viem";
import { privateKeyToAccount } from "viem/accounts";
import {
  L1_Rollup_1_RPC,
  L1_Rollup_2_RPC,
  wallet_private_key,
  ROLLUP_A_CHAIN_ID,
  ROLLUP_B_CHAIN_ID,
  COMPOSE_L2_TO_L2_BRIDGE,
} from "../config";
import { submitXt } from "./xt-submit";
import ComposeL2ToL2BridgeABI from "../sepolia-prod/L2/abis/ComposeL2ToL2Bridge.json";

// ---------------------------------------------------------------------------
// Rollup config
// ---------------------------------------------------------------------------
interface RollupConfig {
  name: string;
  chainId: number;
  l2Rpc: string;
}

const ROLLUP_CONFIGS: Record<string, RollupConfig> = {
  a: { name: "RollupA", chainId: ROLLUP_A_CHAIN_ID, l2Rpc: L1_Rollup_1_RPC },
  b: { name: "RollupB", chainId: ROLLUP_B_CHAIN_ID, l2Rpc: L1_Rollup_2_RPC },
};

// ---------------------------------------------------------------------------
// CLI helpers
// ---------------------------------------------------------------------------
function getArg(flag: string): string | undefined {
  const idx = process.argv.indexOf(flag);
  if (idx !== -1 && idx + 1 < process.argv.length) {
    return process.argv[idx + 1];
  }
  return undefined;
}

function printUsage(): never {
  console.error(`
Usage: npx ts-node scripts/l2-to-l2-ETH.ts --source <a|b> --dest <a|b> --amount <ETH>

  --source   Source rollup: a or b
  --dest     Destination rollup: a or b
  --amount   Amount of ETH to bridge (in ETH, not wei)

Example:
  npx ts-node scripts/l2-to-l2-ETH.ts --source a --dest b --amount 0.01
  `);
  process.exit(1);
}

async function main() {
  // Parse args
  const sourceKey = getArg("--source")?.toLowerCase();
  const destKey = getArg("--dest")?.toLowerCase();
  const amountStr = getArg("--amount");

  if (!sourceKey || !ROLLUP_CONFIGS[sourceKey]) {
    console.error("Error: --source must be 'a' or 'b'");
    printUsage();
  }
  if (!destKey || !ROLLUP_CONFIGS[destKey]) {
    console.error("Error: --dest must be 'a' or 'b'");
    printUsage();
  }
  if (sourceKey === destKey) {
    console.error("Error: --source and --dest must be different");
    printUsage();
  }
  if (!amountStr || isNaN(parseFloat(amountStr)) || parseFloat(amountStr) <= 0) {
    console.error("Error: --amount must be a positive number (in ETH)");
    printUsage();
  }

  const source = ROLLUP_CONFIGS[sourceKey];
  const dest = ROLLUP_CONFIGS[destKey];
  const bridgeAmount = ethers.parseEther(amountStr);
  const tag = `[${source.name}->${dest.name}:ETH]`;

  console.log(`${tag} Setting up...`);

  // ethers providers for balance checks
  const srcEthersProvider = new ethers.JsonRpcProvider(source.l2Rpc);
  const dstEthersProvider = new ethers.JsonRpcProvider(dest.l2Rpc);
  const viemAccount = privateKeyToAccount(`0x${wallet_private_key}` as Hex);
  const walletAddress = viemAccount.address;

  console.log(`${tag} Wallet: ${walletAddress}`);
  console.log(`${tag} Amount: ${amountStr} ETH`);

  // Check source ETH balance
  const srcBalance = await srcEthersProvider.getBalance(walletAddress);
  console.log(`${tag} Source ETH balance: ${ethers.formatEther(srcBalance)} ETH`);
  if (srcBalance < bridgeAmount) {
    throw new Error(`Insufficient ETH on ${source.name}. Have ${ethers.formatEther(srcBalance)}, need ${amountStr}`);
  }

  // Snapshot balances before
  const srcBalanceBefore = await srcEthersProvider.getBalance(walletAddress);
  const dstBalanceBefore = await dstEthersProvider.getBalance(walletAddress);
  console.log(`${tag} Dest ETH balance before: ${ethers.formatEther(dstBalanceBefore)} ETH`);

  // Generate sessionId
  const sessionId = BigInt(Date.now());
  console.log(`${tag} SessionId: ${sessionId.toString()}`);

  // -- Build source tx: bridgeEthTo --
  const bridgeCalldata = encodeFunctionData({
    abi: ComposeL2ToL2BridgeABI,
    functionName: "bridgeEthTo",
    args: [sessionId, BigInt(dest.chainId), walletAddress as Hex],
  });

  // -- Build dest tx: receiveETH --
  const msgHeader = {
    chainSrc: BigInt(source.chainId),
    chainDest: BigInt(dest.chainId),
    sender: COMPOSE_L2_TO_L2_BRIDGE as Hex,
    receiver: walletAddress as Hex,
    sessionId: sessionId,
    label: "SEND_ETH",
  };
  const receiveCalldata = encodeFunctionData({
    abi: ComposeL2ToL2BridgeABI,
    functionName: "receiveETH",
    args: [msgHeader],
  });

  // viem clients for signing
  const srcChain = defineChain({
    id: source.chainId,
    name: source.name,
    nativeCurrency: { name: "ETH", symbol: "ETH", decimals: 18 },
    rpcUrls: { default: { http: [source.l2Rpc] } },
  });
  const dstChain = defineChain({
    id: dest.chainId,
    name: dest.name,
    nativeCurrency: { name: "ETH", symbol: "ETH", decimals: 18 },
    rpcUrls: { default: { http: [dest.l2Rpc] } },
  });

  const srcWalletClient = createWalletClient({ account: viemAccount, chain: srcChain, transport: http(source.l2Rpc) });
  const dstWalletClient = createWalletClient({ account: viemAccount, chain: dstChain, transport: http(dest.l2Rpc) });
  const srcPublicClient = createPublicClient({ chain: srcChain, transport: http(source.l2Rpc) });
  const dstPublicClient = createPublicClient({ chain: dstChain, transport: http(dest.l2Rpc) });

  // Sign source tx
  console.log(`\n${tag} Signing source tx (bridgeEthTo)...`);
  const srcNonce = await srcPublicClient.getTransactionCount({ address: viemAccount.address });
  const srcFeeData = await srcPublicClient.estimateFeesPerGas();
  const srcSignedTx = await srcWalletClient.signTransaction({
    to: COMPOSE_L2_TO_L2_BRIDGE as Hex,
    data: bridgeCalldata,
    value: BigInt(bridgeAmount.toString()),
    gas: 3_000_000n,
    maxFeePerGas: srcFeeData.maxFeePerGas!,
    maxPriorityFeePerGas: srcFeeData.maxPriorityFeePerGas!,
    nonce: srcNonce,
    chainId: source.chainId,
  });

  // Sign dest tx
  console.log(`${tag} Signing dest tx (receiveETH)...`);
  const dstNonce = await dstPublicClient.getTransactionCount({ address: viemAccount.address });
  const dstFeeData = await dstPublicClient.estimateFeesPerGas();
  const dstSignedTx = await dstWalletClient.signTransaction({
    to: COMPOSE_L2_TO_L2_BRIDGE as Hex,
    data: receiveCalldata,
    gas: 3_000_000n,
    maxFeePerGas: dstFeeData.maxFeePerGas!,
    maxPriorityFeePerGas: dstFeeData.maxPriorityFeePerGas!,
    nonce: dstNonce,
    chainId: dest.chainId,
  });

  // Submit cross-chain transaction
  console.log(`\n${tag} Submitting cross-chain transaction...`);
  const xtResult = await submitXt(
    [
      { chainId: source.chainId, rawTx: srcSignedTx },
      { chainId: dest.chainId, rawTx: dstSignedTx },
    ],
    source.l2Rpc
  );
  console.log(`${tag} Submitted via ${xtResult.method}:`, JSON.stringify(xtResult.result));

  // Poll for results
  console.log(`\n${tag} Polling for transaction results...`);

  const MAX_POLLS = 60;
  const POLL_INTERVAL_MS = 10_000;

  let srcConfirmed = false;
  let dstConfirmed = false;

  for (let i = 1; i <= MAX_POLLS; i++) {
    if (!srcConfirmed) {
      const srcBalanceAfter = await srcEthersProvider.getBalance(walletAddress);
      const srcDecrease = BigInt(srcBalanceBefore) - BigInt(srcBalanceAfter);
      if (srcDecrease >= bridgeAmount) {
        console.log(`\n${tag} Source ETH decreased by: ${ethers.formatEther(srcDecrease)} ETH (includes gas)`);
        console.log(`${tag} ASSERT OK: Source ETH decreased by at least bridged amount`);
        srcConfirmed = true;
      }
    }

    if (!dstConfirmed) {
      const dstBalanceAfter = await dstEthersProvider.getBalance(walletAddress);
      const dstIncrease = BigInt(dstBalanceAfter) - BigInt(dstBalanceBefore);
      if (dstIncrease > 0n) {
        console.log(`${tag} Dest ETH balance: ${ethers.formatEther(dstBalanceAfter)} ETH`);
        console.log(`${tag} Dest ETH increased by: ${ethers.formatEther(dstIncrease)} ETH`);
        console.log(`${tag} ASSERT OK: Dest ETH balance increased`);
        dstConfirmed = true;
      }
    }

    if (srcConfirmed && dstConfirmed) {
      console.log(`\n${tag} Bridge complete!`);
      return;
    }

    process.stdout.write(`  Poll ${i}/${MAX_POLLS} — src:${srcConfirmed ? "OK" : "..."} dst:${dstConfirmed ? "OK" : "..."}...\r`);
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  console.log(`\n${tag} Timed out. src:${srcConfirmed ? "OK" : "PENDING"} dst:${dstConfirmed ? "OK" : "PENDING"}`);
  console.log(`  SessionId: ${sessionId.toString()}`);
}

main().catch((err) => {
  console.error("\n[L2->L2:ETH] FATAL:", err.message || err);
  process.exit(1);
});
