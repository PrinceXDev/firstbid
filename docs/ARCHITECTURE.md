# Architecture

Firstbid is a single Go module. No Node, no JavaScript runtime, no SDK at
runtime — the contracts are driven directly from the ABIs DreamDEX publishes.

---

## Why pure Go

The TypeScript SDK is the documented surface, and we started there. Two things
pushed us off it:

1. A quoting engine wants one goroutine per market window with a hard deadline,
   and exactly one serialised writer. Go models that in the type system, and the
   lifetime of a `context.WithDeadline` maps exactly onto the lifetime of a
   trading window.
2. It was a deliberate choice by the project owner, made with the cost known.

An earlier version of this document gave a third reason: that the SDK's
order-book read returned empty for live markets. **That was our own bug** — we
read `book.bids` on a type that exposes `yesBids`/`yesAsks`. The SDK is fine.
See the retraction at the top of `docs/SDK-FEEDBACK.md`.

The honest summary is that pure Go was a preference rather than a necessity. It
cost roughly two days and produced something the ecosystem did not have: a
working Go client for Event Contracts.

The cost was real — roughly two days rebuilding grid snapping, revert decoding
and the read layer — and the docs explicitly invite it ("Working from a non-JS
stack? …exports the ABIs you need directly"). The byproduct is
`internal/venue`: the first Go client for Event Contracts.

---

## Data sources, and which one is truth

| Need | Source | Why |
| --- | --- | --- |
| Which windows exist | indexer GraphQL | Only place that enumerates markets |
| Market status, pool, token ids, expiry | **chain** | Indexer status lags by seconds; an order to a Locked market reverts |
| Order book | **chain** (`getBookLevels`) | One pinned block for both sides, so a crossed book cannot be an artifact of two reads |
| Tick / lot / min size | **chain** | Indexer returns `null` for binary markets |
| Our resting orders | **chain** (`getOwnOpenOrders`) | Ids parsed from receipts proved unreliable |
| Index spot price | price-feed GraphQL | ~1.8s fresh |
| Opening price ("the line") | indexer → oracle answer | `strike` is 0 on reference-mode markets |

**Rule: discover off-chain, decide on-chain.** Every write is gated on a
chain read taken in the same pass.

---

## Process shape

```
  price feed ──▶ pollSpots ──┐ one shared snapshot, 2s
                             │ (N markets cost 1 request, not N)
  indexer ──▶ supervisor ────┤ 15s discovery sweep
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
   marketLoop           marketLoop           marketLoop
   BTC/5m               ETH/15m              BTC/60m
   ctx deadline =       ctx deadline =       ctx deadline =
   window expiry        window expiry        window expiry
        │                    │                    │
        └────────── intents (buffered 256) ───────┘
                             │
                             ▼
                    ONE executor goroutine
                    (serialised · nonce-safe)
                             │
                             ▼
                    Somnia Shannon (50312)
```

### One goroutine per window

A market is a finite state machine that is born and dies:

```
Listed(0) → Trading(1) → Locked(2) → Resolved(4) | Voided(5)
```

A goroutine scoped by `context.WithDeadline(expiry)` **is** that state machine.
Inventory, working orders and quote state are goroutine-local, so they are not
shared state and most concurrency bugs cannot occur. When the window ends the
context fires, the loop returns, and a `defer` reaps it.

The alternative — one sweep loop over all markets — has to pick a single tick
rate. Cadences from 5 to 1440 minutes run simultaneously; a rate that serves a
5-minute window burns RPC on a 24-hour one, and a rate that suits the 24-hour
window is useless near a 5-minute expiry, which is exactly where the edge is.

### Exactly one writer

One signing key means one nonce sequence. N goroutines may **decide**
concurrently; only one may **write**. Intents flow over a buffered channel to a
single executor goroutine that owns every transaction. The trader adds a second
guard (a 1-slot semaphore) so the invariant holds even if the engine is embedded
elsewhere.

### Settlement is watched, never poked

Resolution on Somnia is keeper-free: the oracle answer is delivered into the
module's callback by chain reactivity. When a window closes we detach a watcher
with `context.WithoutCancel` — the window's own context has just been cancelled,
but settlement happens *after* expiry — and poll until the market reports
resolved or voided, then write the outcome to the ledger.

---

## Packages

```
internal/venue     pure-Go DreamDEX client
  client.go        chain reads, block-pinned book, revert decoding
  trader.go        signing, nonce, grid snapping, receipt decoding
  indexer.go       market discovery
  pricefeed.go     index prices + opening prices
  calib.go         builds the calibration dataset
  abis/            36 ABIs extracted from the SDK, embedded

internal/model     calibrated fair value (no direction, ever)
internal/maker     quoting, risk gates, supervisor, executor
internal/ledger    SQLite P&L attribution
```

`internal/model` has no dependency on `venue` — it is pure arithmetic over
floats, which is why it is exhaustively unit-testable without a chain.

---

## Correctness decisions worth naming

These each came from a bug that reached running code:

| Decision | The bug it prevents |
| --- | --- |
| Read both book sides at **one pinned block** | Two unpinned calls straddle a block and show a bid above the ask — a crossed book that never existed |
| Cancel from `getOwnOpenOrders`, not receipt ids | Receipt-parsed ids resolved to orders we did not own (`IncorrectSender`) |
| Approve **per pool**, lazily | Pools are per-window; a global approval does not exist |
| Cap order expiry at market expiry | `OrderExpiryBeyondMarket` on any cadence shorter than the docs' example |
| Store Down cost as `1 − yesPrice` | The wire price is YES terms; storing it raw inflated reported edge ~30× |
| Refuse uncalibrated cadences | Extrapolating √t from 60m to 1440m is a guess, and a maker that guesses pays |
| Anchor oracle scale to live spot | `numericValue` scale varies per question (1e2 and 1e4 both observed) |
| Dashboard calls `maker.Compute` | A dashboard that reimplements the strategy eventually lies about it |

---

## What we deliberately did not build

- **Postgres, Redis, workers, a queue.** At ~150 fills a day venue-wide this
  would be architecture theatre. SQLite means `git clone && go run` works on a
  judge's laptop with no infrastructure.
- **Our own indexer or WebSocket layer.** Polling at 2s is well inside the
  venue's tempo and has no reconnect semantics to get wrong.
- **An LLM anywhere in the trade path.** The decision is a closed-form
  probability. A language model would be slower, non-deterministic, and worse.
- **A custody vault.** Pooled deposits are the obvious next step, but unaudited
  custody written in a week is a liability, not a feature.
