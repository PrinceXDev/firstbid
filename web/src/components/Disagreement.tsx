"use client";

import type { LiveRow } from "@/lib/api";
import { marketMid } from "@/lib/api";

/**
 * The product's central visual: market and model on ONE axis, with the noise
 * floor drawn behind the gap between them.
 *
 * Every other prediction interface renders the book as a two-column table,
 * separate from any notion of fair value. Putting both on the same probability
 * axis makes disagreement *spatial* — you see whether the model sits inside or
 * outside the book without reading a number. It only works because binary
 * contracts have a bounded 0–1 price axis.
 *
 * The gap is coloured only when it exceeds the noise floor. A difference
 * smaller than our own uncertainty is not a signal, and the interface must not
 * dress it up as one.
 */
export function DisagreementBar({
  row,
  height = 64,
  showScale = true,
}: {
  row: LiveRow;
  height?: number;
  showScale?: boolean;
}) {
  const W = 1000;
  const mid = marketMid(row);
  const noise = Math.max(row.uncertainty, 0.004);

  const view = focusWindow(row, noise);
  const x = (p: number) =>
    ((Math.max(view.lo, Math.min(view.hi, p)) - view.lo) / view.span) * W;

  const gap = mid !== null && row.calibrated ? row.fair - mid : 0;
  const meaningful = Math.abs(gap) > noise;
  const gapColour = gap > 0 ? "var(--color-up)" : "var(--color-down)";

  const contextH = showScale ? 10 : 0;
  const axis = height - (showScale ? 22 : 6) - contextH;

  return (
    <svg
      viewBox={`0 0 ${W} ${height}`}
      className="w-full"
      style={{ height }}
      role="img"
      aria-label={
        row.calibrated && mid !== null
          ? `Market ${mid.toFixed(3)}, model ${row.fair.toFixed(3)}, difference ${gap.toFixed(3)}, noise floor ${noise.toFixed(3)}`
          : "No model comparison available for this market"
      }
    >
      <line x1="0" y1={axis} x2={W} y2={axis} stroke="var(--color-hairline)" />

      {showScale &&
        ticksFor(view).map((t) => (
          <g key={t}>
            <line x1={x(t)} y1={axis} x2={x(t)} y2={axis + 5} stroke="var(--color-hairline-2)" />
            <text
              x={x(t)}
              y={axis + 17}
              fill="var(--color-ink-3)"
              fontSize="10"
              fontFamily="var(--font-mono)"
              textAnchor={x(t) < 20 ? "start" : x(t) > W - 20 ? "end" : "middle"}
            >
              {t.toFixed(2)}
            </text>
          </g>
        ))}

      {/* the book, as a bracket spanning bid to ask */}
      {row.bid > 0 && row.ask > 0 && (
        <>
          <rect
            x={x(row.bid)}
            y={axis - 20}
            width={Math.max(1, x(row.ask) - x(row.bid))}
            height="20"
            fill="var(--color-ink-3)"
            fillOpacity="0.16"
          />
          <line x1={x(row.bid)} y1={axis - 24} x2={x(row.bid)} y2={axis} stroke="var(--color-ink-2)" strokeWidth="2" />
          <line x1={x(row.ask)} y1={axis - 24} x2={x(row.ask)} y2={axis} stroke="var(--color-ink-2)" strokeWidth="2" />
        </>
      )}

      {row.calibrated && row.fair > 0 && (
        <>
          {/* noise floor: the band inside which a difference means nothing */}
          <rect
            x={x(row.fair - noise)}
            y={axis - 34}
            width={Math.max(1, x(row.fair + noise) - x(row.fair - noise))}
            height="34"
            fill="var(--color-model)"
            fillOpacity="0.12"
          />
          {mid !== null && meaningful && (
            <rect
              x={Math.min(x(mid), x(row.fair))}
              y={axis - 30}
              width={Math.abs(x(row.fair) - x(mid))}
              height="30"
              fill={gapColour}
              fillOpacity="0.18"
            />
          )}
          <line
            x1={x(row.fair)}
            y1={axis - 38}
            x2={x(row.fair)}
            y2={axis}
            stroke="var(--color-model)"
            strokeWidth="2.5"
          />
          <circle cx={x(row.fair)} cy={axis - 38} r="3.5" fill="var(--color-model)" />
        </>
      )}

      {mid !== null && (
        <line
          x1={x(mid)}
          y1={axis - 20}
          x2={x(mid)}
          y2={axis}
          stroke="var(--color-ink)"
          strokeWidth="1.5"
          strokeDasharray="2 2"
        />
      )}

      {/* Full 0–1 context strip. The zoomed axis above would otherwise leave a
          reader unable to tell whether 0.62 is a near-certainty or a coin flip. */}
      {showScale && view.zoomed && (
        <g transform={`translate(0 ${height - contextH})`}>
          <rect x="0" y="2" width={W} height="5" fill="var(--color-hairline)" rx="2" />
          <rect
            x={view.lo * W}
            y="0"
            width={Math.max(3, view.span * W)}
            height="9"
            fill="var(--color-model)"
            fillOpacity="0.35"
            rx="2"
          />
          <text x="0" y="9" fill="var(--color-ink-3)" fontSize="9" fontFamily="var(--font-mono)">0</text>
          <text x={W} y="9" fill="var(--color-ink-3)" fontSize="9" fontFamily="var(--font-mono)" textAnchor="end">1</text>
        </g>
      )}
    </svg>
  );
}

