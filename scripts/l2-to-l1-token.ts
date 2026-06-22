import { ethers } from "ethers";
import * as fs from "fs";
import * as path from "path";
import {
  L1_RPC,
  L1_Rollup_1_RPC,
  L1_Rollup_2_RPC,
  wallet_private_key,
  L1_CHAIN_ID,
  COMPOSE_L1_BRIDGE_ROLLUP_A,
  COMPOSE_L1_BRIDGE_ROLLUP_B,
  COMPOSE_L2_BRIDGE_ROLLUP_A,
  COMPOSE_L2_BRIDGE_ROLLUP_B,
  COMPOSE_PORTAL_ROLLUP_A,
  COMPOSE_PORTAL_ROLLUP_B,
  CET_FACTORY,
} from "../config";
import ComposeL1BridgeABI from "../sepolia-prod/L1/abis/ComposeL1Bridge.json";
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
import CETFactoryABI from "../sepolia-prod/L2/abis/CETFactory.json";

const ERC20_ABI = [
  "function name() view returns (string)",
  "function symbol() view returns (string)",
  "function decimals() view returns (uint8)",
  "function balanceOf(address) view returns (uint256)",
  "function approve(address spender, uint256 amount) returns (bool)",
  "function mint(address to, uint256 amount)",
];

const MINTABLE_TOKEN_ABI = [
  "constructor(string name, string symbol, uint8 decimals_)",
  ...ERC20_ABI,
];

const MINTABLE_TOKEN_BYTECODE =
  "0x608060405234801562000010575f80fd5b5060405162000a8f38038062000a8f83398101604081905262000033916200012b565b5f62000040848262000236565b5060016200004f838262000236565b506002805460ff191660ff9290921691909117905550620002fe9050565b634e487b7160e01b5f52604160045260245ffd5b5f82601f83011262000091575f80fd5b81516001600160401b0380821115620000ae57620000ae6200006d565b604051601f8301601f19908116603f01168101908282118183101715620000d957620000d96200006d565b81604052838152602092508683858801011115620000f5575f80fd5b5f91505b83821015620001185785820183015181830184015290820190620000f9565b5f93810190920192909252949350505050565b5f805f606084860312156200013e575f80fd5b83516001600160401b038082111562000155575f80fd5b620001638783880162000081565b9450602086015191508082111562000179575f80fd5b50620001888682870162000081565b925050604084015160ff811681146200019f575f80fd5b809150509250925092565b600181811c90821680620001bf57607f821691505b602082108103620001de57634e487b7160e01b5f52602260045260245ffd5b50919050565b601f82111562000231575f81815260208120601f850160051c810160208610156200020c5750805b601f850160051c820191505b818110156200022d5782815560010162000218565b5050505b505050565b81516001600160401b038111156200025257620002526200006d565b6200026a81620002638454620001aa565b84620001e4565b602080601f831160018114620002a0575f8415620002885750858301515b5f19600386901b1c1916600185901b1785556200022d565b5f85815260208120601f198616915b82811015620002d057888601518255948401946001909101908401620002af565b5085821015620002ee57878501515f19600388901b60f8161c191681555b5050505050600190811b01905550565b610783806200030c5f395ff3fe608060405234801561000f575f80fd5b506004361061009b575f3560e01c806340c10f191161006357806340c10f191461012957806370a082311461013e57806395d89b411461015d578063a9059cbb14610165578063dd62ed3e14610178575f80fd5b806306fdde031461009f578063095ea7b3146100bd57806318160ddd146100e057806323b872dd146100f7578063313ce5671461010a575b5f80fd5b6100a76101a2565b6040516100b491906105c3565b60405180910390f35b6100d06100cb366004610629565b61022d565b60405190151581526020016100b4565b6100e960035481565b6040519081526020016100b4565b6100d0610105366004610651565b610299565b6002546101179060ff1681565b60405160ff90911681526020016100b4565b61013c610137366004610629565b61044f565b005b6100e961014c36600461068a565b60046020525f908152604090205481565b6100a76104d5565b6100d0610173366004610629565b6104e2565b6100e96101863660046106aa565b600560209081525f928352604080842090915290825290205481565b5f80546101ae906106db565b80601f01602080910402602001604051908101604052809291908181526020018280546101da906106db565b80156102255780601f106101fc57610100808354040283529160200191610225565b820191905f5260205f20905b81548152906001019060200180831161020857829003601f168201915b505050505081565b335f8181526005602090815260408083206001600160a01b038716808552925280832085905551919290917f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925906102879086815260200190565b60405180910390a35060015b92915050565b6001600160a01b0383165f9081526005602090815260408083203384529091528120548211156103095760405162461bcd60e51b8152602060048201526016602482015275696e73756666696369656e7420616c6c6f77616e636560501b60448201526064015b60405180910390fd5b6001600160a01b0384165f908152600460205260409020548211156103675760405162461bcd60e51b8152602060048201526014602482015273696e73756666696369656e742062616c616e636560601b6044820152606401610300565b6001600160a01b0384165f90815260056020908152604080832033845290915281208054849290610399908490610727565b90915550506001600160a01b0384165f90815260046020526040812080548492906103c5908490610727565b90915550506001600160a01b0383165f90815260046020526040812080548492906103f190849061073a565b92505081905550826001600160a01b0316846001600160a01b03167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef8460405161043d91815260200190565b60405180910390a35060019392505050565b8060035f828254610460919061073a565b90915550506001600160a01b0382165f908152600460205260408120805483929061048c90849061073a565b90915550506040518181526001600160a01b038316905f907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9060200160405180910390a35050565b600180546101ae906106db565b335f908152600460205260408120548211156105375760405162461bcd60e51b8152602060048201526014602482015273696e73756666696369656e742062616c616e636560601b6044820152606401610300565b335f9081526004602052604081208054849290610555908490610727565b90915550506001600160a01b0383165f908152600460205260408120805484929061058190849061073a565b90915550506040518281526001600160a01b0384169033907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef90602001610287565b5f6020808352835180828501525f5b818110156105ee578581018301518582016040015282016105d2565b505f604082860101526040601f19601f8301168501019250505092915050565b80356001600160a01b0381168114610624575f80fd5b919050565b5f806040838503121561063a575f80fd5b6106438361060e565b946020939093013593505050565b5f805f60608486031215610663575f80fd5b61066c8461060e565b925061067a6020850161060e565b9150604084013590509250925092565b5f6020828403121561069a575f80fd5b6106a38261060e565b9392505050565b5f80604083850312156106bb575f80fd5b6106c48361060e565b91506106d26020840161060e565b90509250929050565b600181811c908216806106ef57607f821691505b60208210810361070d57634e487b7160e01b5f52602260045260245ffd5b50919050565b634e487b7160e01b5f52601160045260245ffd5b8181038181111561029357610293610713565b808201808211156102935761029361071356fea26469706673582212206f24f4f523f3527fa0861762239b948bb5596cf1743604001ae31f92dc47042f64736f6c63430008140033";

