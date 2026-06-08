/**
 * Tiny shell-out helper called by Go's helpers.SubmitXTRaw in rpc mode.
 *
 * Reads the XT entries (an array of {chainId, rawTx}) from --entries and
 * writes {"payload": "0x.."} to stdout. The payload is what
 * eth_sendXTransaction expects as its single param.
 *
 * The wire format produced by encodeXtMessage isn't trivially re-implementable
 * in Go, so we shell out for this one step. Everything else (signing, RPC,
 * receipts) stays in Go.
 *
 * Usage:
 *   npx ts-node scripts/encode-xt.ts --entries '[{"chainId":11113,"rawTx":"0x.."},...]'
 */
import { encodeXtMessage } from "@ssv-labs/ethera-sdk";
import type { Hex } from "viem";

function getArg(flag: string): string | undefined {
  const idx = process.argv.indexOf(flag);
  if (idx !== -1 && idx + 1 < process.argv.length) {
    return process.argv[idx + 1];
  }
  return undefined;
}

function fail(msg: string): never {
  console.error(`[encode-xt.ts] ${msg}`);
  process.exit(1);
  // Unreachable, but keeps TS happy when `process.exit` isn't typed as
  // returning `never` (e.g. if @types/node hasn't been installed yet).
  throw new Error(msg);
}

async function main() {
  const entriesArg = getArg("--entries");
  if (!entriesArg) fail("missing --entries");

  let parsed: Array<{ chainId: number; rawTx: string }>;
  try {
    parsed = JSON.parse(entriesArg!);
  } catch (e: any) {
    fail(`invalid --entries JSON: ${e.message || e}`);
  }
  if (!Array.isArray(parsed!) || parsed!.length === 0) {
    fail("--entries must be a non-empty array");
  }

  for (const e of parsed!) {
    if (typeof e.chainId !== "number" || typeof e.rawTx !== "string") {
      fail(`invalid entry: ${JSON.stringify(e)}`);
    }
  }

  const payload = encodeXtMessage({
    entries: parsed!.map((e) => ({ chainId: e.chainId, rawTx: e.rawTx as Hex })),
  });

  process.stdout.write(JSON.stringify({ payload }) + "\n");
}

main().catch((err) => {
  fail(err?.message || String(err));
});
