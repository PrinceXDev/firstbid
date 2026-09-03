import { SomniaMarkets, SOMNIA_TESTNET_ADDRESSES } from "@somnia-chain/markets-sdk";
import { somniaShannon } from "@somnia-chain/markets-sdk/chains";

const exchange = new SomniaMarkets({
  indexerUrl: "https://dev.smk.somnia.host/v1/graphql",
  chain: somniaShannon,
  wsRpcUrl: "wss://api.infra.testnet.somnia.network/ws",
  addresses: SOMNIA_TESTNET_ADDRESSES,
});

const c = exchange.client;
const now = Date.now() / 1000;

const live = await c.listLiveBinaryMarkets({ limit: 40 });
console.log(`listLiveBinaryMarkets -> ${live.length} rows\n`);

let tradable = 0, empty = 0, twoSided = 0;

for (const m of live.slice(0, 12)) {
  const onchain = await c.getMarketOnchain(m.marketId as `0x${string}`);
  const secsLeft = Number(m.expiry) - now;
  if (onchain.status !== 1) {
    console.log(`  [skip status=${onchain.status}] ${m.asset}/${Number(m.intervalSec) / 60}m`);
    continue;
  }
  tradable++;
  const book = await c.getBinaryOrderBook(onchain.pool);
  const bid = book.bids?.[0], ask = book.asks?.[0];
  const nb = book.bids?.length ?? 0, na = book.asks?.length ?? 0;
  if (nb === 0 && na === 0) empty++;
  if (nb > 0 && na > 0) twoSided++;
  console.log(
    `  ${m.asset}/${Number(m.intervalSec) / 60}m  t-${secsLeft.toFixed(0)}s  ` +
    `levels ${nb}b/${na}a  ` +
    `bid=${bid ? JSON.stringify(bid.price ?? bid[0]) : "—"} ask=${ask ? JSON.stringify(ask.price ?? ask[0]) : "—"}  ` +
    `trades=${m.tradeCount}`
  );
}

console.log(`\n=== tradable=${tradable}  completely empty books=${empty}  two-sided=${twoSided}`);

const params = await c.getBinaryBookParams(
  (await c.getMarketOnchain(live[0].marketId as `0x${string}`)).pool
);
console.log("book params (tick/lot/min):", params);
process.exit(0);
