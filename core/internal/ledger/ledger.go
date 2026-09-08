// Package ledger records what we traded and what it earned.
//
// Event contracts have a property no other DeFi primitive has: every position
// reaches a terminal, exact, verifiable value within an hour. There is no
// mark-to-model and no open-ended exposure. That makes per-window profit an
// arithmetic fact rather than an estimate, and it is the reason this ledger can
// attribute earnings precisely instead of reporting one blended number.
package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, so the repo builds anywhere
)

type DB struct{ sql *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS orders (
  tx_hash     TEXT PRIMARY KEY,
  market_id   TEXT NOT NULL,
  label       TEXT NOT NULL,
  kind        TEXT NOT NULL,      -- BUY_UP | BUY_DN
  mode        TEXT NOT NULL,      -- make | take
  price       REAL NOT NULL,      -- in the side's own terms
  quantity    REAL NOT NULL,
  fair        REAL NOT NULL,      -- our model's value at decision time
  spot        REAL NOT NULL,
  open_px     REAL NOT NULL,
  secs_left   REAL NOT NULL,
  rested      INTEGER NOT NULL,
  fills       INTEGER NOT NULL,
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS fills (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  -- Stable identity for one on-chain fill. Reconciliation rescans overlapping
  -- windows and restarts replay history, so without this a fill would be
  -- counted several times and the P&L would drift upward on every sweep.
  fill_key    TEXT NOT NULL UNIQUE,
  tx_hash     TEXT NOT NULL,
  market_id   TEXT NOT NULL,
  kind        TEXT NOT NULL,
  price       REAL NOT NULL,      -- realised fill price, side's own terms
  quantity    REAL NOT NULL,
  fair        REAL NOT NULL,      -- fair value at the moment of the fill
  -- 0 for a fill reconciled from a counterparty's transaction: reconcile()
  -- has no decision-time model snapshot for those, so it stores the
  -- execution price in "fair" instead, which makes P&L's edge contribution
  -- honestly zero. Scoring that placeholder as a probability in the live
  -- health check would measure price-vs-outcome, not model calibration.
  has_model_fair INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS windows (
  market_id     TEXT PRIMARY KEY,
  label         TEXT NOT NULL,
  asset         TEXT NOT NULL,
  interval_sec  INTEGER NOT NULL,
  expiry        INTEGER NOT NULL,
  open_px       REAL,
  close_px      REAL,
  winner        INTEGER,          -- 0 = Up, 1 = Down, NULL = unresolved
  voided        INTEGER NOT NULL DEFAULT 0,
  settled_at    INTEGER
);

CREATE INDEX IF NOT EXISTS idx_fills_market ON fills(market_id);
CREATE INDEX IF NOT EXISTS idx_orders_market ON orders(market_id);

-- One row per submitted order: how long signing, RPC submission and mining
-- together took. The engine and the dashboard are separate processes -- this
-- table is how a live latency reading crosses that boundary, the same way
-- orders/fills/windows already do.
CREATE TABLE IF NOT EXISTS latencies (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  market_id   TEXT NOT NULL,
  phase       TEXT NOT NULL,      -- submit-to-receipt (only phase measured so far)
  millis      REAL NOT NULL,
  created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_latencies_created ON latencies(created_at);

-- Current aggregate position per asset, across every window open on that
-- asset right now. One row per asset, replaced on every inventory change.
-- Per-window inventory (see the "orders" and "fills" tables joined against
-- "windows") caps a single market; this is the number a cross-window risk
-- cap must consult, and the only one that would have caught the 2026-09-03
-- loss if it had spanned two windows instead of one.
CREATE TABLE IF NOT EXISTS exposure (
  asset       TEXT PRIMARY KEY,
  net_up      REAL NOT NULL,      -- signed: positive = net long Up
  cap         REAL NOT NULL,
  updated_at  INTEGER NOT NULL
);
`

func Open(path string) (*DB, error) {
	// The dashboard and the live engine can both hold this file open at once
	// (e.g. sharing one Docker volume), one reading while the other writes.
	// Without WAL a writer's transaction locks the whole file against readers,
	// and without a busy timeout the loser of that race fails immediately
	// instead of waiting — which is how a window, fill or settlement goes
	// missing from attribution without either side noticing.
	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := d.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	// CREATE TABLE IF NOT EXISTS does not add columns to a table that already
	// exists — a database from before has_model_fair was introduced needs an
	// explicit migration, or every fill written before this column existed
	// would silently read back as 0 (NOT NULL with no default on ALTER means
	// SQLite backfills existing rows with the column's DEFAULT, so this also
	// correctly treats pre-existing fills as having a real model fair value,
	// which is what they always were).
	if _, err := d.Exec(`ALTER TABLE fills ADD COLUMN has_model_fair INTEGER NOT NULL DEFAULT 1`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			return nil, fmt.Errorf("migrate fills.has_model_fair: %w", err)
		}
	}
	return &DB{sql: d}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// OrderRow is one submitted order, with the model state that justified it.
// Recording the reasoning alongside the action is what makes the P&L explainable
// after the fact rather than merely countable.
type OrderRow struct {
	TxHash   string
	MarketID string
	Label    string
	Kind     string
	Mode     string
	Price    float64
	Quantity float64
	Fair     float64
	Spot     float64
	OpenPx   float64
	SecsLeft float64
	Rested   bool
	Fills    int
}

func (d *DB) RecordOrder(ctx context.Context, o OrderRow) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT OR REPLACE INTO orders
		(tx_hash, market_id, label, kind, mode, price, quantity, fair, spot, open_px, secs_left, rested, fills, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.TxHash, o.MarketID, o.Label, o.Kind, o.Mode, o.Price, o.Quantity,
		o.Fair, o.Spot, o.OpenPx, o.SecsLeft, b2i(o.Rested), o.Fills, time.Now().Unix())
	return err
}

type FillRow struct {
	// Key uniquely identifies this fill on chain. Reconciliation supplies the
	// indexer's row id; receipt-decoded fills synthesise one from the
	// transaction hash and the fill's position within it.
	Key      string
	TxHash   string
	MarketID string
	Kind     string
	Price    float64
	Quantity float64
	Fair     float64
	// HasModelFair is false only for a fill reconciled from a counterparty's
	// transaction, where Fair holds the execution price rather than a genuine
	// decision-time model value. Callers must set this explicitly -- there is
	// no safe default, since a caller that forgets would silently score a
	// price as a prediction.
	HasModelFair bool
}

// RecordFill is idempotent: re-recording a fill we already have is a no-op.
func (d *DB) RecordFill(ctx context.Context, f FillRow) error {
	if f.Key == "" {
		return fmt.Errorf("fill has no key; refusing to record an un-deduplicable row")
	}
	_, err := d.sql.ExecContext(ctx, `
		INSERT OR IGNORE INTO fills
		(fill_key, tx_hash, market_id, kind, price, quantity, fair, has_model_fair, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		f.Key, f.TxHash, f.MarketID, f.Kind, f.Price, f.Quantity, f.Fair, b2i(f.HasModelFair), time.Now().Unix())
	return err
}

// HasFill reports whether a fill is already recorded, so a reconciler can skip
// the work of pricing one it has seen.
func (d *DB) HasFill(ctx context.Context, key string) (bool, error) {
	var n int
	err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM fills WHERE fill_key = ?`, key).Scan(&n)
	return n > 0, err
}

// KnownMarket reports whether we have ever traded this market, so reconciliation
// can ignore fills belonging to somebody else's strategy on the same wallet.
func (d *DB) KnownMarket(ctx context.Context, marketID string) (bool, error) {
	var n int
	err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM windows WHERE market_id = ?`, marketID).Scan(&n)
	return n > 0, err
}

type WindowRow struct {
	MarketID    string
	Label       string
	Asset       string
	IntervalSec int64
	Expiry      int64
}

func (d *DB) UpsertWindow(ctx context.Context, w WindowRow) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO windows (market_id, label, asset, interval_sec, expiry)
		VALUES (?,?,?,?,?)
		ON CONFLICT(market_id) DO UPDATE SET label=excluded.label`,
		w.MarketID, w.Label, w.Asset, w.IntervalSec, w.Expiry)
	return err
}

// Settle records the outcome so realised P&L becomes arithmetic.
func (d *DB) Settle(ctx context.Context, marketID string, winner int, voided bool, openPx, closePx float64) error {
	_, err := d.sql.ExecContext(ctx, `
		UPDATE windows SET winner=?, voided=?, open_px=?, close_px=?, settled_at=?
		WHERE market_id=?`,
		winner, b2i(voided), openPx, closePx, time.Now().Unix(), marketID)
	return err
}

// Attribution decomposes one settled window's profit into its causes.
//
// Every filled contract is bought at some price p and redeems at a known value
// v (1 if its side won, 0 if it lost, 0.5 on a void). Total profit is sum(v-p).
// Splitting that against our fair value at fill time separates skill from luck:
//
//	Edge      = sum(fair - p)   what we believed we were capturing
//	Selection = sum(v - fair)   how the outcome differed from our belief
//	Net       = Edge + Selection
//
// Edge is the part a market maker controls. Selection is the part it does not,
// and over many windows it should average toward zero if the model is calibrated.
// Reporting them separately is the difference between "we made money" and
// "we know why we made money".
type Attribution struct {
	MarketID    string
	Label       string
	Asset       string
	IntervalSec int64
	Contracts   float64
	Cost      float64
	Payout    float64
	Edge      float64
	Selection float64
	Net       float64
	Winner    int
	Voided    bool
	Fills     int
}

func (a Attribution) Verdict() string {
	switch {
	case a.Fills == 0:
		return "no fills"
	case a.Net > 0:
		return "profit"
	case a.Net < 0:
		return "loss"
	}
	return "flat"
}

// Attribute computes realised P&L for every settled window we traded.
func (d *DB) Attribute(ctx context.Context) ([]Attribution, error) {
	return d.attribute(ctx, "", "", 0)
}

// AttributeMarket computes realised P&L for a single settled window.
//
// Fetching one window's attribution must not read the whole ledger: a trace
// endpoint that grows linearly with total history is a trace endpoint that
// stops working once the engine has been running for a while.
func (d *DB) AttributeMarket(ctx context.Context, marketID string) (*Attribution, error) {
	as, err := d.attribute(ctx, marketID, "", 0)
	if err != nil {
		return nil, err
	}
	if len(as) == 0 {
		return nil, nil
	}
	return &as[0], nil
}

// AttributeCadence computes realised P&L for one asset/interval pair, e.g.
// ("BTC", 14400) for BTC/240m. It exists so a cadence that was only just
// admitted into coverage — see docs/COVERAGE.md — can be judged on its own
// P&L rather than buried in the all-cadence total.
func (d *DB) AttributeCadence(ctx context.Context, asset string, intervalSec int64) ([]Attribution, error) {
	return d.attribute(ctx, "", asset, intervalSec)
}

func (d *DB) attribute(ctx context.Context, onlyMarket, onlyAsset string, onlyIntervalSec int64) ([]Attribution, error) {
	query := `
		SELECT w.market_id, w.label, w.asset, w.interval_sec, w.winner, w.voided,
		       f.kind, f.price, f.quantity, f.fair
		FROM windows w
		JOIN fills f ON f.market_id = w.market_id
		WHERE w.settled_at IS NOT NULL`
	args := []any{}
	if onlyMarket != "" {
		query += ` AND w.market_id = ?`
		args = append(args, onlyMarket)
	}
	if onlyAsset != "" {
		query += ` AND w.asset = ?`
		args = append(args, onlyAsset)
	}
	if onlyIntervalSec != 0 {
		query += ` AND w.interval_sec = ?`
		args = append(args, onlyIntervalSec)
	}
	query += ` ORDER BY w.expiry ASC`

	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byMarket := map[string]*Attribution{}
	var order []string

	for rows.Next() {
		var mid, label, asset, kind string
		var intervalSec int64
		var winner sql.NullInt64
		var voided int
		var price, qty, fair float64
		if err := rows.Scan(&mid, &label, &asset, &intervalSec, &winner, &voided, &kind, &price, &qty, &fair); err != nil {
			return nil, err
		}
		a, ok := byMarket[mid]
		if !ok {
			a = &Attribution{MarketID: mid, Label: label, Asset: asset, IntervalSec: intervalSec, Voided: voided == 1, Winner: -1}
			if winner.Valid {
				a.Winner = int(winner.Int64)
			}
			byMarket[mid] = a
			order = append(order, mid)
		}

		// Value each contract at settlement, in the side's own terms.
		var value float64
		switch {
		case a.Voided:
			value = 0.5 // a void refunds 0.5 per contract, whichever side
		case kind == "BUY_UP":
			value = boolTo(a.Winner == 0)
		case kind == "BUY_DN":
			value = boolTo(a.Winner == 1)
		}
		// Fair value in this side's terms: a Down contract is worth 1 - fairUp.
		sideFair := fair
		if kind == "BUY_DN" {
			sideFair = 1 - fair
		}

		a.Fills++
		a.Contracts += qty
		a.Cost += price * qty
		a.Payout += value * qty
		a.Edge += (sideFair - price) * qty
		a.Selection += (value - sideFair) * qty
		a.Net = a.Payout - a.Cost
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]Attribution, 0, len(order))
	for _, id := range order {
		out = append(out, *byMarket[id])
	}
	return out, nil
}

// Totals is the headline: what the strategy earned and where it came from.
type Totals struct {
	Windows   int
	Fills     int
	Contracts float64
	Edge      float64
	Selection float64
	Net       float64
}

func Sum(as []Attribution) Totals {
	var t Totals
	for _, a := range as {
		if a.Fills == 0 {
			continue
		}
		t.Windows++
		t.Fills += a.Fills
		t.Contracts += a.Contracts
		t.Edge += a.Edge
		t.Selection += a.Selection
		t.Net += a.Net
	}
	return t
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func boolTo(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// PendingWindow is a window the ledger knows about, for reconciliation.
type PendingWindow struct {
	MarketID string
	Label    string
	Winner   sql.NullInt64
	Voided   bool
	Settled  bool
}

// Windows lists every window we have traded, settled or not.
func (d *DB) Windows(ctx context.Context) ([]PendingWindow, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT market_id, label, winner, voided, settled_at IS NOT NULL FROM windows ORDER BY expiry ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingWindow
	for rows.Next() {
		var w PendingWindow
		var voided int
		if err := rows.Scan(&w.MarketID, &w.Label, &w.Winner, &voided, &w.Settled); err != nil {
			return nil, err
		}
		w.Voided = voided == 1
		out = append(out, w)
	}
	return out, rows.Err()
}

// OpenPosition is one market's net signed inventory, for restoring the
// engine's in-memory exposure state after a restart.
type OpenPosition struct {
	MarketID string
	Asset    string
	Signed   float64 // net Up contracts: BUY_UP fills add, BUY_DN fills subtract
}

// OpenPositions sums signed fills per market for every window that has not
// settled yet. A fresh Engine starts with empty in-memory inventory; without
// this, a restart mid-window treats real, still-open positions as flat until
// new fills happen to repopulate the map, letting the asset exposure cap pass
// trades it would otherwise have refused.
func (d *DB) OpenPositions(ctx context.Context) ([]OpenPosition, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT f.market_id, w.asset,
		       SUM(CASE WHEN f.kind = 'BUY_UP' THEN f.quantity ELSE -f.quantity END) AS signed
		FROM fills f
		JOIN windows w ON w.market_id = f.market_id
		WHERE w.settled_at IS NULL
		GROUP BY f.market_id, w.asset`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OpenPosition{}
	for rows.Next() {
		var p OpenPosition
		if err := rows.Scan(&p.MarketID, &p.Asset, &p.Signed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- live telemetry: latency & exposure -----------------------------------

// RecordLatency logs how long one submitted order took from send to mined
// receipt. Called by the engine only -- the dashboard reads this table, never
// writes it, so a stale reading is a stopped engine, not a race.
func (d *DB) RecordLatency(ctx context.Context, marketID, phase string, millis float64) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO latencies (market_id, phase, millis, created_at) VALUES (?,?,?,?)`,
		marketID, phase, millis, time.Now().Unix())
	return err
}

// LatencySample is one measured order's submit-to-receipt time.
type LatencySample struct {
	MarketID  string  `json:"marketId"`
	Phase     string  `json:"phase"`
	Millis    float64 `json:"millis"`
	CreatedAt int64   `json:"createdAt"`
}

// RecentLatencies returns the most recent latency samples, newest first, for
// the dashboard to summarise. Percentiles belong to the caller: returning raw
// samples rather than a pre-computed mean keeps the ledger from silently
// changing what "latency" means as the definition evolves.
func (d *DB) RecentLatencies(ctx context.Context, limit int) ([]LatencySample, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT market_id, phase, millis, created_at FROM latencies
		ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LatencySample{}
	for rows.Next() {
		var s LatencySample
		if err := rows.Scan(&s.MarketID, &s.Phase, &s.Millis, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpsertExposure records the engine's current aggregate position for one
// asset, across every window open on it right now, and the cap it is being
// held against. Replaced wholesale on every change rather than accumulated,
// because the dashboard needs the CURRENT number, not a history of deltas.
func (d *DB) UpsertExposure(ctx context.Context, asset string, netUp, cap float64) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO exposure (asset, net_up, cap, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(asset) DO UPDATE SET net_up=excluded.net_up, cap=excluded.cap, updated_at=excluded.updated_at`,
		asset, netUp, cap, time.Now().Unix())
	return err
}

// ExposureRow is one asset's current aggregate position, for display.
type ExposureRow struct {
	Asset     string  `json:"asset"`
	NetUp     float64 `json:"netUp"`
	Cap       float64 `json:"cap"`
	UpdatedAt int64   `json:"updatedAt"`
}

// Exposures lists every asset's current aggregate position.
func (d *DB) Exposures(ctx context.Context) ([]ExposureRow, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT asset, net_up, cap, updated_at FROM exposure ORDER BY asset`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExposureRow{}
	for rows.Next() {
		var e ExposureRow
		if err := rows.Scan(&e.Asset, &e.NetUp, &e.Cap, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- live model health -----------------------------------------------------

// HealthPoint is one settled, filled contract scored against the fair value
// it was bought at -- the live equivalent of one row in the offline backtest.
type HealthPoint struct {
	SettledAt int64   `json:"settledAt"`
	Fair      float64 `json:"fair"`  // side-adjusted fair value at fill time
	Value     float64 `json:"value"` // 1, 0, or 0.5 on a void
}

// RecentHealth returns the last `limit` settled, filled contracts in
// settlement order, for a rolling live Brier score. It is the production
// analogue of docs/AUTOPSY.md's offline backtest: the same score, computed
// continuously against what the engine has actually traded rather than once
// against history, so a drift in live calibration is visible before it costs
// as much as the 2026-09-03 loss did.
//
// Excludes fills where has_model_fair = 0: those are reconciled from a
// counterparty's transaction and carry the execution price in "fair", not a
// genuine decision-time prediction. Scoring them would measure price-vs-
// outcome calibration rather than the engine model this check exists to watch.
func (d *DB) RecentHealth(ctx context.Context, limit int) ([]HealthPoint, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT w.settled_at, w.winner, w.voided, f.kind, f.fair
		FROM fills f
		JOIN windows w ON w.market_id = f.market_id
		WHERE w.settled_at IS NOT NULL AND f.has_model_fair = 1
		ORDER BY w.settled_at DESC, f.id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HealthPoint{}
	for rows.Next() {
		var settledAt int64
		var winner sql.NullInt64
		var voided int
		var kind string
		var fair float64
		if err := rows.Scan(&settledAt, &winner, &voided, &kind, &fair); err != nil {
			return nil, err
		}
		sideFair := fair
		if kind == "BUY_DN" {
			sideFair = 1 - fair
		}
		var value float64
		switch {
		case voided == 1:
			value = 0.5
		case kind == "BUY_UP":
			value = boolTo(winner.Valid && winner.Int64 == 0)
		case kind == "BUY_DN":
			value = boolTo(winner.Valid && winner.Int64 == 1)
		}
		out = append(out, HealthPoint{SettledAt: settledAt, Fair: sideFair, Value: value})
	}
	return out, rows.Err()
}

// ---- decision trace ------------------------------------------------------

// TraceOrder is one decision we made inside a window, with the model state that
// justified it. Recording the belief alongside the action is what lets the trace
// replay reasoning rather than just prices.
type TraceOrder struct {
	At       int64   `json:"at"`
	Mode     string  `json:"mode"`
	Kind     string  `json:"kind"`
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	Fair     float64 `json:"fair"`
	Spot     float64 `json:"spot"`
	OpenPx   float64 `json:"openPx"`
	SecsLeft float64 `json:"secsLeft"`
	Rested   bool    `json:"rested"`
	Fills    int     `json:"fills"`
	TxHash   string  `json:"txHash"`
}

// TraceFill is one execution, valued at settlement.
type TraceFill struct {
	At       int64   `json:"at"`
	Kind     string  `json:"kind"`
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	Fair     float64 `json:"fair"`
	TxHash   string  `json:"txHash"`
}

// Trace is everything that happened in one window: what we believed, what we
// did about it, and what it turned out to be worth.
type Trace struct {
	MarketID     string       `json:"marketId"`
	Label        string       `json:"label"`
	Asset        string       `json:"asset"`
	IntervalSec  int64        `json:"intervalSec"`
	Expiry       int64        `json:"expiry"`
	TradingStart int64        `json:"tradingStart"`
	Winner       int          `json:"winner"`
	Voided       bool         `json:"voided"`
	Settled      bool         `json:"settled"`
	Orders       []TraceOrder `json:"orders"`
	Fills        []TraceFill  `json:"fills"`
	Attribution  *Attribution `json:"attribution"`
}

// GetTrace assembles one window's full decision history.
func (d *DB) GetTrace(ctx context.Context, marketID string) (*Trace, error) {
	t := &Trace{MarketID: marketID, Winner: -1}

	var winner sql.NullInt64
	var settled sql.NullInt64
	var voided int
	err := d.sql.QueryRowContext(ctx, `
		SELECT label, asset, interval_sec, expiry, winner, voided, settled_at
		FROM windows WHERE market_id = ?`, marketID).
		Scan(&t.Label, &t.Asset, &t.IntervalSec, &t.Expiry, &winner, &voided, &settled)
	if err != nil {
		return nil, fmt.Errorf("window %s: %w", marketID, err)
	}
	t.Voided = voided == 1
	t.Settled = settled.Valid
	if winner.Valid {
		t.Winner = int(winner.Int64)
	}
	t.TradingStart = t.Expiry - t.IntervalSec

	rows, err := d.sql.QueryContext(ctx, `
		SELECT created_at, mode, kind, price, quantity, fair, spot, open_px,
		       secs_left, rested, fills, tx_hash
		FROM orders WHERE market_id = ? ORDER BY created_at ASC`, marketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o TraceOrder
		var rested int
		if err := rows.Scan(&o.At, &o.Mode, &o.Kind, &o.Price, &o.Quantity, &o.Fair,
			&o.Spot, &o.OpenPx, &o.SecsLeft, &rested, &o.Fills, &o.TxHash); err != nil {
			return nil, err
		}
		o.Rested = rested == 1
		t.Orders = append(t.Orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	frows, err := d.sql.QueryContext(ctx, `
		SELECT created_at, kind, price, quantity, fair, tx_hash
		FROM fills WHERE market_id = ? ORDER BY created_at ASC`, marketID)
	if err != nil {
		return nil, err
	}
	defer frows.Close()
	for frows.Next() {
		var f TraceFill
		if err := frows.Scan(&f.At, &f.Kind, &f.Price, &f.Quantity, &f.Fair, &f.TxHash); err != nil {
			return nil, err
		}
		t.Fills = append(t.Fills, f)
	}
	if err := frows.Err(); err != nil {
		return nil, err
	}

	if t.Settled {
		// A failure here is a failure of the endpoint. Returning 200 with a null
		// attribution would present a database error as valid, incomplete history.
		a, err := d.AttributeMarket(ctx, marketID)
		if err != nil {
			return nil, fmt.Errorf("attribution for %s: %w", marketID, err)
		}
		t.Attribution = a
	}
	return t, nil
}

// ListTraceable returns settled windows that carry at least one fill, newest
// first — the windows worth replaying.
func (d *DB) ListTraceable(ctx context.Context, limit int) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT w.market_id FROM windows w
		WHERE w.settled_at IS NOT NULL
		  AND EXISTS (SELECT 1 FROM fills f WHERE f.market_id = w.market_id)
		ORDER BY w.expiry DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
