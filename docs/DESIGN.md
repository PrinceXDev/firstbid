# Firstbid — Product & Interface Design

> Design document. Written before implementation, deliberately.

---

## 0. The constraint that shapes everything

Most hackathon UIs are built on mock data. Ours cannot be, and this is not a
matter of taste.

This project's entire credibility rests on measured, verifiable facts: 10,209
settled markets, a model validated out-of-sample, chain-verified P&L. The
moment the interface shows a fabricated number, a judge who asks one question
discovers it, and everything else we claim becomes suspect.

**So: every number in this interface is real, or it is explicitly labelled as a
replay of something that was real.** Demo Mode replays *recorded live sessions*,
never synthetic data. That is a harder constraint than mocking, and it is the
reason the product will feel different.

It also means the interface has to be honest about our current result. The first
live run lost 37% of deployed capital, and the interface has to say so.

**Note, 2026-09-03:** an earlier draft of this document diagnosed that loss as
adverse selection. That was wrong. Edge stayed positive while selection went to
−41.44, which is the signature of a *mistaken belief*, not of being picked off —
and the mistaken belief traced to a look-ahead bug in our own backtest that made
the model overconfident. See [`AUTOPSY.md`](AUTOPSY.md).

The correction is kept here rather than edited away, because it is the same
mistake the interface is designed to prevent a user from making: reading a
confident number without its error bar, and inventing a story to explain it.

---

## 1. Product concept

### The one-sentence version

**Firstbid is an instrument for watching a probability get decided — and for
seeing exactly where a trading edge survives or dies.**

### What it is not

It is not a place to bet on Bitcoin. There are many of those, and the data says
they are selling a coin flip: across 10,209 settled DreamDEX windows, P(Up) =
**0.4978**. Direction is not predictable. Any interface whose main verb is
"pick a side" is decoration on a 50/50.

### What it is

Every event contract is a **probability with a deadline**. Over 15 minutes it
travels from 0.50 to either 0 or 1, and the path it takes is the actual
information. Firstbid makes that path legible:

- what the market thinks (the book)
- what a calibrated model thinks (fair value)
- how much of the gap is real signal versus noise (uncertainty)
- what happened when we acted on the gap (attribution)

The product's spine is **belief → action → outcome → attribution**, closed in
under an hour, on chain, every time.

---

## 2. Design philosophy

Five rules. Everything else follows.

**1. Show the disagreement, not the price.**
A price alone is inert. Two prices that disagree is a story. The signature
visual is never "0.63" — it is the *distance between* what the market says and
what the model says, with the uncertainty band that tells you whether that
distance means anything.

**2. Uncertainty is the subject, not a caveat.**
Every probability we render carries its error bar, always, at the same visual
weight. This is the opposite of every trading UI, which renders a confident
number and hides the confidence. Our whole thesis is that mispriced *certainty*
is the opportunity.

**3. Time is a physical dimension.**
These instruments die on a schedule. Time is not a countdown in the corner; it
is the horizontal axis of the entire product. As a window approaches expiry the
interface tightens — literally: uncertainty bands narrow, the layout
compresses toward the decision.

**4. Never claim what we cannot verify.**
Every figure links to its source: a transaction hash, an oracle question, a
block number. Anything modelled is marked as modelled. Anything replayed is
marked as replayed.

**5. Restraint is the aesthetic.**
Near-monochrome. Colour is reserved for meaning and used sparingly enough that
when it appears it *means* something. No gradients as decoration, no glow, no
glass. The data is the ornament.

---

## 3. Visual identity

### Name of the language: **Instrument**

The reference is not a crypto app. It is scientific instrumentation — an
oscilloscope, a spectrum analyser, a flight data recorder. Objects built to show
a measurement honestly, where the housing is neutral so the reading is legible.

### Colour

Deliberately narrow. Most of the interface is greyscale.

