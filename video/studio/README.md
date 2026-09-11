# Firstbid — demo film

A 4:53 technical demo built with [Remotion](https://www.remotion.dev/), cut around
the existing screen recording at `../firstbid.mp4`.

```bash
npm install
npm run studio          # interactive editor at localhost:3000
npm run render          # out/firstbid-demo.mp4  (1920x1080, 30fps, H.264, CRF 15)
```

## How it is put together

The film is **timing-driven, not hand-placed**. `src/timeline.ts` reads the
*measured* length of every narration clip from `src/vo-manifest.json` and lays
the beats end to end, so the picture can never drift from the voice. Editing a
line of narration and re-running `tts.py` re-times the entire film.

```
script.json ──tts.py──▶ public/vo/*.mp3  +  src/vo-manifest.json
                                                    │
                                      src/timeline.ts (single source of truth)
                                                    │
                                   ┌────────────────┴────────────────┐
                              scenes/*.tsx                    components/*.tsx
```

Four constants in `timeline.ts` control the whole pace:

| Constant | Meaning |
| --- | --- |
| `INTRA_GAP` | breath between beats inside an act |
| `ACT_GAP` | beat between acts |
| `PRE_ROLL` | silence before the first word |
| `TAIL` | hold after the last word |

## Structure

| Act | Runs | Source |
| --- | --- | --- |
| PROBLEM | 0:00 | motion graphics |
| TITLE | 0:33 | motion graphics |
| READ | 0:37 | recording, t≈7s and t≈12s |
| REFUSAL | 1:05 | recording, t≈40–47s |
| MODEL | 1:36 | motion graphics |
| EVIDENCE | 1:53 | recording, t=62.0 (held) |
| AUTOPSY | 2:26 | motion graphics |
| COVERAGE | 2:57 | recording, t=78.6 (held) |
| ARCHITECTURE | 3:30 | motion graphics |
| MECHANISM | 3:58 | motion graphics |
| LIVE | 4:18 | recording, t≈101–104s |
| PITCH | 4:30 | motion graphics |

## The camera

`components/Footage.tsx` frames the recording by **source rectangle**, not by
CSS transforms written by hand. A rect of `w: 1920` is a 1:1 pixel mapping, so
un-zoomed footage is never resampled. `Callout`, `Spotlight` and `Arrow` take
coordinates in the same source space and project them through whatever the
camera is doing, so a highlight stays locked to the UI element under it.

The browser chrome (top 92px) is cropped away throughout.

Two pages — Evidence and Coverage — scroll while they are on screen in the
recording, which means overlay coordinates taken at one moment do not hold at
the next. Both acts therefore **hold one verified frame** and let the camera do
the moving. Both show settled artefacts rather than live data, so nothing is
lost by holding them.

## Sound

- **Narration** — `tts.py`, Microsoft Neural TTS (`en-US-AndrewMultilingualNeural`)
  at −4% rate. It also captures word-level timings, which drive the captions.
  `fix_manifest.py` restores the punctuation the boundary events strip.
- **Score** — `music.py` synthesises the bed from scratch (no samples, no
  third-party audio): detuned pads over Am–F–C–G, a sub drone, a sparse pulse,
  and an FFT convolution reverb. Section dynamics are baked in; `Score` in
  `components/Sound.tsx` adds a speech-aware duck to 0.30 under every beat.

## Accuracy

Every figure spoken or shown is traceable to the repository or to the
recording itself:

| Claim | Source |
| --- | --- |
| P(Up) = 0.4978 over 10,209 settled windows | `README.md` |
| flat 0.024–0.029 quoted spread | `README.md`, `pool.getBookLevels` |
| 0.812 model / 0.942 mid / 0.049 vs 0.057 / 0.021 vs 0.120 | on screen in the recording |
| Brier 0.1357 vs 0.2500, +45.7%, 5,352 train / 5,353 test | `docs/calibration.json`, on screen |
| σ per asset and cadence, n per series | `README.md` σ table |
| +59.5% retracted, 59s look-ahead, −37% across 53 fills | `docs/AUTOPSY.md` |
| BTC/240m admitted within 5.3%; ETH/240m refused at +18.2% | `docs/COVERAGE.md`, on screen |
| 1 cadence admitted, 3 refused, deepest book still refused | `docs/COVERAGE.md` |
| pure Go, one goroutine per window, one executor / one nonce | `README.md`, `docs/ARCHITECTURE.md` |
| MINT_A_PAIR at 22–42% of fills; pair redeems to 1.00 | `README.md` |
| 100ms average block time, latest block | on screen in the recording |
| "nothing has settled yet" | on screen — stated as such, not worked around |

No transaction hash, fill, P&L figure, user count or deployment claim appears
anywhere in the film. The Trace and Ledger panels were empty in the recording,
and the film says so.

## The presenter card

`src/speaker.ts` controls the portrait in the closing act. Drop an image at
`public/speaker.jpg` and set `enabled: true`. Until then the outro renders a
monogram in the same frame, so the film always builds.
