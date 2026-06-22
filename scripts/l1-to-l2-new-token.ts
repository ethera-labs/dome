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

// Minimal ERC-20 ABI (standard functions)
const ERC20_ABI = [
  "function name() view returns (string)",
  "function symbol() view returns (string)",
  "function decimals() view returns (uint8)",
  "function balanceOf(address) view returns (uint256)",
  "function approve(address spender, uint256 amount) returns (bool)",
  "function mint(address to, uint256 amount)",
];

// MintableToken bytecode — compiled from Solidity 0.8.20 with forge (optimizer enabled).
// Source: a simple ERC-20 with public mint(), no access control (test use only).
// Constructor: (string name, string symbol, uint8 decimals_)
const MINTABLE_TOKEN_BYTECODE =
  "0x608060405234801562000010575f80fd5b5060405162000a8f38038062000a8f83398101604081905262000033916200012b565b5f62000040848262000236565b5060016200004f838262000236565b506002805460ff191660ff9290921691909117905550620002fe9050565b634e487b7160e01b5f52604160045260245ffd5b5f82601f83011262000091575f80fd5b81516001600160401b0380821115620000ae57620000ae6200006d565b604051601f8301601f19908116603f01168101908282118183101715620000d957620000d96200006d565b81604052838152602092508683858801011115620000f5575f80fd5b5f91505b83821015620001185785820183015181830184015290820190620000f9565b5f93810190920192909252949350505050565b5f805f606084860312156200013e575f80fd5b83516001600160401b038082111562000155575f80fd5b620001638783880162000081565b9450602086015191508082111562000179575f80fd5b50620001888682870162000081565b925050604084015160ff811681146200019f575f80fd5b809150509250925092565b600181811c90821680620001bf57607f821691505b602082108103620001de57634e487b7160e01b5f52602260045260245ffd5b50919050565b601f82111562000231575f81815260208120601f850160051c810160208610156200020c5750805b601f850160051c820191505b818110156200022d5782815560010162000218565b5050505b505050565b81516001600160401b038111156200025257620002526200006d565b6200026a81620002638454620001aa565b84620001e4565b602080601f831160018114620002a0575f8415620002885750858301515b5f19600386901b1c1916600185901b1785556200022d565b5f85815260208120601f198616915b82811015620002d057888601518255948401946001909101908401620002af565b5085821015620002ee57878501515f19600388901b60f8161c191681555b5050505050600190811b01905550565b610783806200030c5f395ff3fe608060405234801561000f575f80fd5b506004361061009b575f3560e01c806340c10f191161006357806340c10f191461012957806370a082311461013e57806395d89b411461015d578063a9059cbb14610165578063dd62ed3e14610178575f80fd5b806306fdde031461009f578063095ea7b3146100bd57806318160ddd146100e057806323b872dd146100f7578063313ce5671461010a575b5f80fd5b6100a76101a2565b6040516100b491906105c3565b60405180910390f35b6100d06100cb366004610629565b61022d565b60405190151581526020016100b4565b6100e960035481565b6040519081526020016100b4565b6100d0610105366004610651565b610299565b6002546101179060ff1681565b60405160ff90911681526020016100b4565b61013c610137366004610629565b61044f565b005b6100e961014c36600461068a565b60046020525f908152604090205481565b6100a76104d5565b6100d0610173366004610629565b6104e2565b6100e96101863660046106aa565b600560209081525f928352604080842090915290825290205481565b5f80546101ae906106db565b80601f01602080910402602001604051908101604052809291908181526020018280546101da906106db565b80156102255780601f106101fc57610100808354040283529160200191610225565b820191905f5260205f20905b81548152906001019060200180831161020857829003601f168201915b505050505081565b335f8181526005602090815260408083206001600160a01b038716808552925280832085905551919290917f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925906102879086815260200190565b60405180910390a35060015b92915050565b6001600160a01b0383165f9081526005602090815260408083203384529091528120548211156103095760405162461bcd60e51b8152602060048201526016602482015275696e73756666696369656e7420616c6c6f77616e636560501b60448201526064015b60405180910390fd5b6001600160a01b0384165f908152600460205260409020548211156103675760405162461bcd60e51b8152602060048201526014602482015273696e73756666696369656e742062616c616e636560601b6044820152606401610300565b6001600160a01b0384165f90815260056020908152604080832033845290915281208054849290610399908490610727565b90915550506001600160a01b0384165f90815260046020526040812080548492906103c5908490610727565b90915550506001600160a01b0383165f90815260046020526040812080548492906103f190849061073a565b92505081905550826001600160a01b0316846001600160a01b03167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef8460405161043d91815260200190565b60405180910390a35060019392505050565b8060035f828254610460919061073a565b90915550506001600160a01b0382165f908152600460205260408120805483929061048c90849061073a565b90915550506040518181526001600160a01b038316905f907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef9060200160405180910390a35050565b600180546101ae906106db565b335f908152600460205260408120548211156105375760405162461bcd60e51b8152602060048201526014602482015273696e73756666696369656e742062616c616e636560601b6044820152606401610300565b335f9081526004602052604081208054849290610555908490610727565b90915550506001600160a01b0383165f908152600460205260408120805484929061058190849061073a565b90915550506040518281526001600160a01b0384169033907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef90602001610287565b5f6020808352835180828501525f5b818110156105ee578581018301518582016040015282016105d2565b505f604082860101526040601f19601f8301168501019250505092915050565b80356001600160a01b0381168114610624575f80fd5b919050565b5f806040838503121561063a575f80fd5b6106438361060e565b946020939093013593505050565b5f805f60608486031215610663575f80fd5b61066c8461060e565b925061067a6020850161060e565b9150604084013590509250925092565b5f6020828403121561069a575f80fd5b6106a38261060e565b9392505050565b5f80604083850312156106bb575f80fd5b6106c48361060e565b91506106d26020840161060e565b90509250929050565b600181811c908216806106ef57607f821691505b60208210810361070d57634e487b7160e01b5f52602260045260245ffd5b50919050565b634e487b7160e01b5f52601160045260245ffd5b8181038181111561029357610293610713565b808201808211156102935761029361071356fea26469706673582212206f24f4f523f3527fa0861762239b948bb5596cf1743604001ae31f92dc47042f64736f6c63430008140033";

