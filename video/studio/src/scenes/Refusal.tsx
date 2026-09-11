import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { FootageAct, type BeatShot } from "../components/FootageAct";
import { Callout, Spotlight } from "../components/Callout";
import { C, F } from "../theme";
import { FPS } from "../timeline";

/** THE REFUSAL — the half that matters.
 *  Source coordinates from the recording at t=43..46s (verified on a grid).
 *  The reading holds steady from t=40 to t=49, so there is room to dwell. */

const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

/** 0.021 against 0.120 — both read straight off the dashboard. */
const SpreadCompare: React.FC<{ at: number }> = ({ at }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.5], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const g1 = interpolate(t, [at + 0.35, at + 1.15], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const g2 = interpolate(t, [at + 0.75, at + 1.8], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });

  const MAX = 940;
  const row = (
    label: string,
    value: string,
    frac: number,
    grow: number,
    col: string,
    note: string
  ) => (
    <div style={{ marginBottom: 34 }}>
      <div style={{ display: "flex", alignItems: "baseline", gap: 18, marginBottom: 10 }}>
        <span style={{ fontFamily: F.mono, fontSize: 21, letterSpacing: 2.6, color: C.inkFaint, width: 260 }}>
          {label}
        </span>
        <span style={{ fontFamily: F.mono, fontSize: 40, fontWeight: 700, color: col, letterSpacing: -0.5 }}>
          {value}
        </span>
        <span style={{ fontFamily: F.sans, fontSize: 21, color: C.inkDim }}>{note}</span>
      </div>
      <div style={{ width: MAX, height: 16, background: "rgba(126,156,255,0.07)", borderRadius: 3 }}>
        <div
          style={{
            width: MAX * frac * grow,
            height: 16,
            borderRadius: 3,
            background: col,
            boxShadow: "0 0 22px " + col + "88",
          }}
        />
      </div>
    </div>
  );

  return (
    <div
      style={{
        opacity: a,
        transform: "translateY(" + (1 - a) * 14 + "px)",
        background: "rgba(5,8,12,0.93)",
        border: "1px solid " + C.panelEdge,
        borderRadius: 12,
        padding: "38px 46px 26px",
        boxShadow: "0 30px 90px rgba(0,0,0,0.75)",
      }}
    >
      <div
        style={{
          fontFamily: F.mono,
          fontSize: 22,
          letterSpacing: 5,
          color: C.model,
          textTransform: "uppercase",
          marginBottom: 30,
        }}
      >
        the quote it would post instead
      </div>
      {row("THE INCUMBENT", "0.021", 0.021 / 0.13, g1, C.book, "the same width, always")}
      {row("FIRSTBID, HERE", "0.120", 0.12 / 0.13, g2, C.model, "because the outcome is genuinely open")}
      <div
        style={{
          marginTop: 8,
          fontFamily: F.sans,
          fontSize: 26,
          color: C.ink,
          borderTop: "1px solid " + C.panelEdge,
          paddingTop: 20,
        }}
      >
        Wide when uncertainty is real. Tight when it is not.
      </div>
    </div>
  );
};

/** Shared panel so an overlay can sit over the live table and stay legible. */
const Panel: React.FC<{ children: React.ReactNode; style?: React.CSSProperties }> = ({
  children, style,
}) => (
  <div
    style={{
      background: "rgba(5,8,12,0.94)",
      border: "1px solid " + C.panelEdge,
      borderRadius: 12,
      padding: "26px 40px",
      boxShadow: "0 26px 80px rgba(0,0,0,0.78)",
      ...style,
    }}
  >
    {children}
  </div>
);

/** Sits in the empty band under the hero card: the two numbers, side by side,
 *  and the inequality that decides whether Firstbid is allowed to act. */
const Ledger: React.FC<{ at: number; floorAt: number }> = ({ at, floorAt }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.5], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const b = interpolate(t, [floorAt, floorAt + 0.5], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const c = interpolate(t, [floorAt + 1.1, floorAt + 1.7], [0, 1], clampOp);

  const cell = (label: string, value: string, col: string, op: number) => (
    <div style={{ opacity: op, transform: "translateY(" + (1 - op) * 10 + "px)", minWidth: 300 }}>
      <div style={{ fontFamily: F.mono, fontSize: 20, letterSpacing: 2.6, color: C.inkFaint, textTransform: "uppercase" }}>
        {label}
      </div>
      <div style={{ fontFamily: F.mono, fontSize: 62, fontWeight: 700, color: col, letterSpacing: -1, marginTop: 6 }}>
        {value}
      </div>
    </div>
  );

  return (
    <div
      style={{
        position: "absolute",
        left: 0, right: 0, top: 620,
        display: "flex",
        justifyContent: "center",
        // the panel arrives with its first number, never as an empty box
        opacity: a,
      }}
    >
      <Panel>
        <div style={{ display: "flex", alignItems: "flex-end", gap: 52 }}>
          {cell("disagreement with the mid", "0.049", C.model, a)}
          <div style={{ opacity: c, fontFamily: F.mono, fontSize: 46, color: C.warn, paddingBottom: 8 }}>&lt;</div>
          {cell("the model's own noise floor", "0.057", C.warn, b)}
        </div>
      </Panel>
    </div>
  );
};

