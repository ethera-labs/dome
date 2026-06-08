import { ethers, randomBytes } from "ethers";
import {
  L1_Rollup_1_RPC,
  L1_Rollup_2_RPC,
  wallet_private_key,
  ROLLUP_A_CHAIN_ID,
  ROLLUP_B_CHAIN_ID,
  COMPOSE_L2_TO_L2_BRIDGE,
  ROLLUP_A_USDC,
} from "../config";
import ComposeL2ToL2BridgeABI from "../sepolia-prod/L2/abis/ComposeL2ToL2Bridge.json";
import ComposableERC20ABI from "../sepolia-prod/L2/abis/ComposableERC20.json";

const ERC20_ABI = [
  "function name() view returns (string)",
  "function symbol() view returns (string)",
  "function decimals() view returns (uint8)",
  "function balanceOf(address) view returns (uint256)",
  "function approve(address spender, uint256 amount) returns (bool)",
];

const BRIDGE_AMOUNT = ethers.parseUnits("1", 6); // 1 USDC (6 decimals) — adjust per token

// ---------------------------------------------------------------------------
// Session ID generation (per spec: version << 240 | keccak256(...) >> 16)
// ---------------------------------------------------------------------------
async function generateSessionId(
  wallet: ethers.Wallet,
  provider: ethers.JsonRpcProvider
): Promise<bigint> {
  const version = 1n;
  const nonce = await provider.getTransactionCount(wallet.address);
  const blockNumber = await provider.getBlockNumber();
  const salt = BigInt("0x" + Buffer.from(randomBytes(4)).toString("hex"));

  const packed = ethers.solidityPacked(
    ["address", "uint32", "uint64", "uint32"],
    [wallet.address, nonce, blockNumber, salt]
  );
  const hash = BigInt(ethers.keccak256(packed));
  return (version << 240n) | (hash >> 16n);
}

// ---------------------------------------------------------------------------
// SEND: bridge native ERC-20 from RollupA to RollupB
// ---------------------------------------------------------------------------
async function sendERC20() {
  console.log("[L2->L2:SEND-ERC20] Bridging native ERC-20 from RollupA to RollupB...\n");

  const provider = new ethers.JsonRpcProvider(L1_Rollup_1_RPC);
  const wallet = new ethers.Wallet(wallet_private_key, provider);
  const walletAddress = wallet.address;

  console.log(`  Wallet:  ${walletAddress}`);
  console.log(`  Token:   ${ROLLUP_A_USDC} (USDC on RollupA)`);
  console.log(`  Amount:  ${ethers.formatUnits(BRIDGE_AMOUNT, 6)} USDC`);

  // Check balance
  const token = new ethers.Contract(ROLLUP_A_USDC, ERC20_ABI, wallet);
  const balance = await token.balanceOf(walletAddress);
  console.log(`  Balance: ${ethers.formatUnits(balance, 6)} USDC`);
  if (balance < BRIDGE_AMOUNT) {
    throw new Error(`Insufficient USDC balance. Have ${ethers.formatUnits(balance, 6)}, need ${ethers.formatUnits(BRIDGE_AMOUNT, 6)}`);
  }

  // Generate sessionId
  const sessionId = await generateSessionId(wallet, provider);
  console.log(`\n  SessionId: ${sessionId.toString()}`);

  // Approve
  console.log("\n  Approving ComposeL2ToL2Bridge...");
  const approveTx = await token.approve(COMPOSE_L2_TO_L2_BRIDGE, BRIDGE_AMOUNT);
  await approveTx.wait();
  console.log(`  Approved. Tx: ${approveTx.hash}`);

  // Bridge
  console.log("\n  Calling bridgeERC20To...");
  const bridge = new ethers.Contract(
    COMPOSE_L2_TO_L2_BRIDGE,
    ComposeL2ToL2BridgeABI,
    wallet
  );

  const bridgeTx = await bridge.bridgeERC20To(
    ROLLUP_B_CHAIN_ID,   // chainDest
    ROLLUP_A_USDC,        // tokenSrc (native ERC-20 on RollupA)
    BRIDGE_AMOUNT,        // amount
    walletAddress,        // receiver (ourselves on RollupB)
    sessionId             // sessionId
  );
  const receipt = await bridgeTx.wait();

  console.log(`\n[L2->L2:SEND-ERC20] Bridge tx confirmed!`);
  console.log(`  Tx hash:  ${receipt!.hash}`);
  console.log(`  Block:    ${receipt!.blockNumber}`);
  console.log(`  Gas used: ${receipt!.gasUsed}`);

  // Print receive instructions
  console.log("\n--- To claim on RollupB, run: ---");
  console.log(`npx ts-node l2-to-l2.ts receive \\`);
  console.log(`  --session-id ${sessionId.toString()} \\`);
  console.log(`  --sender ${walletAddress}`);
  console.log("(Wait for the coordinator to relay the message first)");
}

