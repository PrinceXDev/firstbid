# Firstbid — Demo Video Script

**Target: 2:45.** Hard cap 3:00.

Everything below is real and on screen. Nothing is mocked, and nothing needs to
be faked — which is the point.

---

## Before you record

```bash
# terminal 1 — the dashboard (leave running)
cd core && go run ./cmd/dashboard

# terminal 2 — the engine, started ~2 minutes before you hit record so
# there is live activity to cut to
cd core && set -a && . ../executor/.env && set +a
go run ./cmd/firstbid -net=testnet -live -size=2
```

Have open in browser tabs:
1. `http://localhost:8080` — the dashboard
2. `https://shannon-explorer.somnia.network` — for the transaction
3. `https://app.dreamdex.io/event-contracts` — the real venue

Font size up in the terminal. Dark theme. Close everything else.

---

## 00:00 – 00:18 · The hook

**Screen:** the calibration panel, then zoom the `P(Up)` figure from the README
table (or just the terminal running `go run ./cmd/calibrate`).

> "We pulled every settled DreamDEX event contract — ten thousand two hundred
> and nine of them — and counted how often the market closed up.
>
> Forty-nine point seven eight percent.
>
> These markets are a coin flip. Not approximately: measurably. So the honest
> conclusion is that **nobody can predict them, and we don't try to.**"

*Why this opens the video: it is a verifiable fact that quietly invalidates any
project whose pitch is "our AI predicts the direction". Lead with it.*

---

## 00:18 – 00:40 · The real problem

**Screen:** the live markets panel. Point at the `THEIR SPREAD` column, all
showing ~0.024.

> "DreamDEX opens about seven hundred and seventy of these markets a day. Only
> nineteen percent of them ever see a single trade, and the venue does around
> fifty traders a day.
>
> Here's why. Look at the spread — two and a half cents, on a contract that pays
> one dollar. Every quote on this venue is that same flat width, all day, in
> every market."

---

## 00:40 – 01:05 · The insight

**Screen:** the **Where the edge lives** table. Let it sit — this is the argument.

> "But how *certain* the outcome is changes enormously inside a single window.
>
> Twenty percent through, our model is only twenty-two percent better than a
> coin flip — genuine uncertainty. Ninety percent through, it's eighty-nine
> percent better; the outcome is nearly decided.
>
> A flat spread is wrong at both ends. It's **too tight early**, where the
> quoter gets picked off, and **far too wide late**, which is exactly when
> people most want to trade. That's why nobody trades here.
>
> Firstbid prices the spread to the actual uncertainty. Twelve cents when risk
> is real. Half a cent when it isn't."

---

## 01:05 – 01:30 · The proof

**Screen:** scroll to the reliability curve. Trace the diagonal with the cursor.

> "This is the part we'd want a judge to check.
>
> We replayed every resolved window using only what was knowable at the time.
> The model's one free parameter was fitted on the **older half** of the data,
> then scored on the **newer half it had never seen** — seven thousand
> predictions — with no lookahead: every sample sees only the price knowable
> at that instant.
>
> When it says thirty-five percent, it happens thirty-four percent of the time.
> When it says seventy-five, it happens seventy-three. Brier score of point one
> four against point two five for a coin flip — **forty-four percent better,
> out of sample.**
>
> And at the moment a window opens, the formula returns exactly zero point five
> zero zero — against a measured base rate of zero point four nine seven eight.
> It reproduces a number nobody told it."

---

## 01:30 – 02:00 · ⭐ The magic moment

**Screen:** split — dashboard live panel on the left, engine terminal on the
right. Wait for a `TAKE` verdict to appear, then cut to the terminal showing the
same market executing.

> "Now watch this happen live.
>
> The dashboard is reading the order book straight from the pool contract. Right
> here — our model prices this contract at thirty-eight cents. The market maker
> that's been quoting this venue on autopilot is asking twenty-five.
>
> That quote is stale by thirteen cents. So we don't rest behind it — we take it."

**Screen:** the terminal line, then paste the tx hash into the explorer.

```
[BTC/5m] t-179s fair=0.381 book=0.226/0.253
         TAKE BUY_UP edge=0.128 (ask is below fair value)
SENT     BUY_UP 0.258 x2.0  rested=false fills=1
```

> "Filled. On chain. That's a validated statistical model beating the incumbent
> market maker, in real time, on the live venue."

*If no TAKE appears within ~40s of recording, cut to a pre-captured one — but
try to get it live. It fires several times an hour.*

---

## 02:00 – 02:22 · Why this primitive is special

**Screen:** the P&L attribution panel.

> "Event contracts have one property nothing else in DeFi has: every position
> reaches a terminal, exact value within the hour. No mark-to-model, no
> open-ended exposure.
>
> So profit isn't an estimate, it's arithmetic — and we can split it. **Edge**
> is what we believed we captured. **Selection** is how the outcome differed
> from that belief. Edge is skill. Selection is variance, and it should average
> to zero if the model is honest.
>
> Every fifteen minutes, that's settled, verifiable, and on chain."

---

## 02:22 – 02:40 · The build

**Screen:** the repo tree, then `docs/SDK-FEEDBACK.md`.

> "This is pure Go. No Node, no JavaScript runtime — we drive the contracts
> directly from the ABIs DreamDEX exports.
>
> That turned out to matter. The SDK's documented order-book read returns
> **empty** for markets that demonstrably have depth — so anything built on it
> is looking at a dead market. We only saw the real book because we were reading
> the chain.
>
> We've written that up, with seven other reproducible findings, as a feedback
> report for the DreamDEX team."

---

## 02:40 – 02:55 · Close

**Screen:** back to the calibration panel.

> "Firstbid doesn't guess where Bitcoin is going. It measures how uncertain the
> outcome actually is, quotes that honestly, and takes the book when the book is
> wrong.
>
> Ten thousand markets say direction can't be predicted. So we priced the one
> thing that can."

---

## Delivery notes

- **Do not rush the reliability curve.** It is the most credible thing in the
  video and most submissions will have nothing like it. Give it its five seconds.
- **Say the numbers slowly.** 0.4978, 44%, 7,696. They land only if they're clear.
- Say "**out of sample**" at least twice. A judge who knows statistics is
  listening for exactly that, and its absence is the first thing they'd attack.
- If the P&L panel is still small or slightly negative, **show it anyway and say
  so**. A team that shows a modest measured result with correct attribution reads
  as more credible than one showing implausible profit. Suggested line: *"small
  numbers — we've been live for hours, not weeks — but every one of them is
  attributed and verifiable."*
- Don't claim we beat the incumbents on spread width everywhere. We don't, and
  the honest claim is stronger: **correctly priced across the window's life.**

## What NOT to say

- ❌ "Our AI predicts the market." It doesn't, and the data says nothing can.
- ❌ "We provide liquidity where there is none." Books have depth; the mispricing
  is the story.
- ❌ Any projected returns or APY. We have hours of live data, not a track record.