const BRIDGE_AMOUNT = ethers.parseUnits("100", 18);
const MIN_GAS_LIMIT = 2_500_000;

// Per-rollup addresses — from config (network-aware)
const L2_COMPOSE_BRIDGE: Record<string, string> = {
  a: COMPOSE_L2_BRIDGE_ROLLUP_A,
  b: COMPOSE_L2_BRIDGE_ROLLUP_B,
};

const L1_COMPOSE_PORTAL: Record<string, string> = {
  a: COMPOSE_PORTAL_ROLLUP_A,
  b: COMPOSE_PORTAL_ROLLUP_B,
};

const L1_COMPOSE_BRIDGE: Record<string, string> = {
  a: COMPOSE_L1_BRIDGE_ROLLUP_A,
  b: COMPOSE_L1_BRIDGE_ROLLUP_B,
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

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// State file
// ---------------------------------------------------------------------------
interface TokenWithdrawalState {
  sourceKey: string;
  tokenAddress: string;
  predictedCET: string;
  withdrawalHash: string;
  withdrawalNonce: string;
  withdrawalSender: string;
  withdrawalTarget: string;
  withdrawalValue: string;
  withdrawalGasLimit: string;
  withdrawalData: string;
  l2TxHash: string;
  l2Block: number;
  l1TokenBalanceBefore: string;
  bridgeToL2TxHash: string;
  withdrawL2TxHash: string;
}

function stateFilePath(sourceKey: string): string {
  return path.join(__dirname, `.l2-to-l1-token-state-${sourceKey}`);
}

function loadTokenState(sourceKey: string): TokenWithdrawalState | null {
  try { return JSON.parse(fs.readFileSync(stateFilePath(sourceKey), "utf-8")); } catch { return null; }
}

function saveTokenState(state: TokenWithdrawalState) {
  fs.writeFileSync(stateFilePath(state.sourceKey), JSON.stringify(state, null, 2) + "\n");
}

function deleteTokenState(sourceKey: string) {
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

function printUsage(): never {
  console.error(`
Usage:
  Part 1 — Deploy token, bridge L1->L2, withdraw L2->L1 (saves state):
    npx ts-node scripts/l2-to-l1-token.ts withdraw --source <a|b>

  Part 2 — Prove + finalize on L1 (loads state):
    npx ts-node scripts/l2-to-l1-token.ts finalize --source <a|b>

  Both parts in one run:
    npx ts-node scripts/l2-to-l1-token.ts --source <a|b>

Example:
  npx ts-node scripts/l2-to-l1-token.ts withdraw --source a
  # ... wait hours/days ...
  npx ts-node scripts/l2-to-l1-token.ts finalize --source a
  `);
  process.exit(1);
}

// ---------------------------------------------------------------------------
// Part 1: Deploy token on L1, bridge to L2, withdraw from L2, save state
// ---------------------------------------------------------------------------
async function withdrawPhase(sourceKey: string) {

  const chainName = CHAIN_NAMES[sourceKey];
  const tag = `[L1<->${chainName}:TOKEN]`;

  // Setup
  console.log(`${tag} Setting up...`);
  const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
  const l2Provider = new ethers.JsonRpcProvider(L2_RPC[sourceKey]);
  const l1Wallet = new ethers.Wallet(wallet_private_key, l1Provider);
  const l2Wallet = new ethers.Wallet(wallet_private_key, l2Provider);
  const walletAddress = l1Wallet.address;

  console.log(`${tag} Wallet: ${walletAddress}`);

  // =========================================================================
  // PHASE 1: L1 → L2 (deploy token, bridge to L2, wait for CET)
  // =========================================================================
  console.log(`\n${"=".repeat(60)}`);
  console.log(`${tag} PHASE 1: L1 -> ${chainName} (deploy + bridge)`);
  console.log(`${"=".repeat(60)}`);

  // Deploy token on L1
  console.log(`\n${tag} Deploying test ERC-20 on L1...`);
  const tokenFactory = new ethers.ContractFactory(MINTABLE_TOKEN_ABI, MINTABLE_TOKEN_BYTECODE, l1Wallet);
  const deployTx = await tokenFactory.deploy("RoundTripToken", "RTT", 18);
  const tokenDeployment = await deployTx.waitForDeployment();
  const tokenAddress = await tokenDeployment.getAddress();
  console.log(`${tag} Token deployed at: ${tokenAddress}`);

  const l1Token = new ethers.Contract(tokenAddress, ERC20_ABI, l1Wallet);

  // Mint
  console.log(`${tag} Minting 100 RTT...`);
  const mintTx = await l1Token.mint(walletAddress, BRIDGE_AMOUNT);
  const mintReceipt = await mintTx.wait();
  console.log(`${tag} Mint tx: ${mintReceipt!.hash}`);

  // Compute predicted CET on L2
  const cetFactory = new ethers.Contract(CET_FACTORY, CETFactoryABI, l2Provider);
  const predictedCET: string = await cetFactory.predictAddress(tokenAddress, L1_CHAIN_ID);
  console.log(`${tag} Predicted CET on ${chainName}: ${predictedCET}`);

  // Read metadata + encode extraData
  const name = await l1Token.name();
  const symbol = await l1Token.symbol();
  const decimals: number = await l1Token.decimals();
  const extraData = ethers.AbiCoder.defaultAbiCoder().encode(
    ["string", "string", "uint8", "bytes"],
    [name, symbol, decimals, "0x"]
  );

  // Approve + Bridge L1 -> L2
  console.log(`${tag} Approving and bridging to ${chainName}...`);
  const approveTx = await l1Token.approve(L1_COMPOSE_BRIDGE[sourceKey], BRIDGE_AMOUNT);
  await approveTx.wait();

  const l1Bridge = new ethers.Contract(L1_COMPOSE_BRIDGE[sourceKey], ComposeL1BridgeABI, l1Wallet);
  const bridgeToL2Tx = await l1Bridge.bridgeERC20To(
    tokenAddress, predictedCET, walletAddress, BRIDGE_AMOUNT, MIN_GAS_LIMIT, extraData
  );
  const bridgeToL2Receipt = await bridgeToL2Tx.wait();
  console.log(`${tag} L1->L2 bridge tx: ${bridgeToL2Receipt!.hash}`);

  // Assert L1 token balance is now 0
  const l1BalanceAfterBridge = await l1Token.balanceOf(walletAddress);
  if (BigInt(l1BalanceAfterBridge) !== 0n) {
    throw new Error(`ASSERT FAILED: L1 token balance should be 0 after bridge, got ${ethers.formatUnits(l1BalanceAfterBridge, decimals)}`);
  }
  console.log(`${tag} ASSERT OK: L1 token balance is 0 (locked in bridge)`);

  // Poll for CET arrival on L2
  console.log(`\n${tag} Waiting for CET arrival on ${chainName}...`);
  const cetToken = new ethers.Contract(predictedCET, ERC20_ABI, l2Provider);
  const MAX_POLLS_L2 = 60;

  for (let i = 1; i <= MAX_POLLS_L2; i++) {
    try {
      const cetBalance = BigInt(await cetToken.balanceOf(walletAddress));
      if (cetBalance > 0n) {
        console.log(`\n${tag} CET arrived! Balance: ${ethers.formatUnits(cetBalance, decimals)} ${symbol}`);
        if (cetBalance.toString() !== BRIDGE_AMOUNT.toString()) {
          throw new Error(`ASSERT FAILED: Expected CET ${ethers.formatUnits(BRIDGE_AMOUNT, decimals)}, got ${ethers.formatUnits(cetBalance, decimals)}`);
        }
        console.log(`${tag} ASSERT OK: L2 CET balance matches bridged amount`);
        break;
      }
    } catch (err) {
      if (err instanceof Error && err.message.startsWith("ASSERT FAILED")) throw err;
    }
    if (i === MAX_POLLS_L2) {
      throw new Error("Timed out waiting for CET on L2. Cannot proceed with L2->L1 withdrawal.");
    }
    process.stdout.write(`  Poll ${i}/${MAX_POLLS_L2} — waiting for CET...\r`);
    await new Promise((r) => setTimeout(r, 10_000));
  }

  // =========================================================================
  // PHASE 2: L2 → L1 (withdraw CET back to L1, wait for proof, finalize)
  // =========================================================================
  console.log(`\n${"=".repeat(60)}`);
  console.log(`${tag} PHASE 2: ${chainName} -> L1 (withdraw + prove + finalize)`);
  console.log(`${"=".repeat(60)}`);

  // Snapshot L1 token balance before withdrawal (should be 0 — tokens are in lockbox)
  const l1TokenBalanceBefore = await l1Token.balanceOf(walletAddress);
  console.log(`\n${tag} L1 token balance before withdrawal: ${ethers.formatUnits(l1TokenBalanceBefore, decimals)}`);

  // Initiate withdrawal: bridgeERC20To on L2 (burns CET, sends cross-domain message)
  // _localToken = CET on L2, _remoteToken = original token on L1
  console.log(`${tag} Initiating withdrawal on ${chainName}...`);
  const l2Bridge = new ethers.Contract(L2_COMPOSE_BRIDGE[sourceKey], ComposeL2BridgeABI, l2Wallet);

  const withdrawTx = await l2Bridge.bridgeERC20To(
    predictedCET,     // _localToken: CET on L2
    tokenAddress,     // _remoteToken: original token on L1
    walletAddress,    // _to: ourselves on L1
    BRIDGE_AMOUNT,    // _amount
    MIN_GAS_LIMIT,    // _minGasLimit
    "0x"              // _extraData
  );
  const withdrawReceipt = await withdrawTx.wait();

  console.log(`${tag} Withdrawal initiated!`);
  console.log(`  Tx hash: ${withdrawReceipt!.hash}`);
  console.log(`  Block:   ${withdrawReceipt!.blockNumber}`);

  // Assert CET balance on L2 is now 0
  const l2CetBalanceAfter = BigInt(await cetToken.balanceOf(walletAddress));
  if (l2CetBalanceAfter !== 0n) {
    throw new Error(`ASSERT FAILED: L2 CET balance should be 0 after withdrawal, got ${ethers.formatUnits(l2CetBalanceAfter, decimals)}`);
  }
  console.log(`${tag} ASSERT OK: L2 CET balance is 0 (burned)`);

  // Extract withdrawal hash from MessagePassed event
  console.log(`\n${tag} Extracting withdrawal details...`);
  const msgPasserIface = new ethers.Interface(MESSAGE_PASSER_ABI);
  let withdrawalHash: string | null = null;
  let withdrawalNonce: bigint | null = null;
  let withdrawalSender: string | null = null;
  let withdrawalTarget: string | null = null;
  let withdrawalValue: bigint | null = null;
  let withdrawalGasLimit: bigint | null = null;
  let withdrawalData: string | null = null;

  for (const log of withdrawReceipt!.logs) {
    if (log.address.toLowerCase() === L2_TO_L1_MESSAGE_PASSER.toLowerCase()) {
      try {
        const parsed = msgPasserIface.parseLog({ topics: log.topics as string[], data: log.data });
        if (parsed && parsed.name === "MessagePassed") {
          withdrawalNonce = parsed.args[0];
          withdrawalSender = parsed.args[1];
          withdrawalTarget = parsed.args[2];
          withdrawalValue = parsed.args[3];
          withdrawalGasLimit = parsed.args[4];
          withdrawalData = parsed.args[5];
          withdrawalHash = parsed.args[6];
          break;
        }
      } catch {}
    }
  }

  if (!withdrawalHash) {
    throw new Error("Failed to find MessagePassed event in withdrawal receipt");
  }

  console.log(`${tag} Withdrawal hash: ${withdrawalHash}`);
  console.log(`${tag} Nonce: ${withdrawalNonce}`);
  console.log(`${tag} Value: ${ethers.formatEther(withdrawalValue!)} ETH`);

  // Save state for part 2
  const state: TokenWithdrawalState = {
    sourceKey,
    tokenAddress,
    predictedCET,
    withdrawalHash: withdrawalHash!,
    withdrawalNonce: withdrawalNonce!.toString(),
    withdrawalSender: withdrawalSender!,
    withdrawalTarget: withdrawalTarget!,
    withdrawalValue: withdrawalValue!.toString(),
    withdrawalGasLimit: withdrawalGasLimit!.toString(),
    withdrawalData: withdrawalData!,
    l2TxHash: withdrawReceipt!.hash,
    l2Block: withdrawReceipt!.blockNumber,
    l1TokenBalanceBefore: l1TokenBalanceBefore.toString(),
    bridgeToL2TxHash: bridgeToL2Receipt!.hash,
    withdrawL2TxHash: withdrawReceipt!.hash,
  };
  saveTokenState(state);
  console.log(`\n${tag} State saved to ${stateFilePath(sourceKey)}`);
  console.log(`${tag} Run 'finalize --source ${sourceKey}' later to prove + finalize.`);
}

// ---------------------------------------------------------------------------
// Part 2: Prove + finalize on L1
// ---------------------------------------------------------------------------
async function finalizePhase(sourceKey: string) {
  const state = loadTokenState(sourceKey);
  if (!state) throw new Error(`No withdrawal state for source ${sourceKey}. Run 'withdraw' first.`);

  const chainName = CHAIN_NAMES[sourceKey];
  const tag = `[L1<->${chainName}:TOKEN]`;

  const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
  const l2Provider = new ethers.JsonRpcProvider(L2_RPC[sourceKey]);
  const l1Wallet = new ethers.Wallet(wallet_private_key, l1Provider);
  const walletAddress = l1Wallet.address;

  const l1Token = new ethers.Contract(state.tokenAddress, ERC20_ABI, l1Wallet);
  const decimals: number = await l1Token.decimals();

  console.log(`${tag} Loaded withdrawal state:`);
  console.log(`  Token: ${state.tokenAddress}`);
  console.log(`  Withdrawal hash: ${state.withdrawalHash}`);
  console.log(`  L2 block: ${state.l2Block}`);

  const portal = new ethers.Contract(L1_COMPOSE_PORTAL[sourceKey], ComposePortalABI, l1Wallet);
  const withdrawalTxStruct = {
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
    deleteTokenState(sourceKey);
    return;
  }

  // Check if already proven
  const numSubmitters = await portal.numProofSubmitters(state.withdrawalHash);
  const POLL_INTERVAL_MS = 10_000;

  if (Number(numSubmitters) > 0) {
    console.log(`${tag} Withdrawal already proven. Skipping to finalize.`);
  } else {
    // Find dispute game + prove
    const dgf = new ethers.Contract(await portal.disputeGameFactory(), DGF_ABI, l1Provider);
    const respectedGameType = await portal.respectedGameType();

    // Find dispute game covering our L2 block
    console.log(`\n${tag} Waiting for dispute game covering L2 block ${state.l2Block}...`);
    let foundGameIndex = -1;
    let foundGameL2Block = 0;

    for (let poll = 1; poll <= 360; poll++) {
      const count = await dgf.gameCount();
      for (let gi = Number(count) - 1; gi >= Math.max(0, Number(count) - 10); gi--) {
        const gameInfo = await dgf.gameAtIndex(gi);
        if (Number(gameInfo.gameType) !== Number(respectedGameType)) continue;
        const game = new ethers.Contract(gameInfo.proxy, GAME_ABI, l1Provider);
        const extraData = await game.extraData();
        const hexStr = extraData.slice(2);
        for (let pos = 0; pos < hexStr.length - 64; pos += 64) {
          const val = BigInt("0x" + hexStr.slice(pos, pos + 64));
          if (val > 1_000_000n && val < 100_000_000n && Number(val) >= state.l2Block) {
            foundGameIndex = gi;
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
    const storageSlotProof = ethers.keccak256(
      ethers.AbiCoder.defaultAbiCoder().encode(["bytes32", "uint256"], [state.withdrawalHash, 0])
    );
    const storageProof = await l2Provider.send("eth_getProof", [
      "0x4200000000000000000000000000000000000016", [storageSlotProof], ethers.toQuantity(foundGameL2Block),
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
      withdrawalTxStruct, foundGameIndex, outputRootProof,
      storageProof.storageProof[0].proof, { gasLimit: 500_000 }
    );
    const proveReceipt = await proveTx.wait();
    console.log(`${tag} Prove tx: ${proveReceipt!.hash}`);
  } // end if not already proven

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
      const finalizeTx = await portal.finalizeWithdrawalTransaction(withdrawalTxStruct);
      const finalizeReceipt = await finalizeTx.wait();

      console.log(`${tag} Finalization confirmed! Tx: ${finalizeReceipt!.hash}`);

      // Assert L1 token balance restored
      const l1TokenBalanceAfter = await l1Token.balanceOf(walletAddress);
      const l1Increase = BigInt(l1TokenBalanceAfter) - BigInt(state.l1TokenBalanceBefore);
      console.log(`${tag} L1 token balance after: ${ethers.formatUnits(l1TokenBalanceAfter, decimals)}`);

      if (l1Increase.toString() !== BRIDGE_AMOUNT.toString()) {
        throw new Error(
          `ASSERT FAILED: Expected L1 token increase of ${ethers.formatUnits(BRIDGE_AMOUNT, decimals)}, ` +
          `got ${ethers.formatUnits(l1Increase, decimals)}`
        );
      }
      console.log(`${tag} ASSERT OK: L1 token balance restored`);

      console.log(`\n${"=".repeat(60)}`);
      console.log(`${tag} ROUND TRIP COMPLETE!`);
      console.log(`${"=".repeat(60)}`);
      deleteTokenState(sourceKey);
      return;
    } catch (err: any) {
      const msg = err.message || String(err);
      if (msg.includes("Unproven") || msg.includes("ProofNotOldEnough") || msg.includes("CALL_EXCEPTION")) {
        // Not ready yet
      } else if (msg.includes("AlreadyFinalized")) {
        console.log(`\n${tag} Withdrawal already finalized!`);
        deleteTokenState(sourceKey);
        return;
      } else if (msg.startsWith("ASSERT FAILED")) {
        throw err;
      } else {
        throw err;
      }
    }
    process.stdout.write(`  Poll ${poll}/2160 — waiting for finalization window...\r`);
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  console.log(`\n${tag} Timed out. Run 'finalize' again later.`);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
async function main() {
  const mode = process.argv[2];
  const sourceKey = getArg("--source")?.toLowerCase();
  if (!sourceKey || !L2_COMPOSE_BRIDGE[sourceKey]) { console.error("Error: --source must be 'a' or 'b'"); printUsage(); }

  if (mode === "withdraw") {
    await withdrawPhase(sourceKey);
  } else if (mode === "finalize") {
    await finalizePhase(sourceKey);
  } else {
    // Legacy: both parts
    await withdrawPhase(sourceKey);
    console.log("\n" + "=".repeat(60));
    console.log("Proceeding to prove + finalize...");
    console.log("=".repeat(60) + "\n");
    await finalizePhase(sourceKey);
  }
}

main().catch((err) => {
  console.error("\n[L2->L1:TOKEN] FATAL:", err.message || err);
  process.exit(1);
});
