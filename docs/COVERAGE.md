# Coverage — the 97.9% we were refusing to quote

**Status:** measured, one cadence added, the rest still refused.
**Effect:** BTC/240m is now priced. ETH/240m, and everything at 1440m and
beyond, remains refused by the same measurement that admitted BTC/240m.
**Measured:** 2026-09-08, Shannon testnet indexer, chain, and oracle price feed.

---

## Summary

`internal/model` refused every cadence with no resolved venue history, and
[the README](../README.md) presented that refusal as a virtue:

> **We refuse to quote what we have not validated.** 240-minute and
> 1440-minute windows have no resolved history to fit, so the engine skips them
> rather than extrapolating √t four times further than it was tested.

As a refusal it was correct. The reason given for it was too strong, and the
cost of the overreach turned out to be most of the venue.

Two measurements, both reproducible by a command in this repository:

1. **`cmd/surface`** — a census of the live order book. The cadences the model
   refused carry **187 of 191 trades (97.9%)** and **~99% of quote volume**, and
   over 72 complete polls **411 of 429 legs (95.8%)** rested on a cadence the
   model had no opinion about.
2. **`cmd/volscale`** — σ measured from 30 days of M1 index candles using
   non-overlapping returns. Realised σ/min agrees with this repo's
   resolved-window fit **to within 5.3%**, and √t holds at a 240-minute
   aggregation for BTC.

The load-bearing error was treating *"no resolved venue history"* as
*"no evidence."* σ is a property of the index price process. The venue's
settlement log is one way to observe it, and not the only one.

---

## What the refusal cost

Live markets on 2026-09-08, from the indexer, grouped by cadence:

| Cadence | Trades | Quote volume | Engine status |
| --- | ---: | ---: | --- |
| ETH/1440m | 46 | 1.33e9 | refused |
| BTC/64800m | 37 | 2.92e9 | refused |
| ETH/64800m | 32 | 0.71e9 | refused |
| BTC/1440m | 30 | 1.11e9 | refused |
| ETH/240m | 22 | 0.83e9 | refused |
| BTC/240m | 20 | 0.76e9 | refused |
| ETH/60m | **3** | 0.06e9 | quoted |
| BTC/60m | **1** | 0.01e9 | quoted |

`cmd/surface` reads both sides of every live book at a single block and finds
the same shape in depth rather than in trade count:

```
     ETH/60m  left=1172s   bid=0.314 ask=0.342  bq=200.00     aq=200.00      fit=0.000506
     BTC/60m  left=1172s   bid=0.529 ask=0.559  bq=200.00     aq=200.00      fit=0.000370
   ETH/1440m  left=65972s  bid=0.349 ask=0.378  bq=200.00     aq=200.00      fit=0.000000
  ETH/64800m  left=3521972s bid=0.495 ask=0.505 bq=49980.62   aq=49975.00    fit=0.000000
  BTC/64800m  left=3521972s bid=0.495 ask=0.505 bq=49675.21   aq=49442.88    fit=0.000000
```

`fit=0.000000` is the model declining to have an opinion. Three of the five
two-sided books live at that moment were ones the engine would not quote.

That snapshot holds over a 72-poll census, every poll of which read every market
it discovered. It also shows where the depth actually is — and it is not where
the trades are:

| Series | legs seen | status | median depth/side |
| --- | ---: | --- | ---: |
| ETH/64800m | 72 | refused | **49,975** |
| BTC/64800m | 72 | refused | **49,443** |
| ETH/1440m | 72 | refused | 200 |
| BTC/1440m | 72 | refused | 25 |
| ETH/240m | 72 | refused | one-sided throughout |
| BTC/240m | 51 | **quoted** | 25 |
| ETH/60m | 9 | quoted | 200 |
| BTC/60m | 9 | quoted | one-sided throughout |

**Depth is not the argument, and it would be dishonest to present it as one.**
Nearly all of it sits on 64800m — ~250× the 60m books — and that cadence stays
refused below. The 240m books are *thinner* than the ones already quoted, at a
median of 25. Adding BTC/240m buys the right to price a cadence that trades, not
a deep one, which is why "priced, not proven" appears under open questions.

