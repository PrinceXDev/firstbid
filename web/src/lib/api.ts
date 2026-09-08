/**
 * Every value in this product comes from one of these endpoints, served by the
 * Go engine. Nothing here is generated, simulated, or padded — if a field is
 * missing the UI says so rather than inventing a plausible number.
 */

export type LiveRow = {
  label: string;
  asset: string;
  secondsLeft: number;
  spot: number;
  open: number;
  fair: number;
  uncertainty: number;
  bid: number;
  ask: number;
  spread: number;
  ourSpread: number;
  verdict: string;
  calibrated: boolean;
};

export type LivePayload = { rows: LiveRow[]; spotAgeSec: number };

export type CalibrationBucket = {
  lo: number;
  hi: number;
  n: number;
  predicted: number;
  realised: number;
};

export type ProgressRow = {
  elapsed: number;
  n: number;
  brier: number;
  skill: number;
};

export type Calibration = {
  generatedAt: string;
  k: number;
  trainN: number;
  testN: number;
  brierModel: number;
  brierBaseline: number;
  skill: number;
  buckets: CalibrationBucket[];
  byProgress: ProgressRow[];
  coverage: { asset: string; sigmaPerMin: number; n: number }[];
};

export type Attribution = {
  MarketID: string;
  Label: string;
  Asset: string;
  IntervalSec: number;
  Contracts: number;
  Cost: number;
  Payout: number;
  Edge: number;
  Selection: number;
  Net: number;
  Winner: number;
  Voided: boolean;
  Fills: number;
};

export type PnL = {
  windows: Attribution[];
  totals: {
    Windows: number;
    Fills: number;
    Contracts: number;
    Edge: number;
    Selection: number;
    Net: number;
  };
};

export type TraceOrder = {
  at: number;
  mode: string;
  kind: string;
  price: number;
  quantity: number;
  fair: number;
  spot: number;
  openPx: number;
  secsLeft: number;
  rested: boolean;
  fills: number;
  txHash: string;
};

export type TraceFill = {
  at: number;
  kind: string;
  price: number;
  quantity: number;
  fair: number;
  txHash: string;
};

export type Trace = {
  marketId: string;
  label: string;
  asset: string;
  intervalSec: number;
  expiry: number;
  tradingStart: number;
  winner: number;
  voided: boolean;
  settled: boolean;
  orders: TraceOrder[];
  fills: TraceFill[];
  attribution: Attribution | null;
};

export type CoverageRow = {
  label: string;
  asset: string;
  intervalSec: number;
  quotable: boolean;
  reason: string;
};

export type CoveragePayload = { rows: CoverageRow[] };

export type HealthPoint = { settledAt: number; fair: number; value: number };

export type HealthPayload = {
  n: number;
  brierLive: number;
  brierBaseline: number;
  skillLive: number;
  points: HealthPoint[];
};

export type ExposureRow = { asset: string; netUp: number; cap: number; updatedAt: number };

export type RiskPayload = { assets: ExposureRow[] };

export type LatencySample = { marketId: string; phase: string; millis: number; createdAt: number };

export type ChainPayload = {
  blockNumber: number;
  blockTimeMs: number;
  sampledBlocks: number;
  latency: {
    n: number;
    meanMs: number;
    p50Ms: number;
    p90Ms: number;
    samples: LatencySample[];
  };
};

/** In development the Go engine runs on :8080; in production it serves this page. */
const BASE =
  process.env.NODE_ENV === "development" ? "http://localhost:8080" : "";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { signal, cache: "no-store" });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) detail = body.error;
    } catch {
      /* the body was not JSON; the status text will have to do */
    }
    throw new ApiError(detail, res.status);
  }
  return (await res.json()) as T;
}

export const api = {
  live: (signal?: AbortSignal) => get<LivePayload>("/api/live", signal),
  calibration: (signal?: AbortSignal) =>
    get<Calibration>("/api/calibration", signal),
  pnl: (signal?: AbortSignal) => get<PnL>("/api/pnl", signal),
  traces: (signal?: AbortSignal) =>
    get<{ marketIds: string[] }>("/api/traces", signal),
  trace: (id: string, signal?: AbortSignal) =>
    get<Trace>(`/api/trace/${id}`, signal),
  coverage: (signal?: AbortSignal) => get<CoveragePayload>("/api/coverage", signal),
  health: (signal?: AbortSignal) => get<HealthPayload>("/api/health", signal),
  risk: (signal?: AbortSignal) => get<RiskPayload>("/api/risk", signal),
  chain: (signal?: AbortSignal) => get<ChainPayload>("/api/chain", signal),
};

/* ---------- formatting -------------------------------------------------- */

/** Probabilities always render to three decimals: the venue's tick is 0.001. */
export const p3 = (v: number) => v.toFixed(3);

export const pct = (v: number, digits = 1) => `${(v * 100).toFixed(digits)}%`;

export const signed = (v: number, digits = 4) =>
  `${v >= 0 ? "+" : "−"}${Math.abs(v).toFixed(digits)}`;

export function clock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "--:--";
  const s = Math.floor(seconds);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${pad(m)}:${pad(sec)}`;
}

export const price = (v: number) =>
  v >= 1000
    ? v.toLocaleString("en-US", { maximumFractionDigits: 2, minimumFractionDigits: 2 })
    : v.toFixed(2);

/**
 * How interesting a window is: the model's disagreement with the market,
 * measured in units of the model's own uncertainty.
 *
 * This is the ranking that matters. Sorting by volume shows you where people
 * already are; sorting by disagreement-over-noise shows you where the price may
 * be wrong. A gap of 0.04 means nothing if uncertainty is 0.08, and means a
 * great deal if uncertainty is 0.01.
 */
export function tension(r: LiveRow): number {
  if (!r.calibrated || r.fair <= 0) return -1;
  const mid = r.bid > 0 && r.ask > 0 ? (r.bid + r.ask) / 2 : 0;
  if (mid <= 0) return -1;
  const noise = Math.max(r.uncertainty, 0.004);
  return Math.abs(r.fair - mid) / noise;
}

/** The market's own mid, or null when the book is one-sided or empty. */
export function marketMid(r: LiveRow): number | null {
  if (r.bid > 0 && r.ask > 0) return (r.bid + r.ask) / 2;
  return null;
}

/**
 * Picks the window the hero should show.
 *
 * Ranking purely by disagreement surfaces markets whose outcome is already
 * decided — a model reading 0.000 against a book at 0.015 is a large "tension"
 * and a useless headline. The hero must be a window that is still genuinely
 * undecided, because the product is about watching a probability *get* decided.
 */
export function pickHero(rows: LiveRow[]): LiveRow | null {
  const live = rows.filter(
    (r) =>
      r.calibrated &&
      r.fair > 0.04 &&
      r.fair < 0.96 &&
      r.secondsLeft > 45 &&
      r.bid > 0 &&
      r.ask > 0,
  );
  if (live.length) {
    // Among genuinely undecided windows, prefer the one closest to expiry:
    // that is where the model earns its keep and where the clock is dramatic.
    return live.sort((a, b) => a.secondsLeft - b.secondsLeft)[0];
  }
  const anyCalibrated = rows.filter((r) => r.calibrated && r.fair > 0);
  if (anyCalibrated.length) {
    return anyCalibrated.sort((a, b) => tension(b) - tension(a))[0];
  }
  return rows[0] ?? null;
}