```
--void        #08090b   page ground
--surface     #0f1115   panel
--raised      #15181e   elevated / hover
--hairline    #1e232c   1px structure
--ink         #e8ecf1   primary text
--ink-2       #9aa5b4   secondary
--ink-3       #5f6a7a   tertiary / axis

--up          #3ddc97   Up outcome, favourable
--down        #ff6b6b   Down outcome, adverse
--model       #7aa2f7   our estimate — the ONLY blue in the product
--warn        #f5a623   stale, risk, degraded
```

Rules:
- `--model` blue appears **only** on model-derived values. It is the product's
  signature and its meaning must never be diluted.
- Up/Down colour is applied to *outcomes*, never to UI chrome.
- No colour on a surface. Colour is for data marks and status only.
- Every colour-coded state also carries a shape, label, or position cue.

### Typography

Two families, used with intent.

```
Display / UI   Inter (or system sans)     -0.02em tracking on large sizes
Numeric        JetBrains Mono / ui-monospace, tabular-nums ALWAYS
```

Every number in the product is monospaced with tabular figures, so digits do not
jitter as they update. This is not fussiness — a probability that shifts its own
layout while ticking is unreadable, and it is the single most common flaw in
trading UIs.

Scale (major third, tightened at the top):
```
display  56 / 1.02   the hero probability
h1       32 / 1.15
h2       21 / 1.25
body     14 / 1.55
data     13 / 1.4    mono
micro    11 / 1.3    mono, +0.06em, uppercase — labels and axes only
```

### Space & structure

8px base grid. Panels are separated by **1px hairlines, not shadows**. Radius is
6px, uniformly, with no exceptions — varied radii read as inconsistency.

Density target: Bloomberg-legible, not Bloomberg-hostile. Roughly 60% of a
terminal's density, with the removed 40% spent on hierarchy.

---

## 4. Information architecture

```
/                Field — the live grid of every window, ranked by what matters
/m/[id]          Instrument — one window, in depth
/evidence        Why you should believe the model (calibration, backtest)
/ledger          What actually happened (attribution, honest P&L)
/learn           Interactive primer for someone who has never seen this
```

Five routes. No settings page, no dashboard-of-dashboards, no navigation the
user has to learn.

Progressive disclosure is handled by **one global control**, not per-panel
toggles:

```
[ READ ]  ←→  [ TRADE ]  ←→  [ DISSECT ]
```

- **READ** — probability, time, what changed. Legible to a newcomer.
- **TRADE** — adds book, spread, size, risk, execution.
- **DISSECT** — adds model internals, uncertainty decomposition, order flow,
  attribution.

The mode persists, is keyboard-switchable (`1` `2` `3`), and never rearranges
the layout — it only reveals. Spatial stability is the point: things do not move
when detail appears, they *appear where they already were*.

---

## 5. The signature interaction — **The Decision Trace**

This is the thing a judge remembers.

Every window that has settled can be **scrubbed**. Drag along the timeline and
the entire interface returns to its exact state at that moment: the book as it
stood, the model's fair value, its uncertainty band, our verdict, and — if we
traded — the order we sent and why.

At the end of the scrub, the outcome lands, and the interface draws the line
between what we believed and what happened.

```
  open ●───────────────────────────────────────● expiry
                    ▲ scrub

  market   ·········──────╲__                    0.71
  model    ─────────────────╲___                 0.64  ± 0.05
                        ▲
                        └─ 09:42:18  TAKE BUY_UP @ 0.68
                           "ask below fair by 0.09"
                           tx 0x68a65b06…

  outcome                                     ●  0.00   ✕ lost
  ───────────────────────────────────────────────────────
  edge     +0.09      what we believed we captured
  selection −0.68     how reality differed
  net       −0.59
```

**Why this is the wow moment:** it is not a chart. It is a *replay of a belief
being tested*. The user watches a model be confident, act, and then be right or
wrong — with the arithmetic of why, attached. No other prediction-market
interface shows this, because no other primitive settles fast enough or cleanly
enough to make it possible. It only exists because event contracts terminate in
minutes with an exact value.

