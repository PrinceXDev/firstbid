import React from "react";
import {
  AbsoluteFill, OffthreadVideo, staticFile, useCurrentFrame,
  interpolate, Easing,
} from "remotion";
import { C, SRC, VIEW, FULL } from "../theme";
import { FPS } from "../timeline";

/** A camera rect in SOURCE pixel coordinates (the raw 1920x1028 recording).
 *  The rect is what fills the footage viewport, so a smaller rect = a zoom. */
export type Rect = { x: number; y: number; w: number; h: number };

export const full = (): Rect => ({ ...FULL });

/** Build a rect centred on a point in source coords at a given zoom. */
export const at = (cx: number, cy: number, zoom: number): Rect => {
  const w = VIEW.w / zoom;
  const h = VIEW.h / zoom;
  let x = cx - w / 2;
  let y = cy - h / 2;
  // keep the camera inside the cropped page
  x = Math.max(0, Math.min(SRC.w - w, x));
  y = Math.max(SRC.chrome, Math.min(SRC.h - h, y));
  return { x, y, w, h };
};

const lerpRect = (a: Rect, b: Rect, p: number): Rect => ({
  x: a.x + (b.x - a.x) * p,
  y: a.y + (b.y - a.y) * p,
  w: a.w + (b.w - a.w) * p,
  h: a.h + (b.h - a.h) * p,
});

/** CSS transform that makes `r` exactly fill the viewport, at 1:1 when
 *  r.w === 1920 (so un-zoomed footage is never resampled). */
const transformFor = (r: Rect) => {
  const s = VIEW.w / r.w;
  return {
    transform: `scale(${s}) translate(${-r.x}px, ${-r.y}px)`,
    transformOrigin: "0 0" as const,
  };
};

export type Shot = {
  /** source in/out, in seconds of the recording */
  srcIn: number;
  srcOut: number;
  /** camera move; omit `to` to hold */
  from?: Rect;
  to?: Rect;
  /** ease the move (default: gentle inOut) */
  easing?: (t: number) => number;
};

/** One continuous piece of the recording, re-timed to fill `durationInFrames`
 *  and framed by a camera move. Placed by a parent <Sequence>. */
export const Footage: React.FC<{
  shot: Shot;
  durationInFrames: number;
  /** dim the page so an overlay can read on top */
  dim?: number;
}> = ({ shot, durationInFrames, dim = 0 }) => {
  const f = useCurrentFrame();
  const p = durationInFrames <= 1 ? 0 : f / (durationInFrames - 1);

  const a = shot.from ?? full();
  const b = shot.to ?? a;
  const ease = shot.easing ?? Easing.inOut(Easing.ease);
  const r = lerpRect(a, b, ease(Math.max(0, Math.min(1, p))));

  const srcSpan = shot.srcOut - shot.srcIn;
  const outSpan = durationInFrames / FPS;
  // a very low floor rather than 0.06: some pages are static, and holding
  // one verified frame is the only way to keep overlay coordinates exact
  const rate = Math.max(0.002, srcSpan / Math.max(outSpan, 0.001));

  return (
    <AbsoluteFill style={{ backgroundColor: C.bg }}>
      <div
        style={{
          position: "absolute",
          left: VIEW.x,
          top: VIEW.y,
          width: VIEW.w,
          height: VIEW.h,
          overflow: "hidden",
          backgroundColor: "#06080c",
        }}
      >
        <div style={{ position: "absolute", inset: 0, ...transformFor(r) }}>
          <OffthreadVideo
            src={staticFile("firstbid.mp4")}
            trimBefore={Math.round(shot.srcIn * FPS)}
            playbackRate={rate}
            muted
            style={{ width: SRC.w, height: SRC.h, display: "block" }}
          />
        </div>
        {dim > 0 && (
          <div
            style={{
              position: "absolute",
              inset: 0,
              background: "rgba(4,6,10,1)",
              opacity: dim,
            }}
          />
        )}
      </div>
      {/* soft inner edge so the crop doesn't look like a hard cut-out */}
      <div
        style={{
          position: "absolute",
          left: VIEW.x, top: VIEW.y, width: VIEW.w, height: VIEW.h,
          boxShadow: "inset 0 0 90px rgba(0,0,0,0.55)",
          pointerEvents: "none",
        }}
      />
    </AbsoluteFill>
  );
};

/** Convert a point in SOURCE coords to FRAME coords under camera `r`,
 *  so callouts can track whatever the camera is doing. */
export const project = (r: Rect, x: number, y: number) => {
  const s = VIEW.w / r.w;
  return { x: (x - r.x) * s + VIEW.x, y: (y - r.y) * s + VIEW.y, s };
};

/** The camera rect at progress p of a shot — mirrors <Footage> internals so a
 *  sibling overlay can project onto the same frame. */
export const shotRect = (shot: Shot, durationInFrames: number, f: number): Rect => {
  const p = durationInFrames <= 1 ? 0 : f / (durationInFrames - 1);
  const a = shot.from ?? full();
  const b = shot.to ?? a;
  const ease = shot.easing ?? Easing.inOut(Easing.ease);
  return lerpRect(a, b, ease(Math.max(0, Math.min(1, p))));
};
