import React from "react";
import { useCurrentFrame, interpolate, Easing, spring, useVideoConfig } from "remotion";
import { C, F, VIEW } from "../theme";
import { project, shotRect, type Rect, type Shot } from "./Footage";
import { FPS } from "../timeline";

type Side = "right" | "left" | "top" | "bottom";

const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

/** A box drawn around a region of the RECORDING, with an optional label chip.
 *  Coordinates are source pixels, so the overlay tracks the camera move. */
export const Callout: React.FC<{
  shot: Shot;
  durationInFrames: number;
  /** region to ring, in source coords */
  box: Rect;
  label?: string;
  note?: string;
  side?: Side;
  /** seconds into this sequence */
  appearAt?: number;
  holdUntil?: number;
  color?: string;
  /** draw only the label, no ring */
  bare?: boolean;
}> = ({
  shot, durationInFrames, box, label, note, side = "right",
  appearAt = 0, holdUntil, color = C.model, bare = false,
}) => {
  const f = useCurrentFrame();
  const { fps } = useVideoConfig();
  const t = f / FPS;
  const out = holdUntil ?? durationInFrames / FPS + 1;

  if (t < appearAt - 0.4 || t > out + 0.5) return null;

  const r = shotRect(shot, durationInFrames, f);
  const a = project(r, box.x, box.y);
  const bb = project(r, box.x + box.w, box.y + box.h);
  const w = bb.x - a.x;
  const h = bb.y - a.y;

  const grow = spring({
    frame: f - Math.round(appearAt * FPS),
    fps,
    config: { damping: 200, mass: 0.6, stiffness: 110 },
    durationInFrames: 18,
  });
  const fade = interpolate(t, [out, out + 0.45], [1, 0], clampOp);
  const op = grow * fade;
  // a gentle breathing glow so the eye is pulled without a flash
  const pulse = 0.72 + 0.28 * Math.sin((f / FPS) * 3.1);

  // label placement around the ring
  const gap = 18;
  const pos: React.CSSProperties =
    side === "right" ? { left: a.x + w + gap, top: a.y + h / 2, transform: "translateY(-50%)" }
    : side === "left" ? { left: a.x - gap, top: a.y + h / 2, transform: "translate(-100%,-50%)" }
    : side === "top" ? { left: a.x + w / 2, top: a.y - gap, transform: "translate(-50%,-100%)" }
    : { left: a.x + w / 2, top: a.y + h + gap, transform: "translate(-50%,0)" };

  return (
    <>
      {!bare && (
        <div
          style={{
            position: "absolute",
            left: a.x, top: a.y,
            width: Math.max(0, w) * grow + (1 - grow) * Math.max(0, w) * 0.88,
            height: Math.max(0, h),
            border: `2px solid ${color}`,
            borderRadius: 7,
            boxShadow: `0 0 ${16 * pulse}px ${color}55, inset 0 0 ${22 * pulse}px ${color}1f`,
            opacity: op,
            transform: `scale(${0.97 + 0.03 * grow})`,
            transformOrigin: "center",
            pointerEvents: "none",
          }}
        />
      )}
      {(label || note) && (
        <div
          style={{
            position: "absolute",
            ...pos,
            opacity: op,
            pointerEvents: "none",
            maxWidth: 430,
          }}
        >
          <div
            style={{
              background: "rgba(6,9,14,0.93)",
              border: `1px solid ${color}66`,
              borderLeft: `3px solid ${color}`,
              borderRadius: 6,
              padding: "9px 15px",
              boxShadow: "0 8px 30px rgba(0,0,0,0.6)",
            }}
          >
            {label && (
              <div
                style={{
                  fontFamily: F.mono,
                  fontSize: 25,
                  fontWeight: 700,
                  color,
                  letterSpacing: 1.4,
                  textTransform: "uppercase",
                  whiteSpace: "nowrap",
                }}
              >
                {label}
              </div>
            )}
            {note && (
              <div
                style={{
                  fontFamily: F.sans,
                  fontSize: 21,
                  color: C.inkDim,
                  marginTop: label ? 3 : 0,
                  lineHeight: 1.3,
                }}
              >
                {note}
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
};

/** Darken everything except one region of the recording. */
export const Spotlight: React.FC<{
  shot: Shot;
  durationInFrames: number;
  box: Rect;
  appearAt?: number;
  holdUntil?: number;
  strength?: number;
  pad?: number;
}> = ({ shot, durationInFrames, box, appearAt = 0, holdUntil, strength = 0.66, pad = 10 }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const out = holdUntil ?? durationInFrames / FPS + 1;
  if (t < appearAt - 0.35 || t > out + 0.5) return null;

  const r = shotRect(shot, durationInFrames, f);
  const a = project(r, box.x, box.y);
  const bb = project(r, box.x + box.w, box.y + box.h);

  const op =
    interpolate(t, [appearAt - 0.35, appearAt + 0.2], [0, strength], clampOp) *
    interpolate(t, [out, out + 0.45], [1, 0], clampOp);

  const x0 = a.x - pad, y0 = a.y - pad;
  const x1 = bb.x + pad, y1 = bb.y + pad;

  return (
    <div
      style={{
        position: "absolute",
        left: VIEW.x, top: VIEW.y, width: VIEW.w, height: VIEW.h,
        pointerEvents: "none",
        opacity: op,
        background: "rgba(3,5,8,1)",
        WebkitMaskImage: `linear-gradient(#000,#000)`,
        clipPath: `polygon(
          0 0, 100% 0, 100% 100%, 0 100%, 0 0,
          ${x0 - VIEW.x}px ${y0 - VIEW.y}px,
          ${x0 - VIEW.x}px ${y1 - VIEW.y}px,
          ${x1 - VIEW.x}px ${y1 - VIEW.y}px,
          ${x1 - VIEW.x}px ${y0 - VIEW.y}px,
          ${x0 - VIEW.x}px ${y0 - VIEW.y}px
        )`,
      }}
    />
  );
};

/** Thin animated arrow from a label toward a point in the recording. */
export const Arrow: React.FC<{
  shot: Shot;
  durationInFrames: number;
  fromPt: { x: number; y: number };
  toPt: { x: number; y: number };
  appearAt?: number;
  holdUntil?: number;
  color?: string;
}> = ({ shot, durationInFrames, fromPt, toPt, appearAt = 0, holdUntil, color = C.model }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const out = holdUntil ?? durationInFrames / FPS + 1;
  if (t < appearAt || t > out + 0.5) return null;

  const r = shotRect(shot, durationInFrames, f);
  const p0 = project(r, fromPt.x, fromPt.y);
  const p1 = project(r, toPt.x, toPt.y);

  const draw = interpolate(t, [appearAt, appearAt + 0.45], [0, 1], {
    ...clampOp,
    easing: Easing.out(Easing.cubic),
  });
  const fade = interpolate(t, [out, out + 0.4], [1, 0], clampOp);

  const dx = p1.x - p0.x, dy = p1.y - p0.y;
  const len = Math.hypot(dx, dy);
  const ang = (Math.atan2(dy, dx) * 180) / Math.PI;

  return (
    <div style={{ position: "absolute", left: p0.x, top: p0.y, opacity: fade, pointerEvents: "none" }}>
      <div
        style={{
          width: len * draw,
          height: 2,
          background: `linear-gradient(90deg, ${color}22, ${color})`,
          transform: `rotate(${ang}deg)`,
          transformOrigin: "0 50%",
          boxShadow: `0 0 8px ${color}66`,
        }}
      />
      <div
        style={{
          position: "absolute",
          left: dx * draw, top: dy * draw,
          width: 9, height: 9,
          marginLeft: -4.5, marginTop: -4.5,
          borderRadius: 9,
          background: color,
          boxShadow: `0 0 12px ${color}`,
          opacity: draw,
        }}
      />
    </div>
  );
};
