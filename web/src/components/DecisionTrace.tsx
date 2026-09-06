"use client";

import { useEffect, useMemo, useState } from "react";
import type { Trace, TraceOrder } from "@/lib/api";
import { clock, p3, signed } from "@/lib/api";

/**
 * The Decision Trace: a settled window replayed as belief → action → outcome.
 *
 * This is not a price chart. It is a record of what the model believed at each
 * moment, what it did about that belief, and what the belief turned out to be
 * worth. Scrubbing moves the whole interface back to a moment in the window's
 * life, so a confident wrong call can be watched being made.
 *
 * It only exists because event contracts terminate in minutes with an exact,
 * on-chain value. On a slower primitive there is no settled truth to replay
 * against.
 */
export function DecisionTrace({ trace }: { trace: Trace }) {
  const events = useMemo(() => buildEvents(trace), [trace]);
  const [idx, setIdx] = useState(events.length); // start at the outcome

  // Selecting a different window must start at its settlement, not at whatever
  // decision index the previous window happened to be scrubbed to.
  useEffect(() => {
    setIdx(events.length);
  }, [trace.marketId, events.length]);

  // Arrow keys scrub. A timeline you can only drag is a timeline nobody
  // examines carefully; stepping one decision at a time is how you actually
  // read what happened.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = document.activeElement;
      if (el instanceof HTMLInputElement || el instanceof HTMLSelectElement) return;
      if (e.key === "ArrowLeft") {
        setIdx((i) => Math.max(0, i - 1));
        e.preventDefault();
      } else if (e.key === "ArrowRight") {
        setIdx((i) => Math.min(events.length, i + 1));
        e.preventDefault();
      } else if (e.key === "Home") {
        setIdx(0);
      } else if (e.key === "End") {
        setIdx(events.length);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [events.length]);

  const atEnd = idx >= events.length;
  const current = atEnd ? null : events[idx];

  // Executions are their own record. Inferring them from an order's `fills`
  // count and charging the requested size at the submitted limit would miss
  // maker fills found by reconciliation (which have no order at all) and would
  // misprice partial or price-improved executions.
  const fills = useMemo(() => executions(trace), [trace]);
  // Time runs from high secondsLeft to low, so a fill has already happened at
  // the scrub point when its secondsLeft is at least the cutoff. At settlement
  // the cutoff is the end of the window, which admits every fill.
  const cutoff = atEnd
    ? Number.NEGATIVE_INFINITY
    : (current?.secsLeft ?? Number.NEGATIVE_INFINITY);
  const filledSoFar = fills.filter((f) => f.secsLeft >= cutoff);

  return (
    <div className="panel overflow-hidden">
      <header className="hairline-b flex flex-wrap items-center gap-x-5 gap-y-2 px-5 py-3">
        <span className="t-h2">{trace.label}</span>
        <span className="t-micro">
          {trace.orders.length} decisions · {trace.fills.length} fills
        </span>
        <span className="ml-auto">
          <Outcome trace={trace} />
        </span>
      </header>

      <div className="px-5 py-6">
        <Plot trace={trace} events={events} fills={fills} upto={idx} cutoff={cutoff} />

        <Scrubber
          max={events.length}
          value={idx}
          onChange={setIdx}
          label={
            atEnd
              ? "settlement"
              : `t−${clock(current?.secsLeft ?? 0)} · ${current?.label ?? ""}`
          }
        />

        <div className="mt-6 grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,320px)]">
          <Moment trace={trace} event={current} atEnd={atEnd} />
          <Running trace={trace} fills={filledSoFar} atEnd={atEnd} />
        </div>
      </div>
    </div>
  );
}

/* ------------------------------------------------------------- events --- */

type Event = {
  secsLeft: number;
  fair: number;
  spot: number;
  open: number;
  order: TraceOrder;
  label: string;
};

