# Autopsy — 59 seconds of borrowed future

**Status:** cause found, fixed, and regression-tested.
**Cost:** −37.3% of deployed capital across 53 fills.
**Measured:** 2026-09-03, mainnet indexer and oracle price feed.

---

## Summary

Firstbid's fair-value model is a driftless-diffusion estimate with one scale
parameter, σ. That parameter was fitted by `cmd/backtest`, which replayed
resolved windows and chose the σ multiplier minimising Brier score. It reported
**k = 0.530** and an out-of-sample skill of **+58.7%**, with reliability that
tracked the diagonal in every decile.

Both numbers were artifacts of a bug in the replay, not properties of the model.

`internal/venue/pricefeed.go` indexed M1 candles by the minute each bucket
**opened**, and stored the bucket's **close**:

```go
s[t/60] = v / 1e18              // key: bucket start. value: bucket close.

func (s SpotSeries) At(t int64) (float64, bool) {
    m := t / 60
    for back := int64(0); back <= 5; back++ {
        if v, ok := s[m-back]; ok { return v, true }   // the bucket CONTAINING t
    }
}
```

A decision timestamped 09:41:02 was therefore priced with the close of the
09:41:00–09:42:00 candle — a price observed 58 seconds **after** the moment the
model claimed to predict. On a 5-minute window sampled at 40% elapsed, 180
seconds remain and up to 59 of them were already known: roughly a third of the
diffusion the model exists to forecast had already happened.

With part of the future known, less variance remains to explain, so the fitter
chose a σ that was far too small. That is where k = 0.530 came from, and the
resulting model was badly overconfident — most severely in the tails. The take
rule fires on confidence, so every live fill landed in the worst-calibrated
region of the surface.

**The bug validated itself.** The σ table had 0.530 baked into it and the fitter
multiplied a second time, so re-running the backtest reported k = 0.980 —
"nothing further to correct."

---

## What it cost

`core/run.db`, 53 fills across 11 markets on Shannon testnet:

| | |
| --- | ---: |
| Capital deployed | 95.76 |
| **Edge** — Σ (fair − price) × qty | **+5.68** |
| **Selection** — Σ (value − fair) × qty | **−41.44** |
| **Net** | **−35.76**  (−37.3%) |

Edge was positive on every fill: the engine really did buy below its own stated
fair value. Selection is supposed to be variance, averaging toward zero. At a
claimed 0.975 it should have been near zero; instead it was −41.44 across 11
independent markets.

The attribution split did its job. It said the *belief* was wrong, not the
execution — which is the only reason the cause was findable at all.

Every one of the 53 fills had a model fair value ≥ 0.70, and 46 of them ≥ 0.90.
Predicted 0.975; realised 0.609.

---

## The measurement

Replaying the identical model against the 1-second `PricePoint` feed the live
engine actually polls — instead of M1 candles — gives a different answer.

**Out-of-sample skill**

| Replay series | n | Skill vs coin flip |
| --- | ---: | ---: |
| M1 candles, with the leak | 7,586 | +58.7% |
| 1-second feed, causal | 5,353 | **+45.7%** |

**Out-of-sample tail reliability (p ≥ 0.9)**

| Replay series | Predicted | Realised | Error | n |
| --- | ---: | ---: | ---: | ---: |
| M1 candles, with the leak | 0.982 | 0.975 | −0.008 | 1,903 |
| 1-second feed, causal | 0.978 | 0.966 | −0.011 | 955 |

The leaked backtest did not merely overstate skill by 14 points. It hid *where*
the model was wrong. Conditioning the tail on window progress — using the
overconfident k = 0.530 the engine actually shipped — shows the error
concentrating exactly where the engine traded:

| Regime (p ≥ 0.9, k = 0.530 as shipped) | n | Predicted | Realised | Error |
| --- | ---: | ---: | ---: | ---: |
| 0–25% elapsed | 97 | 0.949 | 0.876 | −0.073 |
| **25–50% elapsed** | 497 | 0.966 | 0.853 | **−0.112** |
| 50–75% elapsed | 968 | 0.976 | 0.918 | −0.057 |
| 75–100% elapsed | 1,443 | 0.989 | 0.980 | −0.009 |
| 5-minute cadence | 1,969 | 0.980 | 0.933 | −0.047 |
| 60-minute cadence | 186 | 0.975 | 0.849 | −0.125 |
| **p ≥ 0.9, 25–50% elapsed, 5m** — every live fill | 309 | 0.964 | 0.819 | **−0.146** |

Late-window predictions, where the model is nearly honest, were never traded:
the book agrees with the model there, so no edge appears. The take rule
systematically selected the cells where the model was worst.

---

## Two hypotheses ruled out first

Both were plausible, and checking them was cheaper than assuming.

**1. Feed–oracle basis.** If the tradable index and the settlement oracle were
different series, tail probabilities would be unidentifiable from the feed.
Measured across 270 resolved windows, `ln(feed / oracleClose)` has a standard
deviation of **0.98 bps** (BTC) and **1.55 bps** (ETH). Against a model
denominator of 4–8 bps late in a window, that inflates total uncertainty by
1–9% — real, but an order of magnitude too small to explain a 15-point
calibration error. *Not the cause.*

