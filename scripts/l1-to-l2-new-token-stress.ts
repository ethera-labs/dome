import { ethers } from "ethers";
import {
  L1_RPC,
  L1_Rollup_1_RPC,
  L1_Rollup_2_RPC,
  wallet_private_key,
  L1_CHAIN_ID,
  COMPOSE_L1_BRIDGE_ROLLUP_A,
  COMPOSE_L1_BRIDGE_ROLLUP_B,
  CET_FACTORY,
} from "../config";
import ComposeL1BridgeABI from "../sepolia-prod/L1/abis/ComposeL1Bridge.json";
import CETFactoryABI from "../sepolia-prod/L2/abis/CETFactory.json";

// Higher than ETH stress — token bridge retries cost more gas per failed attempt
const FUND_AMOUNT = ethers.parseEther("0.5");
const TOKEN_AMOUNT = ethers.parseUnits("100", 18);
// _minGasLimit for L2 deposit tx:
// - First bridge deploys CET contract via CETFactory (~2.5M gas on L2)
// - Subsequent bridges just mint (~200K gas on L2)
// Using high _minGasLimit for all txs overwhelms the portal's gas metering.
const MIN_GAS_LIMIT_FIRST = 2_500_000;  // for the first bridge (deploys CET)
const MIN_GAS_LIMIT = 200_000;           // for subsequent bridges (just mint)
const BRIDGE_GAS_LIMIT = 5_000_000n;

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
Usage: npx ts-node scripts/l1-to-l2-token-stress.ts --dest <a|b> [--num-acc <N>] [--new-wallets]

  --dest         Target rollup: a or b
  --num-acc      Number of accounts (default: 100)
  --new-wallets  Generate fresh random wallets instead of deterministic ones

