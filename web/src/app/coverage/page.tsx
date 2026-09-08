"use client";

import { useEffect, useState } from "react";
import { Shell } from "@/components/Shell";
import { api, type CoverageRow, type CoveragePayload } from "@/lib/api";

export default function Page() {
  return (
    <Shell>
      <Coverage />
    </Shell>
  );
}

function Coverage() {
  const [d, setD] = useState<CoveragePayload | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    const load = () =>
      api
        .coverage(ac.signal)
        .then(setD)
        .catch((e) => !ac.signal.aborted && setErr(String(e.message ?? e)));
    load();
    const id = setInterval(load, 20_000);
    return () => {
      ac.abort();
      clearInterval(id);
    };
  }, []);

  return (
    <Wrap>
      <header className="mb-8 max-w-[72ch]">
        <h1 className="t-h1 mb-3">What we are allowed to price, live</h1>
        <p style={{ color: "var(--color-ink-2)" }}>
          Every cadence the indexer reports live right now, quotable or refused,
          with the measurement that decided it. This is the same table
          docs/COVERAGE.md documents statically — read here instead of trusted.
          A cadence refuses by default: it must have a resolved-window fit or a
          horizon-scaled measurement on record, or the engine will not price it.
        </p>
      </header>

      {err && (
        <div className="panel px-8 py-14 text-center">
          <div className="t-h2 mb-2" style={{ color: "var(--color-warn)" }}>
            Coverage is unavailable.
          </div>
          <p className="text-[14px]" style={{ color: "var(--color-ink-2)" }}>
            {err}
          </p>
        </div>
      )}

      {!err && !d && (
        <div className="panel" style={{ height: 260 }} aria-busy="true" />
      )}

      {d && d.rows.length === 0 && (
        <div className="panel px-8 py-14 text-center">
          <div className="t-h2 mb-2">No live markets discovered.</div>
        </div>
      )}

      {d && d.rows.length > 0 && (
        <section className="panel overflow-x-auto">
          <table className="w-full min-w-[720px]">
            <thead>
              <tr>
                {["cadence", "verdict", "evidence"].map((h, i) => (
                  <th
                    key={h}
                    className="t-micro px-4 py-3"
                    style={{ textAlign: i === 0 ? "left" : "left" }}
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {d.rows.map((row) => (
                <Row key={row.label} row={row} />
              ))}
            </tbody>
          </table>
        </section>
      )}

      <p className="t-micro mt-6" style={{ textTransform: "none", letterSpacing: 0 }}>
        Refreshes every 20s from the live indexer. A cadence appearing here
        means it currently has at least one live market on the venue — the
        list itself, not just the verdicts, moves as the venue reshapes.
      </p>
    </Wrap>
  );
}

function Row({ row }: { row: CoverageRow }) {
  return (
    <tr className="hairline-t">
      <td className="px-4 py-3 font-mono text-[13px]">{row.label}</td>
      <td className="px-4 py-3">
        <span
          className="t-micro rounded-full px-2 py-0.5"
          style={{
            color: row.quotable ? "var(--color-up)" : "var(--color-ink-3)",
            border: `1px solid ${row.quotable ? "var(--color-up)" : "var(--color-hairline)"}`,
          }}
        >
          {row.quotable ? "quotable" : "refused"}
        </span>
      </td>
      <td className="px-4 py-3 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
        {row.reason}
      </td>
    </tr>
  );
}

function Wrap({ children }: { children: React.ReactNode }) {
  return <div className="mx-auto max-w-[1400px] px-6 py-10">{children}</div>;
}
