/**
 * Chain definitions and AA contracts for Smart Account scripts.
 * Uses RPCs, chain IDs, and AA addresses from config.ts instead of the SDK's hardcoded Sepolia values.
 * Import this instead of rollupA/rollupB/rollupsAccountAbstractionContracts from @ssv-labs/ethera-sdk.
 */
import { defineChain } from "viem";
import {
  L1_Rollup_1_RPC,
  L1_Rollup_2_RPC,
  ROLLUP_A_CHAIN_ID,
  ROLLUP_B_CHAIN_ID,
  AA_KERNEL_IMPL,
  AA_KERNEL_FACTORY,
  AA_MULTICHAIN_VALIDATOR,
} from "../config";

export const rollupA = defineChain({
  id: ROLLUP_A_CHAIN_ID,
  name: "Rollup A",
  nativeCurrency: { name: "Ethereum", symbol: "ETH", decimals: 18 },
  rpcUrls: {
    default: { http: [L1_Rollup_1_RPC] },
  },
  blockExplorers: {
    default: { name: "Rollup A Explorer", url: "https://rollup-a.explorer.testnet.compose.network" },
  },
  testnet: true,
});

export const rollupB = defineChain({
  id: ROLLUP_B_CHAIN_ID,
  name: "Rollup B",
  nativeCurrency: { name: "Ethereum", symbol: "ETH", decimals: 18 },
  rpcUrls: {
    default: { http: [L1_Rollup_2_RPC] },
  },
  blockExplorers: {
    default: { name: "Rollup B Explorer", url: "https://rollup-b.explorer.testnet.compose.network" },
  },
  testnet: true,
});

export const accountAbstractionContracts = {
  kernelImpl: AA_KERNEL_IMPL as `0x${string}`,
  kernelFactory: AA_KERNEL_FACTORY as `0x${string}`,
  multichainValidator: AA_MULTICHAIN_VALIDATOR as `0x${string}`,
} as const;
