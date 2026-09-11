import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { C, F } from "../theme";
import { Backdrop } from "../components/Backdrop";
import { In, Card } from "../components/Kit";
import { act, byId, FPS } from "../timeline";

/** THE AUTOPSY — docs/AUTOPSY.md.
 *  An earlier +59.5% was produced by a backtest that could read the index
 *  price up to 59s after the moment it claimed to predict. Caught twice,
 *  fixed, re-measured at +45.7%. Kept in the film because the method is the
 *  point, not the number. */

const A = act("AUTOPSY");
const L = (id: string) => byId(id).start - A.start;
const Lend = (id: string) => byId(id).end - A.start;
const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

/** The leak, drawn on a time axis. */
const LookAhead: React.FC<{ at: number }> = ({ at }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const axis = interpolate(t, [at, at + 0.6], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const band = interpolate(t, [at + 0.8, at + 1.8], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const leak = interpolate(t, [at + 2.0, at + 3.0], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const verdict = interpolate(t, [at + 3.2, at + 3.8], [0, 1], clampOp);

  const W = 1240;
  const H = 210;
  const x0 = 150; // the predicted moment
  const perSec = (W - x0 - 120) / 70;
  const x59 = x0 + 59 * perSec;

  return (
    <div style={{ position: "relative", width: W, height: H + 120 }}>
      <svg width={W} height={H + 120} style={{ overflow: "visible" }}>
        {/* the window's own price path, drawn only to make time legible */}
        <line x1={0} y1={H} x2={W * axis} y2={H} stroke="rgba(126,156,255,0.28)" strokeWidth={2} />

        {/* the illegal window of future information */}
        <g opacity={band}>
          <rect x={x0} y={40} width={(x59 - x0) * band} height={H - 40} fill="rgba(201,107,122,0.13)" />
          <line x1={x59} y1={40} x2={x59} y2={H} stroke={C.book} strokeWidth={2} strokeDasharray="6 5" />
        </g>

        {/* the moment being predicted */}
        <g opacity={axis}>
          <line x1={x0} y1={20} x2={x0} y2={H + 14} stroke={C.model} strokeWidth={3} />
          <circle cx={x0} cy={H} r={7} fill={C.model} />
        </g>

        {/* the leak: future price pulled back into the prediction */}
        <g opacity={leak}>
          <path
            d={"M " + x59 + " " + 70 + " C " + (x59 - 90) + " " + 4 + ", " + (x0 + 90) + " " + 4 + ", " + (x0 + 12) + " " + 62}
            fill="none"
            stroke={C.book}
            strokeWidth={2.5}
            strokeDasharray={String(520) + " " + String(520)}
            strokeDashoffset={String(520 * (1 - leak))}
          />
          <polygon
            points={x0 + 12 + "," + 62 + " " + (x0 + 24) + "," + 46 + " " + (x0 + 2) + "," + 44}
            fill={C.book}
            opacity={leak > 0.9 ? 1 : 0}
          />
        </g>

        <text x={x0} y={H + 42} textAnchor="middle" fill={C.model} fontSize={20} fontFamily={F.mono}>
          t
        </text>
        <text x={x0} y={H + 66} textAnchor="middle" fill={C.inkFaint} fontSize={17} fontFamily={F.sans}>
          the moment it claimed to predict
        </text>
        <text x={x59} y={H + 42} textAnchor="middle" fill={C.book} fontSize={20} fontFamily={F.mono} opacity={band}>
          t + 59s
        </text>
        <text x={x59} y={H + 66} textAnchor="middle" fill={C.inkFaint} fontSize={17} fontFamily={F.sans} opacity={band}>
          price it could still read
        </text>
      </svg>

      <div
        style={{
          position: "absolute",
          left: x0 + 26,
          top: 96,
          opacity: band,
          fontFamily: F.mono,
          fontSize: 23,
          letterSpacing: 2,
          color: C.book,
        }}
      >
        LOOK-AHEAD
      </div>
      <div
        style={{
          position: "absolute",
          left: x0 + 26,
          top: 128,
          opacity: verdict,
          fontFamily: F.sans,
          fontSize: 22,
          color: C.inkDim,
          maxWidth: 620,
        }}
      >
        roughly 1σ at BTC's fitted volatility — enough to make the model badly overconfident
      </div>
    </div>
  );
};

const Chain: React.FC<{ at: number }> = ({ at }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const steps = ["MEASURE", "FIND THE ERROR", "FIX", "RE-MEASURE", "SHIP"];
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
      {steps.map((s, i) => {
        const a = interpolate(t, [at + i * 0.28, at + 0.32 + i * 0.28], [0, 1], clampOp);
        const last = i === steps.length - 1;
        return (
          <React.Fragment key={s}>
            <div
              style={{
                opacity: a,
                transform: "translateY(" + (1 - a) * 8 + "px)",
                fontFamily: F.mono,
                fontSize: 23,
                fontWeight: 700,
                letterSpacing: 1.8,
                color: last ? C.take : C.ink,
                border: "1px solid " + (last ? C.take : C.panelEdge),
                background: last ? C.take + "12" : C.panel,
                borderRadius: 6,
                padding: "10px 18px",
                whiteSpace: "nowrap",
              }}
            >
              {s}
            </div>
            {!last ? (
              <span style={{ opacity: a, color: C.inkFaint, fontSize: 24, fontFamily: F.mono }}>→</span>
            ) : null}
          </React.Fragment>
        );
      })}
    </div>
  );
};

export const Autopsy: React.FC = () => {
  const f = useCurrentFrame();
  const t = f / FPS;

  const strike = interpolate(t, [L("a6_02") + 0.3, L("a6_02") + 1.1], [0, 1], {
    ...clampOp,
    easing: Easing.out(Easing.cubic),
  });

  return (
    <AbsoluteFill>
      <Backdrop intensity={0.72} particles={22} seed="autopsy" />

      {/* a6_01 / a6_02 — the discarded number */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
        <In at={L("a6_01") - 0.2} until={Lend("a6_02") - 4.6} rise={16} style={{ textAlign: "center" }}>
          <div
            style={{
              fontFamily: F.mono,
              fontSize: 22,
              letterSpacing: 5,
              color: C.inkFaint,
              textTransform: "uppercase",
              marginBottom: 26,
            }}
          >
            an earlier version of this README claimed
          </div>
          <div style={{ position: "relative", display: "inline-block" }}>
            <span
              style={{
                fontFamily: F.mono,
                fontSize: 150,
                fontWeight: 700,
                color: C.inkDim,
                letterSpacing: -2,
              }}
            >
              +59.5%
            </span>
            <div
              style={{
                position: "absolute",
                left: -14,
                right: -14,
                top: "52%",
                height: 6,
                background: C.book,
                transformOrigin: "0 50%",
                transform: "scaleX(" + strike + ")",
                boxShadow: "0 0 20px " + C.book,
              }}
            />
          </div>
          <div
            style={{
              marginTop: 26,
              fontFamily: F.sans,
              fontSize: 34,
              color: C.ink,
              opacity: strike,
            }}
          >
            It was <span style={{ color: C.book }}>wrong</span>.
          </div>
        </In>
      </AbsoluteFill>

      {/* a6_02 — the look-ahead, drawn */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
        <In at={Lend("a6_02") - 4.4} until={Lend("a6_03") - 4.0} rise={18}>
          <div
            style={{
              fontFamily: F.sans,
              fontSize: 36,
              fontWeight: 600,
              color: C.ink,
              marginBottom: 46,
              textAlign: "center",
            }}
          >
            Our own backtest was reading the index price{" "}
            <span style={{ color: C.book }}>after</span> the moment it predicted.
          </div>
          <LookAhead at={Lend("a6_02") - 4.0} />
        </In>
      </AbsoluteFill>

      {/* a6_03 — what it cost */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
        <In at={Lend("a6_03") - 4.0} until={Lend("a6_03") + 0.2} rise={16}>
          <div style={{ display: "flex", gap: 26 }}>
            <Card accent={C.book} style={{ width: 430, textAlign: "center" }}>
              <div style={{ fontFamily: F.mono, fontSize: 76, fontWeight: 700, color: C.book }}>−37%</div>
              <div
                style={{
                  fontFamily: F.mono,
                  fontSize: 19,
                  letterSpacing: 2.4,
                  color: C.inkFaint,
                  textTransform: "uppercase",
                  marginTop: 10,
                }}
              >
                of deployed capital
              </div>
            </Card>
            <Card accent={C.book} style={{ width: 430, textAlign: "center" }}>
              <div style={{ fontFamily: F.mono, fontSize: 76, fontWeight: 700, color: C.ink }}>53</div>
              <div
                style={{
                  fontFamily: F.mono,
                  fontSize: 19,
                  letterSpacing: 2.4,
                  color: C.inkFaint,
                  textTransform: "uppercase",
                  marginTop: 10,
                }}
              >
                live fills it cost it across
              </div>
            </Card>
          </div>
          <div
            style={{
              marginTop: 30,
              textAlign: "center",
              fontFamily: F.sans,
              fontSize: 26,
              color: C.inkDim,
            }}
          >
            An overconfident model does not lose slowly.
          </div>
        </In>
      </AbsoluteFill>

      {/* a6_04 — caught twice, then the method */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
        <In at={L("a6_04") - 0.2} until={Lend("a6_04") + 0.25} rise={18}>
          <div style={{ display: "flex", gap: 22, justifyContent: "center", marginBottom: 46 }}>
            {[
              ["CODE REVIEW", "someone read the timestamp boundary"],
              ["LIVE P&L ATTRIBUTION", "edge stayed positive while selection collapsed"],
            ].map(([h, s], i) => (
              <Card key={h} accent={C.take} style={{ width: 520 }}>
                <div style={{ fontFamily: F.mono, fontSize: 22, fontWeight: 700, letterSpacing: 2, color: C.take }}>
                  {h}
                </div>
                <div style={{ fontFamily: F.sans, fontSize: 21, color: C.inkDim, marginTop: 9 }}>{s}</div>
              </Card>
            ))}
          </div>
          <div style={{ display: "flex", justifyContent: "center" }}>
            <Chain at={L("a6_04") + 2.4} />
          </div>
          <div
            style={{
              marginTop: 34,
              textAlign: "center",
              fontFamily: F.sans,
              fontSize: 24,
              color: C.inkFaint,
            }}
          >
            Two independent instruments pointing at one line of code.
          </div>
        </In>
      </AbsoluteFill>

      {/* a6_05 — the honest number */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
        <In at={L("a6_05") - 0.35} rise={14} style={{ textAlign: "center" }}>
          <div
            style={{
              fontFamily: F.mono,
              fontSize: 150,
              fontWeight: 700,
              color: C.take,
              letterSpacing: -2,
              textShadow: "0 0 60px rgba(78,201,154,0.32)",
            }}
          >
            +45.7%
          </div>
          <div
            style={{
              marginTop: 20,
              fontFamily: F.mono,
              fontSize: 25,
              letterSpacing: 6,
              color: C.inkDim,
              textTransform: "uppercase",
            }}
          >
            the honest number
          </div>
        </In>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};
