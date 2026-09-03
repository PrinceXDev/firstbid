"use client";

import type { LiveRow } from "@/lib/api";
import { marketMid } from "@/lib/api";

/**
 * The product's central visual: market and model on ONE axis, with the noise
 * floor drawn behind the gap between them.
 *
 * Every other prediction interface renders the book as a two-column table,
 * separate from any notion of fair value. Putting both on the same 0–1 axis
 * makes disagreement *spatial* — you see whether the model sits inside or
 * outside the book without reading a single number. This is only sensible
 * because binary contracts have a bounded price axis; it would not work on a
 * spot market.
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
  const x = (p: number) => Math.max(0, Math.min(1, p)) * W;

  const noise = Math.max(row.uncertainty, 0.004);
  const gap = mid !== null && row.calibrated ? row.fair - mid : 0;
  const meaningful = Math.abs(gap) > noise;
  const gapColour = gap > 0 ? "var(--color-up)" : "var(--color-down)";

  const axis = height - 22;

  return (
    <svg
      viewBox={`0 0 ${W} ${height}`}
      className="w-full"
      style={{ height }}
      role="img"
      aria-label={
        row.calibrated && mid !== null
          ? `Market ${mid.toFixed(3)}, model ${row.fair.toFixed(3)}, difference ${gap.toFixed(3)}, noise floor ${noise.toFixed(3)}`
          : "No comparison available for this market"
      }
    >
      {/* axis */}
      <line x1="0" y1={axis} x2={W} y2={axis} stroke="var(--color-hairline)" strokeWidth="1" />
      {showScale &&
        [0, 0.25, 0.5, 0.75, 1].map((t) => (
          <g key={t}>
            <line
              x1={x(t)}
              y1={axis}
              x2={x(t)}
              y2={axis + 5}
              stroke="var(--color-hairline-2)"
            />
            <text
              x={x(t)}
              y={axis + 17}
              fill="var(--color-ink-3)"
              fontSize="10"
              fontFamily="var(--font-mono)"
              textAnchor={t === 0 ? "start" : t === 1 ? "end" : "middle"}
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
          {/* the gap, coloured only when it clears the noise */}
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
          {/* the model's own mark */}
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

      {/* market mid */}
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
    </svg>
  );
}

/** Compact legend. Rendered once per page, not per bar. */
export function DisagreementLegend() {
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2 t-micro">
      <Key colour="var(--color-model)" label="model" />
      <Key colour="var(--color-ink-2)" label="book (bid–ask)" />
      <Key colour="var(--color-model)" faded label="noise floor" />
      <span style={{ color: "var(--color-ink-3)" }}>
        gap shown only when it exceeds the noise
      </span>
    </div>
  );
}

function Key({
  colour,
  label,
  faded,
}: {
  colour: string;
  label: string;
  faded?: boolean;
}) {
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
