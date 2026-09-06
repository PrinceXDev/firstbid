"use client";

import { createContext, useContext, useEffect, useState } from "react";

/**
 * Progressive disclosure is handled by one global control rather than a toggle
 * per panel. Detail appears where it already was; nothing moves. Spatial
 * stability is the point — a layout that rearranges when you ask for more
 * information forces the user to re-find everything.
 */
export type Mode = "read" | "trade" | "dissect";

const ModeCtx = createContext<{ mode: Mode; setMode: (m: Mode) => void }>({
  mode: "read",
  setMode: () => {},
});

export const useMode = () => useContext(ModeCtx);

const MODES: { id: Mode; key: string; hint: string }[] = [
  { id: "read", key: "1", hint: "probability, time, what changed" },
  { id: "trade", key: "2", hint: "adds book, spread, execution" },
  { id: "dissect", key: "3", hint: "adds model internals and attribution" },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const [mode, setMode] = useState<Mode>("read");

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const el = document.activeElement;
      if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return;
      const hit = MODES.find((m) => m.key === e.key);
      if (hit) setMode(hit.id);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <ModeCtx.Provider value={{ mode, setMode }}>
      <div className="min-h-screen">
        <TopBar mode={mode} setMode={setMode} />
        <MobileNav />
      <main id="main">{children}</main>
        <Footer />
      </div>
    </ModeCtx.Provider>
  );
}

function TopBar({ mode, setMode }: { mode: Mode; setMode: (m: Mode) => void }) {
  return (
    <header className="sticky top-0 z-40 hairline-b" style={{ background: "rgba(8,9,11,0.86)", backdropFilter: "blur(12px)" }}>
      <div className="mx-auto flex h-14 max-w-[1400px] items-center gap-8 px-6">
        <a href="/" className="flex items-baseline gap-2">
          <span className="text-[17px] font-semibold tracking-tight">Firstbid</span>
          <span className="t-micro hidden sm:inline">event contracts · somnia</span>
        </a>

        <nav className="hidden items-center gap-6 md:flex" aria-label="Sections">
          <NavLink href="/">Field</NavLink>
          <NavLink href="/evidence/">Evidence</NavLink>
          <NavLink href="/trace/">Trace</NavLink>
          <NavLink href="/ledger/">Ledger</NavLink>
        </nav>

        <div className="ml-auto flex items-center gap-3">
          <div
            className="hidden items-center rounded-md border p-0.5 sm:flex"
            style={{ borderColor: "var(--color-hairline)" }}
            role="group"
            aria-label="Detail level"
          >
            {MODES.map((m) => (
              <button
                key={m.id}
                onClick={() => setMode(m.id)}
                title={`${m.hint}  (${m.key})`}
                aria-pressed={mode === m.id}
                className="rounded px-2.5 py-1 t-micro transition-colors"
                style={{
                  background: mode === m.id ? "var(--color-raised)" : "transparent",
                  color: mode === m.id ? "var(--color-ink)" : "var(--color-ink-3)",
                }}
              >
                {m.id}
              </button>
            ))}
          </div>
        </div>
      </div>
    </header>
  );
}

/**
 * Navigation for narrow screens.
 *
 * The desktop bar hides its links below md, which left mobile with no way to
 * reach Evidence, Trace or Ledger at all. A scrollable row under the header
 * costs one line of height and restores the whole product.
 */
function MobileNav() {
  return (
    <nav
      className="hairline-b flex gap-5 overflow-x-auto px-6 py-2.5 md:hidden"
      aria-label="Sections"
      style={{ background: "var(--color-surface)" }}
    >
      <NavLink href="/">Field</NavLink>
      <NavLink href="/evidence/">Evidence</NavLink>
      <NavLink href="/trace/">Trace</NavLink>
      <NavLink href="/ledger/">Ledger</NavLink>
    </nav>
  );
}

function NavLink({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      className="whitespace-nowrap text-[13px] transition-colors hover:text-[var(--color-ink)]"
      style={{ color: "var(--color-ink-2)" }}
    >
      {children}
    </a>
  );
}

function Footer() {
  return (
    <footer className="hairline-t mt-24">
      <div className="mx-auto max-w-[1400px] px-6 py-10">
        <p className="t-micro max-w-[70ch]" style={{ textTransform: "none", letterSpacing: 0 }}>
          Every figure on this site is read from Somnia or computed from settled
          on-chain history. Model values are marked in blue. Nothing is simulated.
          Fair value = Φ( ln(spot/open) / (σ·√minutes) ), with σ measured per asset
          and cadence from resolved windows. No directional forecast is made or
          implied.
        </p>
      </div>
    </footer>
  );
}

/** A live/stale indicator that never lies about freshness. */
export function LiveDot({ ageSec, staleAfter = 10 }: { ageSec: number; staleAfter?: number }) {
  const stale = ageSec > staleAfter;
  return (
    <span className="inline-flex items-center gap-1.5 t-micro">
      <span
        className={stale ? "" : "breathe"}
        style={{
          width: 7,
          height: 7,
          borderRadius: 99,
          background: stale ? "var(--color-warn)" : "var(--color-up)",
          display: "inline-block",
        }}
      />
      {stale ? `stale ${ageSec.toFixed(0)}s` : "live"}
    </span>
  );
}
