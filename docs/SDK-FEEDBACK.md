# Event Contracts — SDK & Documentation Feedback

Submitted as part of the Somnia × DreamDEX Event Contracts Hackathon.

Every item below was hit while building **Firstbid**, a pure-Go market maker for
Event Contracts. Each one is reproducible, and each cost real debugging time.
They are ordered by how much damage they can do silently.

Environment: `@somnia-chain/markets-sdk` 0.29.0 · Somnia Shannon (50312) and
mainnet (5031) · measurements taken 2026-09-03.

---

## 1. `getBinaryOrderBook()` returns an empty book while the pool has depth

**Severity: high — silently wrong, not an error.**

Reading the same live markets at the same moment through two paths gives
contradictory answers:

| Read path | Result across 12 live markets |
| --- | --- |
| `client.getBinaryOrderBook(pool)` (documented) | **12/12 completely empty** |
| `BinaryPool.getBookLevels(isBid, n)` (on-chain) | **11/12 two-sided, 2–4 levels a side, ~0.028 spread** |

Reproduction: take any market from `listLiveBinaryMarkets`, gate on
`getMarketOnchain().status == 1`, then read the book both ways.

Why it matters: the SDK is the only supported surface for Event Contracts (the
HTTP API is spot-only), so anything built on the documented path renders an
empty market and cannot price, quote or take. An application looking at a dead
book has no way to tell that the venue is actually liquid.

**Suggested fix:** either back `getBinaryOrderBook` with the pool's
`getBookLevels`, or document that it reflects indexer-materialised state which
is not populated for binary pools, and point callers at the chain read.

---

## 2. The oracle's `numericValue` scale varies between questions

**Severity: high — a hardcoded divisor misprices every order and nothing reverts.**

Sampling recent `OracleAnswer` rows on mainnet:

```
qid=50683  numericValue="777620000"  outcomeLabel=">= 77630.9413"   -> 1e4 scale
qid=50709  numericValue="7771450"    outcomeLabel=">= 0.00"          -> 1e2 scale
qid=50710  numericValue="240369"     (ETH)                           -> 1e2 scale
```

There is no documented field carrying the scale, and `outcomeLabel` only hints
at it when the threshold is non-zero. A consumer that assumes one exponent will
compute an opening price that is 100× off, produce a fair value of 0 or 1, and
quote confidently into a loss — with no revert and no error.

**Workaround we shipped:** anchor the oracle value to a live index price and
rescale by powers of ten until the two agree within a 3× band. Safe only because
no supported asset moves 3× inside one window.

**Suggested fix:** expose `decimals` (or a fixed, documented scale) on
`OracleAnswer` / `OracleQuestion`.

---

## 3. The documented NO-side price convention is the inverse of the behaviour

**Severity: high — produces mirror-image quotes that look plausible.**

The Recipes page says:

> `price: ticks(0.05), // always in YES terms: a NO price is ONE - ticks(p)`

Read naturally, that says a NO order's price field should be `1 - p`. Empirically
it is not. Placing a `BUY_NO` with the price field set to `0.123`:

```
before: bids[]  asks[]
place BUY_NO, price field = 0.123
after : bids[]  asks[0.123]
```

The order rests as an **ask at 0.123 in Up terms**. So the price field is plain
YES terms for all four order kinds, and the NO cost is `1 - price`. We had
implemented the documented reading and rested every Down quote at its mirror
price before catching this with the probe above.

**Suggested fix:** state it as "the price field is in YES terms for all four
kinds; buying NO at cost `d` means submitting `price = 1 - d`", with one worked
example per kind.

---

## 4. The pool *read* ABI is not exported, though non-JS stacks are invited

**Severity: medium — blocks the documented non-JS path.**

Contracts & Addresses says:

> Working from a non-JS stack? `@somnia-chain/markets-sdk` exports the ABIs you
> need directly (`binaryModuleReadAbi`, `binaryModuleWriteAbi`, …)