/**
 * Chooses the slice of the 0–1 axis worth showing.
 *
 * A fixed full-range axis spends most of its width on empty probability space:
 * when the book sits at 0.83/0.86 and the model at 0.85, everything interesting
 * happens inside 3% of the picture. Zooming to the action makes the gap legible
 * at a glance; the context strip below keeps the absolute position readable.
 */
function focusWindow(row: LiveRow, noise: number) {
  const points: number[] = [];
  if (row.bid > 0) points.push(row.bid);
  if (row.ask > 0) points.push(row.ask);
  if (row.calibrated && row.fair > 0) {
    points.push(row.fair - noise, row.fair + noise);
  }
  if (points.length === 0) return { lo: 0, hi: 1, span: 1, zoomed: false };

  let lo = Math.min(...points);
  let hi = Math.max(...points);

  // Breathing room, and a floor on the width so a tight book does not become a
  // microscope that exaggerates a one-tick difference into a chasm.
  const pad = Math.max((hi - lo) * 0.35, 0.02);
  lo -= pad;
  hi += pad;

  const MIN_SPAN = 0.12;
  if (hi - lo < MIN_SPAN) {
    const centre = (hi + lo) / 2;
    lo = centre - MIN_SPAN / 2;
    hi = centre + MIN_SPAN / 2;
  }
  // Keep the window inside [0,1] without shrinking it.
  if (lo < 0) {
    hi -= lo;
    lo = 0;
  }
  if (hi > 1) {
    lo -= hi - 1;
    hi = 1;
  }
  lo = Math.max(0, lo);
  hi = Math.min(1, hi);

  const span = hi - lo;
  return { lo, hi, span, zoomed: span < 0.98 };
}

/** Round tick values inside the focus window, at a sensible density. */
function ticksFor(view: { lo: number; hi: number; span: number }): number[] {
  const step = view.span > 0.6 ? 0.25 : view.span > 0.3 ? 0.1 : view.span > 0.12 ? 0.05 : 0.02;
  const out: number[] = [];
  for (let t = Math.ceil(view.lo / step) * step; t <= view.hi + 1e-9; t += step) {
    out.push(Number(t.toFixed(4)));
  }
  return out;
}

/** Compact legend. Rendered once per page, not per bar. */
export function DisagreementLegend() {
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2 t-micro">
      <Key colour="var(--color-model)" label="model" />
      <Key colour="var(--color-ink-2)" label="book (bid–ask)" />
      <Key colour="var(--color-model)" faded label="noise floor" />
      <span style={{ color: "var(--color-ink-3)" }}>
        axis zooms to the action · gap coloured only when it exceeds the noise
      </span>
    </div>
  );
}

function Key({ colour, label, faded }: { colour: string; label: string; faded?: boolean }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        style={{
          width: 10,
          height: 10,
          background: colour,
          opacity: faded ? 0.22 : 1,
          borderRadius: 2,
          display: "inline-block",
        }}
      />
      {label}
    </span>
  );
}
