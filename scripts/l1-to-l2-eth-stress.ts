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

const FUND_AMOUNT = ethers.parseEther("0.1");
const BRIDGE_AMOUNT = ethers.parseEther("0.01");
// _minGasLimit for the L2 deposit tx — portal minimum for 0 bytes calldata is 21,000.
// Keep as low as possible: the portal's resource metering charges proportional to this,
// limiting how many deposits can fit per block.
const MIN_GAS_LIMIT = 21_000;
// Set high enough to cover portal gas metering escalation when many deposits
// land in the same block. Unused gas is refunded — only actual consumption is charged.
const BRIDGE_GAS_LIMIT = 5_000_000n;

// ---------------------------------------------------------------------------
// Rollup config
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
// CLI
// ---------------------------------------------------------------------------
function getArg(flag: string): string | undefined {
  const idx = process.argv.indexOf(flag);
  if (idx !== -1 && idx + 1 < process.argv.length) {
    return process.argv[idx + 1];
  }
  return undefined;
}

function hasFlag(flag: string): boolean {
  return process.argv.includes(flag);
}

function printUsage(): never {
  console.error(`
Usage: npx ts-node scripts/l1-to-l2-stress.ts --dest <a|b> [--num-acc <N>] [--new-wallets]

  --dest         Target rollup: a or b
  --num-acc      Number of accounts (default: 100)
  --new-wallets  Generate fresh random wallets instead of deterministic ones

Example:
  npx ts-node scripts/l1-to-l2-stress.ts --dest a --num-acc 5
  npx ts-node scripts/l1-to-l2-stress.ts --dest a --num-acc 100 --new-wallets
  `);
  process.exit(1);
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function deriveAccounts(masterKey: string, count: number): ethers.Wallet[] {
  const wallets: ethers.Wallet[] = [];
  for (let i = 0; i < count; i++) {
    const derived = ethers.keccak256(
      ethers.solidityPacked(["bytes32", "uint256"], [`0x${masterKey}`, i])
    );
    wallets.push(new ethers.Wallet(derived));
  }
  return wallets;
}

function generateRandomAccounts(count: number): ethers.Wallet[] {
  const wallets: ethers.Wallet[] = [];
  for (let i = 0; i < count; i++) {
    const random = ethers.Wallet.createRandom();
    wallets.push(new ethers.Wallet(random.privateKey));
  }
  return wallets;
}

function elapsed(start: number): string {
  return ((Date.now() - start) / 1000).toFixed(1) + "s";
}

async function main() {
  const destKey = getArg("--dest")?.toLowerCase();
  const numAcc = parseInt(getArg("--num-acc") || "100", 10);

  if (!destKey || !ROLLUP_CONFIGS[destKey]) { console.error("Error: --dest must be 'a' or 'b'"); printUsage(); }
  if (isNaN(numAcc) || numAcc < 1) { console.error("Error: --num-acc must be a positive integer"); printUsage(); }

  const rollup = ROLLUP_CONFIGS[destKey];
  const tag = `[STRESS:L1->${rollup.name}]`;
  const totalStart = Date.now();

  console.log(`${tag} Stress test: ${numAcc} accounts, bridge 0.01 ETH each to ${rollup.name}`);

  // Setup providers
  const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
  const l2Provider = new ethers.JsonRpcProvider(rollup.l2Rpc);
  const funderWallet = new ethers.Wallet(wallet_private_key, l1Provider);

  console.log(`${tag} Funder: ${funderWallet.address}`);
  const funderBalance = await l1Provider.getBalance(funderWallet.address);
  const requiredBalance = FUND_AMOUNT * BigInt(numAcc);
  console.log(`${tag} Funder balance: ${ethers.formatEther(funderBalance)} ETH`);
  console.log(`${tag} Required: ~${ethers.formatEther(requiredBalance)} ETH (+ gas)`);

  if (funderBalance < requiredBalance) {
    throw new Error(`Funder needs at least ${ethers.formatEther(requiredBalance)} ETH`);
  }

  // =========================================================================
  // Step 1: Generate accounts
  // =========================================================================
  const useNewWallets = hasFlag("--new-wallets");
  console.log(`\n${tag} Step 1: Generating ${numAcc} ${useNewWallets ? "random" : "deterministic"} accounts...`);
  const accounts = useNewWallets
    ? generateRandomAccounts(numAcc)
    : deriveAccounts(wallet_private_key, numAcc);
  console.log(`${tag} First: ${accounts[0].address}`);
  console.log(`${tag} Last:  ${accounts[numAcc - 1].address}`);

  // =========================================================================
  // Step 2: Fund accounts on L1 (skip already-funded accounts)
  // =========================================================================
  console.log(`\n${tag} Step 2: Funding accounts with ${ethers.formatEther(FUND_AMOUNT)} ETH each...`);
  const fundStart = Date.now();

  // Check existing balances to skip already-funded accounts
  const existingBalances = await Promise.all(
    accounts.map((acc) => l1Provider.getBalance(acc.address))
  );
  // Account needs enough for bridge value + gas (~0.05 ETH buffer)
  const MIN_REQUIRED = BRIDGE_AMOUNT + ethers.parseEther("0.05");
  const needsFunding = [];
  for (let i = 0; i < numAcc; i++) {
    if (existingBalances[i] < MIN_REQUIRED) needsFunding.push(i);
  }

  if (needsFunding.length === 0) {
    console.log(`${tag} All ${numAcc} accounts already funded, skipping`);
  } else {
    console.log(`${tag} ${needsFunding.length}/${numAcc} accounts need funding`);

    const feeData = await l1Provider.getFeeData();
    const chainId = (await l1Provider.getNetwork()).chainId;
    const baseNonce = await l1Provider.getTransactionCount(funderWallet.address, "pending");
    console.log(`${tag} Funder nonce: ${baseNonce}`);

    // Build and sign funding txs only for accounts that need it
    const signedFundTxs: string[] = [];
    for (let j = 0; j < needsFunding.length; j++) {
      const i = needsFunding[j];
      const tx = {
        to: accounts[i].address,
        value: FUND_AMOUNT,
        nonce: baseNonce + j,
        gasLimit: 21_000n,
        maxFeePerGas: feeData.maxFeePerGas!,
        maxPriorityFeePerGas: feeData.maxPriorityFeePerGas!,
        chainId,
        type: 2,
      };
      const signed = await funderWallet.signTransaction(tx);
      signedFundTxs.push(signed);
    }
    console.log(`${tag} Signed ${needsFunding.length} funding txs in ${elapsed(fundStart)}`);

    // Broadcast all at once
    const broadcastStart = Date.now();
    const fundResponses = await Promise.all(
      signedFundTxs.map((signed) => l1Provider.broadcastTransaction(signed))
    );
    console.log(`${tag} Broadcast ${needsFunding.length} funding txs in ${elapsed(broadcastStart)}`);

    // Wait for all receipts
    const receiptStart = Date.now();
    const fundReceipts = await Promise.all(
      fundResponses.map((resp) => resp.wait())
    );
    const fundFailed = fundReceipts.filter((r) => r!.status !== 1);
    console.log(`${tag} All ${needsFunding.length} funding txs confirmed in ${elapsed(receiptStart)}`);
    if (fundFailed.length > 0) {
      throw new Error(`ASSERT FAILED: ${fundFailed.length} funding txs failed`);
    }
  }

  // Assert all balances
  const balanceChecks = await Promise.all(
    accounts.map((acc) => l1Provider.getBalance(acc.address))
  );
  const underfunded = balanceChecks.filter((b) => b < BRIDGE_AMOUNT);
  if (underfunded.length > 0) {
    throw new Error(`ASSERT FAILED: ${underfunded.length} accounts have insufficient balance for bridging`);
  }
  console.log(`${tag} ASSERT OK: All ${numAcc} accounts have sufficient balance`);
  console.log(`${tag} Funding phase took ${elapsed(fundStart)}`);

  // =========================================================================
  // Step 3: Bridge ETH from all accounts to L2
  // =========================================================================
  console.log(`\n${tag} Step 3: Bridging ${ethers.formatEther(BRIDGE_AMOUNT)} ETH from each account to ${rollup.name}...`);
  const bridgeStart = Date.now();

  const bridgeIface = new ethers.Interface(ComposeL1BridgeABI);
  const bridgeCalldata = bridgeIface.encodeFunctionData("bridgeETHTo", [
    ethers.ZeroAddress, // placeholder — will be replaced per account
    MIN_GAS_LIMIT,
    "0x",
  ]);

  // Get chain ID once
  const chainId = (await l1Provider.getNetwork()).chainId;
  const bridgeFeeData = await l1Provider.getFeeData();

  // Get nonces for all accounts (may be > 0 if script was run before)
  const accNonces = await Promise.all(
    accounts.map((acc) => l1Provider.getTransactionCount(acc.address, "pending"))
  );

  // Sign all bridge txs
  const signedBridgeTxs: string[] = [];
  for (let i = 0; i < numAcc; i++) {
    const accWallet = accounts[i].connect(l1Provider);
    const calldata = bridgeIface.encodeFunctionData("bridgeETHTo", [
      accounts[i].address, // _to: bridge to self
      MIN_GAS_LIMIT,
      "0x",
    ]);
    const tx = {
      to: rollup.l1Bridge,
      data: calldata,
      value: BRIDGE_AMOUNT,
      nonce: accNonces[i],
      gasLimit: BRIDGE_GAS_LIMIT,
      maxFeePerGas: bridgeFeeData.maxFeePerGas!,
      maxPriorityFeePerGas: bridgeFeeData.maxPriorityFeePerGas!,
      chainId,
      type: 2,
    };
    const signed = await accWallet.signTransaction(tx);
    signedBridgeTxs.push(signed);
  }
  console.log(`${tag} Signed ${numAcc} bridge txs in ${elapsed(bridgeStart)}`);

  // Broadcast all at once — simulates concurrent users.
  // The portal's gas metering limits deposits per block (OutOfGas error).
  // Failed txs are retried in subsequent rounds — this is realistic since
  // real users would also retry after a failed tx.
  const bridgeBroadcastStart = Date.now();
  const MAX_RETRIES = 10;
  const RETRY_DELAY_MS = 15_000; // ~1 L1 block
  const succeeded = new Set<number>();
  const pendingIndices = Array.from({ length: numAcc }, (_, i) => i);

  for (let attempt = 0; attempt <= MAX_RETRIES && pendingIndices.length > 0; attempt++) {
    if (attempt > 0) {
      console.log(`${tag} Retry ${attempt}/${MAX_RETRIES}: ${pendingIndices.length} txs remaining, waiting for next block...`);
      await new Promise((r) => setTimeout(r, RETRY_DELAY_MS));

      // Re-sign with fresh nonces and fee data
      const retryFeeData = await l1Provider.getFeeData();
      const retryNonces = await Promise.all(
        pendingIndices.map((i) => l1Provider.getTransactionCount(accounts[i].address, "pending"))
      );
      for (let j = 0; j < pendingIndices.length; j++) {
        const i = pendingIndices[j];
        const accWallet = accounts[i].connect(l1Provider);
        const calldata = bridgeIface.encodeFunctionData("bridgeETHTo", [
          accounts[i].address, MIN_GAS_LIMIT, "0x",
        ]);
        signedBridgeTxs[i] = await accWallet.signTransaction({
          to: rollup.l1Bridge, data: calldata, value: BRIDGE_AMOUNT,
          nonce: retryNonces[j], gasLimit: BRIDGE_GAS_LIMIT,
          maxFeePerGas: retryFeeData.maxFeePerGas!,
          maxPriorityFeePerGas: retryFeeData.maxPriorityFeePerGas!,
          chainId, type: 2,
        });
      }
    }

    // Broadcast pending txs
    const txsToSend = pendingIndices.map((i) => signedBridgeTxs[i]);
    const responses = await Promise.all(
      txsToSend.map((signed) => l1Provider.broadcastTransaction(signed).catch(() => null))
    );

    // Wait for receipts
    const receipts = await Promise.all(
      responses.map((resp) =>
        resp
          ? resp.wait().catch((err: any) => err.receipt || l1Provider.getTransactionReceipt(resp.hash))
          : null
      )
    );

    // Check results
    const stillFailed: number[] = [];
    for (let j = 0; j < pendingIndices.length; j++) {
      const i = pendingIndices[j];
      const r = receipts[j];
      if (r && r.status === 1) {
        succeeded.add(i);
      } else {
        stillFailed.push(i);
      }
    }

    const attemptLabel = attempt === 0 ? "Initial" : `Retry ${attempt}`;
    console.log(`${tag} ${attemptLabel}: ${pendingIndices.length - stillFailed.length} succeeded, ${stillFailed.length} failed`);

    // Update pending list
    pendingIndices.length = 0;
    pendingIndices.push(...stillFailed);
  }

  console.log(`${tag} ${succeeded.size}/${numAcc} bridge txs succeeded in ${elapsed(bridgeBroadcastStart)}`);

  if (succeeded.size < numAcc) {
    const missing = Array.from({ length: numAcc }, (_, i) => i).filter((i) => !succeeded.has(i));
    console.error(`${tag} ASSERT FAILED: ${missing.length}/${numAcc} bridge txs could not succeed after retries`);
    for (const i of missing.slice(0, 5)) {
      console.error(`  Account ${i}: ${accounts[i].address}`);
    }
  } else {
    console.log(`${tag} ASSERT OK: All ${numAcc} bridge txs succeeded on L1`);
  }
  console.log(`${tag} Bridge phase took ${elapsed(bridgeStart)}`);

  // =========================================================================
  // Step 4: Poll for ETH arrival on L2 + assert exact amounts
  // =========================================================================
  console.log(`\n${tag} Step 4: Polling for ETH arrival on ${rollup.name}...`);
  console.log("  (This may take several minutes for op-node to derive deposit txs)\n");
  const pollStart = Date.now();

  // Snapshot L2 balances before (should all be 0 for fresh accounts)
  const l2BalancesBefore = await Promise.all(
    accounts.map((acc) => l2Provider.getBalance(acc.address))
  );

  const received = new Set<number>();
  const MAX_POLLS = 60; // 10 minutes
  const POLL_INTERVAL_MS = 10_000;

  for (let poll = 1; poll <= MAX_POLLS; poll++) {
    // Check all accounts that haven't received yet
    const pending = [];
    for (let i = 0; i < numAcc; i++) {
      if (!received.has(i)) pending.push(i);
    }

    if (pending.length === 0) break;

    // Check balances in parallel
    const balances = await Promise.all(
      pending.map((i) => l2Provider.getBalance(accounts[i].address))
    );

    for (let j = 0; j < pending.length; j++) {
      const idx = pending[j];
      const increase = balances[j] - l2BalancesBefore[idx];
      if (increase > 0n) {
        received.add(idx);
      }
    }

    const pct = ((received.size / numAcc) * 100).toFixed(0);
    process.stdout.write(`  Poll ${poll}/${MAX_POLLS} — ${received.size}/${numAcc} received (${pct}%)...\r`);

    if (received.size === numAcc) break;
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  console.log(`\n${tag} ${received.size}/${numAcc} accounts received ETH on ${rollup.name} in ${elapsed(pollStart)}`);

  if (received.size < numAcc) {
    const missing = [];
    for (let i = 0; i < numAcc; i++) {
      if (!received.has(i)) missing.push(i);
    }
    console.error(`${tag} ASSERT FAILED: ${missing.length} accounts did not receive ETH`);
    console.error(`  First few missing: ${missing.slice(0, 5).map((i) => accounts[i].address).join(", ")}`);
  } else {
    console.log(`${tag} ASSERT OK: All ${numAcc} accounts received ETH on ${rollup.name}`);
  }

  // Assert exact amounts on L2
  console.log(`\n${tag} Verifying exact L2 balances...`);
  const l2BalancesAfter = await Promise.all(
    accounts.map((acc) => l2Provider.getBalance(acc.address))
  );
  let amountMismatch = 0;
  for (let i = 0; i < numAcc; i++) {
    const increase = l2BalancesAfter[i] - l2BalancesBefore[i];
    if (increase !== BRIDGE_AMOUNT) {
      amountMismatch++;
      if (amountMismatch <= 3) {
        console.error(`  Account ${i} (${accounts[i].address}): expected +${ethers.formatEther(BRIDGE_AMOUNT)} ETH, got +${ethers.formatEther(increase)} ETH`);
      }
    }
  }
  if (amountMismatch > 0) {
    console.error(`${tag} ASSERT FAILED: ${amountMismatch}/${numAcc} accounts have incorrect L2 balance`);
  } else {
    console.log(`${tag} ASSERT OK: All ${numAcc} accounts received exactly ${ethers.formatEther(BRIDGE_AMOUNT)} ETH on ${rollup.name}`);
  }

  // =========================================================================
  // Summary
  // =========================================================================
  console.log(`\n${"=".repeat(60)}`);
  console.log(`${tag} STRESS TEST COMPLETE`);
  console.log(`${"=".repeat(60)}`);
  console.log(`  Accounts:         ${numAcc}`);
  console.log(`  Funded:           ${numAcc}/${numAcc}`);
  console.log(`  Bridge txs:       ${succeeded.size}/${numAcc} succeeded`);
  console.log(`  L2 received:      ${received.size}/${numAcc}`);
  console.log(`  Total time:       ${elapsed(totalStart)}`);
}

main().catch((err) => {
  console.error("\n[STRESS] FATAL:", err.message || err);
  process.exit(1);
});
