import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { C, F } from "../theme";
import { Backdrop } from "../components/Backdrop";
import { In, Counter } from "../components/Kit";
import { act, byId, FPS } from "../timeline";

const A = act("PROBLEM");
const L = (id: string) => byId(id).start - A.start; // local start, seconds
const Lend = (id: string) => byId(id).end - A.start;
const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

/** Sigma-root-t decay of genuine uncertainty across a window's life:
 *  wide while the outcome is open, collapsing as it becomes determined. */
const trueSpread = (p: number) => 0.005 + 0.125 * Math.pow(1 - p, 0.62);
const VENUE = 0.0245;
/** Where the flat quote stops being too tight and starts being too wide. */
const CROSS = 1 - Math.pow((VENUE - 0.005) / 0.125, 1 / 0.62);

const SpreadChart: React.FC<{ at: number }> = ({ at }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const draw = interpolate(t, [at, at + 1.5], [0, 1], {
    ...clampOp,
    easing: Easing.inOut(Easing.ease),
  });
  const showBands = interpolate(t, [at + 1.6, at + 2.4], [0, 1], clampOp);

  const W = 1180;
  const Hh = 340;
  const X = (p: number) => p * W;
  const Y = (v: number) => Hh - (v / 0.14) * Hh;

  const N = 120;
  const pts: string[] = [];
  for (let i = 0; i <= N; i++) {
    const p = i / N;
    if (p > draw) break;
    pts.push(X(p).toFixed(1) + "," + Y(trueSpread(p)).toFixed(1));
  }

  /** The gap the incumbent leaves open: between its flat line and real
   *  uncertainty, for as long as real uncertainty is the larger of the two. */
  const gap: string[] = [];
  const gapEnd = Math.min(draw, CROSS);
  for (let i = 0; i <= N; i++) {
    const p = (i / N) * gapEnd;
    gap.push(X(p).toFixed(1) + "," + Y(trueSpread(p)).toFixed(1));
  }
  gap.push(X(gapEnd).toFixed(1) + "," + Y(VENUE).toFixed(1));
  gap.push("0," + Y(VENUE).toFixed(1));

  return (
    <div style={{ position: "relative", width: W, height: Hh, marginTop: 18 }}>
      <svg width={W} height={Hh} style={{ overflow: "visible" }}>
        {[0.0, 0.035, 0.07, 0.105, 0.14].map((v) => (
          <g key={v}>
            <line x1={0} y1={Y(v)} x2={W} y2={Y(v)} stroke="rgba(126,156,255,0.08)" strokeWidth={1} />
            <text x={-14} y={Y(v) + 6} textAnchor="end" fill={C.inkFaint} fontSize={17} fontFamily={F.mono}>
              {v.toFixed(3)}
            </text>
          </g>
        ))}

        {/* the mispricing, shaded only where it actually exists */}
        <polygon points={gap.join(" ")} fill="rgba(201,107,122,0.16)" opacity={showBands} />

        <line
          x1={0}
          y1={Y(VENUE)}
          x2={W * draw}
          y2={Y(VENUE)}
          stroke={C.book}
          strokeWidth={3}
          strokeDasharray="9 6"
        />
        <polyline
          points={pts.join(" ")}
          fill="none"
          stroke={C.model}
          strokeWidth={3.5}
          style={{ filter: "drop-shadow(0 0 9px " + C.modelGlow + ")" }}
        />

        {draw > 0.02 ? (
          <circle cx={X(Math.min(draw, 1))} cy={Y(trueSpread(Math.min(draw, 1)))} r={5.5} fill={C.model} />
        ) : null}

        {/* leader to the tail, where the flat quote becomes the expensive one */}
        <g opacity={showBands}>
          <line
            x1={X(CROSS)} y1={Y(VENUE)} x2={X(CROSS) - 26} y2={Y(VENUE) - 86}
            stroke={C.warn} strokeWidth={1.5}
          />
          <circle cx={X(CROSS)} cy={Y(VENUE)} r={4} fill={C.warn} />
        </g>

        <line x1={0} y1={Hh} x2={W} y2={Hh} stroke="rgba(126,156,255,0.22)" strokeWidth={1.5} />
        <text x={0} y={Hh + 30} fill={C.inkFaint} fontSize={18} fontFamily={F.mono}>
          WINDOW OPENS
        </text>
        <text x={W} y={Hh + 30} textAnchor="end" fill={C.inkFaint} fontSize={18} fontFamily={F.mono}>
          EXPIRY
        </text>
      </svg>

      <div style={{ position: "absolute", left: 0, top: -44, display: "flex", gap: 34 }}>
        <span style={{ fontFamily: F.mono, fontSize: 19, color: C.model }}>GENUINE UNCERTAINTY</span>
        <span style={{ fontFamily: F.mono, fontSize: 19, color: C.book }}>THE VENUE'S FLAT SPREAD</span>
      </div>

      <div style={{ opacity: showBands }}>
        <div
          style={{
            position: "absolute",
            left: X(0.18),
            top: 96,
            width: 340,
            fontFamily: F.mono,
            fontSize: 23,
            fontWeight: 700,
            color: C.book,
            letterSpacing: 1.5,
          }}
        >
          TOO TIGHT
          <div
            style={{
              fontFamily: F.sans,
              fontSize: 19,
              color: C.inkDim,
              fontWeight: 400,
              letterSpacing: 0,
              marginTop: 4,
            }}
          >
            real uncertainty is far wider than the quote — a static quoter gets picked off
          </div>
        </div>
        <div
          style={{
            position: "absolute",
            left: X(CROSS) - 330,
            top: Y(VENUE) - 150,
            width: 300,
            textAlign: "right",
            fontFamily: F.mono,
            fontSize: 23,
            fontWeight: 700,
            color: C.warn,
            letterSpacing: 1.5,
          }}
        >
          FAR TOO WIDE
          <div
            style={{
              fontFamily: F.sans,
              fontSize: 19,
              color: C.inkDim,
              fontWeight: 400,
              letterSpacing: 0,
              marginTop: 4,
            }}
          >
            near expiry the outcome is all but determined — taking here is structurally negative EV
          </div>
        </div>
      </div>
    </div>
  );
};

