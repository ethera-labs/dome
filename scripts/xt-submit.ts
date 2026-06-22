/**
 * Cross-chain transaction submission helper.
 * Handles two submission methods:
 * - Sidecar REST API (sepolia-stage): POST /xt with {transactions: {chainId: [signedTx]}}
 * - Compose sequencer RPC (hoodi, sepolia-prod): eth_sendXTransaction with encodeXtMessage
 */
import { encodeXtMessage } from "@ssv-labs/ethera-sdk";
import { SIDECAR_URL } from "../config";
import type { Hex } from "viem";

interface XtEntry {
  chainId: number;
  rawTx: string; // signed tx hex with 0x prefix
}

interface XtResult {
  method: "sidecar" | "rpc";
  result: any;
}

/**
 * Submit a cross-chain transaction.
 * If SIDECAR_URL is set, uses the sidecar REST API.
 * Otherwise, uses encodeXtMessage + eth_sendXTransaction to the sourceRpc.
 */
export async function submitXt(
  entries: XtEntry[],
  sourceRpc: string
): Promise<XtResult> {
  if (SIDECAR_URL) {
    return submitViaSidecar(entries);
  } else {
    return submitViaRpc(entries, sourceRpc);
  }
}

async function submitViaSidecar(entries: XtEntry[]): Promise<XtResult> {
  // Build {chainId: [signedTx]} map
  const transactions: Record<string, string[]> = {};
  for (const entry of entries) {
    const key = entry.chainId.toString();
    if (!transactions[key]) transactions[key] = [];
    transactions[key].push(entry.rawTx);
  }

  const response = await fetch(SIDECAR_URL, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ transactions }),
  });

  const text = await response.text();
  let result: any;
  try {
    result = JSON.parse(text);
  } catch {
    result = { raw: text };
  }

  if (!response.ok && !result.instance_id) {
    throw new Error(`Sidecar submission failed (${response.status}): ${text}`);
  }

  return { method: "sidecar", result };
}

async function submitViaRpc(entries: XtEntry[], sourceRpc: string): Promise<XtResult> {
  const xtPayload = encodeXtMessage({
    entries: entries.map((e) => ({
      chainId: e.chainId,
      rawTx: e.rawTx as Hex,
    })),
  });

  const response = await fetch(sourceRpc, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      jsonrpc: "2.0",
      method: "eth_sendXTransaction",
      params: [xtPayload],
      id: 1,
    }),
  });

  const rpcResult = await response.json();

  if (rpcResult.error) {
    throw new Error(`eth_sendXTransaction failed: ${JSON.stringify(rpcResult.error)}`);
  }

  return { method: "rpc", result: rpcResult.result };
}
