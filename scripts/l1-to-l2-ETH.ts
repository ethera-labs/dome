import { ethers } from "ethers";
import {
  L1_RPC,
  L1_Rollup_1_RPC,
  L1_Rollup_2_RPC,
  wallet_private_key,
  COMPOSE_L1_BRIDGE_ROLLUP_A,
  COMPOSE_L1_BRIDGE_ROLLUP_B,
} from "../config";
import ComposeL1BridgeABI from "../sepolia-prod/L1/abis/ComposeL1Bridge.json";

const MIN_GAS_LIMIT = 2_500_000;

// ---------------------------------------------------------------------------
// Rollup config lookup
// ---------------------------------------------------------------------------
interface RollupConfig {
  name: string;
  l2Rpc: string;
  l1Bridge: string;
}

const ROLLUP_CONFIGS: Record<string, RollupConfig> = {
  a: { name: "RollupA", l2Rpc: L1_Rollup_1_RPC, l1Bridge: COMPOSE_L1_BRIDGE_ROLLUP_A },
  b: { name: "RollupB", l2Rpc: L1_Rollup_2_RPC, l1Bridge: COMPOSE_L1_BRIDGE_ROLLUP_B },
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
Usage: npx ts-node scripts/l1-to-l2-ETH.ts --dest <a|b> --amount <ETH>

  --dest   Target rollup: a or b
  --amount   Amount of ETH to bridge (in ETH, not wei). e.g. 0.01

Example:
  npx ts-node scripts/l1-to-l2-ETH.ts --dest a --amount 0.01
  `);
  process.exit(1);
}

async function main() {
  // Parse args
  const destKey = getArg("--dest")?.toLowerCase();
  const amountStr = getArg("--amount");

  if (!destKey || !ROLLUP_CONFIGS[destKey]) {
    console.error("Error: --dest must be 'a' or 'b'");
    printUsage();
  }
  if (!amountStr || isNaN(parseFloat(amountStr)) || parseFloat(amountStr) <= 0) {
    console.error("Error: --amount must be a positive number (in ETH)");
    printUsage();
  }

  const rollup = ROLLUP_CONFIGS[destKey];
  const bridgeAmount = ethers.parseEther(amountStr);
  const tag = `[L1->${rollup.name}:ETH]`;

  // -- Setup --
  console.log(`${tag} Setting up providers...`);
  const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
  const l2Provider = new ethers.JsonRpcProvider(rollup.l2Rpc);
  const l1Wallet = new ethers.Wallet(wallet_private_key, l1Provider);
  const walletAddress = l1Wallet.address;

  console.log(`${tag} Wallet: ${walletAddress}`);
  console.log(`${tag} L1 Bridge: ${rollup.l1Bridge}`);
  console.log(`${tag} Amount: ${amountStr} ETH`);

  // Check L1 ETH balance
  const l1EthBalance = await l1Provider.getBalance(walletAddress);
  console.log(`${tag} L1 ETH balance: ${ethers.formatEther(l1EthBalance)} ETH`);
  if (l1EthBalance < bridgeAmount) {
    throw new Error(
      `Insufficient L1 ETH. Have ${ethers.formatEther(l1EthBalance)}, need ${amountStr}`
    );
  }

  // Snapshot L1 ETH balance before
  const l1BalanceBefore = await l1Provider.getBalance(walletAddress);

  // Snapshot L2 ETH balance before
  const l2BalanceBefore = await l2Provider.getBalance(walletAddress);
  console.log(`${tag} L2 ETH balance before: ${ethers.formatEther(l2BalanceBefore)} ETH`);

  // -- Bridge ETH --
  console.log(`\n${tag} Bridging ${amountStr} ETH to ${rollup.name}...`);
  const bridge = new ethers.Contract(rollup.l1Bridge, ComposeL1BridgeABI, l1Wallet);

  const bridgeTx = await bridge.bridgeETHTo(
    walletAddress,    // _to: receiver on L2 (ourselves)
    MIN_GAS_LIMIT,    // _minGasLimit
    "0x",             // _extraData
    { value: bridgeAmount }
  );
  const bridgeReceipt = await bridgeTx.wait();

  console.log(`${tag} Bridge tx confirmed!`);
  console.log(`  Tx hash:  ${bridgeReceipt!.hash}`);
  console.log(`  Block:    ${bridgeReceipt!.blockNumber}`);
  console.log(`  Gas used: ${bridgeReceipt!.gasUsed}`);

  // Assert: L1 ETH balance decreased (by at least bridgeAmount, plus gas)
  const l1BalanceAfter = await l1Provider.getBalance(walletAddress);
  const l1Decrease = BigInt(l1BalanceBefore) - BigInt(l1BalanceAfter);
  console.log(`${tag} L1 ETH decreased by: ${ethers.formatEther(l1Decrease)} ETH (includes gas)`);
  if (l1Decrease < bridgeAmount) {
    throw new Error(
      `ASSERT FAILED: L1 ETH should have decreased by at least ${amountStr}, ` +
      `but only decreased by ${ethers.formatEther(l1Decrease)}`
    );
  }
  console.log(`${tag} ASSERT OK: L1 ETH decreased by at least bridged amount`);

  // -- Poll for ETH arrival on L2 --
  console.log(`\n${tag} Polling for ETH arrival on ${rollup.name}...`);
  console.log("  (This may take several minutes for op-node to derive the deposit tx)\n");

  const MAX_POLLS = 60;
  const POLL_INTERVAL_MS = 10_000;

  for (let i = 1; i <= MAX_POLLS; i++) {
    const l2BalanceNow = await l2Provider.getBalance(walletAddress);
    const l2Increase = BigInt(l2BalanceNow) - BigInt(l2BalanceBefore);

    if (l2Increase > 0n) {
      console.log(`\n${tag} ETH arrived on ${rollup.name}!`);
      console.log(`${tag} L2 ETH balance: ${ethers.formatEther(l2BalanceNow)} ETH`);
      console.log(`${tag} L2 ETH increased by: ${ethers.formatEther(l2Increase)} ETH`);

      // Assert: L2 balance increased by bridged amount
      if (l2Increase.toString() !== bridgeAmount.toString()) {
        throw new Error(
          `ASSERT FAILED: Expected L2 ETH increase of ${amountStr}, ` +
          `got ${ethers.formatEther(l2Increase)}`
        );
      }
      console.log(`${tag} ASSERT OK: L2 ETH balance increased by expected amount`);

      console.log(`\n${tag} Bridge complete!`);
      console.log(`  Bridge tx: ${bridgeReceipt!.hash}`);
      return;
    }

    process.stdout.write(`  Poll ${i}/${MAX_POLLS} — waiting...\r`);
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  console.log(`\n${tag} Timed out waiting for ETH. The deposit may still be processing.`);
  console.log(`  Bridge tx: ${bridgeReceipt!.hash}`);
}

main().catch((err) => {
  console.error("\n[L1->L2:ETH] FATAL:", err.message || err);
  process.exit(1);
});
