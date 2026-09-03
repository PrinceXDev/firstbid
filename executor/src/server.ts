import express from "express";
import { exchange, client, hasSigner, serialise, jsonSafe } from "./venue.js";

const app = express();
app.use(express.json());

const wrap =
  (fn: (req: express.Request, res: express.Response) => Promise<unknown>) =>
  async (req: express.Request, res: express.Response) => {
    try {
      res.json(jsonSafe({ ok: true, data: await fn(req, res) }));
    } catch (err) {
      // Reverts arrive decoded from 0.23.0 onward. Surface the name so Go can branch on it.
      const message = err instanceof Error ? err.message : String(err);
      res.status(400).json({ ok: false, error: message });
    }
  };

/** Live, on-chain-gated markets with everything Go needs to quote them. */
app.get(
  "/markets",
  wrap(async (req) => {
    const minSecs = Number(req.query.minSecs ?? 0);
    const rows = await client.listLiveBinaryMarkets({ limit: 60 });
    const now = Date.now() / 1000;
    const out = [];

    for (const m of rows) {
      const onchain = await client.getMarketOnchain(m.marketId as `0x${string}`);
      if (onchain.status !== 1) continue;              // 1 = Trading. Indexer lags; chain does not.
      const secondsLeft = Number(m.expiry) - now;
      if (secondsLeft < minSecs) continue;

      const params = await client.getBinaryBookParams(onchain.pool);
      out.push({
        marketId: m.marketId,
        asset: m.asset,
        intervalSec: Number(m.intervalSec),
        tradingStart: Number(m.tradingStart),
        expiry: Number(m.expiry),
        secondsLeft,
        pool: onchain.pool,
        marketAddress: onchain.marketAddress,
        outcomeToken: onchain.outcomeToken,
        yesId: onchain.yesId,
        noId: onchain.noId,
        upSymbol: m.outcomes?.[0]?.symbol ?? null,
        downSymbol: m.outcomes?.[1]?.symbol ?? null,
        collateralDecimals: Number(m.quoteDecimals),
        tickSize: params.tickSize,
        lotSize: params.lotSize,
        minQuantity: params.minQuantity,
        tradeCount: Number(m.tradeCount ?? 0),
        lastPrice: m.lastPrice ?? null,
        oracleQuestionId: m.oracleQuestionId ?? null,
      });
    }
    return out;
  }),
);

app.get("/book/:pool", wrap(async (req) => client.getBinaryOrderBook(req.params.pool as `0x${string}`)));

app.get(
  "/market/:marketId",
  wrap(async (req) => client.getMarketOnchain(req.params.marketId as `0x${string}`)),
);

/** Place one order. Go sends human units; the SDK snaps to the venue grid (>=0.28). */
app.post(
  "/orders",
  wrap(async (req) => {
    const { symbol, side, size, price, postOnly, ioc } = req.body as {
      symbol: string; side: "buy" | "sell"; size: number; price: number;
      postOnly?: boolean; ioc?: boolean;
    };
    return serialise(async () => {
      const order = await exchange.createOrder(symbol, "limit", side, size, price, {
        ...(postOnly ? { postOnly: true } : {}),
        ...(ioc ? { timeInForce: "IOC" as const } : {}),
      });
      return { id: order.id, filled: order.filled, amount: order.amount, info: order.info };
    });
  }),
);

app.delete(
  "/orders/:id",
  wrap(async (req) =>
    serialise(() => exchange.cancelOrder(req.params.id, String(req.query.symbol))),
  ),
);

app.get("/orders", wrap(async (req) => exchange.fetchOpenOrders(String(req.query.symbol))));

/** Outcome-token balances: ERC-6909 ids on one shared contract, read by id. */
app.get(
  "/positions/:marketId",
  wrap(async (req) => {
    const oc = await client.getMarketOnchain(req.params.marketId as `0x${string}`);
    const me = exchange.walletAddress;
    if (!me) throw new Error("no signer configured");
    return {
      up: await client.getOutcomeBalance(oc.outcomeToken, me, oc.yesId),
      down: await client.getOutcomeBalance(oc.outcomeToken, me, oc.noId),
      status: oc.status,
      isResolved: oc.isResolved,
      isVoided: oc.isVoided,
      winningOutcome: oc.winningOutcome,
    };
  }),
);

app.post(
  "/faucet",
  wrap(async () => serialise(() => exchange.trader.faucet())),
);

app.get(
  "/health",
  wrap(async () => ({
    signer: exchange.walletAddress ?? null,
    hasSigner,
    sync: await client.getSyncStatus(),
  })),
);

const port = Number(process.env.FIRSTBID_PORT ?? 8787);
app.listen(port, () => {
  console.log(`[executor] venue adapter on :${port}  signer=${exchange.walletAddress ?? "none"}`);
});
