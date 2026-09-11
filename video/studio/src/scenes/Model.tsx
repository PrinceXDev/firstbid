import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { C, F } from "../theme";
import { Backdrop } from "../components/Backdrop";
import { In } from "../components/Kit";
import { act, byId, FPS } from "../timeline";

/** THE MODEL — one line, no directional view.
 *  Formula and sigma table are from README.md / internal/model/fair.go. */

const A = act("MODEL");
const L = (id: string) => byId(id).start - A.start;
const Lend = (id: string) => byId(id).end - A.start;
const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

/** Measured volatility per asset and cadence (README.md sigma table). */
const VOL: [string, string, string, boolean][] = [
  ["BTC / 5m", "512", "0.000513", false],
  ["BTC / 15m", "2,880", "0.000504", false],
  ["BTC / 60m", "720", "0.000517", false],
  ["BTC / 240m", "177", "0.000560", true],
  ["ETH / 5m", "470", "0.000683", false],
  ["ETH / 15m", "2,880", "0.000681", false],
  ["ETH / 60m", "720", "0.000707", false],
];

const Term: React.FC<{
  children: React.ReactNode; at: number; note?: string; color?: string; noteSide?: "top" | "bottom";
}> = ({ children, at, note, color = C.ink, noteSide = "bottom" }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.34], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const lit = interpolate(t, [at, at + 0.34, at + 1.5], [0, 1, 0.16], clampOp);
  return (
    <span style={{ position: "relative", display: "inline-block", opacity: a, color }}>
      <span style={{ textShadow: "0 0 " + 26 * lit + "px " + C.modelGlow }}>{children}</span>
      {note ? (
        <span
          style={{
            position: "absolute",
            left: "50%",
            [noteSide === "bottom" ? "top" : "bottom"]: "108%",
            transform: "translateX(-50%)",
            whiteSpace: "nowrap",
            fontFamily: F.mono,
            fontSize: 19,
            letterSpacing: 1.6,
            color: C.inkFaint,
            opacity: interpolate(t, [at + 0.3, at + 0.75], [0, 1], clampOp),
          } as React.CSSProperties}
        >
          {note}
        </span>
      ) : null}
    </span>
  );
};

export const Model: React.FC = () => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const base = L("a4_01") + 0.1;
  const tableAt = L("a4_03") - 0.3;

  const formulaOut = interpolate(t, [tableAt - 0.2, tableAt + 0.5], [1, 0.0], clampOp);
  const formulaLift = interpolate(t, [tableAt - 0.2, tableAt + 0.5], [0, -70], clampOp);

  return (
    <AbsoluteFill>
      <Backdrop intensity={0.8} particles={26} seed="model" />

      {/* the formula */}
      <AbsoluteFill
        style={{ alignItems: "center", justifyContent: "center", opacity: formulaOut, transform: "translateY(" + formulaLift + "px)" }}
      >
        <div style={{ textAlign: "center" }}>
          <In at={base} rise={14}>
            <div
              style={{
                fontFamily: F.mono,
                fontSize: 23,
                letterSpacing: 5.5,
                color: C.model,
                textTransform: "uppercase",
                marginBottom: 58,
              }}
            >
              fair value · no directional view
            </div>
          </In>

          <div
            style={{
              fontFamily: F.mono,
              fontSize: 62,
              color: C.ink,
              letterSpacing: -0.5,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              gap: 14,
              lineHeight: 2.4,
            }}
          >
            <Term at={base + 0.5} note="probability of UP" color={C.model}>
              P(Up)
            </Term>
            <Term at={base + 0.9}>=</Term>
            <Term at={base + 1.2} note="the normal CDF" noteSide="top" color={C.model}>
              Φ
            </Term>
            <Term at={base + 1.5}>(</Term>
            <Term at={base + 2.4} note="how far spot has moved" noteSide="top">
              ln( spot / open )
            </Term>
            <Term at={base + 3.0}>/</Term>
            <Term at={base + 4.2} note="measured volatility × time left" color={C.warn}>
              ( σ · √minutes )
            </Term>
            <Term at={base + 4.7}>)</Term>
          </div>

          <In at={base + 6.0} rise={12}>
            <div style={{ marginTop: 64, fontFamily: F.sans, fontSize: 32, color: C.inkDim }}>
              The probability a <span style={{ color: C.ink }}>driftless random walk</span> finishes
              above where it started.
            </div>
            <div style={{ marginTop: 14, fontFamily: F.sans, fontSize: 26, color: C.inkFaint }}>
              No view is taken on direction — only on how uncertain the outcome still is.
            </div>
          </In>
        </div>
      </AbsoluteFill>

      {/* the measured sigma table */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
        <In at={tableAt} rise={22}>
          <div style={{ textAlign: "center", marginBottom: 34 }}>
            <div style={{ fontFamily: F.sans, fontSize: 42, fontWeight: 600, color: C.ink }}>
              σ is <span style={{ color: C.model }}>measured</span>, never assumed.
            </div>
          </div>
          <div
            style={{
              background: C.panel,
              border: "1px solid " + C.panelEdge,
              borderRadius: 10,
              padding: "24px 34px",
              boxShadow: "0 26px 80px rgba(0,0,0,0.6)",
            }}
          >
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "230px 190px 230px",
                fontFamily: F.mono,
                fontSize: 19,
                letterSpacing: 2.4,
                color: C.inkFaint,
                textTransform: "uppercase",
                paddingBottom: 14,
                borderBottom: "1px solid " + C.panelEdge,
              }}
            >
              <span>series</span>
              <span style={{ textAlign: "right" }}>windows fitted</span>
              <span style={{ textAlign: "right" }}>σ per minute</span>
            </div>
            {VOL.map(([series, n, s, scaled], i) => {
              const a = interpolate(t, [tableAt + 0.4 + i * 0.14, tableAt + 0.75 + i * 0.14], [0, 1], clampOp);
              return (
                <div
                  key={series}
                  style={{
                    display: "grid",
                    gridTemplateColumns: "230px 190px 230px",
                    fontFamily: F.mono,
                    fontSize: 26,
                    padding: "11px 0",
                    borderBottom: i === VOL.length - 1 ? "none" : "1px solid rgba(126,150,190,0.07)",
                    opacity: a,
                    transform: "translateX(" + (1 - a) * -12 + "px)",
                  }}
                >
                  <span style={{ color: C.ink }}>{series}</span>
                  <span style={{ textAlign: "right", color: C.inkDim, fontVariantNumeric: "tabular-nums" }}>{n}</span>
                  <span
                    style={{
                      textAlign: "right",
                      color: scaled ? C.warn : C.model,
                      fontVariantNumeric: "tabular-nums",
                    }}
                  >
                    {s}
                  </span>
                </div>
              );
            })}
          </div>
          <In at={tableAt + 1.9} rise={8}>
            <div
              style={{
                marginTop: 22,
                textAlign: "center",
                fontFamily: F.sans,
                fontSize: 23,
                color: C.inkFaint,
              }}
            >
              Stable within an asset across a 12× range of window lengths — the √t the model assumes,
              <span style={{ color: C.inkDim }}> measured rather than asserted</span>.
            </div>
          </In>
        </In>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};
