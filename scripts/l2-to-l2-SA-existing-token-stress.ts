import { ethers } from "ethers";
import * as fs from "fs";
import * as path from "path";
import {
  encodeFunctionData,
  http,
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
  L1_RPC,
  wallet_private_key,
  COMPOSE_L1_BRIDGE_ROLLUP_A,
  COMPOSE_L1_BRIDGE_ROLLUP_B,
  COMPOSE_L2_TO_L2_BRIDGE,
  CET_FACTORY,
} from "../config";
import { composeOps } from "./sa-compose-helper";
import ComposeL1BridgeABI from "../sepolia-prod/L1/abis/ComposeL1Bridge.json";
import ComposeL2ToL2BridgeABI from "../sepolia-prod/L2/abis/ComposeL2ToL2Bridge.json";
import CETFactoryABI from "../sepolia-prod/L2/abis/CETFactory.json";

const L1_BRIDGE: Record<string, string> = {
  a: COMPOSE_L1_BRIDGE_ROLLUP_A,
  b: COMPOSE_L1_BRIDGE_ROLLUP_B,
};

const ERC20_ABI = [
  "function name() view returns (string)",
  "function symbol() view returns (string)",
  "function decimals() view returns (uint8)",
  "function balanceOf(address) view returns (uint256)",
  "function approve(address spender, uint256 amount) returns (bool)",
  "function transfer(address to, uint256 amount) returns (bool)",
  "function mint(address to, uint256 amount)",
];

const MINTABLE_TOKEN_ABI = [
  "constructor(string name, string symbol, uint8 decimals_)",
  ...ERC20_ABI,
];

const MINTABLE_TOKEN_BYTECODE =
  "0x608060405234801562000010575f80fd5b5060405162000a8f38038062000a8f83398101604081905262000033916200012b565b5f62000040848262000236565b5060016200004f838262000236565b506002805460ff191660ff9290921691909117905550620002fe9050565b634e487b7160e01b5f52604160045260245ffd5b5f82601f83011262000091575f80fd5b81516001600160401b0380821115620000ae57620000ae6200006d565b604051601f8301601f19908116603f01168101908282118183101715620000d957620000d96200006d565b81604052838152602092508683858801011115620000f5575f80fd5b5f91505b83821015620001185785820183015181830184015290820190620000f9565b5f93810190920192909252949350505050565b5f805f606084860312156200013e575f80fd5b83516001600160401b038082111562000155575f80fd5b620001638783880162000081565b9450602086015191508082111562000179575f80fd5b50620001888682870162000081565b925050604084015160ff811681146200019f575f80fd5b809150509250925092565b600181811c90821680620001bf57607f821691505b602082108103620001de57634e487b7160e01b5f52602260045260245ffd5b50919050565b601f82111562000231575f81815260208120601f850160051c810160208610156200020c5750805b601f850160051c820191505b818110156200022d5782815560010162000218565b5050505b505050565b81516001600160401b038111156200025257620002526200006d565b6200026a81620002638454620001aa565b84620001e4565b602080601f831160018114620002a0575f8415620002885750858301515b5f19600386901b1c1916600185901b1785556200022d565b5f85815260208120601f198616915b82811015620002d057888601518255948401946001909101908401620002af565b5085821015620002ee57878501515f19600388901b60f8161c191681555b5050505050600190811b01905550565b610783806200030c5f395ff3fe608060405234801561000f575f80fd5b506004361061009b575f3560e01c806340c10f191161006357806340c10f191461012957806370a082311461013e57806395d89b411461015d578063a9059cbb14610165578063dd62ed3e14610178575f80fd5b806306fdde031461009f578063095ea7b3146100bd57806318160ddd146100e057806323b872dd146100f7578063313ce5671461010a575b5f80fd5b6100a76101a2565b6040516100b491906105c3565b60405180910390f35b6100d06100cb366004610629565b61022d565b60405190151581526020016100b4565b6100e960035481565b6040519081526020016100b4565b6100d0610105366004610651565b610299565b6002546101179060ff1681565b60405160ff90911681526020016100b4565b61013c610137366004610629565b61044f565b005b6100e961014c36600461068a565b60046020525f908152604090205481565b6100a76104d5565b6100d0610173366004610629565b6104e2565b6100e96101863660046106aa565b600560209081525f928352604080842090915290825290205481565b5f80546101ae906106db565b80601f01602080910402602001604051908101604052809291908181526020018280546101da906106db565b80156102255780601f106101fc57610100808354040283529160200191610225565b820191905f5260205f20905b81548152906001019060200180831161020857829003601f168201915b505050505081565b335f8181526005602090815260408083206001600160a01b038716808552925280832085905551919290917f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925906102879086815260200190565b60405180910390a35060015b92915050565b6001600160a01b0383165f9081526005602090815260408083203384529091528120548211156103095760405162461bcd60e51b8152602060048201526016602482015275696e73756666696369656e7420616c6c6f77616e636560501b60448201526064015b60405180910390fd5b6001600160a01b0384165f908152600460205260409020548211156103675760405162461bcd60e51b8152602060048201526014602482015273696e73756666696369656e742062616c616e636560601b6044820152606401610300565b6001600160a01b0384165f90815260056020908152604080832033845290915281208054849290610399908490610727565b90915550506001600160a01b0384165f90815260046020526040812080548492906103c5908490610727565b90915550506001600160a01b0383165f90815260046020526040812080548492906103f190849061073a565b92505081905550826001600160a01b0316846001600160a01b03167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef8460405161043d91815260200190565b60405180910390a35060019392505050565b8060035f828254610460919061073a565b90915550506001600160a01b0382165f908152600460205260408120805483929061048c90849061073a565b90915550506040518181526001600160a01b038316905f907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9060200160405180910390a35050565b600180546101ae906106db565b335f908152600460205260408120548211156105375760405162461bcd60e51b8152602060048201526014602482015273696e73756666696369656e742062616c616e636560601b6044820152606401610300565b335f9081526004602052604081208054849290610555908490610727565b90915550506001600160a01b0383165f908152600460205260408120805484929061058190849061073a565b90915550506040518281526001600160a01b0384169033907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef90602001610287565b5f6020808352835180828501525f5b818110156105ee578581018301518582016040015282016105d2565b505f604082860101526040601f19601f8301168501019250505092915050565b80356001600160a01b0381168114610624575f80fd5b919050565b5f806040838503121561063a575f80fd5b6106438361060e565b946020939093013593505050565b5f805f60608486031215610663575f80fd5b61066c8461060e565b925061067a6020850161060e565b9150604084013590509250925092565b5f6020828403121561069a575f80fd5b6106a38261060e565b9392505050565b5f80604083850312156106bb575f80fd5b6106c48361060e565b91506106d26020840161060e565b90509250929050565b600181811c908216806106ef57607f821691505b60208210810361070d57634e487b7160e01b5f52602260045260245ffd5b50919050565b634e487b7160e01b5f52601160045260245ffd5b8181038181111561029357610293610713565b808201808211156102935761029361071356fea26469706673582212206f24f4f523f3527fa0861762239b948bb5596cf1743604001ae31f92dc47042f64736f6c63430008140033";