// ---------------------------------------------------------------------------
// SEND: bridge CET from RollupA to RollupB
// ---------------------------------------------------------------------------
async function sendCET(cetAddress: string) {
  console.log("[L2->L2:SEND-CET] Bridging CET from RollupA to RollupB...\n");

  const provider = new ethers.JsonRpcProvider(L1_Rollup_1_RPC);
  const wallet = new ethers.Wallet(wallet_private_key, provider);
  const walletAddress = wallet.address;

  // Read CET info
  const cet = new ethers.Contract(cetAddress, ComposableERC20ABI, wallet);
  const name = await cet.name();
  const symbol = await cet.symbol();
  const decimals = await cet.decimals();
  const balance = await cet.balanceOf(walletAddress);

  console.log(`  Wallet:  ${walletAddress}`);
  console.log(`  CET:     ${cetAddress} (${name} / ${symbol})`);
  console.log(`  Balance: ${ethers.formatUnits(balance, decimals)} ${symbol}`);

  const amount = balance < BRIDGE_AMOUNT ? balance : BRIDGE_AMOUNT;
  if (amount === 0n) {
    throw new Error("No CET balance to bridge.");
  }
  console.log(`  Amount:  ${ethers.formatUnits(amount, decimals)} ${symbol}`);

  // Generate sessionId
  const sessionId = await generateSessionId(wallet, provider);
  console.log(`\n  SessionId: ${sessionId.toString()}`);

  // Approve
  console.log("\n  Approving ComposeL2ToL2Bridge...");
  const approveTx = await cet.approve(COMPOSE_L2_TO_L2_BRIDGE, amount);
  await approveTx.wait();
  console.log(`  Approved. Tx: ${approveTx.hash}`);

  // Bridge
  console.log("\n  Calling bridgeCETTo...");
  const bridge = new ethers.Contract(
    COMPOSE_L2_TO_L2_BRIDGE,
    ComposeL2ToL2BridgeABI,
    wallet
  );

  const bridgeTx = await bridge.bridgeCETTo(
    ROLLUP_B_CHAIN_ID,   // chainDest
    cetAddress,           // cetTokenSrc
    amount,               // amount
    walletAddress,        // receiver (ourselves on RollupB)
    sessionId             // sessionId
  );
  const receipt = await bridgeTx.wait();

  console.log(`\n[L2->L2:SEND-CET] Bridge tx confirmed!`);
  console.log(`  Tx hash:  ${receipt!.hash}`);
  console.log(`  Block:    ${receipt!.blockNumber}`);
  console.log(`  Gas used: ${receipt!.gasUsed}`);

  // Print receive instructions
  console.log("\n--- To claim on RollupB, run: ---");
  console.log(`npx ts-node l2-to-l2.ts receive \\`);
  console.log(`  --session-id ${sessionId.toString()} \\`);
  console.log(`  --sender ${walletAddress}`);
  console.log("(Wait for the coordinator to relay the message first)");
}

