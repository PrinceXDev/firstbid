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
  tx_hash     TEXT NOT NULL,
  market_id   TEXT NOT NULL,
  kind        TEXT NOT NULL,
  price       REAL NOT NULL,      -- realised fill price, side's own terms
  quantity    REAL NOT NULL,
  fair        REAL NOT NULL,      -- fair value at the moment of the fill
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
`

func Open(path string) (*DB, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := d.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
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
	TxHash   string
	MarketID string
	Kind     string
	Price    float64
	Quantity float64
	Fair     float64
}

func (d *DB) RecordFill(ctx context.Context, f FillRow) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO fills (tx_hash, market_id, kind, price, quantity, fair, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		f.TxHash, f.MarketID, f.Kind, f.Price, f.Quantity, f.Fair, time.Now().Unix())
	return err
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
	MarketID  string
	Label     string
	Contracts float64
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
	rows, err := d.sql.QueryContext(ctx, `
		SELECT w.market_id, w.label, w.winner, w.voided,
		       f.kind, f.price, f.quantity, f.fair
		FROM windows w
		JOIN fills f ON f.market_id = w.market_id
		WHERE w.settled_at IS NOT NULL
		ORDER BY w.expiry ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byMarket := map[string]*Attribution{}
	var order []string

	for rows.Next() {
		var mid, label, kind string
		var winner sql.NullInt64
		var voided int
		var price, qty, fair float64
		if err := rows.Scan(&mid, &label, &winner, &voided, &kind, &price, &qty, &fair); err != nil {
			return nil, err
		}
		a, ok := byMarket[mid]
		if !ok {
			a = &Attribution{MarketID: mid, Label: label, Voided: voided == 1, Winner: -1}
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
