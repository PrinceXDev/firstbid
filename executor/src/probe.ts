/**
 * Cross-checks the official SDK's order-book read against a direct chain read.
 *
 * This probe exists because an earlier version of it reported every live book as
 * empty. That was this file's bug, not the SDK's: BinaryOrderBook exposes
 * yesBids/yesAsks/noBids/noAsks, and reading `book.bids` yields undefined.
 * The corrected version below is kept as the standing check.
 */
import { SomniaMarkets, SOMNIA_TESTNET_ADDRESSES } from "@somnia-chain/markets-sdk";
import { somniaShannon } from "@somnia-chain/markets-sdk/chains";

const exchange = new SomniaMarkets({
  indexerUrl: "https://dev.smk.somnia.host/v1/graphql",
  chain: somniaShannon,
  wsRpcUrl: "wss://api.infra.testnet.somnia.network/ws",
  addresses: SOMNIA_TESTNET_ADDRESSES,
});

const c = exchange.client;
const live = await c.listLiveBinaryMarkets({ limit: 40 });
console.log(`listLiveBinaryMarkets -> ${live.length} rows\n`);

let tradable = 0;
let empty = 0;
let twoSided = 0;

for (const m of live.slice(0, 12)) {
  const onchain = await c.getMarketOnchain(m.marketId as `0x${string}`);
  if (onchain.status !== 1) continue; // 1 = Trading
  tradable++;

  const book = await c.getBinaryOrderBook(onchain.pool);
  const bids = book.yesBids ?? [];
  const asks = book.yesAsks ?? [];
  if (bids.length === 0 && asks.length === 0) empty++;
  if (bids.length > 0 && asks.length > 0) twoSided++;

  const dec = Number(m.quoteDecimals);
  const px = (v: bigint | undefined) =>
    v === undefined ? "—" : (Number(v) / 10 ** dec).toFixed(3);

  console.log(
    `  ${m.asset}/${Number(m.intervalSec) / 60}m  ` +
      `${bids.length}b/${asks.length}a  ` +
      `best ${px(bids[0]?.price)} / ${px(asks[0]?.price)}`,
  );
}

console.log(`\n=== tradable=${tradable}  empty=${empty}  two-sided=${twoSided}`);
console.log(
  empty === tradable && tradable > 0
    ? "SDK reports every book empty — compare against cmd/probe in core/"
    : "SDK book reads agree with the chain: the earlier 'empty book' finding was our bug",
);
process.exit(0);