**181 of 429 legs were one-sided**, and an earlier version of this census
discarded them — which understated the refused share at 88.9% instead of 95.8%.
A one-sided book still prices a market, and a vertical trade needs only one side
of each leg, so dropping them threw away both the evidence and the very pairs
the census exists to count. Caught in review on
[PR #4](https://github.com/PrinceXDev/firstbid/pull/4), along with four other
defects in the tool — including per-market read failures that let a partial poll
print a definitive verdict. `cmd/surface` now records completeness per poll and
reports `INCONCLUSIVE` rather than `KILLED` when it cannot read what it found.
The figures above are the corrected measurement, from 72 polls that each read
every market they discovered.

Note the last two rows. Both are quoted `0.495/0.505` — flat, at the coin flip —
while BTC sat **1.15% below** its strike and ETH **1.1% above** its. Whatever
those quotes are, they are not a function of moneyness.

**The venue also reshaped under us.** The README's problem statement is built on
~770 markets a day of which 19.2% ever trade, measured 2026-09-03. Five days
later there are **8 live markets**, long-dated and heavily traded. That figure
is not wrong, it is stale, and any thesis resting on a wide thin cross-section
should be re-checked before it is believed.

---

## The measurement that changed the answer

`cmd/volscale` measures σ/min from M1 index candles at several aggregation
horizons. Two rules make it a test rather than a formality:

- **Non-overlapping returns only.** Overlapping returns multiply *n* by *k*
  while adding almost no independent information. Their standard deviation looks
  reassuringly tight and is not — the same family of mistake as reading a price
  59 seconds after the moment it claims to describe.
- **Population σ about zero, not about the sample mean.** The pricing formula is
  explicitly driftless. Subtracting a fitted mean would remove realised drift
  the formula assumes is absent, and understate the dispersion it must survive.

```
$ go run ./cmd/volscale -days=30

BTC -- 43028 candles spanning 30.0 days
  realised sigma/min (1m, n=43023) : 0.000530
  venue-fitted sigma/min (15m)     : 0.000504   ratio 1.053

  horizon         n    sigma/min       dev  verdict
       1m    43023     0.000530     +0.0%  already quoted
       5m     8603     0.000550     +3.7%  already quoted
      15m     2866     0.000517     -2.6%  already quoted
      60m      715     0.000552     +4.0%  already quoted
     240m      177     0.000589    +11.1%  QUOTABLE - sqrt(t) holds
    1440m       27     0.000695    +31.1%  REFUSE - n<30

ETH -- 43028 candles spanning 30.0 days
  realised sigma/min (1m, n=43023) : 0.000718
  venue-fitted sigma/min (15m)     : 0.000681   ratio 1.055

  horizon         n    sigma/min       dev  verdict
     240m      177     0.000849    +18.2%  REFUSE - sqrt(t) breaks by 18%
    1440m       27     0.000959    +33.4%  REFUSE - n<30
```

**Two independent estimators agree.** Realised 1-minute σ from price history is
within 5.3% (BTC) and 5.5% (ETH) of σ fitted from resolved venue windows. Those
share no data and no code path. Agreement is the licence for everything below;
disagreement would have meant the venue fit was measuring something that is not
the diffusion of this price series, and this document would not exist.

**The tool refused half of what it tested.** BTC/240m deviates 11.1% and passes;
ETH/240m deviates 18.2% on the same 30 days, at the same *n*, and fails. A
measurement that blessed both would be a rubber stamp.

---

## What was changed

One entry in `internal/model.fitted`:

```go
{"BTC", 14400}: {Asset: "BTC", SigmaPerMin: 0.000560, N: 177, Source: SourceHorizonScaled},
```

**Only the ratio is imported, never the absolute level.** `Calibration = 0.715`
was fitted by `cmd/backtest` to correct *resolved-window* σ on the 5m–60m
cadences. Handing it a realised-candle σ would correct the number twice — the
same hazard that let the look-ahead bug validate itself for days. So the 240m
value is this repo's own resolved-window anchor scaled by the horizon ratio
`cmd/volscale` measured:

```
0.000504 (BTC/900, resolved windows) × 1.111 (240m / 1m, price history) = 0.000560
```

That keeps the existing calibration chain intact and imports only the part the
new measurement actually validated: how σ moves *between* horizons.

`Vol` gained a `Source` field, because the table now holds two kinds of
evidence and `N` means different things for each — resolved windows for one,
independent price returns for the other. Provenance travels with the number for
the same reason `SpotObs` carries an observable-at time: the expensive bug was
not a wrong value, it was a value whose origin nobody had written down.

The sign is checked, not assumed. The deviation is **positive**: realised 240m
moves are larger than √t from the 15m fit implies, so this σ yields *less*
confident probabilities than a naive extrapolation would. Under-confidence
forgoes trades; overconfidence cost 37% of deployed capital.

### It prices

```
$ go run ./cmd/edge

series      t-left       spot       open    fair     unc  book           spread  verdict
BTC/240m     6954s   78587.49   78897.13   0.181   0.012  0.153/0.000         —  one-sided
```

An hour earlier that row did not exist: `SigmaPerMin` returned `!ok` and the
window was skipped before a price was ever computed.

### Four tests hold the line

| Test | What it prevents |
| --- | --- |
| `TestOnlyValidatedCadencesAreCalibrated` | BTC/240m quotable; 60s, 1440m, 64800m still refused |
| `TestHorizonScalingIsNotBlanketPermission` | ETH/240m appearing without its own passing measurement |
| `TestEveryFittedEntryDeclaresItsProvenance` | an entry added without saying where its number came from |
| `TestHorizonScaledSigmaExceedsItsAnchor` | horizon scaling ever *increasing* confidence on the cadence with the least evidence |

---

## What is still refused, and why

| Cadence | Evidence available in 30 days | Verdict |
| --- | --- | --- |
| ETH/240m | n=177, √t deviates +18.2% | refused — the scaling does not hold |
| BTC/1440m, ETH/1440m | n=27 independent 24-hour returns | refused — below where regime is separable from noise |
| BTC/64800m, ETH/64800m | under **one** independent 45-day return | refused — no measurement is possible |

The 64800m cadence rests the deepest book on the venue and is still refused.
It is not short of a model; it is short of evidence. Extending coverage further
needs a longer sample, not a looser tolerance.

---

## What is still open

**The census killed a better-sounding idea first, and that is why it exists.**
`cmd/surface` was built to test whether simultaneous windows on one underlying
are priced consistently with each other — a same-expiry strike ladder must be
monotone in the strike under *every* probability measure, so a crossed ladder
would be riskless profit and would not require the model to be right at all.
It found **zero same-expiry pairs across 72 complete polls**, spanning the
top-of-hour boundary that is the one moment a 15m and a 60m window can
coincide — and zero crossed books, so the intra-market box was not available
either. Expiries coincide only at alignment
boundaries, and with 8 live markets there is no cross-section to arbitrage. The
tool prints that verdict itself and names the surviving idea; the thesis cost
two commands rather than two days.

**The flat quotes may not be traders.** Books resting ~50,000 units at exactly
`0.495/0.505` on both assets, with modest trade counts against very large
volume, are consistent with seeded venue liquidity rather than participants.
Nothing here should be read as edge against a counterparty who will actually
take the other side.

**BTC/240m has no live P&L.** The cadence is priced and gated; it has not been
quoted live long enough to attribute anything. Coverage is a claim about what
the model may honestly price, not yet a claim that pricing it made money.

**The agreement between estimators is unconditional.** Realised σ matching
fitted σ over 30 days says the two describe the same process on average. It does
not say they agree in the regime a given window falls in, and a maker only ever
trades the windows where someone disagrees with it.

---

## Why this is in the repository

The README lists four theses killed by data. This is the first one killed by
data and then partly *revived* by better data — the refusal was right, its
stated reason was not, and the difference was worth 97.9% of the venue's trades.

Refusing to quote what you have not validated is a discipline. Refusing to look
for evidence anywhere except the one place you first looked is not the same
thing, and it is much easier to mistake for rigour.
