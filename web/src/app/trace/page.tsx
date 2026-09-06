"use client";

import { useEffect, useState } from "react";
import { Shell } from "@/components/Shell";
import { DecisionTrace } from "@/components/DecisionTrace";
import { api, type Trace } from "@/lib/api";

export default function Page() {
  return (
    <Shell>
      <Traces />
    </Shell>
  );
}

function Traces() {
  const [ids, setIds] = useState<string[] | null>(null);
  const [sel, setSel] = useState<string>("");
  const [trace, setTrace] = useState<Trace | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    api
      .traces(ac.signal)
      .then((r) => {
        setIds(r.marketIds);
        if (r.marketIds.length) setSel(r.marketIds[0]);
      })
      .catch((e) => !ac.signal.aborted && setErr(String(e.message ?? e)));
    return () => ac.abort();
  }, []);

  useEffect(() => {
    if (!sel) return;
    const ac = new AbortController();
    setTrace(null);
    api
      .trace(sel, ac.signal)
      .then(setTrace)
      .catch((e) => !ac.signal.aborted && setErr(String(e.message ?? e)));
    return () => ac.abort();
  }, [sel]);

  return (
    <div className="mx-auto max-w-[1400px] px-6 py-10">
      <header className="mb-8 max-w-[74ch]">
        <h1 className="t-h1 mb-3">Decision trace</h1>
        <p style={{ color: "var(--color-ink-2)" }}>
          A settled window, replayed. Scrub through it to see what the model
          believed at each moment, what it did about that belief, and what the
          belief turned out to be worth. Nothing here is reconstructed — every
          mark is a decision we recorded at the time and a transaction on chain.
        </p>
      </header>

      {err && (
        <div className="panel px-8 py-12 text-center">
          <div className="t-h2 mb-2" style={{ color: "var(--color-warn)" }}>
            No traces available.
          </div>
          <p className="text-[14px]" style={{ color: "var(--color-ink-2)" }}>
            {err}
          </p>
        </div>
      )}

      {!err && ids && ids.length === 0 && (
        <div className="panel px-8 py-12 text-center">
          <div className="t-h2 mb-2">Nothing has settled yet.</div>
          <p className="mx-auto max-w-[52ch] text-[14px]" style={{ color: "var(--color-ink-2)" }}>
            A window appears here once it has expired, been resolved by the
            oracle, and carried at least one of our fills.
          </p>
        </div>
      )}

      {!err && ids && ids.length > 0 && (
        <>
          <label className="t-micro mb-2 block" htmlFor="window-select">
            settled window
          </label>
          <select
            id="window-select"
            value={sel}
            onChange={(e) => setSel(e.target.value)}
            className="mb-6 rounded-md px-3 py-2 font-mono text-[13px]"
            style={{
              background: "var(--color-raised)",
              border: "1px solid var(--color-hairline)",
              color: "var(--color-ink)",
              maxWidth: 520,
            }}
          >
            {ids.map((id) => (
              <option key={id} value={id}>
                {id.replace(/^0x0+/, "0x…")}
              </option>
            ))}
          </select>

          {trace ? (
            <DecisionTrace trace={trace} />
          ) : (
            <div className="panel" style={{ height: 420 }} aria-busy="true" />
          )}
        </>
      )}
    </div>
  );
}
