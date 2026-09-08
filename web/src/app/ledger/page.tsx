"use client";

import { useEffect, useState } from "react";
import { Shell } from "@/components/Shell";
import { api, signed, type Attribution, type PnL } from "@/lib/api";

export default function Page() {
  return (
    <Shell>
      <Ledger />
    </Shell>
  );
}

function Ledger() {
  const [d, setD] = useState<PnL | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [cadence, setCadence] = useState<string>("all");

  useEffect(() => {
    const ac = new AbortController();
    const load = () =>
      api
        .pnl(ac.signal)
        .then(setD)
        .catch((e) => !ac.signal.aborted && setErr(String(e.message ?? e)));
    load();
    const id = setInterval(load, 20_000);
    return () => {
      ac.abort();
      clearInterval(id);
    };
  }, []);

  if (err)
    return (
      <Wrap>
        <div className="panel px-8 py-14 text-center">
          <div className="t-h2 mb-2" style={{ color: "var(--color-warn)" }}>
            The ledger is unavailable.
          </div>
          <p className="text-[14px]" style={{ color: "var(--color-ink-2)" }}>
            {err}
          </p>
        </div>
      </Wrap>
    );
  if (!d) return <Wrap><div className="panel" style={{ height: 360 }} aria-busy="true" /></Wrap>;

  const allTraded = d.windows.filter((w) => w.Fills > 0);
  // Grouping by Label rather than Asset+IntervalSec works because the engine
  // formats it as exactly that cadence, e.g. "BTC/240m" — see engine.go's
  // marketLoop. It repeats across every window of that cadence, so it is
  // already the grouping key, not just a display string.
  const cadences = Array.from(new Set(allTraded.map((w) => w.Label))).sort();
  const traded = cadence === "all" ? allTraded : allTraded.filter((w) => w.Label === cadence);
  const t = cadence === "all" ? d.totals : sumOf(traded);

  return (
    <Wrap>
      <header className="mb-8 max-w-[72ch]">
        <h1 className="t-h1 mb-3">What actually happened</h1>
        <p style={{ color: "var(--color-ink-2)" }}>
          Every position here reached a terminal, exact value on chain within the
          hour, so profit is arithmetic rather than an estimate. We split it into
          the part we controlled and the part we did not.
        </p>
      </header>

      {allTraded.length === 0 ? (
        <div className="panel px-8 py-14 text-center">
          <div className="t-h2 mb-2">No settled windows with fills yet.</div>
          <p className="mx-auto max-w-[52ch] text-[14px]" style={{ color: "var(--color-ink-2)" }}>
            Run the engine with <code>-live</code>, then wait for a window to
            expire and the oracle to resolve it. Rows appear here automatically.
          </p>
        </div>
      ) : (
        <>
          {cadences.length > 1 && (
            <CadenceFilter cadences={cadences} value={cadence} onChange={setCadence} />
          )}

          <Split edge={t.Edge} selection={t.Selection} net={t.Net} />

          <div className="mt-8 mb-6 flex flex-wrap gap-x-12 gap-y-5">
            <Kpi v={signed(t.Net, 3)} l="net, tUSDC" colour={t.Net >= 0 ? "var(--color-up)" : "var(--color-down)"} big />
            <Kpi v={signed(t.Edge, 3)} l="edge — what we believed we captured" colour={t.Edge >= 0 ? "var(--color-up)" : "var(--color-down)"} />
            <Kpi v={signed(t.Selection, 3)} l="selection — how reality differed" colour={t.Selection >= 0 ? "var(--color-up)" : "var(--color-down)"} />
            <Kpi v={String(t.Windows)} l="settled windows" />
            <Kpi v={String(t.Fills)} l="fills" />
          </div>

          <Reading edge={t.Edge} selection={t.Selection} net={t.Net} windows={t.Windows} />

          <section className="panel mt-8 overflow-x-auto">
            <table className="w-full min-w-[860px]">
              <thead>
                <tr>
                  {["window", "fills", "contracts", "cost", "payout", "edge", "selection", "net"].map((h, i) => (
                    <th
                      key={h}
                      className="t-micro px-4 py-3"
                      style={{ textAlign: i === 0 ? "left" : "right" }}
                    >
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {traded.map((w) => (
                  <Row key={w.MarketID} w={w} />
                ))}
              </tbody>
            </table>
          </section>
        </>
      )}
    </Wrap>
  );
}

/**
 * Edge and selection as opposing bars from a shared zero.
 *
 * This is the component that makes "we were right and still lost" visible in
 * one glance — the thing a single P&L number can never show.
 */
function Split({ edge, selection, net }: { edge: number; selection: number; net: number }) {
  const scale = Math.max(Math.abs(edge), Math.abs(selection), Math.abs(net), 0.001);
  const bars = [
    { label: "edge", v: edge, note: "what we believed we captured" },
    { label: "selection", v: selection, note: "how the outcome differed from that belief" },
    { label: "net", v: net, note: "what landed in the wallet" },
  ];

  return (
    <section className="panel p-6">
      <h2 className="t-h2 mb-5">Attribution</h2>
      <div className="space-y-4">
        {bars.map((b) => {
          const w = (Math.abs(b.v) / scale) * 50;
          const pos = b.v >= 0;
          return (
            <div key={b.label} className="grid items-center gap-4" style={{ gridTemplateColumns: "110px 1fr 120px" }}>
              <div className="t-micro">{b.label}</div>
              <div className="relative h-7" style={{ background: "var(--color-void)", borderRadius: 4 }}>
                <div className="absolute inset-y-0 left-1/2 w-px" style={{ background: "var(--color-hairline-2)" }} />
                <div
                  className="absolute inset-y-1 transition-all duration-500"
                  style={{
                    left: pos ? "50%" : `${50 - w}%`,
                    width: `${w}%`,
                    background: pos ? "var(--color-up)" : "var(--color-down)",
                    opacity: 0.8,
                    borderRadius: 3,
                  }}
                />
              </div>
              <div
                className="text-right font-mono text-[14px]"
                style={{ color: pos ? "var(--color-up)" : "var(--color-down)" }}
              >
                {signed(b.v, 3)}
              </div>
            </div>
          );
        })}
      </div>
      <div className="mt-5 grid gap-1 text-[12px]" style={{ color: "var(--color-ink-3)" }}>
        {bars.map((b) => (
          <div key={b.label}>
            <span className="font-mono" style={{ color: "var(--color-ink-2)" }}>
              {b.label}
            </span>{" "}
            — {b.note}
          </div>
        ))}
      </div>
    </section>
  );
}

/**
 * States plainly what the numbers mean, including when they are bad.
 *
 * A dashboard that only knows how to narrate profit is not an instrument.
 */
function Reading({
  edge,
  selection,
  net,
  windows,
}: {
  edge: number;
  selection: number;
  net: number;
  windows: number;
}) {
  let title: string;
  let body: string;

  if (net >= 0 && edge > 0) {
    title = "The edge survived execution.";
    body =
      "We bought below our own fair value and the outcomes did not take it back. At this sample size that is encouraging, not proven.";
  } else if (edge > 0 && selection < 0) {
    title = "The model was right about price and wrong about outcome.";
    body =
      "Edge is positive — we consistently bought below fair value — but selection is negative by more, so the trades lost. That is the signature of adverse selection: we only get filled when a counterparty is willing to trade against us, which biases toward the cases where we are wrong. A backtest cannot measure this, because a backtest has no counterparty. This is the honest state of the strategy today.";
  } else if (edge <= 0) {
    title = "We were not buying below fair value.";
    body =
      "Negative edge means the entry prices themselves were wrong, not just the outcomes. That points at the pricing model rather than at execution.";
  } else {
    title = "Mixed result.";
    body = "Too few settled windows to read anything into.";
  }

  return (
    <div className="panel p-5" style={{ borderColor: net < 0 ? "var(--color-warn)" : "var(--color-hairline)" }}>
      <div className="t-micro mb-1.5">reading</div>
      <p className="mb-2 text-[15px]">{title}</p>
      <p className="max-w-[76ch] text-[13px]" style={{ color: "var(--color-ink-2)" }}>
        {body}
      </p>
      {windows < 30 && (
        <p className="mt-3 text-[12px]" style={{ color: "var(--color-ink-3)" }}>
          {windows} settled window{windows === 1 ? "" : "s"} — far too few to
          conclude anything statistically. Shown because it is what we have, not
          because it is significant.
        </p>
      )}
    </div>
  );
}

function Row({ w }: { w: Attribution }) {
  const cells = [
    w.Fills,
    w.Contracts.toFixed(1),
    w.Cost.toFixed(3),
    w.Payout.toFixed(3),
  ];
  return (
    <tr className="hairline-t">
      <td className="px-4 py-2.5 font-mono text-[13px]">
        {w.Label}
        {w.Voided && <span className="t-micro ml-2">voided</span>}
      </td>
      {cells.map((c, i) => (
        <td key={i} className="px-4 py-2.5 text-right font-mono text-[13px]" style={{ color: "var(--color-ink-2)" }}>
          {c}
        </td>
      ))}
      <td className="px-4 py-2.5 text-right font-mono text-[13px]" style={{ color: w.Edge >= 0 ? "var(--color-up)" : "var(--color-down)" }}>
        {signed(w.Edge, 3)}
      </td>
      <td className="px-4 py-2.5 text-right font-mono text-[13px]" style={{ color: w.Selection >= 0 ? "var(--color-up)" : "var(--color-down)" }}>
        {signed(w.Selection, 3)}
      </td>
      <td className="px-4 py-2.5 text-right font-mono text-[13px]" style={{ color: w.Net >= 0 ? "var(--color-up)" : "var(--color-down)" }}>
        {signed(w.Net, 3)}
      </td>
    </tr>
  );
}

/** Mirrors ledger.Sum in Go: recomputed client-side when a cadence filter narrows the rows. */
function sumOf(rows: Attribution[]): PnL["totals"] {
  const t = { Windows: 0, Fills: 0, Contracts: 0, Edge: 0, Selection: 0, Net: 0 };
  for (const w of rows) {
    if (w.Fills === 0) continue;
    t.Windows += 1;
    t.Fills += w.Fills;
    t.Contracts += w.Contracts;
    t.Edge += w.Edge;
    t.Selection += w.Selection;
    t.Net += w.Net;
  }
  return t;
}

/**
 * Isolates one cadence's record from the rest — e.g. BTC/240m, only recently
 * admitted into coverage (docs/COVERAGE.md), judged on its own fills rather
 * than folded into every other cadence's total.
 */
function CadenceFilter({
  cadences,
  value,
  onChange,
}: {
  cadences: string[];
  value: string;
  onChange: (v: string) => void;
}) {
  const options = ["all", ...cadences];
  return (
    <div className="mb-6 flex flex-wrap gap-2">
      {options.map((c) => {
        const active = c === value;
        return (
          <button
            key={c}
            onClick={() => onChange(c)}
            className="rounded-full px-3 py-1 font-mono text-[12px] transition-colors"
            style={{
              border: `1px solid ${active ? "var(--color-ink)" : "var(--color-hairline)"}`,
              background: active ? "var(--color-ink)" : "transparent",
              color: active ? "var(--color-void)" : "var(--color-ink-2)",
            }}
          >
            {c === "all" ? "all cadences" : c}
          </button>
        );
      })}
    </div>
  );
}

function Wrap({ children }: { children: React.ReactNode }) {
  return <div className="mx-auto max-w-[1400px] px-6 py-10">{children}</div>;
}

function Kpi({ v, l, colour, big }: { v: string; l: string; colour?: string; big?: boolean }) {
  return (
    <div>
      <div
        className="font-mono"
        style={{
          fontSize: big ? 42 : 26,
          lineHeight: 1.05,
          letterSpacing: "-0.02em",
          color: colour ?? "var(--color-ink)",
        }}
      >
        {v}
      </div>
      <div className="t-micro mt-1.5 max-w-[26ch]">{l}</div>
    </div>
  );
}