// ---------------------------------------------------------------------------
// RECEIVE: claim tokens on RollupB
// ---------------------------------------------------------------------------
async function receive(sessionId: string, sender: string) {
  console.log("[L2->L2:RECEIVE] Claiming tokens on RollupB...\n");

  const provider = new ethers.JsonRpcProvider(L1_Rollup_2_RPC);
  const wallet = new ethers.Wallet(wallet_private_key, provider);
  const walletAddress = wallet.address;

  console.log(`  Wallet (receiver): ${walletAddress}`);
  console.log(`  Sender:            ${sender}`);
  console.log(`  SessionId:         ${sessionId}`);

  const bridge = new ethers.Contract(
    COMPOSE_L2_TO_L2_BRIDGE,
    ComposeL2ToL2BridgeABI,
    wallet
  );

  // Construct the MessageHeader for the SEND_TOKENS message.
  // NOTE: The label might be "SEND_TOKEN" (without 's') depending on the contract.
  // If receiveTokens reverts, try changing the label.
  const msgHeader = {
    chainSrc: ROLLUP_A_CHAIN_ID,
    chainDest: ROLLUP_B_CHAIN_ID,
    sender: sender,
    receiver: walletAddress,
    sessionId: BigInt(sessionId),
    label: "SEND_TOKENS",
  };

  console.log("\n  Calling receiveTokens...");
  try {
    const tx = await bridge.receiveTokens(msgHeader);
    const receipt = await tx.wait();

    // Parse return values from the logs
    console.log(`\n[L2->L2:RECEIVE] Tokens received!`);
    console.log(`  Tx hash:  ${receipt!.hash}`);
    console.log(`  Block:    ${receipt!.blockNumber}`);
    console.log(`  Gas used: ${receipt!.gasUsed}`);

    // Try to extract the TokensReceived event
    for (const log of receipt!.logs) {
      try {
        const parsed = bridge.interface.parseLog({ topics: log.topics as string[], data: log.data });
        if (parsed && parsed.name === "TokensReceived") {
          console.log(`  Token:    ${parsed.args.token}`);
          console.log(`  Amount:   ${parsed.args.amount}`);
        }
      } catch {
        // Skip logs that don't match our ABI
      }
    }
  } catch (err: any) {
    const msg = err.message || String(err);
    if (msg.includes("NoSendMessage") || msg.includes("No SEND")) {
      console.error("\n[L2->L2:RECEIVE] Message not found in mailbox.");
      console.error("  The coordinator may not have relayed the message yet.");
      console.error("  Wait and try again.");
    } else if (msg.includes("NotReceiver")) {
      console.error("\n[L2->L2:RECEIVE] msg.sender is not the receiver in the header.");
      console.error(`  Expected receiver: ${walletAddress}`);
    } else {
      throw err;
    }
  }
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------
function printUsage() {
  console.log(`
Usage:
  npx ts-node l2-to-l2.ts send-erc20
    Bridge native USDC from RollupA to RollupB.

  npx ts-node l2-to-l2.ts send-cet --cet-address <address>
    Bridge a CET token from RollupA to RollupB.

  npx ts-node l2-to-l2.ts receive --session-id <id> --sender <address>
    Claim tokens on RollupB (after coordinator relay).
  `);
}

function getArg(flag: string): string | undefined {
  const idx = process.argv.indexOf(flag);
  if (idx !== -1 && idx + 1 < process.argv.length) {
    return process.argv[idx + 1];
  }
  return undefined;
}

async function main() {
  const mode = process.argv[2];

  switch (mode) {
    case "send-erc20":
      await sendERC20();
      break;

    case "send-cet": {
      const cetAddress = getArg("--cet-address");
      if (!cetAddress) {
        console.error("Error: --cet-address is required for send-cet mode");
        printUsage();
        process.exit(1);
      }
      await sendCET(cetAddress);
      break;
    }

    case "receive": {
      const sessionId = getArg("--session-id");
      const sender = getArg("--sender");
      if (!sessionId || !sender) {
        console.error("Error: --session-id and --sender are required for receive mode");
        printUsage();
        process.exit(1);
      }
      await receive(sessionId, sender);
      break;
    }

    default:
      printUsage();
      process.exit(1);
  }
}

main().catch((err) => {
  console.error("\n[L2->L2] FATAL:", err.message || err);
  process.exit(1);
});