const TOKEN_AMOUNT = ethers.parseUnits("100", 18);
const ENTRYPOINT_ADDRESS = "0x0000000071727De22E5E9d8BAf0edAc6f37da032";
const ENTRYPOINT_ABI = [
  "function balanceOf(address) view returns (uint256)",
  "function depositTo(address) payable",
];
const MIN_ENTRYPOINT_DEPOSIT = ethers.parseEther("0.1");
const FUND_AMOUNT = ethers.parseEther("0.15");

// ---------------------------------------------------------------------------
// Rollup config + CLI
// ---------------------------------------------------------------------------
const ROLLUP_CONFIGS: Record<string, typeof rollupA | typeof rollupB> = { a: rollupA, b: rollupB };

function getArg(flag: string): string | undefined {
  const idx = process.argv.indexOf(flag);
  return idx !== -1 && idx + 1 < process.argv.length ? process.argv[idx + 1] : undefined;
}

function hasFlag(flag: string): boolean {
  return process.argv.includes(flag);
}

function printUsage(): never {
  console.error(`
Usage: npx ts-node scripts/l2-to-l2-SA-existing-token-stress.ts --source <a|b> --dest <a|b> [--num-acc <N>] [--same-smart-acc]

  --source          Source rollup: a or b
  --dest            Destination rollup: a or b
  --num-acc         Number of accounts (default: 100)
  --same-smart-acc  All accounts share one smart account (sequential, tests single-user throughput)

Example:
  npx ts-node scripts/l2-to-l2-SA-existing-token-stress.ts --source a --dest b --num-acc 5
  npx ts-node scripts/l2-to-l2-SA-existing-token-stress.ts --source a --dest b --num-acc 25 --same-smart-acc
  `);
  process.exit(1);
}

function elapsed(start: number): string {
  return ((Date.now() - start) / 1000).toFixed(1) + "s";
}

// ---------------------------------------------------------------------------
// State file
// ---------------------------------------------------------------------------
interface State {
  tokenAddress: string;
  predictedCET: string;
}

