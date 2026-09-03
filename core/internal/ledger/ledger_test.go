package ledger

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// A matched Up/Down pair always redeems to exactly 1.00, so a maker who bought
// both sides for less than 1.00 profits regardless of which side wins. This is
// the whole thesis, so it is the first thing the ledger must prove.
func TestMatchedPairProfitsWhicheverSideWins(t *testing.T) {
	for _, winner := range []int{0, 1} {
		d := open(t)
		ctx := context.Background()
		if err := d.UpsertWindow(ctx, WindowRow{MarketID: "m1", Label: "BTC/15m", Asset: "BTC", IntervalSec: 900, Expiry: 1}); err != nil {
			t.Fatal(err)
		}
		// Bought Up at 0.48 and Down at 0.50: 0.98 total for a pair worth 1.00.
		mustFill(t, d, FillRow{TxHash: "a", MarketID: "m1", Kind: "BUY_UP", Price: 0.48, Quantity: 10, Fair: 0.50})
		mustFill(t, d, FillRow{TxHash: "b", MarketID: "m1", Kind: "BUY_DN", Price: 0.50, Quantity: 10, Fair: 0.50})
		if err := d.Settle(ctx, "m1", winner, false, 100, 101); err != nil {
			t.Fatal(err)
		}

		as, err := d.Attribute(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(as) != 1 {
			t.Fatalf("want 1 attribution, got %d", len(as))
		}
		got := as[0]
		if math.Abs(got.Net-0.2) > 1e-9 {
			t.Errorf("winner=%d: net = %v, want 0.2 (10 pairs x 0.02)", winner, got.Net)
		}
		if math.Abs(got.Cost-9.8) > 1e-9 {
			t.Errorf("cost = %v, want 9.8", got.Cost)
		}
		if math.Abs(got.Payout-10) > 1e-9 {
			t.Errorf("payout = %v, want 10", got.Payout)
		}
	}
}

// Edge is what we believed we captured; Selection is how reality differed.
// They must always reconstruct Net exactly, or the attribution is lying.
func TestEdgePlusSelectionEqualsNet(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	_ = d.UpsertWindow(ctx, WindowRow{MarketID: "m2", Label: "ETH/15m", Asset: "ETH", IntervalSec: 900, Expiry: 1})
	mustFill(t, d, FillRow{TxHash: "a", MarketID: "m2", Kind: "BUY_UP", Price: 0.40, Quantity: 7, Fair: 0.55})
	mustFill(t, d, FillRow{TxHash: "b", MarketID: "m2", Kind: "BUY_DN", Price: 0.30, Quantity: 3, Fair: 0.55})
	_ = d.Settle(ctx, "m2", 0, false, 1, 2)

	as, _ := d.Attribute(ctx)
	a := as[0]
	if math.Abs((a.Edge+a.Selection)-a.Net) > 1e-9 {
		t.Errorf("edge %v + selection %v = %v, want net %v", a.Edge, a.Selection, a.Edge+a.Selection, a.Net)
	}
}

// A void refunds 0.5 per contract on both sides, so a maker who paid less than
// 0.5 a side still profits and one who overpaid still loses.
func TestVoidPaysHalfToBothSides(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	_ = d.UpsertWindow(ctx, WindowRow{MarketID: "m3", Label: "BTC/5m", Asset: "BTC", IntervalSec: 300, Expiry: 1})
	mustFill(t, d, FillRow{TxHash: "a", MarketID: "m3", Kind: "BUY_UP", Price: 0.45, Quantity: 4, Fair: 0.5})
	mustFill(t, d, FillRow{TxHash: "b", MarketID: "m3", Kind: "BUY_DN", Price: 0.45, Quantity: 4, Fair: 0.5})
	_ = d.Settle(ctx, "m3", -1, true, 1, 1)

	as, _ := d.Attribute(ctx)
	a := as[0]
	if math.Abs(a.Payout-4) > 1e-9 { // 8 contracts x 0.5
		t.Errorf("void payout = %v, want 4", a.Payout)
	}
	if math.Abs(a.Net-0.4) > 1e-9 { // paid 3.6, got 4
		t.Errorf("void net = %v, want 0.4", a.Net)
	}
}

// Losing a side must actually cost money, or the ledger flatters us.
func TestLosingSideIsWorthZero(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	_ = d.UpsertWindow(ctx, WindowRow{MarketID: "m4", Label: "BTC/15m", Asset: "BTC", IntervalSec: 900, Expiry: 1})
	mustFill(t, d, FillRow{TxHash: "a", MarketID: "m4", Kind: "BUY_UP", Price: 0.60, Quantity: 5, Fair: 0.60})
	_ = d.Settle(ctx, "m4", 1, false, 1, 0) // Down won: our Up is worthless

	as, _ := d.Attribute(ctx)
	a := as[0]
	if a.Payout != 0 {
		t.Errorf("payout = %v, want 0", a.Payout)
	}
	if math.Abs(a.Net+3) > 1e-9 {
		t.Errorf("net = %v, want -3", a.Net)
	}
	// We captured no edge (bought exactly at fair); the loss is all selection.
	if math.Abs(a.Edge) > 1e-9 {
		t.Errorf("edge = %v, want 0 when buying exactly at fair value", a.Edge)
	}
}

func TestUnsettledWindowsAreExcluded(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	_ = d.UpsertWindow(ctx, WindowRow{MarketID: "m5", Label: "BTC/15m", Asset: "BTC", IntervalSec: 900, Expiry: 1})
	mustFill(t, d, FillRow{TxHash: "a", MarketID: "m5", Kind: "BUY_UP", Price: 0.5, Quantity: 1, Fair: 0.5})
	as, _ := d.Attribute(ctx)
	if len(as) != 0 {
		t.Errorf("unsettled window should not appear in realised P&L, got %d", len(as))
	}
}

func mustFill(t *testing.T, d *DB, f FillRow) {
	t.Helper()
	if err := d.RecordFill(context.Background(), f); err != nil {
		t.Fatal(err)
	}
}

// A Down contract bought for 0.969 when the model says Down is worth 0.998 has
// captured 0.029 of edge, not 0.967. Prices reaching the ledger must be the
// cost actually paid on that side, never the YES-terms wire price.
func TestDownSideCostsAreRecordedInDownTerms(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	_ = d.UpsertWindow(ctx, WindowRow{MarketID: "m6", Label: "ETH/60m", Asset: "ETH", IntervalSec: 3600, Expiry: 1})
	// fair (Up) = 0.002, so Down is worth 0.998. We paid 0.969 for Down.
	mustFill(t, d, FillRow{TxHash: "a", MarketID: "m6", Kind: "BUY_DN", Price: 0.969, Quantity: 2, Fair: 0.002})
	_ = d.Settle(ctx, "m6", 1, false, 1, 0) // Down won

	as, _ := d.Attribute(ctx)
	a := as[0]
	if math.Abs(a.Edge-0.058) > 1e-9 { // (0.998 - 0.969) * 2
		t.Errorf("edge = %v, want 0.058", a.Edge)
	}
	if math.Abs(a.Net-0.062) > 1e-9 { // payout 2.0 - cost 1.938
		t.Errorf("net = %v, want 0.062", a.Net)
	}
}