/** The engine's conclusion, stated once, large, in the same empty band. */
const Verdict: React.FC<{ at: number }> = ({ at }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.55], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  return (
    <div
      style={{
        position: "absolute",
        left: 0, right: 0, top: 630,
        display: "flex",
        justifyContent: "center",
        opacity: a,
        transform: "translateY(" + (1 - a) * 12 + "px)",
      }}
    >
      <Panel style={{ textAlign: "center", maxWidth: 1180 }}>
        <div style={{ fontFamily: F.sans, fontSize: 40, fontWeight: 600, color: C.ink }}>
          The disagreement is smaller than the model's <span style={{ color: C.warn }}>own error</span>.
        </div>
        <div style={{ marginTop: 14, fontFamily: F.mono, fontSize: 24, letterSpacing: 4, color: C.refuse, textTransform: "uppercase" }}>
          so there is nothing here to trade
        </div>
      </Panel>
    </div>
  );
};

/** How much wider Firstbid's quote would be than the incumbent's. */
const Multiple: React.FC<{ at: number }> = ({ at }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.6], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  return (
    <div style={{ position: "absolute", left: 0, right: 0, top: 664, display: "flex", justifyContent: "center", opacity: a }}>
      <Panel style={{ textAlign: "center" }}>
        <div style={{ fontFamily: F.mono, fontSize: 21, letterSpacing: 3.4, color: C.inkFaint, textTransform: "uppercase" }}>
          0.021 against 0.120
        </div>
        <div style={{ marginTop: 10, fontFamily: F.sans, fontSize: 54, fontWeight: 700, color: C.model, letterSpacing: -1 }}>
          5.7× wider
        </div>
      </Panel>
    </div>
  );
};

const shots: BeatShot[] = [
  // "Now watch the more important half."
  {
    id: "a3_01",
    shot: {
      srcIn: 40.3, srcOut: 41.4,
      from: { x: 180, y: 170, w: 1580, h: 770 },
      to: { x: 195, y: 185, w: 1520, h: 741 },
    },
    overlay: ({ shot, dur }) => (
      <Callout
        shot={shot} durationInFrames={dur}
        box={{ x: 228, y: 296, w: 190, h: 66 }}
        label="0.921" side="right" appearAt={0.5} bare
      />
    ),
  },

  // "Same market. The model sits 0.049 from the mid. But the noise floor is 0.057."
  //  The two numbers are ringed on screen; the comparison is spelled out in the
  //  empty band below the card, where nothing is competing for the space.
  {
    id: "a3_02",
    shot: {
      srcIn: 43.0, srcOut: 44.5,
      from: { x: 592, y: 218, w: 1130, h: 551 },
      to: { x: 600, y: 226, w: 1110, h: 541 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 716, y: 286, w: 58, h: 28 }}
          side="top" appearAt={2.0} color={C.model}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 1086, y: 286, w: 58, h: 28 }}
          side="top" appearAt={5.4} color={C.warn}
        />
        <Ledger at={2.4} floorAt={5.6} />
      </>
    ),
  },

  // "The disagreement is smaller than the model's own error. So it is not a signal."
  {
    id: "a3_03",
    shot: {
      srcIn: 44.5, srcOut: 45.7,
      from: { x: 632, y: 234, w: 1100, h: 536 },
      to: { x: 640, y: 240, w: 1080, h: 526 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Verdict at={0.6} />
        <Spotlight
          shot={shot} durationInFrames={dur}
          box={{ x: 1136, y: 284, w: 290, h: 30 }}
          appearAt={3.0} strength={0.62} pad={9}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 1136, y: 284, w: 290, h: 30 }}
          label="NOT A SIGNAL" side="top" appearAt={3.2} color={C.warn}
        />
      </>
    ),
  },

  // "It does not trade. It widens. Their spread is two cents. Ours would be twelve."
  {
    id: "a3_04",
    shot: {
      srcIn: 45.7, srcOut: 46.8,
      from: { x: 600, y: 322, w: 1120, h: 546 },
      to: { x: 608, y: 328, w: 1100, h: 536 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 1164, y: 332, w: 112, h: 56 }}
          appearAt={2.4} color={C.book}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 1429, y: 332, w: 120, h: 56 }}
          appearAt={3.9} color={C.model}
        />
        <Multiple at={4.5} />
      </>
    ),
  },

  // "Wide when uncertainty is real. Tight when it is not. That is the discipline."
  {
    id: "a3_05",
    shot: {
      srcIn: 46.8, srcOut: 47.6,
      from: { x: 300, y: 200, w: 1400, h: 683 },
      to: { x: 330, y: 215, w: 1340, h: 653 },
    },
    dim: 0.74,
    overlay: () => (
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", paddingBottom: 60 }}>
        <SpreadCompare at={0.35} />
      </AbsoluteFill>
    ),
  },
];

export const Refusal: React.FC = () => (
  <AbsoluteFill>
    <FootageAct actName="REFUSAL" shots={shots} />
  </AbsoluteFill>
);
