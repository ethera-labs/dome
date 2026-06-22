import { ethers } from "ethers";
import {
  createPublicClient,
  createWalletClient,
  http,
  encodeFunctionData,
  type Hex,
} from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { createConfig } from "@wagmi/core";
import {


  createComposeConfig,
  createSmartAccount,

  type UserOPCall,
} from "@ssv-labs/ethera-sdk";
import { rollupA, rollupB, accountAbstractionContracts } from "./sa-chains";
import {
  wallet_private_key,
  COMPOSE_L2_TO_L2_BRIDGE,
} from "../config";
import { composeAndSubmit } from "./sa-compose-helper";
import ComposeL2ToL2BridgeABI from "../sepolia-prod/L2/abis/ComposeL2ToL2Bridge.json";

// ---------------------------------------------------------------------------
// Rollup config
// ---------------------------------------------------------------------------
const ROLLUP_CONFIGS: Record<string, typeof rollupA | typeof rollupB> = {
  a: rollupA,
  b: rollupB,
};

const ENTRYPOINT_ADDRESS = "0x0000000071727De22E5E9d8BAf0edAc6f37da032";
const ENTRYPOINT_ABI = [
  "function balanceOf(address) view returns (uint256)",
  "function depositTo(address) payable",
];

// Min deposit to cover gas for both chains
const MIN_ENTRYPOINT_DEPOSIT = ethers.parseEther("0.05");

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
Usage: npx ts-node scripts/l2-to-l2-SA-ETH.ts --source <a|b> --dest <a|b> --amount <ETH>

  --source   Source rollup: a or b
  --dest     Destination rollup: a or b
  --amount   Amount of ETH to bridge (in ETH, not wei)

