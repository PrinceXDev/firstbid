import React from "react";
import { AbsoluteFill, Img, staticFile, useCurrentFrame, interpolate, Easing } from "remotion";
import { C, F } from "../theme";
import { Backdrop } from "../components/Backdrop";
import { act, byId, FPS, TOTAL_SECONDS } from "../timeline";
import { SPEAKER } from "../speaker";

/** THE PITCH — the four lines, then the plate. */

const A = act("PITCH");
const L = (id: string) => byId(id).start - A.start;
const ACT_END = A.end - A.start;
const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

const LINES: [string, string][] = [
  ["MEASURE UNCERTAINTY.", C.model],
  ["PRICE THE MARKET.", C.ink],
  ["TRADE THE EDGE.", C.take],
  ["REFUSE THE UNKNOWN.", C.warn],
];

/** Presenter card, lower right — a portrait if one is supplied. */
const Speaker: React.FC<{ at: number }> = ({ at }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.7], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  // a slow breathing ring, so the card reads as "the person speaking"
  const ring = 0.55 + 0.45 * Math.sin((t - at) * 1.6);

  return (
    <div
      style={{
        position: "absolute",
        right: 58,
        bottom: 132,
        display: "flex",
        alignItems: "center",
        gap: 20,
        opacity: a,
        transform: "translateY(" + (1 - a) * 18 + "px)",
      }}
    >
      <div style={{ textAlign: "right" }}>
        <div style={{ fontFamily: F.sans, fontSize: 28, fontWeight: 600, color: C.ink, letterSpacing: -0.2 }}>
          {SPEAKER.name}
        </div>
        <div style={{ fontFamily: F.mono, fontSize: 17, letterSpacing: 2, color: C.inkFaint, marginTop: 5 }}>
          {SPEAKER.role}
        </div>
      </div>
      <div style={{ position: "relative", width: 128, height: 128 }}>
        <div
          style={{
            position: "absolute",
            inset: -6,
            borderRadius: 999,
            border: "1px solid " + C.model,
            opacity: 0.25 + ring * 0.4,
            boxShadow: "0 0 " + (14 + ring * 18) + "px " + C.modelGlow,
          }}
        />
        <div
          style={{
            width: 128,
            height: 128,
            borderRadius: 999,
            overflow: "hidden",
            border: "2px solid rgba(126,156,255,0.5)",
            background: C.panel,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            boxShadow: "0 14px 44px rgba(0,0,0,0.6)",
          }}
        >
          {SPEAKER.enabled ? (
            <Img
              src={staticFile(SPEAKER.file)}
              style={{ width: "100%", height: "100%", objectFit: "cover", objectPosition: "center 22%" }}
            />
          ) : (
            <span
              style={{
                fontFamily: F.sans,
                fontSize: 46,
                fontWeight: 700,
                color: C.model,
                letterSpacing: 1,
              }}
            >
              {SPEAKER.initials}
            </span>
          )}
        </div>
      </div>
    </div>
  );
};

export const Pitch: React.FC = () => {
  const f = useCurrentFrame();
  const t = f / FPS;

  const linesAt = L("a11_02") - 0.4;
  const plateAt = L("a11_04") + 2.4;

  const linesOut = interpolate(t, [plateAt - 0.5, plateAt + 0.3], [1, 0], clampOp);
  const plateIn = interpolate(t, [plateAt, plateAt + 0.8], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const openerOut = interpolate(t, [linesAt - 0.1, linesAt + 0.5], [1, 0], clampOp);

  return (
    <AbsoluteFill>
      <Backdrop intensity={1} particles={40} seed="pitch" />

      {/* a11_01 — the disclaimer that is actually the thesis */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", opacity: openerOut }}>
        <div
          style={{
            fontFamily: F.sans,
            fontSize: 56,
            fontWeight: 600,
            color: C.ink,
            textAlign: "center",
            opacity: interpolate(t, [L("a11_01") - 0.3, L("a11_01") + 0.4], [0, 1], clampOp),
            letterSpacing: -1,
          }}
        >
          Firstbid doesn't try to <span style={{ color: C.inkFaint, textDecoration: "line-through" }}>predict</span>{" "}
          the future.
        </div>
      </AbsoluteFill>

      {/* the four lines */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", opacity: linesOut }}>
        <div>
          {LINES.map(([text, col], i) => {
            const at = linesAt + 0.35 + i * 1.45;
            const a = interpolate(t, [at, at + 0.5], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
            const lit = interpolate(t, [at, at + 0.5, at + 2.0], [0, 1, 0.34], clampOp);
            return (
              <div
                key={text}
                style={{
                  fontFamily: F.sans,
                  fontSize: 78,
                  fontWeight: 800,
                  letterSpacing: -1.6,
                  color: col,
                  opacity: a,
                  transform: "translateX(" + (1 - a) * -26 + "px)",
                  textShadow: "0 0 " + 34 * lit + "px " + col + "55",
                  lineHeight: 1.22,
                }}
              >
                {text}
              </div>
            );
          })}
        </div>
      </AbsoluteFill>

      {/* the closing plate */}
      <AbsoluteFill style={{ alignItems: "center", justifyContent: "center", opacity: plateIn }}>
        <div style={{ textAlign: "center", transform: "translateY(" + (1 - plateIn) * 16 + "px)" }}>
          <div
            style={{
              fontFamily: F.sans,
              fontSize: 148,
              fontWeight: 800,
              color: C.ink,
              letterSpacing: -3.4,
              lineHeight: 1,
              textShadow: "0 0 70px rgba(126,156,255,0.3)",
            }}
          >
            Firstbid
          </div>
          <div
            style={{
              height: 1,
              width: 560,
              margin: "32px auto 28px",
              background: "linear-gradient(90deg, transparent, " + C.model + ", transparent)",
            }}
          />
          <div
            style={{
              fontFamily: F.mono,
              fontSize: 27,
              letterSpacing: 7,
              color: C.inkDim,
              textTransform: "uppercase",
            }}
          >
            Somnia × DreamDEX
          </div>
          <div
            style={{
              marginTop: 30,
              fontFamily: F.mono,
              fontSize: 24,
              letterSpacing: 2.4,
              color: C.model,
            }}
          >
            {SPEAKER.handle}
          </div>
          <div
            style={{
              marginTop: 34,
              fontFamily: F.sans,
              fontSize: 25,
              color: C.inkFaint,
              maxWidth: 1000,
            }}
          >
            A market maker built around evidence, not assumptions.
          </div>
        </div>
      </AbsoluteFill>

      <Speaker at={L("a11_01") + 0.3} />
    </AbsoluteFill>
  );
};