function buildEvents(t: Trace): Event[] {
  return t.orders
    .map((o) => ({
      secsLeft: o.secsLeft,
      fair: o.fair,
      spot: o.spot,
      open: o.openPx,
      order: o,
      label: `${o.mode.toUpperCase()} ${o.kind}`,
    }))
    .sort((a, b) => b.secsLeft - a.secsLeft); // window start → expiry
}

/** One actual execution, placed on the window's timeline. */
type Execution = {
  secsLeft: number;
  kind: string;
  price: number;
  quantity: number;
  fair: number;
  txHash: string;
};

/**
 * The window's real executions, from the ledger's fill records.
 *
 * Fills carry a wall-clock timestamp; the window carries its expiry, so the
 * position on the timeline is expiry minus that timestamp. Reconciled maker
 * fills appear here even though no order of ours produced a receipt for them.
 */
function executions(t: Trace): Execution[] {
  return t.fills
    .map((f) => ({
      secsLeft: Math.max(0, t.expiry - f.at),
      kind: f.kind,
      price: f.price,
      quantity: f.quantity,
      fair: f.fair,
      txHash: f.txHash,
    }))
    .sort((a, b) => b.secsLeft - a.secsLeft);
}

/* --------------------------------------------------------------- plot --- */

function Plot({
  trace,
  events,
  fills,
  upto,
  cutoff,
}: {
  trace: Trace;
  events: Event[];
  fills: Execution[];
  upto: number;
  cutoff: number;
}) {
  const W = 1000;
  const H = 260;
  const P = { l: 44, r: 16, t: 16, b: 28 };
  const dur = Math.max(trace.intervalSec, 1);

  // x is time through the window; y is probability.
  const x = (secsLeft: number) =>
    P.l + (1 - Math.max(0, Math.min(1, secsLeft / dur))) * (W - P.l - P.r);
  const y = (p: number) => P.t + (1 - Math.max(0, Math.min(1, p))) * (H - P.t - P.b);

  const path = events
    .map((e, i) => `${i === 0 ? "M" : "L"}${x(e.secsLeft)},${y(e.fair)}`)
    .join(" ");
  const revealed = events
    .slice(0, Math.min(upto + 1, events.length))
    .map((e, i) => `${i === 0 ? "M" : "L"}${x(e.secsLeft)},${y(e.fair)}`)
    .join(" ");

  const settledY = trace.voided ? y(0.5) : y(trace.winner === 0 ? 1 : 0);

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" style={{ height: 260 }} role="img"
      aria-label={`Model probability through the ${trace.label} window, ending ${trace.winner === 0 ? "Up" : "Down"}`}>
      {/* probability gridlines */}
      {[0, 0.25, 0.5, 0.75, 1].map((p) => (
        <g key={p}>
          <line x1={P.l} y1={y(p)} x2={W - P.r} y2={y(p)}
            stroke="var(--color-hairline)" strokeOpacity={p === 0.5 ? 1 : 0.45} />
          <text x={P.l - 8} y={y(p) + 4} fill="var(--color-ink-3)" fontSize="11"
            fontFamily="var(--font-mono)" textAnchor="end">{p.toFixed(2)}</text>
        </g>
      ))}

      {/* the settled truth, drawn as the line everything was travelling toward */}
      <line x1={P.l} y1={settledY} x2={W - P.r} y2={settledY}
        stroke={trace.voided ? "var(--color-warn)" : trace.winner === 0 ? "var(--color-up)" : "var(--color-down)"}
        strokeOpacity="0.35" strokeDasharray="5 5" />
      <text x={W - P.r} y={settledY - 6} fill="var(--color-ink-3)" fontSize="11"
        fontFamily="var(--font-mono)" textAnchor="end">
        {trace.voided ? "voided → 0.5" : trace.winner === 0 ? "Up won → 1.00" : "Down won → 0.00"}
      </text>

      {/* the model's whole path, faint, and the revealed portion, solid */}
      <path d={path} fill="none" stroke="var(--color-model)" strokeOpacity="0.18" strokeWidth="2" />
      <path d={revealed} fill="none" stroke="var(--color-model)" strokeWidth="2.5" />

      {/* decisions: hollow marks at the price we asked for */}
      {events.slice(0, Math.min(upto + 1, events.length)).map((e, i) => {
        const c = e.order.kind === "BUY_UP" ? "var(--color-up)" : "var(--color-down)";
        const py = e.order.kind === "BUY_UP" ? y(e.order.price) : y(1 - e.order.price);
        return (
          <g key={`o${i}`}>
            <line x1={x(e.secsLeft)} y1={y(e.fair)} x2={x(e.secsLeft)} y2={py}
              stroke={c} strokeOpacity="0.25" />
            <circle cx={x(e.secsLeft)} cy={py} r="3.5" fill="none" stroke={c} strokeWidth="1.5">
              <title>{`${e.label} asked at ${p3(e.order.price)}`}</title>
            </circle>
          </g>
        );
      })}

      {/* executions: solid marks at the price that actually traded */}
      {fills.filter((f) => f.secsLeft >= cutoff).map((f, i) => {
        const c = f.kind === "BUY_UP" ? "var(--color-up)" : "var(--color-down)";
        const py = f.kind === "BUY_UP" ? y(f.price) : y(1 - f.price);
        return (
          <circle key={`f${i}`} cx={x(f.secsLeft)} cy={py} r="5" fill={c}>
            <title>{`FILLED ${f.kind} ${f.quantity.toFixed(1)} at ${p3(f.price)}`}</title>
          </circle>
        );
      })}

      {/* the scrub head */}
      {upto < events.length && events[upto] && (
        <line x1={x(events[upto].secsLeft)} y1={P.t} x2={x(events[upto].secsLeft)} y2={H - P.b}
          stroke="var(--color-ink)" strokeOpacity="0.5" />
      )}

      <text x={P.l} y={H - 8} fill="var(--color-ink-3)" fontSize="11" fontFamily="var(--font-mono)">
        window opens
      </text>
      <text x={W - P.r} y={H - 8} fill="var(--color-ink-3)" fontSize="11"
        fontFamily="var(--font-mono)" textAnchor="end">expiry</text>
    </svg>
  );
}

