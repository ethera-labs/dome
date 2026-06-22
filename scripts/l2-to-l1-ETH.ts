import { ethers } from "ethers";
import * as fs from "fs";
import * as path from "path";
import {
  L1_RPC,
  L1_Rollup_1_RPC,
  L1_Rollup_2_RPC,
  wallet_private_key,
  ROLLUP_A_CHAIN_ID,
  ROLLUP_B_CHAIN_ID,
  COMPOSE_L2_BRIDGE_ROLLUP_A,
  COMPOSE_L2_BRIDGE_ROLLUP_B,
  COMPOSE_PORTAL_ROLLUP_A,
  COMPOSE_PORTAL_ROLLUP_B,
} from "../config";
import ComposeL2BridgeABI from "../sepolia-prod/L2/abis/ComposeL2Bridge.json";
import ComposePortalABI from "../sepolia-prod/L1/abis/ComposePortal.json";

const DGF_ABI = [
  "function gameCount() view returns (uint256)",
  "function gameAtIndex(uint256) view returns (uint32 gameType, uint64 timestamp, address proxy)",
];
const GAME_ABI = [
  "function rootClaim() view returns (bytes32)",
  "function extraData() view returns (bytes)",
];

const L2_COMPOSE_BRIDGE: Record<string, string> = {
  a: COMPOSE_L2_BRIDGE_ROLLUP_A,
  b: COMPOSE_L2_BRIDGE_ROLLUP_B,
};

const L1_COMPOSE_PORTAL: Record<string, string> = {
  a: COMPOSE_PORTAL_ROLLUP_A,
  b: COMPOSE_PORTAL_ROLLUP_B,
};

const L2_RPC: Record<string, string> = {
  a: L1_Rollup_1_RPC,
  b: L1_Rollup_2_RPC,
};

const CHAIN_NAMES: Record<string, string> = {
  a: "RollupA",
  b: "RollupB",
};

const L2_TO_L1_MESSAGE_PASSER = "0x4200000000000000000000000000000000000016";
const MESSAGE_PASSER_ABI = [
  "event MessagePassed(uint256 indexed nonce, address indexed sender, address indexed target, uint256 value, uint256 gasLimit, bytes data, bytes32 withdrawalHash)",
];

const MIN_GAS_LIMIT = 2_500_000;
const POLL_INTERVAL_MS = 10_000;

// ---------------------------------------------------------------------------
// State file
// ---------------------------------------------------------------------------
interface WithdrawalState {
  sourceKey: string;
  withdrawalHash: string;
  withdrawalNonce: string;
  withdrawalSender: string;
  withdrawalTarget: string;
  withdrawalValue: string;
  withdrawalGasLimit: string;
  withdrawalData: string;
  l2TxHash: string;
  l2Block: number;
  l1BalanceBefore: string;
  amount: string;
}

function stateFilePath(sourceKey: string): string {
  return path.join(__dirname, `.l2-to-l1-ETH-state-${sourceKey}`);
}

function loadState(sourceKey: string): WithdrawalState | null {
  try { return JSON.parse(fs.readFileSync(stateFilePath(sourceKey), "utf-8")); } catch { return null; }
}

function saveState(state: WithdrawalState) {
  fs.writeFileSync(stateFilePath(state.sourceKey), JSON.stringify(state, null, 2) + "\n");
}