And crucially: **it works on our losses.** Scrubbing a losing window and seeing
`edge +0.09 / selection −0.68` is a more compelling demonstration than any
green number, because it shows the instrument working.

---

## 6. The hero — "Field"

Not a landing page with a screenshot. The landing page *is* the live product.

The top of the page is a single dominant object: the window closest to expiry
that we have a calibrated model for.

```
┌───────────────────────────────────────────────────────────────────┐
│  BTC · 15 MINUTE WINDOW                            TRADING   ● live│
│                                                                    │
│         0.641                    market  0.598 / 0.626             │
│         ▁▂▃▅▆▇█ model            ─────────────────────             │
│         ├──┼──┤ ± 0.038                                            │
│                                  our read:                         │
│  ────────────────────────────    ASK BELOW FAIR BY 0.015           │
│  open ●━━━━━━━━━━━━━━━╋━━━● 04:12   not enough — inside noise      │
│       77,412.08       ▲            ─────────────────────           │
│                    77,489.55                                       │
└───────────────────────────────────────────────────────────────────┘
```

The dominant element is **one number with an error bar next to a market price
that disagrees with it.** That single composition states the entire product
thesis without a word of copy, and it is live, on chain, right now.

Below it, the Field: every other live window as a dense row, ranked by *interest*
— not by volume, but by |model − market| relative to uncertainty. The markets
where the disagreement is real float to the top.

---

## 7. Signature components

| Component | What makes it non-generic |
| --- | --- |
| `ProbabilityWithDoubt` | A value **and** its uncertainty band as one inseparable mark. Never renders a bare number. |
| `DisagreementBar` | Market and model on one axis, with the gap shaded and the noise floor drawn behind it. The gap is only coloured when it exceeds the noise. |
| `WindowTimeline` | Time as the product's main axis; open price, now, expiry, and every event we recorded, on one line. |
| `DecisionTrace` | The scrubbable replay. The signature. |
| `AttributionSplit` | Edge vs selection as opposing bars from a shared zero. Makes "we were right but lost" visible in one glance. |
| `CalibrationPlot` | Reliability curve with the diagonal. Interactive, hover for bucket n. |
| `BookStrip` | The order book as a horizontal depth strip on the probability axis — not a two-column table. Bids left, asks right, our own orders marked. Puts the book in the *same coordinate space* as fair value, so disagreement is spatial. |
| `NoiseFloor` | A recurring visual motif: the band inside which a difference means nothing. Appears on every comparison in the product. |

`BookStrip` deserves emphasis. Every competitor will render a two-column
bid/ask table copied from Binance. Putting the book on the **same axis as
probability and fair value** means you can *see* whether the model sits inside
or outside the book. That is a genuinely different way to read an order book,
and it is only sensible because binary contracts have a bounded 0–1 price axis.

---

## 8. Motion

Motion communicates causality only.

| Event | Motion | Duration |
| --- | --- | --- |
| Value updates | Interpolate the digits, never swap | 400ms `easeOut` |
| Uncertainty band changes | Width tween | 400ms |
| New fill arrives | Mark draws in on the timeline at its true x-position | 250ms |
| Mode change (READ→DISSECT) | Detail fades in place; **nothing moves** | 180ms |
| Scrub | Direct manipulation, 1:1, no easing | — |
| Settlement | The one deliberate flourish: the band collapses to a point at 0 or 1 | 700ms |

That settlement collapse is the only "cinematic" moment in the product, and it
earns it: it is the visual statement of what an event contract *is*.

`prefers-reduced-motion` disables all of it and renders end states directly.

---

## 9. Data architecture

```
Go engine ──▶ SQLite ledger ──┐
                              ├──▶ Go HTTP API ──▶ Next.js (static export)
chain + indexer + feed ───────┘                      │
                                                      └─▶ go:embed → one binary
```

Endpoints (three already exist):

```
GET /api/live          live windows, model vs book, engine verdict   ✅ built
GET /api/calibration   out-of-sample backtest artifact               ✅ built
GET /api/pnl           chain-verified attribution                    ✅ built
GET /api/window/:id    full decision trace for one window            ← new
GET /api/replay/:id    recorded session for Demo Mode                ← new
```

