import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { C, F } from "../theme";
import { Backdrop } from "../components/Backdrop";
import { In } from "../components/Kit";
import { act, byId, FPS } from "../timeline";

/** THE MECHANISM — zero-inventory two-sided quoting.
 *  Up and Down share one book. When a Buy Up crosses a Buy Down, the pool
 *  mints a fresh pair from their combined collateral: no seller needed, and a
 *  matched pair redeems to exactly 1.00 whoever wins. (README.md) */

const A = act("MECHANISM");
const L = (id: string) => byId(id).start - A.start;
const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

const Side: React.FC<{
  title: string; price: string; col: string; at: number; slideTo: number;
}> = ({ title, price, col, at, slideTo }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.45], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const slide = interpolate(t, [slideTo, slideTo + 0.8], [0, 1], {
    ...clampOp,
    easing: Easing.inOut(Easing.cubic),
  });
  // they converge, then hand off to the minted pair rather than passing through
  const handOff = interpolate(t, [slideTo + 0.55, slideTo + 1.0], [1, 0.12], clampOp);
  const dir = title.includes("UP") ? 1 : -1;

  return (
    <div
      style={{
        opacity: a * handOff,
        transform: "translateX(" + dir * slide * 88 + "px) scale(" + (1 - slide * 0.08) + ")",
        width: 380,
        background: C.panel,
        border: "1px solid " + col + "66",
        borderTop: "2px solid " + col,
        borderRadius: 10,
        padding: "22px 26px",
        textAlign: "center",
        boxShadow: "0 16px 50px rgba(0,0,0,0.5)",
      }}
    >
      <div style={{ fontFamily: F.mono, fontSize: 25, fontWeight: 700, letterSpacing: 2, color: col }}>
        {title}
      </div>
      <div
        style={{
          fontFamily: F.mono,
          fontSize: 54,
          fontWeight: 700,
          color: C.ink,
          marginTop: 10,
          fontVariantNumeric: "tabular-nums",
        }}
      >
        {price}
      </div>
      <div style={{ fontFamily: F.mono, fontSize: 17, letterSpacing: 2, color: C.inkFaint, marginTop: 8 }}>
        RESTING BID
      </div>
    </div>
  );
};

export const Mechanism: React.FC = () => {
  const f = useCurrentFrame();
  const t = f / FPS;

  const b1 = L("a9_02");
  const b2 = L("a9_03");
  const cross = b1 + 4.6;   // the two bids meet
  const mint = b1 + 5.7;    // the pool mints a pair

  const mintA = interpolate(t, [mint, mint + 0.6], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const flash = interpolate(t, [mint, mint + 0.45], [1, 0], clampOp);

  return (
    <AbsoluteFill>
      <Backdrop intensity={0.7} particles={22} seed="mech" />

      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
        <div style={{ textAlign: "center" }}>
          <In at={b1 - 0.2} rise={12}>
            <div
              style={{
                fontFamily: F.mono,
                fontSize: 22,
                letterSpacing: 5.5,
                color: C.model,
                textTransform: "uppercase",
                marginBottom: 52,
              }}
            >
              zero-inventory two-sided quoting
            </div>
          </In>

          {/* the two resting bids, which then cross */}
          <div style={{ display: "flex", gap: 60, justifyContent: "center", alignItems: "center" }}>
            <Side title="BUY UP" price="0.62" col={C.take} at={b1 + 1.0} slideTo={cross} />
            <div
              style={{
                fontFamily: F.mono,
                fontSize: 30,
                color: C.inkFaint,
                opacity: interpolate(t, [cross, cross + 0.5], [1, 0], clampOp),
              }}
            >
              +
            </div>
            <Side title="BUY DOWN" price="0.38" col={C.book} at={b1 + 1.9} slideTo={cross} />
          </div>

          {/* the mint */}
          <div style={{ position: "relative", height: 300, marginTop: 24 }}>
            <div
              style={{
                position: "absolute",
                left: "50%",
                top: 8,
                transform: "translateX(-50%)",
                opacity: interpolate(t, [cross + 0.5, cross + 1.0], [0, 1], clampOp),
                fontFamily: F.mono,
                fontSize: 30,
                color: C.inkFaint,
              }}
            >
              ↓
            </div>

            <div
              style={{
                position: "absolute",
                left: "50%",
                top: 52,
                transform: "translateX(-50%) scale(" + (0.9 + 0.1 * mintA) + ")",
                opacity: mintA,
                width: 700,
                background: "rgba(126,156,255,0.08)",
                border: "2px solid " + C.model,
                borderRadius: 12,
                padding: "24px 30px",
                boxShadow: "0 0 " + (40 + flash * 90) + "px rgba(126,156,255," + (0.28 + flash * 0.5) + ")",
              }}
            >
              <div style={{ fontFamily: F.mono, fontSize: 26, fontWeight: 700, letterSpacing: 2.6, color: C.model }}>
                MINT_A_PAIR
              </div>
              <div style={{ fontFamily: F.sans, fontSize: 24, color: C.inkDim, marginTop: 10 }}>
                the pool mints a fresh pair from their combined collateral —{" "}
                <span style={{ color: C.ink }}>no seller required</span>
              </div>
              <div
                style={{
                  marginTop: 18,
                  paddingTop: 16,
                  borderTop: "1px solid rgba(126,156,255,0.22)",
                  display: "flex",
                  justifyContent: "center",
                  alignItems: "baseline",
                  gap: 16,
                  fontFamily: F.mono,
                }}
              >
                <span style={{ fontSize: 26, color: C.take }}>0.62</span>
                <span style={{ fontSize: 22, color: C.inkFaint }}>+</span>
                <span style={{ fontSize: 26, color: C.book }}>0.38</span>
                <span style={{ fontSize: 22, color: C.inkFaint }}>=</span>
                <span style={{ fontSize: 42, fontWeight: 700, color: C.ink }}>1.00</span>
              </div>
            </div>

            {/* the conclusion */}
            <In at={b2 + 1.4} rise={12}>
              <div
                style={{
                  position: "absolute",
                  left: "50%",
                  top: 304,
                  transform: "translateX(-50%)",
                  width: 1180,
                  fontFamily: F.sans,
                  fontSize: 30,
                  color: C.inkDim,
                  lineHeight: 1.45,
                }}
              >
                A complete two-sided quote with{" "}
                <span style={{ color: C.model, fontWeight: 600 }}>no inventory</span> and{" "}
                <span style={{ color: C.model, fontWeight: 600 }}>no directional risk</span>. A matched
                pair redeems to exactly 1.00 — whoever wins.
              </div>
            </In>
          </div>
        </div>
      </AbsoluteFill>

      {/* measured share of fills, from the venue */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "flex-end", paddingBottom: 44 }}>
        <In at={b2 + 4.4} rise={10}>
          <div
            style={{
              fontFamily: F.mono,
              fontSize: 21,
              letterSpacing: 3,
              color: C.inkFaint,
              textTransform: "uppercase",
            }}
          >
            measured at <span style={{ color: C.model }}>22–42%</span> of all fills on this venue
          </div>
        </In>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};