But the package index exports no **pool read** ABI. `binaryPoolReadAbi` — which
carries `getBookLevels`, `getOrderBookParameters`, `getBinaryPoolParams`,
`getOwnOpenOrders` and `getOrder` — lives in `dist/readsAbi.js` and is not
re-exported. Without it a non-JS client cannot read the book, the tick/lot grid,
or its own resting orders: precisely the reads a bot needs most.

We recovered it by importing the internal module directly, which is not a
contract we should be relying on.

**Suggested fix:** re-export `binaryPoolReadAbi` and `binaryMarketReadAbi` from
the package root alongside the write ABIs.

---

## 5. The indexer returns `null` grid parameters for binary markets

**Severity: medium.**

`Market.tickSize`, `Market.lotSize` and `Market.minQuantity` are `null` on every
binary row we queried, on both networks, so the grid must be read from the pool
contract (`getOrderBookParameters`). Since sizing below one lot floors silently
to zero, a caller who trusts the indexer here sends orders that never appear.

**Suggested fix:** populate these on the indexer, or document that binary markets
must read them from the pool.

---

## 6. The documented order expiry example exceeds short windows

**Severity: low — clear revert, easy to hit.**

Gotchas suggests:

```ts
expireTimestampNs: BigInt(Math.floor(Date.now() / 1000) + 300) * 1_000_000_000n
```

On 1-minute and 5-minute cadences (both live on testnet) a 300-second expiry is
routinely past the market's own, and the pool rejects with
`OrderExpiryBeyondMarket()`. The correct rule is `min(now + requoteInterval,
marketExpiry - epsilon)`.

**Suggested fix:** show the capped form in the example, since the venue runs
cadences shorter than the constant used.

---

## 7. Documented cadences are behind what the venue actually runs

**Severity: low — affects anyone hardcoding cadences.**

Docs describe "BTC and ETH markets on 15-minute and 1-hour windows". Live we
observed **1m, 5m, 15m, 60m, 240m and 1440m**, plus occasional irregular
intervals (e.g. 298s, 409s) and at least one non-BTC/ETH asset on testnet.

The 1-minute cadence is genuinely useful — a full lifecycle fits in 60 seconds,
which makes demos and integration tests dramatically easier. Worth advertising.

---

## 8. Offset pagination on the indexer degrades badly

**Severity: low — performance.**

`OutcomeBalance` and `Fill` with `offset` become very slow past a few thousand
rows; a 120-page sweep timed out well before reaching recent data, silently
returning an old slice rather than an error. Keyset pagination on `id` is flat by
comparison and is what we ended up using everywhere.

**Suggested fix:** recommend keyset pagination in the docs for the large tables.

---

## What worked notably well

Not everything was friction, and some of this deserves saying:

- **The settlement rail is excellent.** Zero voided markets across ~800 sampled
  windows on two networks, and 99.4% of winning positions redeemed (25,496 of
  25,660). The keeper-free reactivity callback plus the two permissionless
  backstops is a genuinely good design.
- **`contractErrorsAbi` with 500 error signatures is superb.** Decoding reverts
  turned "execution reverted" into `OrderExpiryBeyondMarket()`,
  `PostOnlyWouldCross()`, `SelfMatchCancelTaker()` and
  `IncorrectSender[a, b]` — each of which pointed straight at the bug. More
  projects should ship this.
- **`getOwnOpenOrders()` on the pool is the right primitive.** Tracking order ids
  out of receipts proved unreliable; asking the pool what we still have resting
  was correct every time.
- **Mint-a-pair genuinely solves cold start.** 22–42% of observed fills were
  `MINT_A_PAIR`, so two opposite-side buyers really do cross with no seller. It
  also means a maker can quote both sides with zero inventory, which is the
  mechanism our whole strategy rests on.
- **Addresses being identical across testnet and mainnet (CREATE3)** removed an
  entire category of configuration error.