**Demo Mode** replays a recorded real session from the ledger at controlled
speed. It is labelled `REPLAY` in the chrome at all times. We never fabricate.

Node is a build-time tool only. `next build` static-exports to
`core/cmd/dashboard/dist`, which is `go:embed`ed. The judge still runs one
command and needs no Node installed.

---

## 10. Three things competitors will not have

1. **Uncertainty rendered at equal weight to value.** Polymarket, Kalshi and
   DreamDEX all show a confident number. We show the number *and whether it
   means anything*. This inverts the genre's central convention.

2. **The Decision Trace.** Scrubbable replay of belief → action → outcome →
   attribution. Impossible on slower-settling primitives; unique to event
   contracts.

3. **An interface that shows its own strategy losing.** The attribution split
   makes adverse selection visible. Every other submission will present a
   flattering number. Showing a negative net *with the decomposition that
   explains it* is a stronger claim of technical seriousness than any profit
   figure, and it is defensible under questioning — which the flattering ones
   are not.

---

## 11. States

Empty, loading and error states are designed, not defaulted.

- **No live windows** → "The venue rolls windows continuously. The next BTC
  window opens in 00:47." with a live countdown. Never "no data".
- **Loading** → skeletons that match the final layout exactly, so nothing jumps.
- **Stale index price** → the price mark greys and a `STALE 14s` badge appears;
  every model value on screen dims simultaneously. The interface refuses to
  imply confidence it does not have.
- **Uncalibrated cadence** → the row renders, greyed, labelled *"no validated
  model — we do not quote this"*. Showing what we deliberately refuse is itself
  a credibility signal.
- **Engine offline** → live panels show last-known with an explicit timestamp.
  Never a blank panel.

---

## 12. Accessibility

Semantic HTML, real landmarks, focus visible on everything. Full keyboard path:
`1/2/3` mode, `j/k` row navigation, `Enter` open, `Esc` close, `←/→` scrub.
Every colour-coded state carries a text or shape cue. Contrast ≥ 4.5:1 for text,
≥ 3:1 for data marks. Live regions announce settlement.

---

## 13. Responsive

Desktop is primary. Mobile is a **reprioritisation**, not a squeeze:

```
mobile:   probability + doubt  →  time  →  disagreement  →  action  →  attribution
          (book, model internals, trace collapse behind a sheet)
```

The Decision Trace works on mobile as a horizontal scrub — it is arguably
*better* with a finger than a mouse.

---

## 14. Build plan

| Phase | Scope | Est. |
| --- | --- | --- |
| 1 | Design tokens, layout shell, typography, mode switch | 45m |
| 2 | `ProbabilityWithDoubt`, `DisagreementBar`, `NoiseFloor`, `BookStrip` | 1h |
| 3 | Field page — hero + ranked live grid, wired to `/api/live` | 1h |
| 4 | Evidence page — calibration plot + skill-by-progress | 45m |
| 5 | Ledger page — `AttributionSplit`, honest P&L | 45m |
| 6 | **Decision Trace** + `/api/window/:id` | 1.5h |
| 7 | Motion pass, states, a11y, responsive | 1h |
| 8 | Static export + `go:embed` + one-command verification | 30m |

≈ 7 hours. We have five days.

**Phase 6 is the one that must not be cut.** If time compresses, drop the Learn
page and the Demo Mode replay before touching the Decision Trace.

---

## 15. The bar

Before this is done, it must survive these:

- Does the first screen state the thesis without copy? (a probability, its
  doubt, and a market disagreeing with it)
- Does every number trace to a transaction, a block, or an explicitly labelled
  model?
- Can a newcomer read the hero in READ mode without a glossary?
- Would a trader respect the book strip?
- Does the Decision Trace still land on a *losing* window?
- Strip the word "Somnia" — is it still obviously an exceptional financial
  instrument?

If any answer is no, iterate.