**2. Double-applied calibration.** The σ table baked in 0.530 and `fitK`
multiplied again, so a naïve reading suggests an effective 0.281. A fresh fit
returns 0.980, so it does not compound. The tooling was genuinely misleading and
is now fixed — raw σ lives in the table, `Calibration` is applied in exactly one
place — but *not the cause.*

---

## The fix

**1. `SpotSeries` is strictly causal.** It is no longer a map keyed by minute.
Every observation carries the wall-clock time at which it became *observable* —
for an M1 candle, `bucketStart + 60`, because a close is not knowable until the
bucket ends — and `At(t)` binary-searches for the last observation stamped at or
before `t`. Look-ahead is now a property of the type, not a convention.

**2. The backtest replays what the engine trades.**
`go run ./cmd/backtest -feed=points` replays the same `PricePoint` table
`Spots()` polls live, so replay and production cannot disagree about what was
knowable when. `-feed=candles` is kept for cross-checking.

**3. Regression tests.** `internal/venue/pricefeed_test.go` asserts that a
candle covering [0,60) is unavailable at t = 59 and available at t = 60, plus a
brute-force causal invariant over randomised irregular arrivals. The original
implementation fails all of them.

**4. σ refitted honestly: k = 0.715.** Fitted on the older half of 10,610
predictions over 2,173 resolved windows and scored on the newer half it never
saw. Out-of-sample skill +45.7%, no decile off by more than 0.032, tail
0.978 → 0.966.

**4b. A calibration map, built and then rejected.** Under the old k = 0.530 the
honest curve was strongly S-shaped, and no scalar can change a curve's shape — so
`internal/model/calibmap.go` fits a monotone isotonic map from raw probability to
observed frequency, on the older half, scored on the newer half.

It failed that test. With the look-ahead gone and σ refitted, the S-shape largely
disappeared, and the map made calibration **worse**: Brier 0.1357 raw against
0.1368 mapped over 5,353 held-out predictions. Twelve knots of 446 observations
each were fitting the training half's noise. `fittedKnots` is therefore empty,
`CalibrationMap()` is the identity, and `cmd/backtest` prints
`the map does NOT improve out of sample; do not ship it` on every run.

The machinery stays. It is the right tool if a larger sample ever shows real
curvature, and the negative result is worth keeping in the repository: a
correction has to earn its place out of sample, and this one did not.

**5. The take rule clears the model's measured error.**
`model.CalibrationError = 0.045` is the largest out-of-sample decile error at the
shipped σ. `ShouldTake` now requires an edge exceeding
`max(TakeEdge, uncertainty, calibrationError)`, and `model.EdgeFloor(p)` widens
that with the map's residual wherever a validated map is ever shipped.

The losing trade was a 0.03 edge on a belief whose own error exceeded it. **An
edge smaller than the model's error is not an edge** — and the old 0.020
threshold was never a threshold at all.

**6. A per-window take budget.** `ShouldTake` consulted neither inventory nor
history, so a book that stayed mispriced was crossed on every tick: eight fills
on one belief in a single BTC/5m window. Takes inside a window are perfectly
correlated — they settle against the same outcome — so N takes on one belief is
one bet at N times the size. `internal/maker/budget.go` caps takes per window,
notional per window, and the interval between them, and logs every refusal as a
`TAKE REFUSED` line with its reason.

---

## What this changes about the claims

| Claim | Before | After |
| --- | --- | --- |
| Out-of-sample skill | +59.5% | **+45.7%** |
| σ multiplier | 0.530 | **0.715** |
| Tail reliability (p ≥ 0.9) | 0.982 → 0.975 | 0.977 → 0.966 |
| Worst out-of-sample decile error | not reported | **0.033** |
| Replay series | M1 candles | the 1s feed the engine trades |
| Minimum edge to cross | 0.020 | **0.045**, from the measured error |
| Effective sample of the live run | reported as 53 fills | **11 markets** |

That last row matters as much as the others. Fills inside one window are one
observation, not many. Reporting 53 made a broken model look merely unlucky.

---

## What is still open

- **The corrected engine has not yet been run live.** Everything above is
  replay and unit tests. The before/after attribution pair does not exist yet.
- **k is regime-dependent.** 30 days of candles fit 0.635; 4 days of the 1s feed
  fit 0.710. We ship the higher, conservative end, but a single scalar is
  evidently doing work that a regime-aware sigma should do.
- **The edge floor is a single measured bound, not a per-probability curve.**
  0.045 is the worst decile error across two replay series (4 days of the 1s
  feed, 30 days of M1 candles) for BTC and ETH at 5/15/60-minute cadences; a
  bigger sample may justify something finer. Cadences without
  enough resolved history are still refused outright rather than extrapolated.
- **Calibration conditional on book disagreement is unmeasured.** Unconditional
  reliability does not license crossing a spread: the taker only ever trades the
  subset where the book disagrees, and disagreement is itself evidence the
  counterparty knows something. The indexer's `Order` history makes this
  measurable and it is the next piece of work.

---

## Why this is in the repository

Because the alternative was to ship the inflated number and hope nobody checked.

The model has genuine out-of-sample skill: +45.7% against a coin flip, measured
on data the fit never saw, on the same feed the engine trades. What it does not
have is the skill originally claimed, and the difference was a bug we wrote
ourselves.

Firstbid's stated method is that theses get killed by data rather than defended.
This is that method applied to its own headline result.
