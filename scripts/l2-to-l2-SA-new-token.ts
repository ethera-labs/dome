import { ethers } from "ethers";
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
  wallet_private_key,
  COMPOSE_L2_TO_L2_BRIDGE,
  CET_FACTORY,
} from "../config";
import { composeOps } from "./sa-compose-helper";
import ComposeL2ToL2BridgeABI from "../sepolia-prod/L2/abis/ComposeL2ToL2Bridge.json";
import CETFactoryABI from "../sepolia-prod/L2/abis/CETFactory.json";

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

const BRIDGE_AMOUNT = ethers.parseUnits("100", 18);

const ENTRYPOINT_ADDRESS = "0x0000000071727De22E5E9d8BAf0edAc6f37da032";
const ENTRYPOINT_ABI = [
  "function balanceOf(address) view returns (uint256)",
  "function depositTo(address) payable",
];
const MIN_ENTRYPOINT_DEPOSIT = ethers.parseEther("0.05");

// ---------------------------------------------------------------------------
// Rollup config
// ---------------------------------------------------------------------------
const ROLLUP_CONFIGS: Record<string, typeof rollupA | typeof rollupB> = {
  a: rollupA,
  b: rollupB,
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
Usage: npx ts-node scripts/l2-to-l2-SA-new-token.ts --source <a|b> --dest <a|b>

  --source   Source rollup: a or b
  --dest     Destination rollup: a or b

Example:
  npx ts-node scripts/l2-to-l2-SA-new-token.ts --source a --dest b
  `);
  process.exit(1);
}

async function main() {
  const sourceKey = getArg("--source")?.toLowerCase();
  const destKey = getArg("--dest")?.toLowerCase();

  if (!sourceKey || !ROLLUP_CONFIGS[sourceKey]) { console.error("Error: --source must be 'a' or 'b'"); printUsage(); }
  if (!destKey || !ROLLUP_CONFIGS[destKey]) { console.error("Error: --dest must be 'a' or 'b'"); printUsage(); }
  if (sourceKey === destKey) { console.error("Error: --source and --dest must be different"); printUsage(); }

  const sourceChain = ROLLUP_CONFIGS[sourceKey];
  const destChain = ROLLUP_CONFIGS[destKey];
  const tag = `[SA:${sourceChain.name}->${destChain.name}:TOKEN]`;

  // -- Step 1: Setup --
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
  const eoaAddress = viemAccount.address;
  console.log(`${tag} EOA: ${eoaAddress}`);

  // -- Step 2: Create smart accounts --
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

  // -- Step 3: Fund EntryPoint if needed --
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

  // -- Step 4: Deploy token on source + mint to smart account --
  console.log(`\n${tag} Step 4: Deploying test ERC-20 on ${sourceChain.name}...`);
  const tokenFactory = new ethers.ContractFactory(MINTABLE_TOKEN_ABI, MINTABLE_TOKEN_BYTECODE, srcEthersWallet);
  const deployTx = await tokenFactory.deploy("BridgeTestSA", "BTSA", 18);
  const tokenDeployment = await deployTx.waitForDeployment();
  const tokenAddress = await tokenDeployment.getAddress();
  console.log(`${tag} Token deployed at: ${tokenAddress}`);

  const token = new ethers.Contract(tokenAddress, ERC20_ABI, srcEthersWallet);

  // Mint tokens to the smart account (not EOA — the SA will bridge them)
  console.log(`${tag} Minting 100 BTSA to smart account...`);
  const mintTx = await token.mint(saAddress, BRIDGE_AMOUNT);
  const mintReceipt = await mintTx.wait();
  console.log(`${tag} Mint tx: ${mintReceipt!.hash}`);

  const saTokenBalance = await token.balanceOf(saAddress);
  console.log(`${tag} SA token balance on source: ${ethers.formatUnits(saTokenBalance, 18)} BTSA`);

  // -- Step 5: Compute predicted CET on dest --
  console.log(`\n${tag} Step 5: Computing predicted CET on ${destChain.name}...`);
  const cetFactory = new ethers.Contract(CET_FACTORY, CETFactoryABI, dstEthersProvider);
  const predictedCET: string = await cetFactory.predictAddress(tokenAddress, sourceChain.id);
  console.log(`${tag} Predicted CET: ${predictedCET}`);

  // Snapshot EOA CET balance on dest before
  const cetToken = new ethers.Contract(predictedCET, ERC20_ABI, dstEthersProvider);
  let eoaCetBalanceBefore = 0n;
  try { eoaCetBalanceBefore = BigInt(await cetToken.balanceOf(eoaAddress)); } catch {}
  console.log(`${tag} EOA CET balance on dest before: ${ethers.formatUnits(eoaCetBalanceBefore, 18)}`);

  // -- Step 6: Build composed UserOps --
  const sessionId = BigInt(Date.now());
  console.log(`\n${tag} Step 6: Building composed transaction...`);
  console.log(`${tag} SessionId: ${sessionId.toString()}`);

  // Source calls: approve + bridgeERC20To
  const approveCalldata = encodeFunctionData({
    abi: [{ type: "function", name: "approve", inputs: [{ name: "spender", type: "address" }, { name: "amount", type: "uint256" }], outputs: [{ type: "bool" }], stateMutability: "nonpayable" }],
    functionName: "approve",
    args: [COMPOSE_L2_TO_L2_BRIDGE as Hex, BigInt(BRIDGE_AMOUNT.toString())],
  });

  const bridgeCalldata = encodeFunctionData({
    abi: ComposeL2ToL2BridgeABI,
    functionName: "bridgeERC20To",
    args: [
      BigInt(destChain.id),
      tokenAddress as Hex,
      BigInt(BRIDGE_AMOUNT.toString()),
      saAddress as Hex, // receiver on dest = smart account (will forward to EOA)
      sessionId,
    ],
  });

  const sourceCalls: UserOPCall[] = [
    { to: tokenAddress as Hex, value: 0n, data: approveCalldata },
    { to: COMPOSE_L2_TO_L2_BRIDGE as Hex, value: 0n, data: bridgeCalldata },
  ];

  // Dest calls: receiveTokens + transfer CET to EOA
  const msgHeader = {
    chainSrc: BigInt(sourceChain.id),
    chainDest: BigInt(destChain.id),
    sender: COMPOSE_L2_TO_L2_BRIDGE as Hex,
    receiver: saAddress as Hex,
    sessionId: sessionId,
    label: "SEND_TOKENS",
  };
  const receiveCalldata = encodeFunctionData({
    abi: ComposeL2ToL2BridgeABI,
    functionName: "receiveTokens",
    args: [msgHeader],
  });

  // Transfer CET from smart account to EOA
  const transferCalldata = encodeFunctionData({
    abi: [{ type: "function", name: "transfer", inputs: [{ name: "to", type: "address" }, { name: "amount", type: "uint256" }], outputs: [{ type: "bool" }], stateMutability: "nonpayable" }],
    functionName: "transfer",
    args: [eoaAddress as Hex, BigInt(BRIDGE_AMOUNT.toString())],
  });

  const destCalls: UserOPCall[] = [
    { to: COMPOSE_L2_TO_L2_BRIDGE as Hex, value: 0n, data: receiveCalldata },
    { to: predictedCET as Hex, value: 0n, data: transferCalldata },
  ];

  // -- Step 7: Create UserOps + compose + submit --
  console.log(`${tag} Creating UserOps...`);
  const originalWarn = console.warn;
  console.warn = () => {};
  const srcUserOp = await srcSA.account.createUserOp(sourceCalls);
  const dstUserOp = await dstSA.account.createUserOp(destCalls);
  console.warn = originalWarn;

  // Override gas limits — defaults are too low for cross-chain token bridge:
  // Source: approve + bridgeERC20To (writes mailbox + reads ACK)
  srcUserOp.userOp.callGasLimit = 3_000_000n;
  // Dest: receiveTokens (deploys new CET contract + mint) + transfer to EOA
  dstUserOp.userOp.callGasLimit = 5_000_000n;
  dstUserOp.userOp.verificationGasLimit = 3_500_000n;

  console.log(`${tag} Composing and submitting...`);
  const composed = await composeOps(
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

  console.log(`\n${tag} Sending composed transaction...`);
  const sendResult = await composed.send();
  console.log(`${tag} Submitted! Hashes:`, sendResult.hashes);

  // -- Step 8: Wait for receipts --
  console.log(`\n${tag} Waiting for receipts...`);
  const receipts = await sendResult.wait();

  const chainLabels = [sourceChain.name, destChain.name];
  for (let i = 0; i < receipts.length; i++) {
    const r = receipts[i];
    const label = chainLabels[i] || `Chain ${i}`;
    console.log(`${tag} ${label}: tx=${r.transactionHash}, status=${r.status}, block=${r.blockNumber}, gasUsed=${r.gasUsed}`);
  }

  // -- Step 9: Assert balances --
  console.log(`\n${tag} Checking final balances...`);

  // Source: SA token balance should be 0 (locked in bridge)
  const saTokenBalanceAfter = await token.balanceOf(saAddress);
  console.log(`${tag} SA token balance on source after: ${ethers.formatUnits(saTokenBalanceAfter, 18)} BTSA`);
  if (BigInt(saTokenBalanceAfter) !== 0n) {
    throw new Error(`ASSERT FAILED: SA should have 0 tokens on source, has ${ethers.formatUnits(saTokenBalanceAfter, 18)}`);
  }
  console.log(`${tag} ASSERT OK: Source SA token balance is 0 (locked in bridge)`);

  // Dest: CET deployed
  const cetCode = await dstEthersProvider.getCode(predictedCET);
  if (cetCode === "0x" || cetCode === "0x0") {
    throw new Error(`ASSERT FAILED: CET contract not deployed on ${destChain.name}`);
  }
  console.log(`${tag} ASSERT OK: CET contract deployed on ${destChain.name}`);

  // Dest: EOA has the CET tokens (transferred from SA)
  const eoaCetBalanceAfter = BigInt(await cetToken.balanceOf(eoaAddress));
  const eoaCetIncrease = eoaCetBalanceAfter - eoaCetBalanceBefore;
  console.log(`${tag} EOA CET balance on dest: ${ethers.formatUnits(eoaCetBalanceAfter, 18)}`);
  console.log(`${tag} EOA CET increased by: ${ethers.formatUnits(eoaCetIncrease, 18)}`);

  if (eoaCetIncrease.toString() !== BRIDGE_AMOUNT.toString()) {
    throw new Error(
      `ASSERT FAILED: Expected EOA CET increase of ${ethers.formatUnits(BRIDGE_AMOUNT, 18)}, ` +
      `got ${ethers.formatUnits(eoaCetIncrease, 18)}`
    );
  }
  console.log(`${tag} ASSERT OK: EOA received bridged tokens on ${destChain.name}`);

  console.log(`\n${tag} Bridge complete! Tokens are in EOA: ${eoaAddress}`);
  console.log("  Hashes:", sendResult.hashes);
  console.log(`  Token (source): ${tokenAddress}`);
  console.log(`  CET (dest): ${predictedCET}`);
}

main().catch((err) => {
  console.error("\n[SA:L2->L2:TOKEN] FATAL:", err.message || err);
  process.exit(1);
});