const ContractCard: React.FC<{ at: number; until: number }> = ({ at, until }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const tick = Math.max(0, 14 * 60 - Math.floor(Math.max(0, t - at) * 60));
  const mm = String(Math.floor(tick / 60)).padStart(2, "0");
  const ss = String(tick % 60).padStart(2, "0");
  return (
    <In at={at} until={until} rise={22}>
      <div
        style={{
          width: 880,
          background: C.panel,
          border: "1px solid " + C.panelEdge,
          borderRadius: 12,
          padding: "30px 36px",
          boxShadow: "0 26px 80px rgba(0,0,0,0.6)",
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline" }}>
          <span style={{ fontFamily: F.mono, fontSize: 27, color: C.ink, letterSpacing: 2 }}>BTC · 15m</span>
          <span style={{ fontFamily: F.mono, fontSize: 20, color: C.inkFaint, letterSpacing: 2 }}>
            SOMNIA · DREAMDEX
          </span>
        </div>
        <div
          style={{
            fontFamily: F.sans,
            fontSize: 40,
            fontWeight: 600,
            color: C.ink,
            margin: "20px 0 26px",
            letterSpacing: -0.6,
          }}
        >
          Will BTC be higher in 15 minutes?
        </div>
        <div style={{ display: "flex", gap: 16 }}>
          <div
            style={{
              flex: 1,
              border: "1px solid " + C.take + "66",
              background: C.take + "12",
              borderRadius: 8,
              padding: "13px 0",
              textAlign: "center",
              fontFamily: F.mono,
              fontSize: 26,
              fontWeight: 700,
              color: C.take,
              letterSpacing: 2,
            }}
          >
            UP
          </div>
          <div
            style={{
              flex: 1,
              border: "1px solid " + C.book + "66",
              background: C.book + "12",
              borderRadius: 8,
              padding: "13px 0",
              textAlign: "center",
              fontFamily: F.mono,
              fontSize: 26,
              fontWeight: 700,
              color: C.book,
              letterSpacing: 2,
            }}
          >
            DOWN
          </div>
        </div>
        <div
          style={{
            marginTop: 22,
            display: "flex",
            justifyContent: "space-between",
            fontFamily: F.mono,
            fontSize: 20,
            color: C.inkFaint,
            letterSpacing: 1.6,
          }}
        >
          <span>
            SETTLES IN {mm}:{ss}
          </span>
          <span>PAYS 1.00</span>
        </div>
      </div>
    </In>
  );
};

export const Problem: React.FC = () => (
  <AbsoluteFill>
    <Backdrop intensity={0.95} seed="prob" />

    {/* a1_01 - what an event contract is */}
    <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
      <ContractCard at={L("a1_01") - 0.5} until={Lend("a1_01") + 0.1} />
    </AbsoluteFill>

    {/* a1_02 / a1_03 - it is a coin */}
    <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
      <In at={L("a1_02") + 0.2} until={Lend("a1_03") + 0.15} style={{ textAlign: "center" }}>
        <div
          style={{
            fontFamily: F.mono,
            fontSize: 22,
            letterSpacing: 5,
            color: C.inkFaint,
            textTransform: "uppercase",
            marginBottom: 22,
          }}
        >
          10,209 settled windows · measured, not assumed
        </div>
        <div style={{ display: "flex", alignItems: "baseline", justifyContent: "center", gap: 24 }}>
          <span style={{ fontFamily: F.mono, fontSize: 36, color: C.inkDim }}>P(Up) =</span>
          <Counter to={0.4978} decimals={4} at={L("a1_02") + 0.9} dur={1.5} size={150} />
        </div>
        <In at={L("a1_03") + 0.25} rise={10}>
          <div style={{ marginTop: 30, fontFamily: F.sans, fontSize: 46, fontWeight: 600, color: C.ink }}>
            That is a <span style={{ color: C.model }}>fair coin</span>.
          </div>
          <div style={{ marginTop: 14, fontFamily: F.sans, fontSize: 30, color: C.inkDim }}>
            Nobody predicts these markets — and it is pointless to try.
          </div>
        </In>
      </In>
    </AbsoluteFill>

    {/* a1_04 / a1_05 - the spread is mispriced */}
    <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", paddingBottom: 40 }}>
      <In at={L("a1_04")} rise={18} style={{ width: 1180 }}>
        <div style={{ fontFamily: F.sans, fontSize: 40, fontWeight: 600, color: C.ink, marginBottom: 62 }}>
          But every window is quoted the <span style={{ color: C.book }}>same width</span>.
        </div>
        <SpreadChart at={L("a1_04") + 0.5} />
      </In>
    </AbsoluteFill>
  </AbsoluteFill>
);
