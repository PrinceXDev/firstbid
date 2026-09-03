"use client";

import { useEffect, useRef, useState } from "react";

/**
 * A probability is never rendered as a bare number in this product.
 *
 * Every trading interface shows a confident value and hides the confidence.
 * Our entire thesis is that mispriced *certainty* is the opportunity, so the
 * uncertainty band is drawn at the same visual weight as the value itself.
 */
export function ProbabilityWithDoubt({
  value,
  doubt,
  size = "display",
  label,
}: {
  value: number;
  doubt: number;
  size?: "display" | "inline";
  label?: string;
}) {
  const shown = useInterpolated(value);
  const lo = Math.max(0, value - doubt);
  const hi = Math.min(1, value + doubt);

  if (size === "inline") {
    return (
      <span className="inline-flex items-baseline gap-1.5 font-mono">
        <span style={{ color: "var(--color-model)" }}>{shown.toFixed(3)}</span>
        <span className="t-micro" style={{ letterSpacing: 0 }}>
          ±{doubt.toFixed(3)}
        </span>
      </span>
    );
  }

  return (
    <div>
      {label && <div className="t-micro mb-2">{label}</div>}
      <div
        className="t-display"
        style={{ color: "var(--color-model)" }}
        aria-label={`Model probability ${(value * 100).toFixed(1)} percent, plus or minus ${(doubt * 100).toFixed(1)}`}
      >
        {shown.toFixed(3)}
      </div>
      <DoubtBar lo={lo} hi={hi} centre={value} />
      <div className="t-micro mt-1.5">
        ± {doubt.toFixed(3)} &nbsp;·&nbsp; {(lo).toFixed(2)}–{(hi).toFixed(2)} likely range
      </div>
    </div>
  );
}

/** The error bar. Deliberately as wide as the number above it. */
function DoubtBar({ lo, hi, centre }: { lo: number; hi: number; centre: number }) {
  return (
    <svg
      viewBox="0 0 200 12"
      className="mt-2 w-full max-w-[220px]"
      role="presentation"
      style={{ height: 12 }}
    >
      <line x1="0" y1="6" x2="200" y2="6" stroke="var(--color-hairline)" strokeWidth="1" />
      <rect
        x={lo * 200}
        y="3"
        width={Math.max(1, (hi - lo) * 200)}
        height="6"
        fill="var(--color-model)"
        fillOpacity="0.28"
      />
      <line x1={lo * 200} y1="1" x2={lo * 200} y2="11" stroke="var(--color-model)" strokeOpacity="0.6" />
      <line x1={hi * 200} y1="1" x2={hi * 200} y2="11" stroke="var(--color-model)" strokeOpacity="0.6" />
      <line
        x1={centre * 200}
        y1="0"
        x2={centre * 200}
        y2="12"
        stroke="var(--color-model)"
        strokeWidth="2"
      />
    </svg>
  );
}

/**
 * Interpolates toward a new value instead of swapping to it.
 *
 * A number that jumps reads as a glitch; a number that travels reads as a
 * measurement changing. Respects reduced-motion by snapping.
 */
export function useInterpolated(target: number, ms = 400): number {
  const [shown, setShown] = useState(target);
  const from = useRef(target);
  const started = useRef(0);
  const raf = useRef(0);

  useEffect(() => {
    if (
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    ) {
      setShown(target);
      return;
    }
    from.current = shown;
    started.current = performance.now();

    const step = (now: number) => {
      const t = Math.min(1, (now - started.current) / ms);
      const eased = 1 - Math.pow(1 - t, 3);
      setShown(from.current + (target - from.current) * eased);
      if (t < 1) raf.current = requestAnimationFrame(step);
    };
    raf.current = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf.current);
    // `shown` is intentionally excluded: it is the animation's own output.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target, ms]);

  return shown;
}

/** A number that briefly lights up in the direction it moved. */
export function Ticking({
  value,
  format,
  className = "",
}: {
  value: number;
  format: (v: number) => string;
  className?: string;
}) {
  const prev = useRef(value);
  const [dir, setDir] = useState<"up" | "down" | null>(null);

  useEffect(() => {
    if (value > prev.current) setDir("up");
    else if (value < prev.current) setDir("down");
    prev.current = value;
    const t = setTimeout(() => setDir(null), 900);
    return () => clearTimeout(t);
  }, [value]);

  return (
    <span
      className={`font-mono ${dir === "up" ? "ticked-up" : dir === "down" ? "ticked-down" : ""} ${className}`}
    >
      {format(value)}
    </span>
  );
}
