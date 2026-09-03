"use client";

import { useEffect, useState } from "react";
import { Shell } from "@/components/Shell";
import { api, type Calibration } from "@/lib/api";

export default function Page() {
  return (
    <Shell>
      <Evidence />
    </Shell>
  );
}

function Evidence() {
  const [c, setC] = useState<Calibration | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    const ac = new AbortController();
    api
      .calibration(ac.signal)
      .then(setC)
      .catch((e) => !ac.signal.aborted && setErr(String(e.message ?? e)));
    return () => ac.abort();
  }, []);

  if (err)
    return (
      <Wrap>
        <div className="panel px-8 py-14 text-center">
          <div className="t-h2 mb-2" style={{ color: "var(--color-warn)" }}>
            The calibration artifact has not been generated.
          </div>
          <p className="text-[14px]" style={{ color: "var(--color-ink-2)" }}>
            Run <code>FB_EXPORT=../docs/calibration.json go run ./cmd/backtest</code>{" "}
            from the core directory.
          </p>
        </div>
      </Wrap>
    );

  if (!c)
    return (
      <Wrap>
        <div className="panel" style={{ height: 420 }} aria-busy="true" />
      </Wrap>
    );

  return (
    <Wrap>
      <header className="mb-10 max-w-[68ch]">
        <h1 className="t-h1 mb-3">Why you should believe the model</h1>
        <p style={{ color: "var(--color-ink-2)" }}>
          Every resolved window was replayed using only information available at
          that moment. The model&rsquo;s single free parameter was fitted on the{" "}
          <strong style={{ color: "var(--color-ink)" }}>older half</strong> of
          the data. Everything below scores the{" "}
          <strong style={{ color: "var(--color-ink)" }}>
            newer half it has never seen
          </strong>
          .
        </p>
      </header>

      <div className="mb-10 flex flex-wrap gap-x-12 gap-y-6">
        <Kpi
          v={`+${c.skill.toFixed(1)}%`}
          l="skill versus a coin flip"
          colour="var(--color-up)"
          big
        />
        <Kpi v={c.brierModel.toFixed(4)} l="Brier — model" />
        <Kpi v={c.brierBaseline.toFixed(4)} l="Brier — always 0.5" dim />
        <Kpi v={c.testN.toLocaleString()} l="out-of-sample predictions" />
        <Kpi v={c.trainN.toLocaleString()} l="used only to fit" dim />
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,420px)]">
        <section className="panel p-6">
          <h2 className="t-h2 mb-1">Reliability</h2>
          <p className="mb-5 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
            A calibrated model tracks the diagonal: when it says 0.35, the thing
            happens 35% of the time. Dot size is sample count.
          </p>
          <Reliability buckets={c.buckets} />
        </section>

        <section className="panel p-6">
          <h2 className="t-h2 mb-1">Where the edge lives</h2>
          <p className="mb-5 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
            Skill rises as a window closes. This is the argument against a flat
            spread: the incumbent quotes the same width at every point in a
            window&rsquo;s life.
          </p>
          <SkillCurve rows={c.byProgress} />
        </section>
      </div>

      <section className="panel mt-6 p-6">
        <h2 className="t-h2 mb-1">What we are allowed to price</h2>
        <p className="mb-4 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
          σ is measured per asset and cadence from resolved history. Anything
          absent from this table has no history to fit against, and the engine
          refuses to quote it rather than extrapolate.
        </p>
        <table className="w-full">
          <thead>
            <tr>
              {["series", "σ per minute", "windows fitted"].map((h) => (
                <th key={h} className="t-micro py-2 text-left first:w-40">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {c.coverage.map((v, i) => (
              <tr key={i} className="hairline-t">
                <td className="py-2 font-mono text-[13px]">{v.asset}</td>
                <td className="py-2 font-mono text-[13px]">
                  {v.sigmaPerMin.toFixed(6)}
                </td>
                <td className="py-2 font-mono text-[13px]" style={{ color: "var(--color-ink-2)" }}>
                  {v.n.toLocaleString()}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <p className="t-micro mt-6" style={{ textTransform: "none", letterSpacing: 0 }}>
        Artifact generated {new Date(c.generatedAt).toUTCString()} · volatility
        multiplier k = {c.k.toFixed(3)} fitted on the training half alone.
      </p>
    </Wrap>
  );
}

/* ------------------------------------------------------------- charts --- */

function Reliability({ buckets }: { buckets: Calibration["buckets"] }) {
  const W = 620,
    H = 420,
    P = 48;
  const x = (v: number) => P + v * (W - P * 2);
  const y = (v: number) => H - P - v * (H - P * 2);
  const maxN = Math.max(...buckets.map((b) => b.n), 1);

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="Reliability diagram">
      <rect x={P} y={P} width={W - P * 2} height={H - P * 2} fill="var(--color-void)" stroke="var(--color-hairline)" />
      {[0, 0.25, 0.5, 0.75, 1].map((t) => (
        <g key={t}>
          <line x1={x(t)} y1={P} x2={x(t)} y2={H - P} stroke="var(--color-hairline)" strokeOpacity="0.5" />
          <line x1={P} y1={y(t)} x2={W - P} y2={y(t)} stroke="var(--color-hairline)" strokeOpacity="0.5" />
          <text x={x(t)} y={H - P + 18} fill="var(--color-ink-3)" fontSize="11" fontFamily="var(--font-mono)" textAnchor="middle">
            {t.toFixed(2)}
          </text>
          <text x={P - 10} y={y(t) + 4} fill="var(--color-ink-3)" fontSize="11" fontFamily="var(--font-mono)" textAnchor="end">
            {t.toFixed(2)}
          </text>
        </g>
      ))}
      {/* perfect calibration */}
      <line x1={x(0)} y1={y(0)} x2={x(1)} y2={y(1)} stroke="var(--color-ink-3)" strokeDasharray="4 4" />
      <polyline
        points={buckets.map((b) => `${x(b.predicted)},${y(b.realised)}`).join(" ")}
        fill="none"
        stroke="var(--color-model)"
        strokeWidth="2"
      />
      {buckets.map((b, i) => (
        <circle
          key={i}
          cx={x(b.predicted)}
          cy={y(b.realised)}
          r={4 + 7 * Math.sqrt(b.n / maxN)}
          fill="var(--color-model)"
          fillOpacity="0.75"
        >
          <title>
            n={b.n}\npredicted {b.predicted.toFixed(3)} → realised {b.realised.toFixed(3)}
          </title>
        </circle>
      ))}
      <text x={W / 2} y={H - 8} fill="var(--color-ink-3)" fontSize="11" fontFamily="var(--font-mono)" textAnchor="middle">
        predicted probability
      </text>
      <text x={14} y={H / 2} fill="var(--color-ink-3)" fontSize="11" fontFamily="var(--font-mono)" textAnchor="middle" transform={`rotate(-90 14 ${H / 2})`}>
        realised frequency
      </text>
    </svg>
  );
}

function SkillCurve({ rows }: { rows: Calibration["byProgress"] }) {
  const W = 420,
    H = 420,
    P = 48;
  const max = Math.max(...rows.map((r) => r.skill), 100);
  const x = (v: number) => P + (v / 100) * (W - P * 2);
  const y = (v: number) => H - P - (v / max) * (H - P * 2);

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="Skill by window progress">
      <rect x={P} y={P} width={W - P * 2} height={H - P * 2} fill="var(--color-void)" stroke="var(--color-hairline)" />
      {[0, 25, 50, 75, 100].map((t) => (
        <g key={t}>
          <line x1={P} y1={y(t)} x2={W - P} y2={y(t)} stroke="var(--color-hairline)" strokeOpacity="0.5" />
          <text x={P - 8} y={y(t) + 4} fill="var(--color-ink-3)" fontSize="11" fontFamily="var(--font-mono)" textAnchor="end">
            {t}%
          </text>
        </g>
      ))}
      <polyline
        points={rows.map((r) => `${x(r.elapsed)},${y(r.skill)}`).join(" ")}
        fill="none"
        stroke="var(--color-up)"
        strokeWidth="2.5"
      />
      {rows.map((r, i) => (
        <g key={i}>
          <circle cx={x(r.elapsed)} cy={y(r.skill)} r="4" fill="var(--color-up)" />
          <text
            x={x(r.elapsed)}
            y={y(r.skill) - 12}
            fill="var(--color-ink-2)"
            fontSize="11"
            fontFamily="var(--font-mono)"
            textAnchor="middle"
          >
            {r.skill.toFixed(0)}
          </text>
          <text x={x(r.elapsed)} y={H - P + 18} fill="var(--color-ink-3)" fontSize="11" fontFamily="var(--font-mono)" textAnchor="middle">
            {r.elapsed.toFixed(0)}%
          </text>
        </g>
      ))}
      <text x={W / 2} y={H - 8} fill="var(--color-ink-3)" fontSize="11" fontFamily="var(--font-mono)" textAnchor="middle">
        window elapsed
      </text>
    </svg>
  );
}

/* ------------------------------------------------------------- pieces --- */

function Wrap({ children }: { children: React.ReactNode }) {
  return <div className="mx-auto max-w-[1400px] px-6 py-10">{children}</div>;
}

function Kpi({
  v,
  l,
  colour,
  dim,
  big,
}: {
  v: string;
  l: string;
  colour?: string;
  dim?: boolean;
  big?: boolean;
}) {
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
      <div className="t-micro mt-1.5">{l}</div>
    </div>
  );
}
