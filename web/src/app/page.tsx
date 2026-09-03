"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Shell, LiveDot, useMode } from "@/components/Shell";
import { ProbabilityWithDoubt, Ticking } from "@/components/Probability";
import { DisagreementBar, DisagreementLegend } from "@/components/Disagreement";
import {
  api,
  clock,
  marketMid,
  p3,
  price,
  pickHero,
  tension,
  type LivePayload,
  type LiveRow,
} from "@/lib/api";

export default function Page() {
  return (
    <Shell>
      <Field />
    </Shell>
  );
}

function Field() {
  const { data, error, stale } = useLive();

  if (error) return <Failure message={error} />;
  if (!data) return <FieldSkeleton />;

  const ranked = [...data.rows].sort((a, b) => tension(b) - tension(a));
  const hero = pickHero(data.rows);
  const rest = ranked.filter((r) => r !== hero);

  if (!hero) return <NoWindows />;

  return (
    <div className="mx-auto max-w-[1400px] px-6 pb-8 pt-8">
      <Hero row={hero} spotAge={data.spotAgeSec} stale={stale} />
      <section className="mt-12">
        <div className="mb-3 flex items-baseline justify-between">
          <h2 className="t-h2">The field</h2>
          <span className="t-micro">
            ranked by disagreement ÷ noise — not by volume
          </span>
        </div>
        <div className="mb-4">
          <DisagreementLegend />
        </div>
        <div className="panel overflow-hidden">
          {rest.map((r, i) => (
            <Row key={r.label + i} row={r} />
          ))}
          {rest.length === 0 && (
            <div className="px-5 py-8 text-center" style={{ color: "var(--color-ink-3)" }}>
              This is the only live window right now.
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

/* ---------------------------------------------------------------- hero --- */

function Hero({ row, spotAge, stale }: { row: LiveRow; spotAge: number; stale: boolean }) {
  const { mode } = useMode();
  const mid = marketMid(row);
  const left = useCountdown(row.secondsLeft);
  const gap = mid !== null && row.calibrated ? row.fair - mid : null;
  const noise = Math.max(row.uncertainty, 0.004);
  const meaningful = gap !== null && Math.abs(gap) > noise;

  return (
    <section className="panel overflow-hidden" style={{ opacity: stale ? 0.75 : 1 }}>
      <div className="hairline-b flex flex-wrap items-center gap-x-5 gap-y-2 px-5 py-3">
        <span className="t-h2">{row.label.replace("/", " · ")}</span>
        <span className="t-micro">window</span>
        <span className="ml-auto flex items-center gap-4">
          <LiveDot ageSec={spotAge} />
          <span className="t-micro">
            {row.calibrated ? "calibrated" : "not modelled"}
          </span>
        </span>
      </div>

      <div className="grid gap-8 px-5 py-7 lg:grid-cols-[minmax(0,340px)_minmax(0,1fr)]">
        {/* the one dominant object */}
        <div>
          {row.calibrated && row.fair > 0 ? (
            <ProbabilityWithDoubt
              value={row.fair}
              doubt={row.uncertainty}
              label="model · probability of Up"
            />
          ) : (
            <div>
              <div className="t-micro mb-2">no validated model</div>
              <div className="t-display" style={{ color: "var(--color-ink-3)" }}>
                —
              </div>
              <p className="mt-3 max-w-[34ch] text-[13px]" style={{ color: "var(--color-ink-2)" }}>
                This cadence has no resolved history to fit against, so we
                refuse to price it rather than extrapolate.
              </p>
            </div>
          )}

          <div className="mt-6 grid grid-cols-2 gap-x-6 gap-y-3">
            <Stat label="time remaining" value={clock(left)} accent={left < 60} />
            <Stat label="market mid" value={mid !== null ? p3(mid) : "—"} />
            <Stat label="spot" value={<Ticking value={row.spot} format={price} />} />
            <Stat label="the line to beat" value={price(row.open)} />
          </div>
        </div>

        {/* the disagreement */}
        <div className="min-w-0">
          <div className="t-micro mb-2">market versus model</div>
          <DisagreementBar row={row} height={92} />

          <div className="mt-5 rounded-md px-4 py-3" style={{ background: "var(--color-raised)" }}>
            <div className="t-micro mb-1">our read</div>
            <p className="text-[15px]" style={{ color: "var(--color-ink)" }}>
              {row.verdict}
            </p>
            {gap !== null && (
              <p className="mt-2 text-[13px]" style={{ color: "var(--color-ink-2)" }}>
                Model is{" "}
                <strong style={{ color: meaningful ? (gap > 0 ? "var(--color-up)" : "var(--color-down)") : "var(--color-ink-2)" }}>
                  {Math.abs(gap).toFixed(3)}
                </strong>{" "}
                {gap > 0 ? "above" : "below"} the market mid, against a noise
                floor of {noise.toFixed(3)}.{" "}
                {meaningful
                  ? "That clears the noise."
                  : "That is inside the noise, so it is not a signal."}
              </p>
            )}
          </div>

          {mode !== "read" && (
            <div className="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4">
              <Stat label="best bid" value={row.bid > 0 ? p3(row.bid) : "—"} />
              <Stat label="best ask" value={row.ask > 0 ? p3(row.ask) : "—"} />
              <Stat label="their spread" value={row.spread > 0 ? p3(row.spread) : "—"} />
              <Stat
                label="ours would be"
                value={row.ourSpread > 0 ? p3(row.ourSpread) : "—"}
                accent={row.ourSpread > 0 && row.spread > 0 && row.ourSpread < row.spread}
              />
            </div>
          )}
        </div>
      </div>
    </section>
  );
}

/* ---------------------------------------------------------------- rows --- */

function Row({ row }: { row: LiveRow }) {
  const { mode } = useMode();
  const mid = marketMid(row);
  const left = useCountdown(row.secondsLeft);
  const t = tension(row);

  return (
    <div
      className="grid items-center gap-4 px-5 py-3 hairline-b transition-colors hover:bg-[var(--color-raised)] lg:grid-cols-[130px_88px_minmax(0,1fr)_150px_minmax(0,240px)]"
      style={{ opacity: row.calibrated ? 1 : 0.55 }}
    >
      <div>
        <div className="font-mono text-[13px]">{row.label}</div>
        <div className="t-micro">{clock(left)} left</div>
      </div>

      <div className="font-mono text-[13px]">
        {row.calibrated && row.fair > 0 ? (
          <span style={{ color: "var(--color-model)" }}>{p3(row.fair)}</span>
        ) : (
          <span style={{ color: "var(--color-ink-3)" }}>—</span>
        )}
        <div className="t-micro" style={{ letterSpacing: 0 }}>
          {row.calibrated ? `±${row.uncertainty.toFixed(3)}` : "no model"}
        </div>
      </div>

      <DisagreementBar row={row} height={38} showScale={false} />

      <div className="font-mono text-[13px]" style={{ color: "var(--color-ink-2)" }}>
        {mid !== null ? `${p3(row.bid)} / ${p3(row.ask)}` : "one-sided"}
        {mode === "dissect" && t > 0 && (
          <div className="t-micro" style={{ letterSpacing: 0 }}>
            tension {t.toFixed(1)}σ
          </div>
        )}
      </div>

      <div className="text-[12px]" style={{ color: "var(--color-ink-2)" }}>
        {row.verdict}
      </div>
    </div>
  );
}

/* -------------------------------------------------------------- pieces --- */

function Stat({
  label,
  value,
  accent,
}: {
  label: string;
  value: React.ReactNode;
  accent?: boolean;
}) {
  return (
    <div>
      <div className="t-micro">{label}</div>
      <div
        className="font-mono text-[15px]"
        style={{ color: accent ? "var(--color-up)" : "var(--color-ink)" }}
      >
        {value}
      </div>
    </div>
  );
}

function FieldSkeleton() {
  return (
    <div className="mx-auto max-w-[1400px] px-6 pt-8">
      <div className="panel" style={{ height: 300 }} aria-busy="true">
        <div className="hairline-b px-5 py-3">
          <Bar w={180} />
        </div>
        <div className="grid gap-8 px-5 py-7 lg:grid-cols-[340px_1fr]">
          <div className="space-y-3">
            <Bar w={120} h={12} />
            <Bar w={220} h={56} />
            <Bar w={200} h={12} />
          </div>
          <div className="space-y-3">
            <Bar w={140} h={12} />
            <Bar w="100%" h={92} />
          </div>
        </div>
      </div>
    </div>
  );
}

function Bar({ w, h = 20 }: { w: number | string; h?: number }) {
  return (
    <div
      style={{
        width: w,
        height: h,
        background: "var(--color-raised)",
        borderRadius: 4,
      }}
    />
  );
}

function NoWindows() {
  return (
    <Empty
      title="No live windows right now."
      body="The venue rolls windows continuously — as one closes the next opens. This page will pick the next one up automatically."
    />
  );
}

function Failure({ message }: { message: string }) {
  const human = humanise(message);
  return <Empty title={human.title} body={human.body} warn />;
}

/** Technical failures are translated, never shown raw. */
function humanise(msg: string): { title: string; body: string } {
  if (/price feed/i.test(msg))
    return {
      title: "The index price feed is unreachable.",
      body: "Without a live spot price we cannot compute fair value, so we show nothing rather than something stale.",
    };
  if (/indexer/i.test(msg))
    return {
      title: "The market indexer is unreachable.",
      body: "Market discovery runs through Somnia's indexer. Chain reads are unaffected; this page needs the list of live windows.",
    };
  if (/failed to fetch|networkerror/i.test(msg))
    return {
      title: "The engine is not running.",
      body: "Start it with `go run ./cmd/dashboard` from the core directory, then reload.",
    };
  return { title: "Something went wrong upstream.", body: msg };
}

function Empty({ title, body, warn }: { title: string; body: string; warn?: boolean }) {
  return (
    <div className="mx-auto max-w-[1400px] px-6 pt-8">
      <div className="panel px-8 py-14 text-center">
        <div
          className="t-h2 mb-2"
          style={{ color: warn ? "var(--color-warn)" : "var(--color-ink)" }}
        >
          {title}
        </div>
        <p className="mx-auto max-w-[52ch] text-[14px]" style={{ color: "var(--color-ink-2)" }}>
          {body}
        </p>
      </div>
    </div>
  );
}

/* --------------------------------------------------------------- hooks --- */

function useLive() {
  const [data, setData] = useState<LivePayload | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [fetchedAt, setFetchedAt] = useState(0);

  useEffect(() => {
    let alive = true;
    const ac = new AbortController();

    const tick = async () => {
      try {
        const d = await api.live(ac.signal);
        if (!alive) return;
        setData(d);
        setError(null);
        setFetchedAt(Date.now());
      } catch (e) {
        if (!alive || ac.signal.aborted) return;
        setError(e instanceof Error ? e.message : String(e));
      }
    };

    tick();
    const id = setInterval(tick, 10_000);
    return () => {
      alive = false;
      ac.abort();
      clearInterval(id);
    };
  }, []);

  // Only claim freshness we actually have.
  const stale = fetchedAt > 0 && Date.now() - fetchedAt > 25_000;
  return { data, error, stale };
}

/** Counts down locally between polls so the clock never appears frozen. */
function useCountdown(seconds: number) {
  const [left, setLeft] = useState(seconds);
  const base = useRef({ at: Date.now(), secs: seconds });

  useEffect(() => {
    base.current = { at: Date.now(), secs: seconds };
    setLeft(seconds);
  }, [seconds]);

  useEffect(() => {
    const id = setInterval(() => {
      const elapsed = (Date.now() - base.current.at) / 1000;
      setLeft(Math.max(0, base.current.secs - elapsed));
    }, 250);
    return () => clearInterval(id);
  }, []);

  return useMemo(() => left, [left]);
}