function deleteState(sourceKey: string) {
  try { fs.unlinkSync(stateFilePath(sourceKey)); } catch {}
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------
function getArg(flag: string): string | undefined {
  const idx = process.argv.indexOf(flag);
  if (idx !== -1 && idx + 1 < process.argv.length) return process.argv[idx + 1];
  return undefined;
}

function hasFlag(flag: string): boolean {
  return process.argv.includes(flag);
}

function printUsage(): never {
  console.error(`
Usage:
  Part 1 — Initiate withdrawal (saves state):
    npx ts-node scripts/l2-to-l1-ETH.ts withdraw --source <a|b> --amount <ETH>

  Part 2 — Prove + finalize (loads state):
    npx ts-node scripts/l2-to-l1-ETH.ts finalize --source <a|b>

  Both parts in one run:
    npx ts-node scripts/l2-to-l1-ETH.ts --source <a|b> --amount <ETH>

Example:
  npx ts-node scripts/l2-to-l1-ETH.ts withdraw --source a --amount 0.1
  # ... wait hours/days for proof maturity ...
  npx ts-node scripts/l2-to-l1-ETH.ts finalize --source a
  `);
  process.exit(1);
}

// ---------------------------------------------------------------------------
// Part 1: Initiate withdrawal on L2
// ---------------------------------------------------------------------------
async function initiateWithdrawal(sourceKey: string, amountStr: string): Promise<WithdrawalState> {
  const chainName = CHAIN_NAMES[sourceKey];
  const bridgeAmount = ethers.parseEther(amountStr);
  const tag = `[${chainName}->L1:ETH]`;

  const l2Provider = new ethers.JsonRpcProvider(L2_RPC[sourceKey]);
  const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
  const l2Wallet = new ethers.Wallet(wallet_private_key, l2Provider);
  const walletAddress = l2Wallet.address;

  console.log(`${tag} Wallet: ${walletAddress}`);
  console.log(`${tag} Amount: ${amountStr} ETH`);

  const l2Balance = await l2Provider.getBalance(walletAddress);
  console.log(`${tag} ${chainName} ETH balance: ${ethers.formatEther(l2Balance)} ETH`);
  if (l2Balance < bridgeAmount) {
    throw new Error(`Insufficient ETH on ${chainName}. Have ${ethers.formatEther(l2Balance)}, need ${amountStr}`);
  }

  const l1BalanceBefore = await l1Provider.getBalance(walletAddress);
  console.log(`${tag} L1 ETH balance before: ${ethers.formatEther(l1BalanceBefore)} ETH`);

  console.log(`\n${tag} Initiating ETH withdrawal on ${chainName}...`);
  const l2Bridge = new ethers.Contract(L2_COMPOSE_BRIDGE[sourceKey], ComposeL2BridgeABI, l2Wallet);

  const withdrawTx = await l2Bridge.bridgeETHTo(
    walletAddress, MIN_GAS_LIMIT, "0x",
    { value: bridgeAmount }
  );
  const withdrawReceipt = await withdrawTx.wait();

  console.log(`${tag} Withdrawal initiated!`);
  console.log(`  Tx hash: ${withdrawReceipt!.hash}`);
  console.log(`  Block:   ${withdrawReceipt!.blockNumber}`);

  // Extract MessagePassed event
  const msgPasserIface = new ethers.Interface(MESSAGE_PASSER_ABI);
  for (const log of withdrawReceipt!.logs) {
    if (log.address.toLowerCase() === L2_TO_L1_MESSAGE_PASSER.toLowerCase()) {
      try {
        const parsed = msgPasserIface.parseLog({ topics: log.topics as string[], data: log.data });
        if (parsed && parsed.name === "MessagePassed") {
          const state: WithdrawalState = {
            sourceKey,
            withdrawalHash: parsed.args[6],
            withdrawalNonce: parsed.args[0].toString(),
            withdrawalSender: parsed.args[1],
            withdrawalTarget: parsed.args[2],
            withdrawalValue: parsed.args[3].toString(),
            withdrawalGasLimit: parsed.args[4].toString(),
            withdrawalData: parsed.args[5],
            l2TxHash: withdrawReceipt!.hash,
            l2Block: withdrawReceipt!.blockNumber,
            l1BalanceBefore: l1BalanceBefore.toString(),
            amount: amountStr,
          };

          saveState(state);
          console.log(`\n${tag} State saved to ${stateFilePath(sourceKey)}`);
          console.log(`${tag} Withdrawal hash: ${state.withdrawalHash}`);
          console.log(`${tag} Run 'finalize --source ${sourceKey}' later to prove + finalize.`);
          return state;
        }
      } catch {}
    }
  }

  throw new Error("Failed to find MessagePassed event in withdrawal receipt");
}

// ---------------------------------------------------------------------------
// Part 2: Prove + finalize on L1
// ---------------------------------------------------------------------------
async function proveAndFinalize(sourceKey: string) {
  const state = loadState(sourceKey);
  if (!state) {
    throw new Error(`No withdrawal state found for source ${sourceKey}. Run 'withdraw' first.`);
  }

  const chainName = CHAIN_NAMES[sourceKey];
  const tag = `[${chainName}->L1:ETH]`;

  const l2Provider = new ethers.JsonRpcProvider(L2_RPC[sourceKey]);
  const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
  const l1Wallet = new ethers.Wallet(wallet_private_key, l1Provider);
  const walletAddress = l1Wallet.address;

  console.log(`${tag} Loaded withdrawal state:`);
  console.log(`  Withdrawal hash: ${state.withdrawalHash}`);
  console.log(`  L2 tx: ${state.l2TxHash}`);
  console.log(`  L2 block: ${state.l2Block}`);
  console.log(`  Amount: ${state.amount} ETH`);

  const portal = new ethers.Contract(L1_COMPOSE_PORTAL[sourceKey], ComposePortalABI, l1Wallet);
  const withdrawalTx = {
    nonce: BigInt(state.withdrawalNonce),
    sender: state.withdrawalSender,
    target: state.withdrawalTarget,
    value: BigInt(state.withdrawalValue),
    gasLimit: BigInt(state.withdrawalGasLimit),
    data: state.withdrawalData,
  };

  // Check if already finalized
  const alreadyFinalized = await portal.finalizedWithdrawals(state.withdrawalHash);
  if (alreadyFinalized) {
    console.log(`${tag} Withdrawal already finalized!`);
    deleteState(sourceKey);
    return;
  }

  // Check if already proven
  const numSubmitters = await portal.numProofSubmitters(state.withdrawalHash);
  if (Number(numSubmitters) > 0) {
    console.log(`${tag} Withdrawal already proven. Skipping to finalize.`);
  } else {
    // Find dispute game + prove
    console.log(`\n${tag} Waiting for dispute game covering L2 block ${state.l2Block}...`);
    const dgf = new ethers.Contract(await portal.disputeGameFactory(), DGF_ABI, l1Provider);
    const respectedGameType = await portal.respectedGameType();

    let foundGameIndex = -1;
    let foundGameL2Block = 0;

    for (let poll = 1; poll <= 360; poll++) {
      const count = await dgf.gameCount();
      for (let i = Number(count) - 1; i >= Math.max(0, Number(count) - 10); i--) {
        const gameInfo = await dgf.gameAtIndex(i);
        if (Number(gameInfo.gameType) !== Number(respectedGameType)) continue;
        const game = new ethers.Contract(gameInfo.proxy, GAME_ABI, l1Provider);
        const extraData = await game.extraData();
        const hexStr = extraData.slice(2);
        for (let pos = 0; pos < hexStr.length - 64; pos += 64) {
          const val = BigInt("0x" + hexStr.slice(pos, pos + 64));
          if (val > 1_000_000n && val < 100_000_000n && Number(val) >= state.l2Block) {
            foundGameIndex = i;
            foundGameL2Block = Number(val);
            break;
          }
        }
        if (foundGameIndex >= 0) break;
      }
      if (foundGameIndex >= 0) break;
      process.stdout.write(`  Poll ${poll}/360 — waiting for dispute game...\r`);
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
    }

    if (foundGameIndex < 0) throw new Error("Timed out waiting for dispute game");
    console.log(`\n${tag} Found dispute game ${foundGameIndex} covering L2 block ${foundGameL2Block}`);

    // Build proof
    console.log(`${tag} Building withdrawal proof...`);
    const l2BlockData = await l2Provider.send("eth_getBlockByNumber", [ethers.toQuantity(foundGameL2Block), false]);
    const storageSlot = ethers.keccak256(
      ethers.AbiCoder.defaultAbiCoder().encode(["bytes32", "uint256"], [state.withdrawalHash, 0])
    );
    const storageProof = await l2Provider.send("eth_getProof", [
      L2_TO_L1_MESSAGE_PASSER, [storageSlot], ethers.toQuantity(foundGameL2Block),
    ]);

    if (storageProof.storageProof[0]?.value !== "0x1") {
      throw new Error("Withdrawal not found in L2ToL1MessagePasser storage");
    }

    const outputRootProof = {
      version: ethers.ZeroHash,
      stateRoot: l2BlockData.stateRoot,
      messagePasserStorageRoot: storageProof.storageHash,
      latestBlockhash: l2BlockData.hash,
    };

    console.log(`${tag} Submitting proveWithdrawalTransaction on L1...`);
    const proveTx = await portal.proveWithdrawalTransaction(
      withdrawalTx, foundGameIndex, outputRootProof,
      storageProof.storageProof[0].proof, { gasLimit: 500_000 }
    );
    const proveReceipt = await proveTx.wait();
    console.log(`${tag} Prove tx: ${proveReceipt!.hash}`);
  }

  // Wait for maturity + finalize
  const maturityDelay = await portal.proofMaturityDelaySeconds();
  console.log(`\n${tag} Proof maturity delay: ${maturityDelay}s (${(Number(maturityDelay) / 3600).toFixed(1)}h)`);

  if (maturityDelay > 0n) {
    console.log(`${tag} Polling for finalization window...\n`);
  }

  for (let poll = 1; poll <= 2160; poll++) {
    try {
      await portal.checkWithdrawal(state.withdrawalHash, walletAddress);

      console.log(`\n${tag} Finalizing on L1...`);
      const finalizeTx = await portal.finalizeWithdrawalTransaction(withdrawalTx);
      const finalizeReceipt = await finalizeTx.wait();

      console.log(`${tag} Finalization confirmed!`);
      console.log(`  Tx hash: ${finalizeReceipt!.hash}`);

      // Assert
      const l1BalanceAfter = await l1Provider.getBalance(walletAddress);
      const l1Increase = BigInt(l1BalanceAfter) - BigInt(state.l1BalanceBefore);
      console.log(`\n${tag} L1 ETH balance after: ${ethers.formatEther(l1BalanceAfter)} ETH`);
      console.log(`${tag} L1 ETH increased by: ${ethers.formatEther(l1Increase)} ETH`);

      if (l1Increase <= 0n) {
        const gasSpent = BigInt(finalizeReceipt!.gasUsed) * BigInt(finalizeReceipt!.gasPrice || 0n);
        const netReceived = l1Increase + gasSpent;
        if (netReceived <= 0n) {
          throw new Error("ASSERT FAILED: L1 ETH balance did not increase after accounting for gas");
        }
      } else {
        console.log(`${tag} ASSERT OK: L1 ETH balance increased`);
      }

      console.log(`\n${tag} Withdrawal complete!`);
      deleteState(sourceKey);
      return;
    } catch (err: any) {
      const msg = err.message || String(err);
      if (msg.includes("Unproven") || msg.includes("ProofNotOldEnough") || msg.includes("revert") || msg.includes("CALL_EXCEPTION")) {
        // Not ready yet
      } else if (msg.includes("AlreadyFinalized")) {
        console.log(`\n${tag} Withdrawal already finalized!`);
        deleteState(sourceKey);
        return;
      } else {
        throw err;
      }
    }
    process.stdout.write(`  Poll ${poll}/2160 — waiting for finalization window...\r`);
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  console.log(`\n${tag} Timed out waiting for finalization. Run 'finalize' again later.`);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
async function main() {
  const mode = process.argv[2];
  const sourceKey = getArg("--source")?.toLowerCase();

  if (!sourceKey || !L2_COMPOSE_BRIDGE[sourceKey]) {
    console.error("Error: --source must be 'a' or 'b'");
    printUsage();
  }

  if (mode === "withdraw") {
    const amountStr = getArg("--amount");
    if (!amountStr || isNaN(parseFloat(amountStr)) || parseFloat(amountStr) <= 0) {
      console.error("Error: --amount required for withdraw");
      printUsage();
    }
    await initiateWithdrawal(sourceKey, amountStr);

  } else if (mode === "finalize") {
    await proveAndFinalize(sourceKey);

  } else {
    // Legacy: both parts in one run
    const amountStr = getArg("--amount");
    if (!amountStr || isNaN(parseFloat(amountStr)) || parseFloat(amountStr) <= 0) {
      console.error("Error: --amount required");
      printUsage();
    }
    const state = await initiateWithdrawal(sourceKey, amountStr);
    console.log("\n" + "=".repeat(60));
    console.log("Proceeding to prove + finalize (this may take a long time)...");
    console.log("=".repeat(60) + "\n");
    await proveAndFinalize(sourceKey);
  }
}

main().catch((err) => {
  console.error("\n[L2->L1:ETH] FATAL:", err.message || err);
  process.exit(1);
});