Example:
  npx ts-node scripts/l1-to-l2-token-stress.ts --dest a --num-acc 5
  `);
  process.exit(1);
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function deriveAccounts(masterKey: string, count: number): ethers.Wallet[] {
  const wallets: ethers.Wallet[] = [];
  for (let i = 0; i < count; i++) {
    // Use a different derivation path than ETH stress to avoid nonce conflicts
    const derived = ethers.keccak256(
      ethers.solidityPacked(["bytes32", "string", "uint256"], [`0x${masterKey}`, "token-stress", i])
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
  const tag = `[STRESS:L1->${rollup.name}:TOKEN]`;
  const totalStart = Date.now();

  console.log(`${tag} Stress test: ${numAcc} accounts, bridge 100 ST each to ${rollup.name}`);

  // Setup
  const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
  const l2Provider = new ethers.JsonRpcProvider(rollup.l2Rpc);
  const funderWallet = new ethers.Wallet(wallet_private_key, l1Provider);
  const chainId = (await l1Provider.getNetwork()).chainId;

  console.log(`${tag} Funder: ${funderWallet.address}`);

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
  // Step 2: Fund accounts with ETH on L1 (for gas)
  // =========================================================================
  console.log(`\n${tag} Step 2: Funding accounts with ETH for gas...`);
  const fundStart = Date.now();

  const existingBalances = await Promise.all(
    accounts.map((acc) => l1Provider.getBalance(acc.address))
  );
  // Need enough for approve + multiple bridge retry attempts (each failed tx costs gas)
  const MIN_REQUIRED = ethers.parseEther("0.2");
  const needsFunding = [];
  for (let i = 0; i < numAcc; i++) {
    if (existingBalances[i] < MIN_REQUIRED) needsFunding.push(i);
  }

  if (needsFunding.length === 0) {
    console.log(`${tag} All ${numAcc} accounts already funded, skipping`);
  } else {
    console.log(`${tag} ${needsFunding.length}/${numAcc} accounts need funding`);

    const feeData = await l1Provider.getFeeData();
    const baseNonce = await l1Provider.getTransactionCount(funderWallet.address, "pending");

    const signedFundTxs: string[] = [];
    for (let j = 0; j < needsFunding.length; j++) {
      const i = needsFunding[j];
      const signed = await funderWallet.signTransaction({
        to: accounts[i].address, value: FUND_AMOUNT,
        nonce: baseNonce + j, gasLimit: 21_000n,
        maxFeePerGas: feeData.maxFeePerGas!, maxPriorityFeePerGas: feeData.maxPriorityFeePerGas!,
        chainId, type: 2,
      });
      signedFundTxs.push(signed);
    }
    console.log(`${tag} Signed ${needsFunding.length} funding txs in ${elapsed(fundStart)}`);

    const fundResponses = await Promise.all(
      signedFundTxs.map((signed) => l1Provider.broadcastTransaction(signed))
    );
    console.log(`${tag} Broadcast ${needsFunding.length} funding txs`);

    const fundReceipts = await Promise.all(fundResponses.map((resp) => resp.wait()));
    const fundFailed = fundReceipts.filter((r) => r!.status !== 1);
    if (fundFailed.length > 0) throw new Error(`${fundFailed.length} funding txs failed`);
  }
  console.log(`${tag} Funding phase took ${elapsed(fundStart)}`);

  // =========================================================================
  // Step 3: Deploy StressToken on L1
  // =========================================================================
  console.log(`\n${tag} Step 3: Deploying StressToken (ST) on L1...`);
  const deployStart = Date.now();

  const tokenFactory = new ethers.ContractFactory(MINTABLE_TOKEN_ABI, MINTABLE_TOKEN_BYTECODE, funderWallet);
  const deployTx = await tokenFactory.deploy("StressToken", "ST", 18);
  const tokenDeployment = await deployTx.waitForDeployment();
  const tokenAddress = await tokenDeployment.getAddress();
  console.log(`${tag} Token deployed at: ${tokenAddress} in ${elapsed(deployStart)}`);

  const token = new ethers.Contract(tokenAddress, ERC20_ABI, funderWallet);
  const tokenName = await token.name();
  const tokenSymbol = await token.symbol();
  const tokenDecimals: number = await token.decimals();

  // Compute predicted CET on L2
  const cetFactory = new ethers.Contract(CET_FACTORY, CETFactoryABI, l2Provider);
  const predictedCET: string = await cetFactory.predictAddress(tokenAddress, L1_CHAIN_ID);
  console.log(`${tag} Predicted CET on ${rollup.name}: ${predictedCET}`);

  // Encode extraData for bridge call
  const extraData = ethers.AbiCoder.defaultAbiCoder().encode(
    ["string", "string", "uint8", "bytes"],
    [tokenName, tokenSymbol, tokenDecimals, "0x"]
  );

  // =========================================================================
  // Step 4: Mint 100 ST to each account
  // =========================================================================
  console.log(`\n${tag} Step 4: Minting ${ethers.formatUnits(TOKEN_AMOUNT, 18)} ST to each account...`);
  const mintStart = Date.now();

  const mintFeeData = await l1Provider.getFeeData();
  const mintBaseNonce = await l1Provider.getTransactionCount(funderWallet.address, "pending");
  const mintIface = new ethers.Interface(ERC20_ABI);

  const signedMintTxs: string[] = [];
  for (let i = 0; i < numAcc; i++) {
    const calldata = mintIface.encodeFunctionData("mint", [accounts[i].address, TOKEN_AMOUNT]);
    const signed = await funderWallet.signTransaction({
      to: tokenAddress, data: calldata,
      nonce: mintBaseNonce + i, gasLimit: 100_000n,
      maxFeePerGas: mintFeeData.maxFeePerGas!, maxPriorityFeePerGas: mintFeeData.maxPriorityFeePerGas!,
      chainId, type: 2,
    });
    signedMintTxs.push(signed);
  }
  console.log(`${tag} Signed ${numAcc} mint txs in ${elapsed(mintStart)}`);

  const mintResponses = await Promise.all(
    signedMintTxs.map((signed) => l1Provider.broadcastTransaction(signed))
  );
  console.log(`${tag} Broadcast ${numAcc} mint txs`);

  const mintReceipts = await Promise.all(mintResponses.map((resp) => resp.wait()));
  const mintFailed = mintReceipts.filter((r) => r!.status !== 1);
  if (mintFailed.length > 0) throw new Error(`${mintFailed.length} mint txs failed`);
  console.log(`${tag} All mints confirmed in ${elapsed(mintStart)}`);

  // =========================================================================
  // Step 5: Approve + Bridge tokens (all accounts)
  // =========================================================================
  console.log(`\n${tag} Step 5: Approving and bridging tokens to ${rollup.name}...`);
  const bridgeStart = Date.now();

  const bridgeIface = new ethers.Interface(ComposeL1BridgeABI);
  const approveFnIface = new ethers.Interface(ERC20_ABI);

  // Get nonces for all accounts
  const accNonces = await Promise.all(
    accounts.map((acc) => l1Provider.getTransactionCount(acc.address, "pending"))
  );
  const bridgeFeeData = await l1Provider.getFeeData();

  // Sign approve + bridge txs (2 per account, sequential nonces)
  // First account uses high _minGasLimit (deploys CET on L2), rest use low (just mint)
  const signedApproveTxs: string[] = [];
  const signedBridgeTxs: string[] = [];

  for (let i = 0; i < numAcc; i++) {
    const accWallet = accounts[i].connect(l1Provider);
    const nonce = accNonces[i];
    const minGas = i === 0 ? MIN_GAS_LIMIT_FIRST : MIN_GAS_LIMIT;

    // Approve tx
    const approveCalldata = approveFnIface.encodeFunctionData("approve", [rollup.l1Bridge, TOKEN_AMOUNT]);
    const signedApprove = await accWallet.signTransaction({
      to: tokenAddress, data: approveCalldata,
      nonce: nonce, gasLimit: 100_000n,
      maxFeePerGas: bridgeFeeData.maxFeePerGas!, maxPriorityFeePerGas: bridgeFeeData.maxPriorityFeePerGas!,
      chainId, type: 2,
    });
    signedApproveTxs.push(signedApprove);

    // Bridge tx
    const bridgeCalldata = bridgeIface.encodeFunctionData("bridgeERC20To", [
      tokenAddress, predictedCET, accounts[i].address, TOKEN_AMOUNT, minGas, extraData,
    ]);
    const signedBridge = await accWallet.signTransaction({
      to: rollup.l1Bridge, data: bridgeCalldata,
      nonce: nonce + 1, gasLimit: BRIDGE_GAS_LIMIT,
      maxFeePerGas: bridgeFeeData.maxFeePerGas!, maxPriorityFeePerGas: bridgeFeeData.maxPriorityFeePerGas!,
      chainId, type: 2,
    });
    signedBridgeTxs.push(signedBridge);
  }
  console.log(`${tag} Signed ${numAcc * 2} txs (approve+bridge) in ${elapsed(bridgeStart)}`);

  // Broadcast all approve txs first, wait for them
  console.log(`${tag} Broadcasting approve txs...`);
  const approveResponses = await Promise.all(
    signedApproveTxs.map((signed) => l1Provider.broadcastTransaction(signed))
  );
  const approveReceipts = await Promise.all(
    approveResponses.map((resp) => resp.wait().catch((err: any) => err.receipt || null))
  );
  const approveFailed = approveReceipts.filter((r) => !r || r.status !== 1);
  if (approveFailed.length > 0) {
    console.error(`${tag} WARNING: ${approveFailed.length} approve txs failed`);
  }
  console.log(`${tag} Approvals confirmed`);

  // Bridge account 0 first (deploys CET on L2 with high _minGasLimit), then the rest
  console.log(`${tag} Bridging account 0 first (deploys CET on L2)...`);
  const firstBridgeResp = await l1Provider.broadcastTransaction(signedBridgeTxs[0]);
  const firstBridgeReceipt = await firstBridgeResp.wait().catch((err: any) => err.receipt || null);
  if (!firstBridgeReceipt || firstBridgeReceipt.status !== 1) {
    throw new Error("First bridge tx (CET deployment) failed on L1");
  }
  console.log(`${tag} Account 0 bridge tx confirmed: ${firstBridgeReceipt.hash}`);

  // Now broadcast the remaining bridge txs with retry logic (low _minGasLimit)
  console.log(`${tag} Broadcasting remaining ${numAcc - 1} bridge txs...`);
  const MAX_RETRIES = 20;
  const RETRY_DELAY_MS = 15_000;
  const succeeded = new Set<number>();
  succeeded.add(0); // account 0 already succeeded above
  const pendingIndices = Array.from({ length: numAcc - 1 }, (_, i) => i + 1);

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
        const bridgeCalldata = bridgeIface.encodeFunctionData("bridgeERC20To", [
          tokenAddress, predictedCET, accounts[i].address, TOKEN_AMOUNT, MIN_GAS_LIMIT, extraData,
        ]);
        signedBridgeTxs[i] = await accWallet.signTransaction({
          to: rollup.l1Bridge, data: bridgeCalldata,
          nonce: retryNonces[j], gasLimit: BRIDGE_GAS_LIMIT,
          maxFeePerGas: retryFeeData.maxFeePerGas!, maxPriorityFeePerGas: retryFeeData.maxPriorityFeePerGas!,
          chainId, type: 2,
        });
      }
    }

    const txsToSend = pendingIndices.map((i) => signedBridgeTxs[i]);
    const responses = await Promise.all(
      txsToSend.map((signed) => l1Provider.broadcastTransaction(signed).catch(() => null))
    );

    const receipts = await Promise.all(
      responses.map((resp) =>
        resp
          ? resp.wait().catch((err: any) => err.receipt || l1Provider.getTransactionReceipt(resp.hash))
          : null
      )
    );

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

    pendingIndices.length = 0;
    pendingIndices.push(...stillFailed);
  }

  console.log(`${tag} ${succeeded.size}/${numAcc} bridge txs succeeded in ${elapsed(bridgeStart)}`);

  // Assert L1 token balances are 0
  console.log(`\n${tag} Verifying L1 token balances...`);
  const l1TokenBalances = await Promise.all(
    Array.from(succeeded).map((i) => token.balanceOf(accounts[i].address))
  );
  const nonZeroL1 = l1TokenBalances.filter((b) => BigInt(b) !== 0n);
  if (nonZeroL1.length > 0) {
    console.error(`${tag} ASSERT FAILED: ${nonZeroL1.length} accounts still have tokens on L1`);
  } else {
    console.log(`${tag} ASSERT OK: All bridged accounts have 0 token balance on L1`);
  }

  if (succeeded.size < numAcc) {
    console.error(`${tag} WARNING: ${numAcc - succeeded.size} bridge txs could not succeed after retries`);
  }
  console.log(`${tag} Bridge phase took ${elapsed(bridgeStart)}`);

  // =========================================================================
  // Step 6: Poll for CET arrival on L2
  // =========================================================================
  console.log(`\n${tag} Step 6: Polling for CET arrival on ${rollup.name}...`);
  console.log(`  CET address: ${predictedCET}`);
  console.log("  (This may take several minutes for op-node to derive deposit txs)\n");
  const pollStart = Date.now();

  const cetToken = new ethers.Contract(predictedCET, ERC20_ABI, l2Provider);

  // Snapshot L2 CET balances before
  let l2BalancesBefore: bigint[] = new Array(numAcc).fill(0n);
  try {
    const bals = await Promise.all(
      accounts.map((acc) => cetToken.balanceOf(acc.address))
    );
    l2BalancesBefore = bals.map((b) => BigInt(b));
  } catch {
    // CET not deployed yet — all zeros
  }

  const received = new Set<number>();
  const MAX_POLLS = 180; // 30 minutes — L2 deposit derivation can be slow with many txs
  const POLL_INTERVAL_MS = 10_000;
  // Only poll for accounts that succeeded on L1
  const succeededArr = Array.from(succeeded);

  for (let poll = 1; poll <= MAX_POLLS; poll++) {
    const pending = succeededArr.filter((i) => !received.has(i));
    if (pending.length === 0) break;

    try {
      const balances = await Promise.all(
        pending.map((i) => cetToken.balanceOf(accounts[i].address))
      );

      for (let j = 0; j < pending.length; j++) {
        const idx = pending[j];
        const increase = BigInt(balances[j]) - l2BalancesBefore[idx];
        if (increase > 0n) {
          received.add(idx);
        }
      }
    } catch {
      // CET contract may not be deployed yet
    }

    const pct = ((received.size / succeeded.size) * 100).toFixed(0);
    process.stdout.write(`  Poll ${poll}/${MAX_POLLS} — ${received.size}/${succeeded.size} received (${pct}%)...\r`);

    if (received.size === succeeded.size) break;
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  console.log(`\n${tag} ${received.size}/${succeeded.size} accounts received CET on ${rollup.name} in ${elapsed(pollStart)}`);

  if (received.size < succeeded.size) {
    const missing = succeededArr.filter((i) => !received.has(i));
    console.error(`${tag} ASSERT FAILED: ${missing.length} accounts did not receive CET`);
    console.error(`  First few: ${missing.slice(0, 5).map((i) => accounts[i].address).join(", ")}`);
  } else {
    console.log(`${tag} ASSERT OK: All bridged accounts received CET on ${rollup.name}`);
  }

  // Assert exact amounts
  if (received.size > 0) {
    console.log(`\n${tag} Verifying exact CET amounts...`);
    const l2BalancesAfter = await Promise.all(
      succeededArr.map((i) => cetToken.balanceOf(accounts[i].address))
    );
    let mismatch = 0;
    for (let j = 0; j < succeededArr.length; j++) {
      const i = succeededArr[j];
      const increase = BigInt(l2BalancesAfter[j]) - l2BalancesBefore[i];
      if (increase !== TOKEN_AMOUNT) {
        mismatch++;
        if (mismatch <= 3) {
          console.error(`  Account ${i} (${accounts[i].address}): expected +100 ST, got +${ethers.formatUnits(increase, 18)}`);
        }
      }
    }
    if (mismatch > 0) {
      console.error(`${tag} ASSERT FAILED: ${mismatch} accounts have incorrect CET balance`);
    } else {
      console.log(`${tag} ASSERT OK: All accounts received exactly 100 ST as CET`);
    }
  }

  // =========================================================================
  // Summary
  // =========================================================================
  console.log(`\n${"=".repeat(60)}`);
  console.log(`${tag} STRESS TEST COMPLETE`);
  console.log(`${"=".repeat(60)}`);
  console.log(`  Token:            ${tokenAddress} (${tokenName} / ${tokenSymbol})`);
  console.log(`  CET on L2:        ${predictedCET}`);
  console.log(`  Accounts:         ${numAcc}`);
  console.log(`  Bridge txs:       ${succeeded.size}/${numAcc} succeeded`);
  console.log(`  L2 received:      ${received.size}/${succeeded.size}`);
  console.log(`  Total time:       ${elapsed(totalStart)}`);
}

main().catch((err) => {
  console.error("\n[STRESS:TOKEN] FATAL:", err.message || err);
  process.exit(1);
});
