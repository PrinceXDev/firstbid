"use client";

import { useEffect, useState } from "react";
import { Shell } from "@/components/Shell";
import { api, signed, type ChainPayload, type HealthPayload, type RiskPayload } from "@/lib/api";

export default function Page() {
  return (
    <Shell>
      <System />
    </Shell>
  );
}

function System() {
  const [health, setHealth] = useState<HealthPayload | null>(null);
  const [risk, setRisk] = useState<RiskPayload | null>(null);
  const [riskErr, setRiskErr] = useState<string | null>(null);
  const [chain, setChain] = useState<ChainPayload | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    const load = () => {
      api.health(ac.signal).then(setHealth).catch((e) => !ac.signal.aborted && setErr(String(e.message ?? e)));
      // A failed read must not look like "no exposure recorded" — that
      // silence is exactly what let a ledger outage go unnoticed here before.
      api
        .risk(ac.signal)
        .then((r) => {
          setRisk(r);
          setRiskErr(null);
        })
        .catch((e) => !ac.signal.aborted && setRiskErr(String(e.message ?? e)));
      api.chain(ac.signal).then(setChain).catch(() => {});
    };
    load();
    const id = setInterval(load, 15_000);
    return () => {
      ac.abort();
      clearInterval(id);
    };
  }, []);

  return (
    <Wrap>
      <header className="mb-10 max-w-[72ch]">
        <h1 className="t-h1 mb-3">Is it still working?</h1>
        <p style={{ color: "var(--color-ink-2)" }}>
          The Evidence page proves the model was calibrated once, offline, on
          history. This proves whether it still is, continuously, against what
          the engine has actually traded — plus the two numbers that make
          Somnia specifically the reason this strategy is tradeable at all.
        </p>
      </header>

      {err && (
        <div className="panel mb-8 px-8 py-6 text-center" style={{ borderColor: "var(--color-warn)" }}>
          <p className="text-[14px]" style={{ color: "var(--color-ink-2)" }}>{err}</p>
        </div>
      )}

      <section className="panel mb-8 p-6">
        <h2 className="t-h2 mb-1">Live model health</h2>
        <p className="mb-5 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
          Brier score over the last {health?.n ?? 0} settled, filled contracts —
          the production analogue of the offline backtest, scored continuously
          instead of once.
        </p>
        {!health || health.n === 0 ? (
          <p className="text-[13px]" style={{ color: "var(--color-ink-3)" }}>
            No settled fills yet. Run the engine with <code>-live</code> and wait
            for a window to resolve.
          </p>
        ) : (
          <div className="flex flex-wrap gap-x-12 gap-y-5">
            <Kpi
              v={`+${health.skillLive.toFixed(1)}%`}
              l="live skill vs. always-0.5"
              colour={health.skillLive >= 0 ? "var(--color-up)" : "var(--color-down)"}
              big
            />
            <Kpi v={health.brierLive.toFixed(4)} l="Brier — live" />
            <Kpi v={health.brierBaseline.toFixed(4)} l="Brier — always 0.5" dim />
            <Kpi v={String(health.n)} l="settled fills scored" />
          </div>
        )}
      </section>

      <section className="panel mb-8 p-6">
        <h2 className="t-h2 mb-1">Cross-window exposure</h2>
        <p className="mb-5 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
          Net Up contracts held per asset, summed across every window
          currently live on it — not just one window's own inventory cap.
          Two correlated windows agreeing with each other are refused before
          their combined exposure clears the cap.
        </p>
        {riskErr ? (
          <p className="text-[13px]" style={{ color: "var(--color-warn)" }}>
            Risk telemetry unavailable: {riskErr}
          </p>
        ) : !risk || risk.assets.length === 0 ? (
          <p className="text-[13px]" style={{ color: "var(--color-ink-3)" }}>
            No exposure recorded yet — nothing has filled since the engine started.
          </p>
        ) : (
          <div className="space-y-4">
            {risk.assets.map((a) => {
              const pct = a.cap > 0 ? Math.min(100, (Math.abs(a.netUp) / a.cap) * 100) : 0;
              const hot = pct > 80;
              return (
                <div key={a.asset} className="grid items-center gap-4" style={{ gridTemplateColumns: "80px 1fr 140px" }}>
                  <div className="font-mono text-[13px]">{a.asset}</div>
                  <div className="relative h-6" style={{ background: "var(--color-void)", borderRadius: 4 }}>
                    <div
                      className="absolute inset-y-1 left-1"
                      style={{
                        width: `${pct}%`,
                        maxWidth: "calc(100% - 8px)",
                        background: hot ? "var(--color-warn)" : "var(--color-model)",
                        opacity: 0.8,
                        borderRadius: 3,
                      }}
                    />
                  </div>
                  <div className="text-right font-mono text-[13px]" style={{ color: "var(--color-ink-2)" }}>
                    {signed(a.netUp, 1)} / {a.cap.toFixed(0)}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </section>

      <section className="panel p-6">
        <h2 className="t-h2 mb-1">Chain speed</h2>
        <p className="mb-5 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
          The edge in this strategy lives in the seconds near a window&rsquo;s
          expiry. A chain slow to confirm could not safely act that close to
          it — this is the number that argument rests on, not an assumption.
        </p>
        {!chain ? (
          <p className="text-[13px]" style={{ color: "var(--color-ink-3)" }}>Loading…</p>
        ) : (
          <>
            <div className="flex flex-wrap gap-x-12 gap-y-5">
              <Kpi v={`${chain.blockTimeMs.toFixed(0)}ms`} l={`avg block time, last ${chain.sampledBlocks} blocks`} big />
              <Kpi v={chain.latency.n > 0 ? `${chain.latency.p50Ms.toFixed(0)}ms` : "—"} l="order p50 (submit→receipt)" />
              <Kpi v={chain.latency.n > 0 ? `${chain.latency.p90Ms.toFixed(0)}ms` : "—"} l="order p90 (submit→receipt)" />
              <Kpi v={String(chain.latency.n)} l="orders measured" dim />
              <Kpi v={chain.blockNumber.toLocaleString()} l="latest block" dim />
            </div>
            {chain.latency.error && (
              <p className="mt-4 text-[13px]" style={{ color: "var(--color-warn)" }}>
                Order latency unavailable: {chain.latency.error}. Block speed above is unaffected — it comes
                straight from the chain, not the ledger.
              </p>
            )}
          </>
        )}
      </section>
    </Wrap>
  );
}

function Wrap({ children }: { children: React.ReactNode }) {
  return <div className="mx-auto max-w-[1400px] px-6 py-10">{children}</div>;
}

function Kpi({ v, l, colour, dim, big }: { v: string; l: string; colour?: string; dim?: boolean; big?: boolean }) {
  return (
    <div>
      <div
        className="font-mono"
        style={{
          fontSize: big ? 42 : 28,
          lineHeight: 1.05,
          letterSpacing: "-0.02em",
          color: colour ?? (dim ? "var(--color-ink-3)" : "var(--color-ink)"),
        }}
      >
        {v}
      </div>
      <div className="t-micro mt-1.5 max-w-[26ch]">{l}</div>
    </div>
  );
}
