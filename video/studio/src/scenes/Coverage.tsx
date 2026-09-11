import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { FootageAct, type BeatShot } from "../components/FootageAct";
import { Callout, Spotlight } from "../components/Callout";
import { C, F } from "../theme";
import { FPS } from "../timeline";

/** COVERAGE — "What we are allowed to price, live".
 *
 *  The rows carry their own evidence text, which says exactly what the
 *  narration says, so the overlays here only point: a spotlight on the row
 *  under discussion, and no chip restating what is already on screen.
 *
 *  Locked to source t=78.0..79.8, whose geometry is verified. Row centres
 *  (source y): BTC/1m 447, /5m 498, /15m 550, /60m 601, /240m 652,
 *  /1440m 703, /64800m 755, ETH/1m 806, /5m 857, /14m 908, /60m 960,
 *  /240m 1011. Columns: cadence 228..320, verdict 390..484, evidence 556..1390. */

const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;
const row = (y: number, h = 38) => ({ x: 220, y: y - h / 2, w: 1186, h });

/** Running tally in the empty margin, so the arithmetic of the act is visible. */
const Tally: React.FC<{ at: number; ok: number; no: number }> = ({ at, ok, no }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.5], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  return (
    <div
      style={{
        position: "absolute",
        right: 78,
        bottom: 132,
        opacity: a,
        transform: "translateY(" + (1 - a) * 12 + "px)",
        textAlign: "right",
      }}
    >
      <div style={{ fontFamily: F.mono, fontSize: 19, letterSpacing: 3, color: C.inkFaint, textTransform: "uppercase" }}>
        the verdict, per cadence
      </div>
      <div style={{ display: "flex", gap: 34, justifyContent: "flex-end", marginTop: 14 }}>
        <div>
          <div style={{ fontFamily: F.mono, fontSize: 62, fontWeight: 700, color: C.take, lineHeight: 1 }}>{ok}</div>
          <div style={{ fontFamily: F.mono, fontSize: 18, letterSpacing: 2.4, color: C.inkFaint, marginTop: 6 }}>
            QUOTABLE
          </div>
        </div>
        <div>
          <div style={{ fontFamily: F.mono, fontSize: 62, fontWeight: 700, color: C.book, lineHeight: 1 }}>{no}</div>
          <div style={{ fontFamily: F.mono, fontSize: 18, letterSpacing: 2.4, color: C.inkFaint, marginTop: 6 }}>
            REFUSED
          </div>
        </div>
      </div>
    </div>
  );
};

const shots: BeatShot[] = [
  // "And refusal is enforced per cadence, then published."
  {
    id: "a7_01",
    shot: {
      srcIn: 78.6, srcOut: 78.64,
      from: { x: 140, y: 150, w: 1660, h: 809 },
      to: { x: 160, y: 190, w: 1580, h: 770 },
    },
    overlay: ({ shot, dur }) => (
      <Callout
        shot={shot} durationInFrames={dur}
        box={{ x: 208, y: 196, w: 560, h: 42 }}
        label="PUBLISHED, NOT ASSERTED" side="bottom" appearAt={1.5} bare
      />
    ),
  },

  // "Bitcoin at five, fifteen and sixty minutes. Quotable, each on its own fitted sample."
  {
    id: "a7_02",
    shot: {
      srcIn: 78.6, srcOut: 78.64,
      from: { x: 196, y: 306, w: 1240, h: 605 },
      to: { x: 200, y: 316, w: 1210, h: 590 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Callout shot={shot} durationInFrames={dur} box={row(498)} appearAt={1.4} color={C.take} />
        <Callout shot={shot} durationInFrames={dur} box={row(550)} appearAt={2.5} color={C.take} />
        <Callout shot={shot} durationInFrames={dur} box={row(601)} appearAt={3.5} color={C.take} />
        <Tally at={4.4} ok={3} no={0} />
      </>
    ),
  },

  // "Bitcoin at two forty is admitted only because a second, independent
  //  measurement agreed within five point three percent."
  {
    id: "a7_03",
    shot: {
      srcIn: 78.6, srcOut: 78.64,
      from: { x: 186, y: 344, w: 1290, h: 629 },
      to: { x: 190, y: 350, w: 1260, h: 614 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Spotlight shot={shot} durationInFrames={dur} box={row(652, 42)} appearAt={1.1} strength={0.62} pad={8} />
        <Callout shot={shot} durationInFrames={dur} box={row(652, 42)} appearAt={1.3} color={C.take} />
        <Callout
          shot={shot} durationInFrames={dur}
          box={row(652, 42)}
          label="A SECOND, INDEPENDENT ESTIMATOR"
          note="cmd/volscale — sharing no data and no code path with the first"
          side="bottom" appearAt={4.8} color={C.model} bare
        />
      </>
    ),
  },

  // "Ethereum at two forty is refused. The identical measurement that admitted
  //  Bitcoin rejects this one."
  {
    id: "a7_04",
    shot: {
      srcIn: 78.6, srcOut: 78.64,
      from: { x: 186, y: 394, w: 1290, h: 629 },
      to: { x: 190, y: 399, w: 1260, h: 614 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Spotlight shot={shot} durationInFrames={dur} box={row(1011, 42)} appearAt={1.0} strength={0.64} pad={8} />
        <Callout shot={shot} durationInFrames={dur} box={row(1011, 42)} appearAt={1.2} color={C.book} />
        <Callout
          shot={shot} durationInFrames={dur}
          box={row(1011, 42)}
          label="THE SAME TEST, THE OPPOSITE ANSWER"
          side="top" appearAt={4.2} color={C.book} bare
        />
      </>
    ),
  },

  // "One cadence admitted. Three still refused. Including the deepest book on the venue."
  {
    id: "a7_05",
    shot: {
      srcIn: 78.6, srcOut: 78.64,
      from: { x: 160, y: 360, w: 1420, h: 692 },
      to: { x: 150, y: 342, w: 1450, h: 707 },
    },
    overlay: ({ shot, dur }) => (
      <>
        <Callout shot={shot} durationInFrames={dur} box={row(447)} appearAt={1.0} color={C.refuse} />
        <Callout shot={shot} durationInFrames={dur} box={row(703)} appearAt={1.3} color={C.refuse} />
        <Callout shot={shot} durationInFrames={dur} box={row(908)} appearAt={1.6} color={C.refuse} />
        <Callout shot={shot} durationInFrames={dur} box={row(1011)} appearAt={2.2} color={C.book} />
        <Callout
          shot={shot} durationInFrames={dur}
          box={row(1011)}
          label="THE DEEPEST BOOK ON THE VENUE — STILL REFUSED"
          side="top" appearAt={2.6} color={C.book} bare
        />
      </>
    ),
  },
];

export const Coverage: React.FC = () => (
  <AbsoluteFill>
    <FootageAct actName="COVERAGE" shots={shots} />
  </AbsoluteFill>
);