function stateFilePath(sourceKey: string, destKey: string): string {
  return path.join(__dirname, `.l2-to-l2-SA-existing-token-stress-state-${sourceKey}-${destKey}`);
}

function loadState(s: string, d: string): State | null {
  try { return JSON.parse(fs.readFileSync(stateFilePath(s, d), "utf-8")); } catch { return null; }
}

function saveState(s: string, d: string, state: State) {
  fs.writeFileSync(stateFilePath(s, d), JSON.stringify(state, null, 2) + "\n");
}

// ---------------------------------------------------------------------------
// Derive deterministic signers
// ---------------------------------------------------------------------------
function deriveSigners(masterKey: string, count: number) {
  const signers = [];
  for (let i = 0; i < count; i++) {
    const pk = ethers.keccak256(
      ethers.solidityPacked(["bytes32", "string", "uint256"], [`0x${masterKey}`, "sa-token-stress", i])
    );
    signers.push(privateKeyToAccount(pk as Hex));
  }
  return signers;
}

async function main() {
  const sourceKey = getArg("--source")?.toLowerCase();
  const destKey = getArg("--dest")?.toLowerCase();
  const numAcc = parseInt(getArg("--num-acc") || "100", 10);

  if (!sourceKey || !ROLLUP_CONFIGS[sourceKey]) { console.error("Error: --source must be 'a' or 'b'"); printUsage(); }
  if (!destKey || !ROLLUP_CONFIGS[destKey]) { console.error("Error: --dest must be 'a' or 'b'"); printUsage(); }
  if (sourceKey === destKey) { console.error("Error: --source and --dest must be different"); printUsage(); }

  const sourceChain = ROLLUP_CONFIGS[sourceKey];
  const destChain = ROLLUP_CONFIGS[destKey];
  const sameSmartAcc = hasFlag("--same-smart-acc");
  const tag = `[SA-STRESS:${sourceChain.name}->${destChain.name}]`;
  const totalStart = Date.now();

  console.log(`${tag} Stress test: ${numAcc} accounts, bridge 100 tokens each`);
  console.log(`${tag} Mode: ${sameSmartAcc ? "SAME smart account (single-user sequential)" : "N smart accounts (multi-user parallel)"}`);

  // Setup
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

  const srcRpc = sourceChain.rpcUrls.default.http[0];
  const dstRpc = destChain.rpcUrls.default.http[0];
  const srcEthersProvider = new ethers.JsonRpcProvider(srcRpc);
  const dstEthersProvider = new ethers.JsonRpcProvider(dstRpc);
  const funderWallet = new ethers.Wallet(wallet_private_key, srcEthersProvider);
  const funderEOA = privateKeyToAccount(`0x${wallet_private_key}` as Hex);

  console.log(`${tag} Funder EOA: ${funderEOA.address}`);

  // =========================================================================
  // Step 1: Generate signers + smart accounts
  // =========================================================================
  const stepStart = Date.now();

  const srcSAs: Awaited<ReturnType<typeof createSmartAccount>>[] = [];
  const dstSAs: Awaited<ReturnType<typeof createSmartAccount>>[] = [];
  let signers: ReturnType<typeof deriveSigners>;
  let saAddresses: string[];

  if (sameSmartAcc) {
    // Single smart account mode: one SA (from funder EOA) used for all operations
    console.log(`\n${tag} Step 1: Creating 1 shared smart account...`);
    signers = [funderEOA]; // just the funder signer, reused for all

    const srcSA = await createSmartAccount(
      { signer: funderEOA, chainId: sourceChain.id, multiChainIds: [rollupA.id, rollupB.id] },
      composeConfig as any
    );
    const dstSA = await createSmartAccount(
      { signer: funderEOA, chainId: destChain.id, multiChainIds: [rollupA.id, rollupB.id] },
      composeConfig as any
    );

    // Reuse the same SA for all N accounts
    for (let i = 0; i < numAcc; i++) {
      srcSAs.push(srcSA);
      dstSAs.push(dstSA);
    }
    // All "accounts" share the same signer and SA
    signers = Array.from({ length: numAcc }, () => funderEOA) as any;
    saAddresses = Array.from({ length: numAcc }, () => srcSA.account.address);

    console.log(`${tag} Shared smart account: ${srcSA.account.address}`);
  } else {
    // Multi smart account mode: one SA per signer
    console.log(`\n${tag} Step 1: Generating ${numAcc} signers + smart accounts...`);
    signers = deriveSigners(wallet_private_key, numAcc);

    for (let i = 0; i < numAcc; i++) {
      const srcSA = await createSmartAccount(
        { signer: signers[i], chainId: sourceChain.id, multiChainIds: [rollupA.id, rollupB.id] },
        composeConfig as any
      );
      const dstSA = await createSmartAccount(
        { signer: signers[i], chainId: destChain.id, multiChainIds: [rollupA.id, rollupB.id] },
        composeConfig as any
      );
      srcSAs.push(srcSA);
      dstSAs.push(dstSA);
      if ((i + 1) % 10 === 0 || i === numAcc - 1) {
        process.stdout.write(`  Created ${i + 1}/${numAcc} smart accounts...\r`);
      }
    }
    saAddresses = srcSAs.map((sa) => sa.account.address);
  }

  console.log(`\n${tag} Smart accounts ready in ${elapsed(stepStart)}`);
  console.log(`${tag} EOA → Kernel Smart Account mapping:`);
  const uniqueMapping = new Map<string, string>();
  for (let i = 0; i < numAcc; i++) {
    const eoa = signers[i].address;
    const sa = saAddresses[i];
    if (!uniqueMapping.has(eoa)) uniqueMapping.set(eoa, sa);
  }
  for (const [eoa, sa] of uniqueMapping) {
    console.log(`  ${eoa} → ${sa}`);
  }

  // =========================================================================
  // Step 1.5: Ensure funder has enough ETH on both L2 chains
  // =========================================================================
  console.log(`\n${tag} Step 1.5: Checking funder ETH on L2 chains...`);

  // Total needed per chain: numUniqueAddrs * FUND_AMOUNT + buffer for minting gas
  const uniqueSACount = new Set(saAddresses).size;
  const neededPerChain = FUND_AMOUNT * BigInt(uniqueSACount) + ethers.parseEther("1"); // extra 1 ETH buffer

  for (const [label, rpc, key] of [
    [sourceChain.name, srcRpc, sourceKey],
    [destChain.name, dstRpc, destKey],
  ] as const) {
    const provider = new ethers.JsonRpcProvider(rpc);
    const balance = await provider.getBalance(funderEOA.address);
    console.log(`${tag} ${label}: funder has ${ethers.formatEther(balance)} ETH, needs ~${ethers.formatEther(neededPerChain)} ETH`);

    if (balance < neededPerChain) {
      const deficit = neededPerChain - balance;
      console.log(`${tag} ${label}: bridging ${ethers.formatEther(deficit)} ETH from L1...`);

      const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
      const l1Wallet = new ethers.Wallet(wallet_private_key, l1Provider);
      const l1Bridge = new ethers.Contract(L1_BRIDGE[key], ComposeL1BridgeABI, l1Wallet);

      const bridgeTx = await l1Bridge.bridgeETHTo(
        funderEOA.address,
        100_000, // _minGasLimit — simple ETH deposit
        "0x",
        { value: deficit }
      );
      const receipt = await bridgeTx.wait();
      console.log(`${tag} ${label}: L1 bridge tx: ${receipt!.hash}`);

      // Poll for ETH arrival on L2
      console.log(`${tag} ${label}: waiting for ETH to arrive on L2...`);
      for (let poll = 0; poll < 60; poll++) {
        const newBal = await provider.getBalance(funderEOA.address);
        if (newBal >= neededPerChain) {
          console.log(`${tag} ${label}: funder now has ${ethers.formatEther(newBal)} ETH`);
          break;
        }
        if (poll === 59) {
          console.log(`${tag} ${label}: timed out waiting for ETH — continuing anyway (current: ${ethers.formatEther(newBal)} ETH)`);
        }
        await new Promise((r) => setTimeout(r, 10_000));
      }
    }
  }

  // =========================================================================
  // Step 2: Fund EntryPoint deposits on both chains
  // =========================================================================
  console.log(`\n${tag} Step 2: Funding EntryPoint deposits...`);
  const fundStart = Date.now();

  for (const [label, rpc] of [[sourceChain.name, srcRpc], [destChain.name, dstRpc]] as const) {
    const provider = new ethers.JsonRpcProvider(rpc);
    const wallet = new ethers.Wallet(wallet_private_key, provider);
    const ep = new ethers.Contract(ENTRYPOINT_ADDRESS, ENTRYPOINT_ABI, wallet);
    const chainId = (await provider.getNetwork()).chainId;

    // Check which unique SAs need funding
    const uniqueSAAddresses = [...new Set(saAddresses)];
    const deposits = await Promise.all(uniqueSAAddresses.map((addr) => ep.balanceOf(addr)));
    const needsFundingAddrs: string[] = [];
    for (let i = 0; i < uniqueSAAddresses.length; i++) {
      if (BigInt(deposits[i]) < MIN_ENTRYPOINT_DEPOSIT) needsFundingAddrs.push(uniqueSAAddresses[i]);
    }

    if (needsFundingAddrs.length === 0) {
      console.log(`${tag} ${label}: all ${uniqueSAAddresses.length} unique SAs already funded`);
      continue;
    }

    console.log(`${tag} ${label}: funding ${needsFundingAddrs.length} SAs...`);
    const feeData = await provider.getFeeData();
    const baseNonce = await provider.getTransactionCount(wallet.address, "pending");

    const signedTxs: string[] = [];
    for (let j = 0; j < needsFundingAddrs.length; j++) {
      const data = ep.interface.encodeFunctionData("depositTo", [needsFundingAddrs[j]]);
      const signed = await wallet.signTransaction({
        to: ENTRYPOINT_ADDRESS, data, value: FUND_AMOUNT,
        nonce: baseNonce + j, gasLimit: 100_000n,
        maxFeePerGas: feeData.maxFeePerGas!, maxPriorityFeePerGas: feeData.maxPriorityFeePerGas!,
        chainId, type: 2,
      });
      signedTxs.push(signed);
    }

    const responses = await Promise.all(signedTxs.map((s) => provider.broadcastTransaction(s)));
    const receipts = await Promise.all(responses.map((r) => r.wait()));
    const failed = receipts.filter((r) => r!.status !== 1);
    if (failed.length > 0) throw new Error(`${failed.length} EntryPoint funding txs failed on ${label}`);
    console.log(`${tag} ${label}: funded ${needsFundingAddrs.length} SAs`);
  }
  console.log(`${tag} Funding took ${elapsed(fundStart)}`);

  // =========================================================================
  // Step 3: Deploy token / load state
  // =========================================================================
  let state = loadState(sourceKey, destKey);
  let tokenAddress: string;
  let predictedCET: string;

  if (!state) {
    console.log(`\n${tag} Step 3: Deploying StressTokenSA (STSA) on ${sourceChain.name}...`);
    const tokenFactory = new ethers.ContractFactory(MINTABLE_TOKEN_ABI, MINTABLE_TOKEN_BYTECODE, funderWallet);
    const deployTx = await tokenFactory.deploy("StressTokenSA", "STSA", 18);
    const deployment = await deployTx.waitForDeployment();
    tokenAddress = await deployment.getAddress();

    const cetFactory = new ethers.Contract(CET_FACTORY, CETFactoryABI, dstEthersProvider);
    predictedCET = await cetFactory.predictAddress(tokenAddress, sourceChain.id);

    state = { tokenAddress, predictedCET };
    saveState(sourceKey, destKey, state);
    console.log(`${tag} Token: ${tokenAddress}, CET: ${predictedCET}`);
  } else {
    tokenAddress = state.tokenAddress;
    predictedCET = state.predictedCET;
    console.log(`\n${tag} Step 3: Loaded state — token: ${tokenAddress}`);
  }

  // =========================================================================
  // Step 4: Mint tokens to all smart accounts
  // =========================================================================
  console.log(`\n${tag} Step 4: Minting 100 STSA to each smart account...`);
  const mintStart = Date.now();

  const mintIface = new ethers.Interface(ERC20_ABI);
  const mintFeeData = await srcEthersProvider.getFeeData();
  const mintBaseNonce = await srcEthersProvider.getTransactionCount(funderWallet.address, "pending");
  const chainId = (await srcEthersProvider.getNetwork()).chainId;

  const signedMints: string[] = [];
  for (let i = 0; i < numAcc; i++) {
    const data = mintIface.encodeFunctionData("mint", [saAddresses[i], TOKEN_AMOUNT]);
    const signed = await funderWallet.signTransaction({
      to: tokenAddress, data, nonce: mintBaseNonce + i, gasLimit: 100_000n,
      maxFeePerGas: mintFeeData.maxFeePerGas!, maxPriorityFeePerGas: mintFeeData.maxPriorityFeePerGas!,
      chainId, type: 2,
    });
    signedMints.push(signed);
  }

  const mintResponses = await Promise.all(signedMints.map((s) => srcEthersProvider.broadcastTransaction(s)));
  const mintReceipts = await Promise.all(mintResponses.map((r) => r.wait()));
  const mintFailed = mintReceipts.filter((r) => r!.status !== 1);
  if (mintFailed.length > 0) throw new Error(`${mintFailed.length} mint txs failed`);
  console.log(`${tag} Minted in ${elapsed(mintStart)}`);

  // =========================================================================
  // Step 5: Compose + submit all UserOps in parallel
  // =========================================================================
  console.log(`\n${tag} Step 5: Creating and submitting ${numAcc} composed UserOps...`);
  const bridgeStart = Date.now();

  const token = new ethers.Contract(tokenAddress, ERC20_ABI, srcEthersProvider);
  const tokenName: string = await token.name();
  const tokenSymbol: string = await token.symbol();
  const tokenDecimals: number = await token.decimals();

  // Snapshot balances before
  const srcBalancesBefore = await Promise.all(
    saAddresses.map((addr) => token.balanceOf(addr).then((b: any) => BigInt(b)))
  );

  const cetToken = new ethers.Contract(predictedCET, ERC20_ABI, dstEthersProvider);
  let dstBalancesBefore: bigint[] = new Array(numAcc).fill(0n);
  try {
    const bals = await Promise.all(
      signers.map((s) => cetToken.balanceOf(s.address).then((b: any) => BigInt(b)))
    );
    dstBalancesBefore = bals;
  } catch {}

  // Build all calls (pure encoding, no RPC)
  const baseSessionId = BigInt(Date.now());

  type AccountCalls = { sourceCalls: UserOPCall[]; destCalls: UserOPCall[] };
  const allCalls: AccountCalls[] = [];

  for (let i = 0; i < numAcc; i++) {
    const sessionId = baseSessionId + BigInt(i);

    const approveCalldata = encodeFunctionData({
      abi: [{ type: "function", name: "approve", inputs: [{ name: "spender", type: "address" }, { name: "amount", type: "uint256" }], outputs: [{ type: "bool" }], stateMutability: "nonpayable" }],
      functionName: "approve",
      args: [COMPOSE_L2_TO_L2_BRIDGE as Hex, BigInt(TOKEN_AMOUNT.toString())],
    });
    const bridgeCalldata = encodeFunctionData({
      abi: ComposeL2ToL2BridgeABI,
      functionName: "bridgeERC20To",
      args: [BigInt(destChain.id), tokenAddress as Hex, BigInt(TOKEN_AMOUNT.toString()), saAddresses[i] as Hex, sessionId],
    });
    const sourceCalls: UserOPCall[] = [
      { to: tokenAddress as Hex, value: 0n, data: approveCalldata },
      { to: COMPOSE_L2_TO_L2_BRIDGE as Hex, value: 0n, data: bridgeCalldata },
    ];

    const msgHeader = {
      chainSrc: BigInt(sourceChain.id),
      chainDest: BigInt(destChain.id),
      sender: COMPOSE_L2_TO_L2_BRIDGE as Hex,
      receiver: saAddresses[i] as Hex,
      sessionId,
      label: "SEND_TOKENS",
    };
    const receiveCalldata = encodeFunctionData({
      abi: ComposeL2ToL2BridgeABI,
      functionName: "receiveTokens",
      args: [msgHeader],
    });
    const transferCalldata = encodeFunctionData({
      abi: [{ type: "function", name: "transfer", inputs: [{ name: "to", type: "address" }, { name: "amount", type: "uint256" }], outputs: [{ type: "bool" }], stateMutability: "nonpayable" }],
      functionName: "transfer",
      args: [signers[i].address as Hex, BigInt(TOKEN_AMOUNT.toString())],
    });
    const destCalls: UserOPCall[] = [
      { to: COMPOSE_L2_TO_L2_BRIDGE as Hex, value: 0n, data: receiveCalldata },
      { to: predictedCET as Hex, value: 0n, data: transferCalldata },
    ];

    allCalls.push({ sourceCalls, destCalls });
  }

  const originalWarn = console.warn;
  console.warn = () => {};

  const sendResults: Array<{ hashes: Hex[]; wait: () => Promise<any[]> } | null> = [];

  if (sameSmartAcc) {
    // Sequential mode: must wait for each tx (same SA nonce)
    for (let i = 0; i < numAcc; i++) {
      try {
        const srcUserOp = await srcSAs[i].account.createUserOp(allCalls[i].sourceCalls);
        const dstUserOp = await dstSAs[i].account.createUserOp(allCalls[i].destCalls);
        srcUserOp.userOp.callGasLimit = 3_000_000n;
        dstUserOp.userOp.callGasLimit = 5_000_000n;
        dstUserOp.userOp.verificationGasLimit = 3_500_000n;

        const composed = await composeOps([srcUserOp, dstUserOp]);
        const result = await composed.send();
        sendResults.push(result);

        try {
          const receipts = await result.wait();
          const allSuccess = receipts.every((r: any) => r.status === "success");
          if (!allSuccess) console.error(`  Account ${i}: receipt status not success`);
        } catch (err: any) {
          console.error(`  Account ${i}: wait failed — ${err.message?.slice(0, 100)}`);
        }
      } catch (err: any) {
        console.error(`  Account ${i} failed: ${err.message?.slice(0, 100)}`);
        sendResults.push(null);
      }
      if ((i + 1) % 5 === 0 || i === numAcc - 1) {
        process.stdout.write(`  Submitted ${i + 1}/${numAcc} composed txs...\r`);
      }
    }
  } else {
    // Parallel mode: create all UserOps, compose all, send all at once
    console.log(`${tag} Creating ${numAcc} UserOps in parallel...`);
    const userOpPairs = await Promise.all(
      allCalls.map(async (calls, i) => {
        try {
          const srcUserOp = await srcSAs[i].account.createUserOp(calls.sourceCalls);
          const dstUserOp = await dstSAs[i].account.createUserOp(calls.destCalls);
          srcUserOp.userOp.callGasLimit = 3_000_000n;
          dstUserOp.userOp.callGasLimit = 5_000_000n;
          dstUserOp.userOp.verificationGasLimit = 3_500_000n;
          return { srcUserOp, dstUserOp, index: i };
        } catch (err: any) {
          console.error(`  Account ${i} createUserOp failed: ${err.message?.slice(0, 100)}`);
          return null;
        }
      })
    );
    console.log(`${tag} UserOps created. Composing and sending...`);

    const composedResults = await Promise.all(
      userOpPairs.map(async (pair) => {
        if (!pair) return null;
        try {
          const composed = await composeOps([pair.srcUserOp, pair.dstUserOp]);
          const result = await composed.send();
          return result;
        } catch (err: any) {
          console.error(`  Account ${pair.index} compose/send failed: ${err.message?.slice(0, 100)}`);
          return null;
        }
      })
    );

    for (const r of composedResults) {
      sendResults.push(r);
    }
  }

  console.warn = originalWarn;
  console.log(`\n${tag} All ${numAcc} composed txs submitted in ${elapsed(bridgeStart)}`);

  // =========================================================================
  // Step 6: Wait for receipts + print tx hashes
  // =========================================================================
  console.log(`\n${tag} Step 6: Collecting results...`);
  const receiptStart = Date.now();

  const succeeded: number[] = [];
  const failed: number[] = [];

  for (let i = 0; i < numAcc; i++) {
    if (!sendResults[i]) {
      failed.push(i);
      continue;
    }
    try {
      const hashes = sendResults[i]!.hashes;

      if (sameSmartAcc) {
        // Already waited in step 5 — just check if it was submitted
        succeeded.push(i);
        console.log(`  Account ${i} (${signers[i].address.slice(0, 12)}...): source tx=${hashes[0]}, dest tx=${hashes[1]}`);
      } else {
        // Parallel mode: wait for receipts now
        const receipts = await sendResults[i]!.wait();
        const allSuccess = receipts.every((r: any) => r.status === "success");
        if (allSuccess) {
          succeeded.push(i);
          console.log(`  Account ${i} (${signers[i].address.slice(0, 12)}...): source tx=${hashes[0]}, dest tx=${hashes[1]}`);
        } else {
          failed.push(i);
          console.error(`  Account ${i} (${signers[i].address.slice(0, 12)}...): FAILED — source tx=${hashes[0]}, dest tx=${hashes[1]}`);
        }
      }
    } catch (err: any) {
      failed.push(i);
      const hashes = sendResults[i]?.hashes;
      console.error(`  Account ${i} (${signers[i].address.slice(0, 12)}...): wait failed — ${err.message?.slice(0, 80)}${hashes ? ` (source tx=${hashes[0]})` : ""}`);
    }
    if ((i + 1) % 10 === 0 || i === numAcc - 1) {
      process.stdout.write(`  Processed ${i + 1}/${numAcc} receipts (${succeeded.length} ok, ${failed.length} fail)...\r`);
    }
  }

  console.log(`\n${tag} ${succeeded.length}/${numAcc} succeeded, ${failed.length} failed in ${elapsed(receiptStart)}`);

  // =========================================================================
  // Step 7: Assert balances
  // =========================================================================
  console.log(`\n${tag} Step 7: Asserting balances...`);

  // Dest: CET deployed
  const cetCode = await dstEthersProvider.getCode(predictedCET);
  if (cetCode === "0x" || cetCode === "0x0") {
    console.error(`${tag} ASSERT FAILED: CET not deployed on ${destChain.name}`);
  } else {
    console.log(`${tag} ASSERT OK: CET deployed on ${destChain.name}`);
  }

  if (sameSmartAcc) {
    // Same-SA mode: check aggregate balances
    const saAddr = saAddresses[0];
    const eoaAddr = signers[0].address;
    const expectedDecrease = TOKEN_AMOUNT * BigInt(succeeded.length);
    const expectedIncrease = TOKEN_AMOUNT * BigInt(succeeded.length);

    const srcBalAfter: bigint = BigInt(await token.balanceOf(saAddr));
    const srcDecrease = srcBalancesBefore[0] - srcBalAfter;
    console.log(`${tag} Source SA balance: ${ethers.formatUnits(srcBalancesBefore[0], 18)} -> ${ethers.formatUnits(srcBalAfter, 18)} (decreased ${ethers.formatUnits(srcDecrease, 18)})`);
    if (srcDecrease.toString() !== expectedDecrease.toString()) {
      console.error(`${tag} ASSERT FAILED: Expected source decrease ${ethers.formatUnits(expectedDecrease, 18)}, got ${ethers.formatUnits(srcDecrease, 18)}`);
    } else {
      console.log(`${tag} ASSERT OK: Source SA decreased by ${ethers.formatUnits(expectedDecrease, 18)} (${succeeded.length} × 100)`);
    }

    const dstBalAfter: bigint = BigInt(await cetToken.balanceOf(eoaAddr));
    const dstIncrease = dstBalAfter - dstBalancesBefore[0];
    console.log(`${tag} Dest EOA CET: ${ethers.formatUnits(dstBalancesBefore[0], 18)} -> ${ethers.formatUnits(dstBalAfter, 18)} (increased ${ethers.formatUnits(dstIncrease, 18)})`);
    if (dstIncrease.toString() !== expectedIncrease.toString()) {
      console.error(`${tag} ASSERT FAILED: Expected dest increase ${ethers.formatUnits(expectedIncrease, 18)}, got ${ethers.formatUnits(dstIncrease, 18)}`);
    } else {
      console.log(`${tag} ASSERT OK: Dest EOA received ${ethers.formatUnits(expectedIncrease, 18)} CET (${succeeded.length} × 100)`);
    }
  } else {
    // Multi-SA mode: check per-account balances
    const srcBalancesAfter = await Promise.all(
      succeeded.map((i) => token.balanceOf(saAddresses[i]).then((b: any) => BigInt(b)))
    );
    let srcMismatch = 0;
    for (let j = 0; j < succeeded.length; j++) {
      const i = succeeded[j];
      const decrease = srcBalancesBefore[i] - srcBalancesAfter[j];
      if (decrease.toString() !== TOKEN_AMOUNT.toString()) {
        srcMismatch++;
        if (srcMismatch <= 3) console.error(`  Account ${i}: src decrease ${ethers.formatUnits(decrease, 18)} != 100`);
      }
    }
    if (srcMismatch > 0) {
      console.error(`${tag} ASSERT FAILED: ${srcMismatch} accounts have wrong source balance`);
    } else {
      console.log(`${tag} ASSERT OK: All ${succeeded.length} source balances decreased correctly`);
    }

    const dstBalancesAfter = await Promise.all(
      succeeded.map((i) => cetToken.balanceOf(signers[i].address).then((b: any) => BigInt(b)))
    );
    let dstMismatch = 0;
    for (let j = 0; j < succeeded.length; j++) {
      const i = succeeded[j];
      const increase = dstBalancesAfter[j] - dstBalancesBefore[i];
      if (increase.toString() !== TOKEN_AMOUNT.toString()) {
        dstMismatch++;
        if (dstMismatch <= 3) console.error(`  Account ${i} EOA (${signers[i].address.slice(0, 12)}): dest increase ${ethers.formatUnits(increase, 18)} != 100`);
      }
    }
    if (dstMismatch > 0) {
      console.error(`${tag} ASSERT FAILED: ${dstMismatch} EOA accounts have wrong dest CET balance`);
    } else {
      console.log(`${tag} ASSERT OK: All ${succeeded.length} EOA accounts received 100 CET on ${destChain.name}`);
    }
  }

  // =========================================================================
  // Summary
  // =========================================================================
  console.log(`\n${"=".repeat(60)}`);
  console.log(`${tag} SA STRESS TEST COMPLETE`);
  console.log(`${"=".repeat(60)}`);
  console.log(`  Token:       ${tokenAddress}`);
  console.log(`  CET:         ${predictedCET}`);
  console.log(`  Accounts:    ${numAcc}`);
  console.log(`  Succeeded:   ${succeeded.length}/${numAcc}`);
  console.log(`  Failed:      ${failed.length}/${numAcc}`);
  console.log(`  Total time:  ${elapsed(totalStart)}`);
}

main().catch((err) => {
  console.error("\n[SA-STRESS] FATAL:", err.message || err);
  process.exit(1);
});