const MINTABLE_TOKEN_ABI = [
  "constructor(string name, string symbol, uint8 decimals_)",
  ...ERC20_ABI,
];

const BRIDGE_AMOUNT = ethers.parseUnits("100", 18); // 100 tokens
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

function parseRollupFlag(): RollupConfig {
  const idx = process.argv.indexOf("--dest");
  const value = idx !== -1 && idx + 1 < process.argv.length ? process.argv[idx + 1].toLowerCase() : undefined;
  if (!value || !ROLLUP_CONFIGS[value]) {
    console.error("Usage: npx ts-node scripts/l1-to-l2-new-token.ts --dest <a|b>");
    process.exit(1);
  }
  return ROLLUP_CONFIGS[value];
}

async function main() {
  const rollup = parseRollupFlag();

  // -- Setup --
  console.log(`[L1->${rollup.name}] Setting up providers...`);
  const l1Provider = new ethers.JsonRpcProvider(L1_RPC);
  const l2Provider = new ethers.JsonRpcProvider(rollup.l2Rpc);
  const l1Wallet = new ethers.Wallet(wallet_private_key, l1Provider);
  const walletAddress = l1Wallet.address;

  console.log(`[L1->${rollup.name}] Wallet: ${walletAddress}`);
  console.log(`[L1->${rollup.name}] L1 Bridge: ${rollup.l1Bridge}`);
  const l1Balance = await l1Provider.getBalance(walletAddress);
  console.log(`[L1->${rollup.name}] L1 ETH balance: ${ethers.formatEther(l1Balance)} ETH`);
  if (l1Balance === 0n) {
    throw new Error("Wallet has no ETH on L1. Fund it first.");
  }

  // -- Step 1: Deploy test ERC-20 on L1 --
  console.log(`\n[L1->${rollup.name}] Step 1: Deploying test ERC-20 on L1...`);
  const tokenFactory = new ethers.ContractFactory(
    MINTABLE_TOKEN_ABI,
    MINTABLE_TOKEN_BYTECODE,
    l1Wallet
  );
  const deployTx = await tokenFactory.deploy("BridgeTest", "BT", 18);
  const tokenDeployment = await deployTx.waitForDeployment();
  const tokenAddress = await tokenDeployment.getAddress();
  console.log(`[L1->${rollup.name}] Token deployed at: ${tokenAddress}`);

  const token = new ethers.Contract(tokenAddress, ERC20_ABI, l1Wallet);

  // -- Step 2: Mint tokens --
  console.log(`\n[L1->${rollup.name}] Step 2: Minting tokens...`);
  const mintTx = await token.mint(walletAddress, BRIDGE_AMOUNT);
  const mintReceipt = await mintTx.wait();
  console.log(`[L1->${rollup.name}] Mint tx: ${mintReceipt!.hash}`);
  const balance = await token.balanceOf(walletAddress);
  console.log(`[L1->${rollup.name}] Minted ${ethers.formatUnits(balance, 18)} BT tokens`);

  // -- Step 3: Compute predicted CET address --
  console.log(`\n[L1->${rollup.name}] Step 3: Computing predicted CET address on ${rollup.name}...`);
  const cetFactory = new ethers.Contract(CET_FACTORY, CETFactoryABI, l2Provider);
  const predictedCET = await cetFactory.predictAddress(tokenAddress, L1_CHAIN_ID);
  console.log(`[L1->${rollup.name}] Predicted CET on ${rollup.name}: ${predictedCET}`);

  // -- Step 4: Read token metadata --
  const name = await token.name();
  const symbol = await token.symbol();
  const decimals = await token.decimals();
  console.log(`[L1->${rollup.name}] Token metadata: ${name} (${symbol}), ${decimals} decimals`);

  // -- Step 5: Encode _extraData --
  const extraData = ethers.AbiCoder.defaultAbiCoder().encode(
    ["string", "string", "uint8", "bytes"],
    [name, symbol, decimals, "0x"]
  );

  // -- Step 6: Approve bridge to spend tokens --
  console.log(`\n[L1->${rollup.name}] Step 6: Approving ComposeL1Bridge...`);
  const approveTx = await token.approve(rollup.l1Bridge, BRIDGE_AMOUNT);
  const approveReceipt = await approveTx.wait();
  console.log(`[L1->${rollup.name}] Approve tx: ${approveReceipt!.hash}`);

  // Snapshot L1 balance before bridging
  const l1BalanceBefore = await token.balanceOf(walletAddress);
  console.log(`[L1->${rollup.name}] L1 token balance before bridge: ${ethers.formatUnits(l1BalanceBefore, decimals)}`);

  // -- Step 7: Bridge tokens --
  console.log(`\n[L1->${rollup.name}] Step 7: Bridging tokens to ${rollup.name}...`);
  const bridge = new ethers.Contract(rollup.l1Bridge, ComposeL1BridgeABI, l1Wallet);

  const bridgeTx = await bridge.bridgeERC20To(
    tokenAddress,     // _localToken
    predictedCET,     // _remoteToken
    walletAddress,    // _to
    BRIDGE_AMOUNT,    // _amount
    MIN_GAS_LIMIT,    // _minGasLimit
    extraData         // _extraData
  );
  const bridgeReceipt = await bridgeTx.wait();

  console.log(`[L1->${rollup.name}] Bridge tx confirmed!`);
  console.log(`  Tx hash:  ${bridgeReceipt!.hash}`);
  console.log(`  Block:    ${bridgeReceipt!.blockNumber}`);
  console.log(`  Gas used: ${bridgeReceipt!.gasUsed}`);

  // Assert L1 balance decreased
  const l1BalanceAfter = await token.balanceOf(walletAddress);
  console.log(`[L1->${rollup.name}] L1 token balance after bridge: ${ethers.formatUnits(l1BalanceAfter, decimals)}`);

  const l1Decrease = BigInt(l1BalanceBefore) - BigInt(l1BalanceAfter);
  console.log(`[L1->${rollup.name}] L1 balance decreased by: ${ethers.formatUnits(l1Decrease, decimals)}`);
  if (l1Decrease.toString() !== BRIDGE_AMOUNT.toString()) {
    throw new Error(
      `ASSERT FAILED: L1 balance should have decreased by ${ethers.formatUnits(BRIDGE_AMOUNT, decimals)}, ` +
      `but decreased by ${ethers.formatUnits(l1Decrease, decimals)}`
    );
  }
  console.log(`[L1->${rollup.name}] ASSERT OK: L1 balance decreased by expected amount`);

  // -- Step 8: Poll for CET arrival --
  console.log(`\n[L1->${rollup.name}] Step 8: Polling for CET arrival on ${rollup.name}...`);
  console.log(`  Watching CET at ${predictedCET}`);
  console.log("  (This may take several minutes for op-node to derive the deposit tx)\n");

  const cetToken = new ethers.Contract(predictedCET, ERC20_ABI, l2Provider);
  const MAX_POLLS = 60;
  const POLL_INTERVAL_MS = 10_000;

  for (let i = 1; i <= MAX_POLLS; i++) {
    try {
      const cetBalance = await cetToken.balanceOf(walletAddress);
      if (cetBalance > 0n) {
        console.log(`\n[L1->${rollup.name}] CET arrived! Balance: ${ethers.formatUnits(cetBalance, decimals)} ${symbol}`);
        console.log(`[L1->${rollup.name}] CET address: ${predictedCET}`);

        if (BigInt(cetBalance).toString() !== BRIDGE_AMOUNT.toString()) {
          throw new Error(
            `ASSERT FAILED: Expected CET balance ${ethers.formatUnits(BRIDGE_AMOUNT, decimals)}, ` +
            `got ${ethers.formatUnits(cetBalance, decimals)}`
          );
        }
        console.log(`[L1->${rollup.name}] ASSERT OK: L2 CET balance matches bridged amount`);

        console.log(`\n[L1->${rollup.name}] Bridge complete!`);
        console.log("  Summary of tx hashes:");
        console.log(`    Deploy:  ${tokenAddress} (contract creation)`);
        console.log(`    Mint:    ${mintReceipt!.hash}`);
        console.log(`    Approve: ${approveReceipt!.hash}`);
        console.log(`    Bridge:  ${bridgeReceipt!.hash}`);
        return;
      }
    } catch (err) {
      if (err instanceof Error && err.message.startsWith("ASSERT FAILED")) throw err;
    }
    process.stdout.write(`  Poll ${i}/${MAX_POLLS} — waiting...\r`);
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }

  console.log(`\n[L1->${rollup.name}] Timed out waiting for CET. The deposit may still be processing.`);
  console.log(`  Check manually: CET at ${predictedCET} on ${rollup.name}`);
  console.log("  Tx hashes:");
  console.log(`    Deploy:  ${tokenAddress} (contract creation)`);
  console.log(`    Mint:    ${mintReceipt!.hash}`);
  console.log(`    Approve: ${approveReceipt!.hash}`);
  console.log(`    Bridge:  ${bridgeReceipt!.hash}`);
}

main().catch((err) => {
  console.error("\n[L1->L2] FATAL:", err.message || err);
  process.exit(1);
});