Example:
  npx ts-node scripts/l2-to-l2-SA-ETH.ts --source a --dest b --amount 0.01
  `);
  process.exit(1);
}

async function main() {
  // Parse args
  const sourceKey = getArg("--source")?.toLowerCase();
  const destKey = getArg("--dest")?.toLowerCase();
  const amountStr = getArg("--amount");

  if (!sourceKey || !ROLLUP_CONFIGS[sourceKey]) { console.error("Error: --source must be 'a' or 'b'"); printUsage(); }
  if (!destKey || !ROLLUP_CONFIGS[destKey]) { console.error("Error: --dest must be 'a' or 'b'"); printUsage(); }
  if (sourceKey === destKey) { console.error("Error: --source and --dest must be different"); printUsage(); }
  if (!amountStr || isNaN(parseFloat(amountStr)) || parseFloat(amountStr) <= 0) { console.error("Error: --amount must be a positive number"); printUsage(); }

  const sourceChain = ROLLUP_CONFIGS[sourceKey];
  const destChain = ROLLUP_CONFIGS[destKey];
  const bridgeAmount = ethers.parseEther(amountStr);
  const tag = `[SA:${sourceChain.name}->${destChain.name}:ETH]`;

  // -- Step 1: Setup wagmi + compose config --
  console.log(`${tag} Setting up...`);

  const wagmiConfig = (createConfig as any)({
    chains: [rollupA, rollupB],
    transports: {
      [rollupA.id]: http(rollupA.rpcUrls.default.http[0]),
      [rollupB.id]: http(rollupB.rpcUrls.default.http[0]),
    },
  });

  const composeConfig = createComposeConfig({
    wagmi: wagmiConfig,
    accountAbstractionContracts: {
      [rollupA.id]: accountAbstractionContracts,
      [rollupB.id]: accountAbstractionContracts,
    },
  });

  const viemAccount = privateKeyToAccount(`0x${wallet_private_key}` as Hex);
  console.log(`${tag} EOA: ${viemAccount.address}`);

  // -- Step 2: Create smart accounts on both chains --
  console.log(`${tag} Creating smart accounts...`);

  const srcSA = await createSmartAccount(
    { signer: viemAccount, chainId: sourceChain.id, multiChainIds: [rollupA.id, rollupB.id] },
    composeConfig as any
  );
  const dstSA = await createSmartAccount(
    { signer: viemAccount, chainId: destChain.id, multiChainIds: [rollupA.id, rollupB.id] },
    composeConfig as any
  );

  const saAddress = srcSA.account.address;
  console.log(`${tag} Smart account: ${saAddress}`);

  // -- Step 3: Fund EntryPoint on both chains if needed --
  console.log(`\n${tag} Checking EntryPoint deposits...`);

  const srcRpc = sourceChain.rpcUrls.default.http[0];
  const dstRpc = destChain.rpcUrls.default.http[0];
  const srcEthersProvider = new ethers.JsonRpcProvider(srcRpc);
  const dstEthersProvider = new ethers.JsonRpcProvider(dstRpc);
  const srcEthersWallet = new ethers.Wallet(wallet_private_key, srcEthersProvider);
  const dstEthersWallet = new ethers.Wallet(wallet_private_key, dstEthersProvider);

  for (const [label, provider, wallet] of [
    [sourceChain.name, srcEthersProvider, srcEthersWallet],
    [destChain.name, dstEthersProvider, dstEthersWallet],
  ] as const) {
    const ep = new ethers.Contract(ENTRYPOINT_ADDRESS, ENTRYPOINT_ABI, wallet);
    const deposit: bigint = await ep.balanceOf(saAddress);
    console.log(`${tag} ${label} EntryPoint deposit: ${ethers.formatEther(deposit)} ETH`);

    if (deposit < MIN_ENTRYPOINT_DEPOSIT) {
      const needed = MIN_ENTRYPOINT_DEPOSIT - deposit;
      console.log(`${tag} Funding EntryPoint on ${label} with ${ethers.formatEther(needed)} ETH...`);
      const tx = await ep.depositTo(saAddress, { value: needed });
      await tx.wait();
      console.log(`${tag} Funded. Tx: ${tx.hash}`);
    }
  }

  // Also fund the smart account itself with ETH for the bridge amount (on source)
  const saBalance = await srcEthersProvider.getBalance(saAddress);
  console.log(`${tag} Smart account ETH on ${sourceChain.name}: ${ethers.formatEther(saBalance)} ETH`);
  if (saBalance < bridgeAmount) {
    const needed = bridgeAmount - saBalance;
    console.log(`${tag} Sending ${ethers.formatEther(needed)} ETH to smart account on ${sourceChain.name}...`);
    const tx = await srcEthersWallet.sendTransaction({ to: saAddress, value: needed });
    await tx.wait();
    console.log(`${tag} Funded. Tx: ${tx.hash}`);
  }

  // Snapshot balances before
  const srcBalanceBefore = await srcEthersProvider.getBalance(saAddress);
  const dstBalanceBefore = await dstEthersProvider.getBalance(saAddress);
  console.log(`\n${tag} SA balance before — source: ${ethers.formatEther(srcBalanceBefore)} ETH, dest: ${ethers.formatEther(dstBalanceBefore)} ETH`);

  // -- Step 4: Build UserOps --
  const sessionId = BigInt(Date.now());
  console.log(`${tag} SessionId: ${sessionId.toString()}`);

  // Source calls: bridgeEthTo
  const bridgeCalldata = encodeFunctionData({
    abi: ComposeL2ToL2BridgeABI,
    functionName: "bridgeEthTo",
    args: [sessionId, BigInt(destChain.id), saAddress as Hex],
  });

  const sourceCalls: UserOPCall[] = [
    {
      to: COMPOSE_L2_TO_L2_BRIDGE as Hex,
      value: BigInt(bridgeAmount.toString()),
      data: bridgeCalldata,
    },
  ];

  // Dest calls: receiveETH
  const msgHeader = {
    chainSrc: BigInt(sourceChain.id),
    chainDest: BigInt(destChain.id),
    sender: COMPOSE_L2_TO_L2_BRIDGE as Hex,
    receiver: saAddress as Hex,
    sessionId: sessionId,
    label: "SEND_ETH",
  };
  const receiveCalldata = encodeFunctionData({
    abi: ComposeL2ToL2BridgeABI,
    functionName: "receiveETH",
    args: [msgHeader],
  });

  const destCalls: UserOPCall[] = [
    {
      to: COMPOSE_L2_TO_L2_BRIDGE as Hex,
      value: 0n,
      data: receiveCalldata,
    },
  ];

  // -- Step 5: Create UserOps via smart accounts --
  // Suppress SDK gas estimation warnings — these calls always fail when simulated
  // individually because they require atomic cross-chain execution.
  console.log(`\n${tag} Creating UserOps...`);
  const originalWarn = console.warn;
  console.warn = () => {};
  const srcUserOp = await srcSA.account.createUserOp(sourceCalls);
  const dstUserOp = await dstSA.account.createUserOp(destCalls);
  console.warn = originalWarn;

  // -- Step 6: Compose, sign and submit --
  console.log(`${tag} Composing and submitting...`);

  const sendResult = await composeAndSubmit(
    [srcUserOp, dstUserOp],
    {
      onSigned: () => console.log(`${tag} UserOps signed`),
      onComposed: (_builds, urls) => {
        console.log(`${tag} Composed.`);
        if (urls.length > 0) console.log(`${tag} Explorer URLs:`, urls);
      },
      onPayloadEncoded: () => console.log(`${tag} Payload encoded`),
    }
  );
  console.log(`${tag} Submitted! Hashes:`, sendResult.hashes);

  // -- Step 7: Wait for receipts --
  console.log(`\n${tag} Waiting for receipts...`);
  const receipts = await sendResult.wait();

  const chainLabels = [sourceChain.name, destChain.name];
  for (let i = 0; i < receipts.length; i++) {
    const r = receipts[i];
    const label = chainLabels[i] || `Chain ${i}`;
    console.log(`${tag} ${label}: tx=${r.transactionHash || r.hash}, status=${r.status}, block=${r.blockNumber}, gasUsed=${r.gasUsed}`)
  }

  // -- Step 8: Assert balances --
  console.log(`\n${tag} Checking final balances...`);

  const srcBalanceAfter = await srcEthersProvider.getBalance(saAddress);
  const dstBalanceAfter = await dstEthersProvider.getBalance(saAddress);

  const srcDecrease = BigInt(srcBalanceBefore) - BigInt(srcBalanceAfter);
  const dstIncrease = BigInt(dstBalanceAfter) - BigInt(dstBalanceBefore);

  console.log(`${tag} Source decreased by: ${ethers.formatEther(srcDecrease)} ETH (includes gas)`);
  console.log(`${tag} Dest increased by: ${ethers.formatEther(dstIncrease)} ETH`);

  if (srcDecrease < bridgeAmount) {
    throw new Error(
      `ASSERT FAILED: Source should have decreased by at least ${amountStr} ETH, ` +
      `but only decreased by ${ethers.formatEther(srcDecrease)}`
    );
  }
  console.log(`${tag} ASSERT OK: Source ETH decreased by at least bridged amount`);

  if (dstIncrease <= 0n) {
    throw new Error("ASSERT FAILED: Dest ETH balance did not increase");
  }
  console.log(`${tag} ASSERT OK: Dest ETH balance increased`);

  console.log(`\n${tag} Bridge complete!`);
  console.log("  Hashes:", sendResult.hashes);
}

main().catch((err) => {
  console.error("\n[SA:L2->L2:ETH] FATAL:", err.message || err);
  process.exit(1);
});
