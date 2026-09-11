import React from "react";
import { AbsoluteFill, useCurrentFrame, interpolate, Easing } from "remotion";
import { C, F } from "../theme";
import { Backdrop } from "../components/Backdrop";
import { act, byId, FPS } from "../timeline";

const A = act("TITLE");
const L = (id: string) => byId(id).start - A.start;
const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

/** Brand plate. Lands on the downbeat of "Firstbid is a market maker...". */
export const Title: React.FC = () => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const at = L("a1_06") - 0.35;

  const rise = interpolate(t, [at, at + 0.7], [0, 1], {
    ...clampOp,
    easing: Easing.out(Easing.cubic),
  });
  const track = interpolate(t, [at, at + 1.4], [26, 8], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const ruleW = interpolate(t, [at + 0.35, at + 1.2], [0, 620], {
    ...clampOp,
    easing: Easing.out(Easing.cubic),
  });
  const subIn = interpolate(t, [at + 0.75, at + 1.3], [0, 1], clampOp);
  const flare = interpolate(t, [at, at + 0.55], [1, 0], clampOp);

  return (
    <AbsoluteFill>
      <Backdrop intensity={1} particles={46} seed="title" />
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center" }}>
        <div style={{ textAlign: "center", opacity: rise, transform: "translateY(" + (1 - rise) * 18 + "px)" }}>
          <div
            style={{
              fontFamily: F.sans,
              fontSize: 156,
              fontWeight: 800,
              color: C.ink,
              letterSpacing: -track * 0.12,
              lineHeight: 1,
              textShadow: "0 0 " + (40 + flare * 90) + "px rgba(126,156,255," + (0.24 + flare * 0.4) + ")",
            }}
          >
            Firstbid
          </div>
          <div
            style={{
              height: 1,
              width: ruleW,
              margin: "30px auto 26px",
              background: "linear-gradient(90deg, transparent, " + C.model + ", transparent)",
              boxShadow: "0 0 14px " + C.modelGlow,
            }}
          />
          <div
            style={{
              fontFamily: F.mono,
              fontSize: 27,
              letterSpacing: 8,
              color: C.inkDim,
              textTransform: "uppercase",
              opacity: subIn,
            }}
          >
            a calibrated market maker
          </div>
          <div
            style={{
              marginTop: 18,
              fontFamily: F.mono,
              fontSize: 21,
              letterSpacing: 4.5,
              color: C.inkFaint,
              textTransform: "uppercase",
              opacity: subIn,
            }}
          >
            pure Go · DreamDEX event contracts · Somnia Shannon
          </div>
        </div>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};
