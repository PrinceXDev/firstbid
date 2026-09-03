import {
  SomniaMarkets,
  SOMNIA_TESTNET_ADDRESSES,
} from "@somnia-chain/markets-sdk";
import { somniaShannon } from "@somnia-chain/markets-sdk/chains";

process.loadEnvFile?.(new URL("../.env", import.meta.url));

const pk = process.env.FIRSTBID_PRIVATE_KEY as `0x${string}` | undefined;

export const exchange = new SomniaMarkets({
  indexerUrl:
    process.env.FIRSTBID_INDEXER_URL ??
    "https://dev.smk.somnia.host/v1/graphql",
  chain: somniaShannon,
  wsRpcUrl:
    process.env.FIRSTBID_WS_RPC_URL ??
    "wss://api.infra.testnet.somnia.network/ws",
  addresses: SOMNIA_TESTNET_ADDRESSES,
  ...(pk ? { privateKey: pk } : {}),
});

export const client = exchange.client;
export const hasSigner = Boolean(pk);

/**
 * One signer means one nonce sequence. Every write on this process is funnelled
 * through this chain so two concurrent requests can never race the nonce.
 * The Go core also serialises, but defence in depth is free here.
 */
let tail: Promise<unknown> = Promise.resolve();
export function serialise<T>(fn: () => Promise<T>): Promise<T> {
  const run = tail.then(fn, fn);
  tail = run.catch(() => {});
  return run;
}

/** bigints are pervasive in this SDK; JSON.stringify dies on them without this. */
export function jsonSafe(v: unknown): unknown {
  return JSON.parse(
    JSON.stringify(v, (_k, x) => (typeof x === "bigint" ? x.toString() : x)),
  );
}
