import React from "react";
import { useCurrentFrame, interpolate, Easing } from "remotion";
import { BEATS, FPS, type Beat, type Word } from "../timeline";
import { C, F } from "../theme";

/** Terms worth lighting up. Matched case-insensitively on the stripped word. */
const KEYWORDS = new Set([
  "firstbid", "somnia", "dreamdex", "fair", "value", "out-of-sample",
  "calibration", "calibrated", "uncertainty", "noise", "floor", "edge",
  "refuses", "refused", "refusal", "quotable", "brier", "skill", "sigma",
  "go", "goroutine", "nonce", "writer", "evidence", "observable",
  "overconfident", "reliability", "diagonal", "collateral", "mints",
  "probability", "coin", "spread", "signal", "measured", "measures",
  "predict", "wrong", "honest", "discipline", "inventory",
  "shannon", "testnet", "go", "pure", "concurrently", "serialised",
  "mint", "pair", "complementary", "quotable", "cadence", "volatility",

]);

const strip = (w: string) => w.toLowerCase().replace(/[^a-z0-9.\-]/g, "");

/** Break a beat's words into caption lines of at most ~46 characters,
 *  never splitting mid-sentence if a sentence fits on its own line. */
type Line = { words: Word[]; s: number; e: number };
const lineate = (b: Beat): Line[] => {
  const out: Line[] = [];
  let cur: Word[] = [];
  let len = 0;
  const flush = () => {
    if (!cur.length) return;
    out.push({ words: cur, s: cur[0].s, e: cur[cur.length - 1].e });
    cur = [];
    len = 0;
  };
  for (const w of b.words) {
    const add = w.w.length + 1;
    if (len + add > 46 && cur.length) flush();
    cur.push(w);
    len += add;
    if (/[.?!:]$/.test(w.w.trim()) && len > 22) flush();
  }
  flush();
  return out;
};

const CACHE = new Map<string, Line[]>();
const linesFor = (b: Beat) => {
  let l = CACHE.get(b.id);
  if (!l) {
    l = lineate(b);
    CACHE.set(b.id, l);
  }
  return l;
};

export const Captions: React.FC = () => {
  const f = useCurrentFrame();
  const t = f / FPS;

  const beat = BEATS.find((b) => t >= b.start - 0.18 && t <= b.end + 0.42);
  if (!beat) return null;

  const local = t - beat.start;
  const lines = linesFor(beat);
  // hold the last line through the trailing silence
  let idx = lines.findIndex((l) => local >= l.s - 0.16 && local <= l.e + 0.30);
  if (idx < 0) idx = local < lines[0].s ? 0 : lines.length - 1;
  const line = lines[idx];

  const appear = interpolate(t, [beat.start + line.s - 0.16, beat.start + line.s + 0.06], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
    easing: Easing.out(Easing.quad),
  });
  const fadeOut = interpolate(t, [beat.end + 0.16, beat.end + 0.42], [1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const op = appear * fadeOut;

  return (
    <div
      style={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom: 21,
        display: "flex",
        justifyContent: "center",
        pointerEvents: "none",
      }}
    >
      <div
        style={{
          maxWidth: 1340,
          padding: "11px 28px",
          borderRadius: 10,
          background: "rgba(4,6,10,0.80)",
          border: `1px solid rgba(126,156,255,0.13)`,
          boxShadow: "0 10px 40px rgba(0,0,0,0.55)",
          backdropFilter: "blur(9px)",
          opacity: op,
          transform: `translateY(${(1 - appear) * 9}px)`,
          display: "flex",
          flexWrap: "wrap",
          justifyContent: "center",
          gap: "0 10px",
          fontFamily: F.sans,
          fontSize: 33,
          fontWeight: 500,
          letterSpacing: 0.1,
          lineHeight: 1.28,
        }}
      >
        {line.words.map((w, i) => {
          const spoken = local >= w.s - 0.02;
          const key = KEYWORDS.has(strip(w.w));
          // a brief lift as each word lands
          const hit = interpolate(local, [w.s - 0.04, w.s + 0.10], [0, 1], {
            extrapolateLeft: "clamp",
            extrapolateRight: "clamp",
          });
          return (
            <span
              key={i}
              style={{
                color: key ? C.model : spoken ? C.ink : C.inkFaint,
                fontWeight: key ? 700 : 500,
                textShadow: key
                  ? `0 0 ${10 + hit * 12}px ${C.modelGlow}`
                  : "0 1px 3px rgba(0,0,0,0.8)",
                transform: `translateY(${(1 - hit) * -1.5}px)`,
                transition: "none",
              }}
            >
              {w.w}
            </span>
          );
        })}
      </div>
    </div>
  );
};