/* ----------------------------------------------------------- scrubber --- */

function Scrubber({
  max,
  value,
  onChange,
  label,
}: {
  max: number;
  value: number;
  onChange: (v: number) => void;
  label: string;
}) {
  return (
    <div className="mt-4">
      <input
        type="range"
        min={0}
        max={max}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        aria-label="Scrub through the window's decisions"
        className="w-full"
        style={{ accentColor: "var(--color-model)" }}
      />
      <div className="t-micro mt-1.5">{label} — drag, or use ← →</div>
    </div>
  );
}

/* ------------------------------------------------------------ panels --- */

function Moment({
  trace,
  event,
  atEnd,
}: {
  trace: Trace;
  event: Event | null;
  atEnd: boolean;
}) {
  if (atEnd || !event) {
    const a = trace.attribution;
    return (
      <div className="rounded-md px-4 py-4" style={{ background: "var(--color-raised)" }}>
        <div className="t-micro mb-2">settlement</div>
        <p className="text-[15px]">
          {trace.voided
            ? "The window voided. Both sides redeem at 0.5."
            : trace.winner === 0
              ? "The window closed at or above its opening price. Up paid 1.00; Down expired worthless."
              : "The window closed below its opening price. Down paid 1.00; Up expired worthless."}
        </p>
        {a && (
          <p className="mt-3 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
            We believed we were capturing{" "}
            <strong style={{ color: a.Edge >= 0 ? "var(--color-up)" : "var(--color-down)" }}>
              {signed(a.Edge, 3)}
            </strong>{" "}
            of edge. The outcome differed from that belief by{" "}
            <strong style={{ color: a.Selection >= 0 ? "var(--color-up)" : "var(--color-down)" }}>
              {signed(a.Selection, 3)}
            </strong>
            , leaving{" "}
            <strong style={{ color: a.Net >= 0 ? "var(--color-up)" : "var(--color-down)" }}>
              {signed(a.Net, 3)}
            </strong>
            .
          </p>
        )}
      </div>
    );
  }

  const o = event.order;
  const sideFair = o.kind === "BUY_UP" ? event.fair : 1 - event.fair;
  const believedEdge = sideFair - o.price;

  return (
    <div className="rounded-md px-4 py-4" style={{ background: "var(--color-raised)" }}>
      <div className="t-micro mb-2">
        t−{clock(event.secsLeft)} · {o.mode === "take" ? "crossing the book" : "resting a quote"}
      </div>
      <p className="text-[15px]">
        {o.kind === "BUY_UP" ? "Buying Up" : "Buying Down"} at{" "}
        <span className="font-mono">{p3(o.price)}</span>, believing it was worth{" "}
        <span className="font-mono" style={{ color: "var(--color-model)" }}>
          {p3(sideFair)}
        </span>
        .
      </p>
      <p className="mt-2 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
        Spot {o.spot.toFixed(2)} against an opening line of {o.openPx.toFixed(2)}.
        Believed edge{" "}
        <strong style={{ color: believedEdge >= 0 ? "var(--color-up)" : "var(--color-down)" }}>
          {signed(believedEdge, 3)}
        </strong>
        . {o.fills > 0 ? "This one filled." : "This one rested and did not fill."}
      </p>
      {o.txHash && (
        <a
          className="t-micro mt-3 inline-block"
          style={{ textTransform: "none", letterSpacing: 0 }}
          href={`https://shannon-explorer.somnia.network/tx/${o.txHash}`}
          target="_blank"
          rel="noreferrer"
        >
          {o.txHash.slice(0, 18)}… ↗
        </a>
      )}
    </div>
  );
}

