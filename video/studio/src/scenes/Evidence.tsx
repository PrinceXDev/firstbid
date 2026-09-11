import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { FootageAct, type BeatShot } from "../components/FootageAct";
import { Callout, Spotlight } from "../components/Callout";
import { C, F } from "../theme";
import { FPS } from "../timeline";

/** EVIDENCE — the dashboard's Evidence page.
 *
 *  The recording scrolls this page while it is on screen, so overlay
 *  coordinates taken at one moment do not hold at the next. Every shot here
 *  therefore holds the single frame whose geometry is verified (t=62.0) and
 *  lets the camera do all the moving. The page shows a settled artefact —
 *  docs/calibration.json — not live data, so holding it costs nothing.
 *
 *  Verified source coordinates:
 *    method paragraph   x 208..888,  y 160..232   ("older half" 552..632 @188)
 *    +45.7%             x 210..378,  y 275..322
 *    0.1357 / 0.2500    x 456..565 / 620..725,    y 278..308
 *    5,353 / 5,352      x 812..900 / 1063..1155,  y 278..308
 *    reliability chart  x 250..1120, y 560..1028 */

const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;
/** The page scrolls during the recording, so every shot holds ONE verified
 *  frame and lets the camera do the moving. Nothing here is live data. */
const SRC_IN = 62.0;
const HOLD = 0.04;

/** One card in the empty right half, where the page has nothing to say. */
const Note: React.FC<{
  at: number; kicker: string; lines: [string, string][]; top: number;
}> = ({ at, kicker, lines, top }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.5], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  return (
    <div
      style={{
        position: "absolute",
        right: 74,
        top,
        width: 520,
        opacity: a,
        transform: "translateY(" + (1 - a) * 12 + "px)",
        background: "rgba(6,9,14,0.90)",
        border: "1px solid " + C.panelEdge,
        borderLeft: "3px solid " + C.model,
        borderRadius: 8,
        padding: "20px 24px",
        boxShadow: "0 16px 50px rgba(0,0,0,0.6)",
      }}
    >
      <div
        style={{
          fontFamily: F.mono, fontSize: 19, letterSpacing: 3,
          color: C.model, textTransform: "uppercase", marginBottom: 14,
        }}
      >
        {kicker}
      </div>
      {lines.map(([k, v], i) => {
        const la = interpolate(t, [at + 0.4 + i * 0.5, at + 0.9 + i * 0.5], [0, 1], clampOp);
        return (
          <div
            key={k}
            style={{
              opacity: la,
              display: "flex",
              justifyContent: "space-between",
              alignItems: "baseline",
              gap: 18,
              padding: "9px 0",
              borderTop: i === 0 ? "none" : "1px solid rgba(126,150,190,0.10)",
            }}
          >
            <span style={{ fontFamily: F.sans, fontSize: 21, color: C.inkDim }}>{k}</span>
            <span style={{ fontFamily: F.mono, fontSize: 25, fontWeight: 700, color: C.ink, whiteSpace: "nowrap" }}>
              {v}
            </span>
          </div>
        );
      })}
    </div>
  );
};

const shots: BeatShot[] = [
  // "And here is why you should believe it." — arrive on the page.
  {
    id: "a5_01",
    shot: {
      srcIn: SRC_IN, srcOut: SRC_IN + HOLD,
      from: { x: 150, y: 130, w: 1640, h: 799 },
      to: { x: 168, y: 144, w: 1580, h: 770 },
    },
  },

  // "Every resolved window replayed using only what was observable at that
  //  moment. Fitted on the older half. Scored on the newer half it has never seen."
  {
    id: "a5_02",
    shot: {
      srcIn: SRC_IN, srcOut: SRC_IN + HOLD,
      from: { x: 170, y: 140, w: 1200, h: 585 },
      to: { x: 180, y: 148, w: 1150, h: 561 },
    },
    overlay: ({ shot, dur }) => (
      <>
        {/* the two phrases the page itself sets in bold */}
        <Spotlight
          shot={shot} durationInFrames={dur}
          box={{ x: 550, y: 180, w: 86, h: 26 }}
          appearAt={4.2} holdUntil={6.2} strength={0.55} pad={8}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 550, y: 180, w: 86, h: 26 }}
          appearAt={4.2} holdUntil={6.4} color={C.inkDim}
        />
        <Spotlight
          shot={shot} durationInFrames={dur}
          box={{ x: 286, y: 224, w: 222, h: 26 }}
          appearAt={6.8} strength={0.58} pad={8}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 286, y: 224, w: 222, h: 26 }}
          appearAt={6.8} color={C.take}
        />
        <Note
          at={1.5}
          kicker="the split"
          top={300}
          lines={[
            ["fitted on the older half", "5,352"],
            ["scored on the newer half", "5,353"],
            ["information used", "observable only"],
          ]}
        />
      </>
    ),
  },

  // "Brier score 0.1357, against 0.25 for always guessing a coin."
  {
    id: "a5_03",
    shot: {
      srcIn: SRC_IN, srcOut: SRC_IN + HOLD,
      from: { x: 190, y: 240, w: 1080, h: 527 },
      to: { x: 198, y: 248, w: 1050, h: 512 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 450, y: 270, w: 124, h: 46 }}
          appearAt={1.6} color={C.model}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 614, y: 270, w: 124, h: 46 }}
          appearAt={4.2} color={C.book}
        />
        <Note
          at={2.0}
          kicker="brier score · lower is better"
          top={330}
          lines={[
            ["Firstbid's model", "0.1357"],
            ["always guessing a coin", "0.2500"],
          ]}
        />
      </>
    ),
  },

  // "Forty five point seven percent skill. Out of sample."
  {
    id: "a5_04",
    shot: {
      srcIn: SRC_IN, srcOut: SRC_IN + HOLD,
      from: { x: 186, y: 250, w: 900, h: 439 },
      to: { x: 192, y: 256, w: 870, h: 424 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Spotlight
          shot={shot} durationInFrames={dur}
          box={{ x: 204, y: 266, w: 184, h: 60 }}
          appearAt={0.4} strength={0.60} pad={14}
        />
        <Callout
          shot={shot} durationInFrames={dur}
          box={{ x: 204, y: 266, w: 184, h: 60 }}
          label="+45.7%" note="skill versus a coin flip · out of sample"
          side="right" appearAt={0.6} color={C.take}
        />
      </>
    ),
  },

  // "The reliability curve tracks the diagonal. When it says 0.35, it happens
  //  35% of the time."
  {
    id: "a5_05",
    shot: {
      srcIn: SRC_IN, srcOut: SRC_IN + HOLD,
      from: { x: 210, y: 330, w: 980, h: 478 },
      to: { x: 228, y: 556, w: 930, h: 453 },
    },
    overlay: ({ shot, dur }) => (
      <Callout
        shot={shot} durationInFrames={dur}
        box={{ x: 296, y: 596, w: 800, h: 404 }}
        label="TRACKS THE DIAGONAL" note="predicted against realised, on held-out windows"
        side="top" appearAt={2.6} color={C.model} bare
        holdUntil={99}
      />
    ),
  },
];

export const Evidence: React.FC = () => (
  <AbsoluteFill>
    <FootageAct actName="EVIDENCE" shots={shots} />
  </AbsoluteFill>
);
