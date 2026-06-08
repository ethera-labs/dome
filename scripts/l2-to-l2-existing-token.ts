import { ethers } from "ethers";
import * as fs from "fs";
import * as path from "path";
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
  CET_FACTORY,
} from "../config";
import { submitXt } from "./xt-submit";
import ComposeL2ToL2BridgeABI from "../sepolia-prod/L2/abis/ComposeL2ToL2Bridge.json";
import CETFactoryABI from "../sepolia-prod/L2/abis/CETFactory.json";

// ethers ABIs for read calls
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
const MAX_UINT256 = ethers.MaxUint256;

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
Usage: npx ts-node scripts/l2-to-l2-existing-token.ts --source <a|b> --dest <a|b>

  --source   Source rollup: a or b
  --dest     Destination rollup: a or b

Example:
  npx ts-node scripts/l2-to-l2-existing-token.ts --source a --dest b
  `);
  process.exit(1);
}

// ---------------------------------------------------------------------------
// State file — keyed by source-dest pair
// ---------------------------------------------------------------------------
interface State {
  tokenAddress: string;
  predictedCET: string;
  source: string;
  dest: string;
}

function stateFilePath(sourceKey: string, destKey: string): string {
  return path.join(__dirname, `.l2-to-l2-existing-token-state-${sourceKey}-${destKey}`);
}

function loadState(sourceKey: string, destKey: string): State | null {
  try {
    const raw = fs.readFileSync(stateFilePath(sourceKey, destKey), "utf-8");
    return JSON.parse(raw) as State;
  } catch {
    return null;
  }
}

function saveState(sourceKey: string, destKey: string, state: State) {
  fs.writeFileSync(stateFilePath(sourceKey, destKey), JSON.stringify(state, null, 2) + "\n");
}

// ---------------------------------------------------------------------------
// First run: deploy token on source, approve bridge, save state
// ---------------------------------------------------------------------------
async function firstRun(
  source: RollupConfig,
  dest: RollupConfig,
  sourceKey: string,
  destKey: string,
  srcWallet: ethers.Wallet,
  dstProvider: ethers.JsonRpcProvider
): Promise<State> {
  const tag = `[${source.name}->${dest.name}]`;

  // Deploy token on source
  console.log(`\n${tag} Step 1: Deploying test ERC-20 on ${source.name}...`);
  const tokenFactory = new ethers.ContractFactory(
    MINTABLE_TOKEN_ABI,
    MINTABLE_TOKEN_BYTECODE,
    srcWallet
  );
  const deployTx = await tokenFactory.deploy("ExistingToken", "ET", 18);
  const tokenDeployment = await deployTx.waitForDeployment();
  const tokenAddress = await tokenDeployment.getAddress();
  console.log(`${tag} Token deployed at: ${tokenAddress}`);

  // Compute predicted CET on dest
  console.log(`\n${tag} Step 2: Computing predicted CET address on ${dest.name}...`);
  const cetFactory = new ethers.Contract(CET_FACTORY, CETFactoryABI, dstProvider);
  const predictedCET: string = await cetFactory.predictAddress(tokenAddress, source.chainId);
  console.log(`${tag} Predicted CET on ${dest.name}: ${predictedCET}`);

  // Approve bridge for max
  console.log(`\n${tag} Step 3: Approving ComposeL2ToL2Bridge for max uint256...`);
  const token = new ethers.Contract(tokenAddress, ERC20_ABI, srcWallet);
  const approveTx = await token.approve(COMPOSE_L2_TO_L2_BRIDGE, MAX_UINT256);
  const approveReceipt = await approveTx.wait();
  console.log(`${tag} Approve tx: ${approveReceipt!.hash}`);

  // Save state
  const state: State = { tokenAddress, predictedCET, source: sourceKey, dest: destKey };
  saveState(sourceKey, destKey, state);
  console.log(`${tag} State saved to ${stateFilePath(sourceKey, destKey)}`);

  return state;
}

// ---------------------------------------------------------------------------
// Subsequent runs: mint, compose bridge + receive, assert
// ---------------------------------------------------------------------------
async function bridgeRun(
  state: State,
  source: RollupConfig,
  dest: RollupConfig,
  sourceKey: string,
  destKey: string
) {
  const tag = `[${source.name}->${dest.name}]`;
  const { tokenAddress, predictedCET } = state;

  // ethers providers for read calls and minting
  const srcEthersProvider = new ethers.JsonRpcProvider(source.l2Rpc);
  const dstEthersProvider = new ethers.JsonRpcProvider(dest.l2Rpc);
  const srcEthersWallet = new ethers.Wallet(wallet_private_key, srcEthersProvider);
  const walletAddress = srcEthersWallet.address;

  const token = new ethers.Contract(tokenAddress, ERC20_ABI, srcEthersWallet);

  // Read token metadata
  const name: string = await token.name();
  const symbol: string = await token.symbol();
  const decimals: number = await token.decimals();

  console.log(`\n${tag} Token: ${name} (${symbol}), ${decimals} decimals`);
  console.log(`${tag} Token address: ${tokenAddress}`);
  console.log(`${tag} Predicted CET: ${predictedCET}`);

  // Step 1: Mint 100 tokens
  console.log(`\n${tag} Step 1: Minting 100 tokens...`);
  const mintTx = await token.mint(walletAddress, BRIDGE_AMOUNT);
  const mintReceipt = await mintTx.wait();
  console.log(`${tag} Mint tx: ${mintReceipt!.hash}`);

  // Snapshot balances before
  const srcBalanceBefore: bigint = await token.balanceOf(walletAddress);
  console.log(`${tag} Source token balance before: ${ethers.formatUnits(srcBalanceBefore, decimals)}`);

  const cetToken = new ethers.Contract(predictedCET, ERC20_ABI, dstEthersProvider);
  let dstBalanceBefore = 0n;
  try {
    dstBalanceBefore = BigInt(await cetToken.balanceOf(walletAddress));
  } catch {
    // CET not deployed yet
  }
  console.log(`${tag} Dest CET balance before: ${ethers.formatUnits(dstBalanceBefore, decimals)}`);

  // Step 2: Build composed cross-chain transaction
  const sessionId = BigInt(Date.now());
  console.log(`\n${tag} Step 2: Building composed cross-chain transaction...`);
  console.log(`${tag} SessionId: ${sessionId.toString()}`);

  // -- Source tx: bridgeERC20To --
  const bridgeCalldata = encodeFunctionData({
    abi: ComposeL2ToL2BridgeABI,
    functionName: "bridgeERC20To",
    args: [
      BigInt(dest.chainId),
      tokenAddress as Hex,
      BigInt(BRIDGE_AMOUNT.toString()),
      walletAddress as Hex,
      sessionId,
    ],
  });

  // -- Dest tx: receiveTokens --
  const msgHeader = {
    chainSrc: BigInt(source.chainId),
    chainDest: BigInt(dest.chainId),
    sender: COMPOSE_L2_TO_L2_BRIDGE as Hex,
    receiver: walletAddress as Hex,
    sessionId: sessionId,
    label: "SEND_TOKENS",
  };
  const receiveCalldata = encodeFunctionData({
    abi: ComposeL2ToL2BridgeABI,
    functionName: "receiveTokens",
    args: [msgHeader],
  });

  // viem account + clients for signing
  const viemAccount = privateKeyToAccount(`0x${wallet_private_key}` as Hex);

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

  const srcWalletClient = createWalletClient({
    account: viemAccount,
    chain: srcChain,
    transport: http(source.l2Rpc),
  });
  const dstWalletClient = createWalletClient({
    account: viemAccount,
    chain: dstChain,
    transport: http(dest.l2Rpc),
  });
  const srcPublicClient = createPublicClient({
    chain: srcChain,
    transport: http(source.l2Rpc),
  });
  const dstPublicClient = createPublicClient({
    chain: dstChain,
    transport: http(dest.l2Rpc),
  });

  // Sign source tx (bridgeERC20To)
  console.log(`${tag} Signing source tx (bridgeERC20To)...`);
  const srcNonce = await srcPublicClient.getTransactionCount({ address: viemAccount.address });
  const srcFeeData = await srcPublicClient.estimateFeesPerGas();
  const srcSignedTx = await srcWalletClient.signTransaction({
    to: COMPOSE_L2_TO_L2_BRIDGE as Hex,
    data: bridgeCalldata,
    gas: 3_000_000n,
    maxFeePerGas: srcFeeData.maxFeePerGas!,
    maxPriorityFeePerGas: srcFeeData.maxPriorityFeePerGas!,
    nonce: srcNonce,
    chainId: source.chainId,
  });

  // Sign dest tx (receiveTokens)
  console.log(`${tag} Signing dest tx (receiveTokens)...`);
  const dstNonce = await dstPublicClient.getTransactionCount({ address: viemAccount.address });
  const dstFeeData = await dstPublicClient.estimateFeesPerGas();
  const dstSignedTx = await dstWalletClient.signTransaction({
    to: COMPOSE_L2_TO_L2_BRIDGE as Hex,
    data: receiveCalldata,
    gas: 5_000_000n, // higher — may deploy CET contract
    maxFeePerGas: dstFeeData.maxFeePerGas!,
    maxPriorityFeePerGas: dstFeeData.maxPriorityFeePerGas!,
    nonce: dstNonce,
    chainId: dest.chainId,
  });

  // Step 3: Submit cross-chain transaction
  console.log(`\n${tag} Step 3: Submitting cross-chain transaction...`);
  const xtResult = await submitXt(
    [
      { chainId: source.chainId, rawTx: srcSignedTx },
      { chainId: dest.chainId, rawTx: dstSignedTx },
    ],
    source.l2Rpc
  );
  console.log(`${tag} Submitted via ${xtResult.method}:`, JSON.stringify(xtResult.result));

  // Step 4: Poll for results and assert
  console.log(`\n${tag} Step 4: Polling for transaction results...`);

  const MAX_POLLS = 60;
  const POLL_INTERVAL_MS = 10_000;

  let srcConfirmed = false;
  let dstConfirmed = false;

  for (let i = 1; i <= MAX_POLLS; i++) {
    // Check source balance change
    if (!srcConfirmed) {
      try {
        const srcBalanceAfter: bigint = await token.balanceOf(walletAddress);
        const srcDecrease = BigInt(srcBalanceBefore) - BigInt(srcBalanceAfter);
        if (srcDecrease > 0n) {
          console.log(`\n${tag} Source balance decreased by: ${ethers.formatUnits(srcDecrease, decimals)}`);
          if (srcDecrease.toString() !== BRIDGE_AMOUNT.toString()) {
            throw new Error(
              `ASSERT FAILED: Source balance should have decreased by ${ethers.formatUnits(BRIDGE_AMOUNT, decimals)}, ` +
              `but decreased by ${ethers.formatUnits(srcDecrease, decimals)}`
            );
          }
          console.log(`${tag} ASSERT OK: Source balance decreased by expected amount`);
          srcConfirmed = true;
        }
      } catch (err) {
        if (err instanceof Error && err.message.startsWith("ASSERT FAILED")) throw err;
      }
    }

    // Check dest CET balance change
    if (!dstConfirmed) {
      try {
        const dstBalanceAfter = BigInt(await cetToken.balanceOf(walletAddress));
        const dstIncrease = dstBalanceAfter - dstBalanceBefore;
        if (dstIncrease > 0n) {
          console.log(`${tag} Dest CET balance: ${ethers.formatUnits(dstBalanceAfter, decimals)} ${symbol}`);
          console.log(`${tag} Dest CET increased by: ${ethers.formatUnits(dstIncrease, decimals)}`);

          // Assert CET deployed
          const cetCode = await dstEthersProvider.getCode(predictedCET);
          if (cetCode === "0x" || cetCode === "0x0") {
            throw new Error(`ASSERT FAILED: CET contract not deployed on ${dest.name}`);
          }
          console.log(`${tag} ASSERT OK: CET contract deployed on ${dest.name}`);

          if (dstIncrease.toString() !== BRIDGE_AMOUNT.toString()) {
            throw new Error(
              `ASSERT FAILED: Expected dest CET increase of ${ethers.formatUnits(BRIDGE_AMOUNT, decimals)}, ` +
              `got ${ethers.formatUnits(dstIncrease, decimals)}`
            );
          }
          console.log(`${tag} ASSERT OK: Dest CET balance increased by expected amount`);
          dstConfirmed = true;
        }
      } catch (err) {
        if (err instanceof Error && err.message.startsWith("ASSERT FAILED")) throw err;
      }
    }

    if (srcConfirmed && dstConfirmed) {
      console.log(`\n${tag} Bridge complete!`);
      console.log("  Tx hashes:");
      console.log(`    Mint:   ${mintReceipt!.hash}`);
      return;
    }

    process.stdout.write(`  Poll ${i}/${MAX_POLLS} — src:${srcConfirmed ? "OK" : "..."} dst:${dstConfirmed ? "OK" : "..."}...\r`);
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  console.log(`\n${tag} Timed out. src:${srcConfirmed ? "OK" : "PENDING"} dst:${dstConfirmed ? "OK" : "PENDING"}`);
  console.log(`  SessionId: ${sessionId.toString()}`);
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
async function main() {
  const sourceKey = getArg("--source")?.toLowerCase();
  const destKey = getArg("--dest")?.toLowerCase();

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

  const source = ROLLUP_CONFIGS[sourceKey];
  const dest = ROLLUP_CONFIGS[destKey];
  const tag = `[${source.name}->${dest.name}]`;

  console.log(`${tag} Setting up...`);

  // For first run, we still use ethers for deploy/approve (regular txs)
  const srcEthersProvider = new ethers.JsonRpcProvider(source.l2Rpc);
  const dstEthersProvider = new ethers.JsonRpcProvider(dest.l2Rpc);
  const srcEthersWallet = new ethers.Wallet(wallet_private_key, srcEthersProvider);

  console.log(`${tag} Wallet: ${srcEthersWallet.address}`);

  let state = loadState(sourceKey, destKey);

  if (!state) {
    console.log(`${tag} No state file found — first run: deploying token + approving bridge.`);
    state = await firstRun(source, dest, sourceKey, destKey, srcEthersWallet, dstEthersProvider);
    console.log(`\n${tag} First run complete. Run again to mint and bridge.`);
  } else {
    console.log(`${tag} State loaded — token: ${state.tokenAddress}`);
    await bridgeRun(state, source, dest, sourceKey, destKey);
  }
}

main().catch((err) => {
  console.error("\n[L2->L2] FATAL:", err.message || err);
  process.exit(1);
});