/** Running totals as the scrub advances, from real executions. */
function Running({
  trace,
  fills,
  atEnd,
}: {
  trace: Trace;
  fills: Execution[];
  atEnd: boolean;
}) {
  const spent = fills.reduce((n, f) => n + f.price * f.quantity, 0);
  const contracts = fills.reduce((n, f) => n + f.quantity, 0);

  return (
    <div className="rounded-md px-4 py-4" style={{ background: "var(--color-raised)" }}>
      <div className="t-micro mb-3">executed so far in this window</div>
      <dl className="grid grid-cols-2 gap-y-3">
        <Cell label="fills" value={String(fills.length)} />
        <Cell label="contracts" value={contracts.toFixed(1)} />
        <Cell label="committed" value={spent.toFixed(3)} />
        <Cell label="of total fills" value={String(trace.fills.length)} />
      </dl>
      {atEnd && trace.attribution && (
        <div className="hairline-t mt-4 pt-3">
          <Cell
            label="net"
            value={signed(trace.attribution.Net, 3)}
            colour={trace.attribution.Net >= 0 ? "var(--color-up)" : "var(--color-down)"}
          />
        </div>
      )}
    </div>
  );
}

function Cell({ label, value, colour }: { label: string; value: string; colour?: string }) {
  return (
    <div>
      <dt className="t-micro">{label}</dt>
      <dd className="font-mono text-[15px]" style={{ color: colour ?? "var(--color-ink)" }}>
        {value}
      </dd>
    </div>
  );
}

function Outcome({ trace }: { trace: Trace }) {
  if (!trace.settled) return <span className="t-micro">unsettled</span>;
  const [text, colour] = trace.voided
    ? ["voided", "var(--color-warn)"]
    : trace.winner === 0
      ? ["Up won", "var(--color-up)"]
      : ["Down won", "var(--color-down)"];
  return (
    <span className="t-micro" style={{ color: colour }}>
      {text}
    </span>
  );
}
