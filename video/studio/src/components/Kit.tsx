import React from "react";
import { useCurrentFrame, interpolate, Easing, spring, useVideoConfig } from "remotion";
import { C, F } from "../theme";
import { FPS } from "../timeline";

const clampOp = { extrapolateLeft: "clamp", extrapolateRight: "clamp" } as const;

/** Fade + rise in, optional fade out. `at`/`until` in seconds. */
export const In: React.FC<{
  at?: number; until?: number; rise?: number; dur?: number;
  children: React.ReactNode; style?: React.CSSProperties;
}> = ({ at = 0, until, rise = 16, dur = 0.5, children, style }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + dur], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const b = until === undefined ? 1 : interpolate(t, [until, until + 0.4], [1, 0], clampOp);
  return (
    <div style={{ opacity: a * b, transform: `translateY(${(1 - a) * rise}px)`, ...style }}>
      {children}
    </div>
  );
};

/** Small uppercase act label with a rule — the film's chapter marker. */
export const SectionTitle: React.FC<{
  kicker?: string; title: string; at?: number; until?: number;
}> = ({ kicker, title, at = 0, until }) => (
  <In at={at} until={until} rise={20} style={{ textAlign: "center" }}>
    {kicker && (
      <div style={{
        fontFamily: F.mono, fontSize: 23, letterSpacing: 6,
        color: C.model, textTransform: "uppercase", marginBottom: 16,
      }}>
        {kicker}
      </div>
    )}
    <div style={{
      fontFamily: F.sans, fontSize: 74, fontWeight: 700, color: C.ink,
      letterSpacing: -1.4, lineHeight: 1.08,
    }}>
      {title}
    </div>
  </In>
);

/** A number that counts up and lands. */
export const Counter: React.FC<{
  to: number; at?: number; dur?: number; decimals?: number;
  prefix?: string; suffix?: string; size?: number; color?: string;
}> = ({ to, at = 0, dur = 1.1, decimals = 0, prefix = "", suffix = "", size = 96, color = C.model }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const p = interpolate(t, [at, at + dur], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const v = to * p;
  return (
    <span style={{
      fontFamily: F.mono, fontSize: size, fontWeight: 700, color,
      letterSpacing: -1.5, textShadow: `0 0 34px ${color}44`,
      fontVariantNumeric: "tabular-nums",
    }}>
      {prefix}{v.toFixed(decimals)}{suffix}
    </span>
  );
};

/** Panel that matches the dashboard's card language. */
export const Card: React.FC<{
  children: React.ReactNode; style?: React.CSSProperties; accent?: string;
}> = ({ children, style, accent }) => (
  <div style={{
    background: C.panel,
    border: `1px solid ${C.panelEdge}`,
    borderTop: accent ? `2px solid ${accent}` : undefined,
    borderRadius: 10,
    padding: "22px 26px",
    boxShadow: "0 18px 60px rgba(0,0,0,0.5)",
    ...style,
  }}>
    {children}
  </div>
);

/** label / value pair, dashboard style. */
export const Metric: React.FC<{
  label: string; value: string; color?: string; size?: number; sub?: string;
}> = ({ label, value, color = C.ink, size = 54, sub }) => (
  <div>
    <div style={{
      fontFamily: F.mono, fontSize: 18, letterSpacing: 2.4,
      color: C.inkFaint, textTransform: "uppercase", marginBottom: 7,
    }}>
      {label}
    </div>
    <div style={{
      fontFamily: F.mono, fontSize: size, fontWeight: 700, color,
      letterSpacing: -0.8, lineHeight: 1, fontVariantNumeric: "tabular-nums",
    }}>
      {value}
    </div>
    {sub && (
      <div style={{ fontFamily: F.sans, fontSize: 19, color: C.inkDim, marginTop: 8 }}>{sub}</div>
    )}
  </div>
);

/** A node in a flow diagram. */
export const Node: React.FC<{
  label: string; sub?: string; at?: number; w?: number;
  color?: string; strong?: boolean; style?: React.CSSProperties;
}> = ({ label, sub, at = 0, w = 260, color = C.model, strong = false, style }) => {
  const f = useCurrentFrame();
  const { fps } = useVideoConfig();
  const s = spring({
    frame: f - Math.round(at * FPS), fps,
    config: { damping: 200, mass: 0.5, stiffness: 120 }, durationInFrames: 16,
  });
  return (
    <div style={{
      width: w,
      background: strong ? "rgba(126,156,255,0.10)" : C.panel,
      border: `${strong ? 2 : 1}px solid ${strong ? color : C.panelEdge}`,
      borderRadius: 9,
      padding: "13px 18px",
      textAlign: "center",
      opacity: s,
      transform: `scale(${0.9 + 0.1 * s})`,
      boxShadow: strong ? `0 0 30px ${color}33` : "0 10px 30px rgba(0,0,0,0.45)",
      ...style,
    }}>
      <div style={{
        fontFamily: F.mono, fontSize: 24, fontWeight: 700,
        color: strong ? color : C.ink, letterSpacing: 1.4, whiteSpace: "nowrap",
      }}>
        {label}
      </div>
      {sub && (
        <div style={{
          fontFamily: F.sans, fontSize: 17, color: C.inkFaint, marginTop: 4, whiteSpace: "nowrap",
        }}>
          {sub}
        </div>
      )}
    </div>
  );
};

/** Animated connector with a travelling data pulse. */
export const Flow: React.FC<{
  x1: number; y1: number; x2: number; y2: number;
  at?: number; color?: string; pulse?: boolean; width?: number;
}> = ({ x1, y1, x2, y2, at = 0, color = C.model, pulse = true, width = 2 }) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const draw = interpolate(t, [at, at + 0.4], [0, 1], { ...clampOp, easing: Easing.out(Easing.cubic) });
  const dx = x2 - x1, dy = y2 - y1;
  const len = Math.hypot(dx, dy);
  const ang = (Math.atan2(dy, dx) * 180) / Math.PI;
  const ph = ((t - at) * 0.55) % 1;

  return (
    <div style={{
      position: "absolute", left: x1, top: y1, width: len * draw, height: width,
      background: `${color}44`, transform: `rotate(${ang}deg)`, transformOrigin: "0 50%",
      borderRadius: width,
    }}>
      {pulse && draw > 0.98 && (
        <div style={{
          position: "absolute", left: `${ph * 100}%`, top: -1.5,
          width: 46, height: width + 3, marginLeft: -23, borderRadius: 4,
          background: `linear-gradient(90deg, transparent, ${color}, transparent)`,
          filter: "blur(0.6px)", opacity: 0.95,
        }} />
      )}
    </div>
  );
};

/** Verdict pill matching the dashboard's QUOTABLE / REFUSED badges. */
export const Pill: React.FC<{ text: string; kind: "ok" | "no"; at?: number }> = ({
  text, kind, at = 0,
}) => {
  const f = useCurrentFrame();
  const t = f / FPS;
  const a = interpolate(t, [at, at + 0.3], [0, 1], clampOp);
  const col = kind === "ok" ? C.take : C.refuse;
  return (
    <span style={{
      display: "inline-block",
      fontFamily: F.mono, fontSize: 19, fontWeight: 700, letterSpacing: 1.6,
      color: col, border: `1px solid ${col}`, borderRadius: 20,
      padding: "3px 14px", opacity: a,
      background: kind === "ok" ? `${col}14` : "transparent",
      textTransform: "uppercase", whiteSpace: "nowrap",
    }}>
      {text}
    </span>
  );
};
