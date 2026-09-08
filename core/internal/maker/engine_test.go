package maker

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"

	"github.com/firstbid/core/internal/ledger"
	"github.com/firstbid/core/internal/venue"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	return New(nil, nil, DefaultParams(), "", log.New(io.Discard, "", 0))
}

// TestAssetExposureSumsAcrossMarkets checks that exposure is read live across
// every market recorded against one asset, not tracked as its own separate
// running total -- the design that closed the bug where a separately
// incremented total could drift from the per-market state it was meant to
// summarise.
func TestAssetExposureSumsAcrossMarkets(t *testing.T) {
	e := testEngine(t)
	e.mu.Lock()
	e.inv["btc-60m"] = 10
	e.marketAsset["btc-60m"] = "BTC"
	e.inv["btc-240m"] = 20
	e.marketAsset["btc-240m"] = "BTC"
	e.inv["eth-60m"] = 5
	e.marketAsset["eth-60m"] = "ETH"
	e.mu.Unlock()

	if got := e.assetExposure("BTC"); got != 30 {
		t.Errorf("assetExposure(BTC) = %v, want 30 (sum of both BTC windows)", got)
	}
	if got := e.assetExposure("ETH"); got != 5 {
		t.Errorf("assetExposure(ETH) = %v, want 5", got)
	}
	if got := e.assetExposure("SOL"); got != 0 {
		t.Errorf("assetExposure(SOL) = %v, want 0 for an asset with no recorded position", got)
	}
}

// TestReapForgetsSettledWindowsExposure is the regression test for the bug
// where a settled or expired window's contribution stayed counted in
// aggregate exposure forever, falsely refusing later, unrelated trades on the
// same asset at a cap that no longer reflected real risk.
func TestReapForgetsSettledWindowsExposure(t *testing.T) {
	e := testEngine(t)
	e.mu.Lock()
	e.inv["btc-60m"] = 40
	e.marketAsset["btc-60m"] = "BTC"
	e.inv["btc-240m"] = 20
	e.marketAsset["btc-240m"] = "BTC"
	e.mu.Unlock()

	if got := e.assetExposure("BTC"); got != 60 {
		t.Fatalf("setup: assetExposure(BTC) = %v, want 60", got)
	}

	// btc-60m expires and is reaped. Its 40 contracts must stop counting
	// against BTC's aggregate exposure immediately, not linger.
	e.reap("btc-60m")

	if got := e.assetExposure("BTC"); got != 20 {
		t.Errorf("after reap, assetExposure(BTC) = %v, want 20 (only btc-240m remains live)", got)
	}
	e.mu.Lock()
	_, stillTracked := e.marketAsset["btc-60m"]
	e.mu.Unlock()
	if stillTracked {
		t.Error("reap left btc-60m in marketAsset; it will keep contributing to every future sum")
	}
}

// TestRestingOrdersAcrossWindowsAreCapped is the regression test for the bug
// where the asset exposure cap only ever looked at REALISED exposure: a
// resting post-only order does not realise anything the moment it is placed,
// so several resting orders on the same asset across different windows could
// each individually pass a realised-only check while none had filled yet,
// and then all fill later -- often discovered asynchronously by reconcile(),
// with no guarded moment left to refuse anything -- and together breach the
// cap. This replays that exact shape: BTC/60m's resting Up order, then
// BTC/240m trying to rest another Up order on top of it.
func TestRestingOrdersAcrossWindowsAreCapped(t *testing.T) {
	e := testEngine(t)
	e.Params.MaxAssetExposure = 60
	e.mu.Lock()
	e.marketAsset["btc-60m"] = "BTC"
	e.marketAsset["btc-240m"] = "BTC"
	e.mu.Unlock()

	// BTC/60m's Up order rests. Nothing has filled -- realised exposure is
	// still 0 -- but the reservation now exists.
	e.setResting("btc-60m", venue.BuyYes, 40)

	// BTC/240m now tries to rest 40 MORE on the same side. A check against
	// realised exposure alone (still 0) would wrongly allow this: 0+40=40 is
	// under the cap. The worst-case check must see BTC/60m's already-resting
	// 40 too.
	baseline := e.exposureBaseline("BTC", "make", 40)
	if ok, prospective := ExposureAllows(baseline, 40, e.Params.MaxAssetExposure); ok {
		t.Fatalf("second window's resting 40 allowed; aggregate would reach %.0f if both filled, cap is %.0f",
			prospective, e.Params.MaxAssetExposure)
	}

	// A smaller order that keeps the worst case within the cap must still be
	// allowed -- the fix must not simply refuse everything.
	baseline = e.exposureBaseline("BTC", "make", 15)
	if ok, _ := ExposureAllows(baseline, 15, e.Params.MaxAssetExposure); !ok {
		t.Errorf("40 (resting) + 15 (new) = 55 refused, want allowed under cap=%.0f", e.Params.MaxAssetExposure)
	}

	// Once BTC/60m's order is discovered filled (as reconcile() would do:
	// realise it, then release the reservation), its 40 contracts are still
	// 40 of the same 60-contract cap -- moving from "resting" to "realised"
	// must not make room appear that was never there. A genuinely new 40 on
	// BTC/240m on top of that would total 80 and must still be refused.
	e.addInventory("btc-60m", 40)
	e.setResting("btc-60m", venue.BuyYes, 0)
	baseline = e.exposureBaseline("BTC", "make", 40)
	if ok, prospective := ExposureAllows(baseline, 40, e.Params.MaxAssetExposure); ok {
		t.Errorf("BTC/240m's 40 allowed on top of BTC/60m's now-realised 40 (prospective %.0f); "+
			"80 exceeds the cap of %.0f regardless of resting vs realised", prospective, e.Params.MaxAssetExposure)
	}

	// But the reservation must genuinely be gone, not double-counted forever:
	// a smaller order that fits alongside the realised 40 must be allowed.
	baseline = e.exposureBaseline("BTC", "make", 20)
	if ok, prospective := ExposureAllows(baseline, 20, e.Params.MaxAssetExposure); !ok {
		t.Errorf("BTC/240m's 20 refused after BTC/60m's fill (prospective %.0f, cap %.0f); "+
			"the released resting reservation must not still be counted", prospective, e.Params.MaxAssetExposure)
	}
}

// TestClearRestingReleasesReservation checks that cancelling a resting order
// (or its window ending) frees the exposure it was reserving, so it does not
// keep blocking unrelated trades on the same asset forever.
func TestClearRestingReleasesReservation(t *testing.T) {
	e := testEngine(t)
	e.mu.Lock()
	e.marketAsset["btc-60m"] = "BTC"
	e.mu.Unlock()

	e.setResting("btc-60m", venue.BuyYes, 40)
	if got := e.assetRestingLong("BTC"); got != 40 {
		t.Fatalf("assetRestingLong(BTC) = %v, want 40", got)
	}

	e.clearResting("btc-60m")
	if got := e.assetRestingLong("BTC"); got != 0 {
		t.Errorf("assetRestingLong(BTC) = %v after clearResting, want 0", got)
	}
}

// TestRestoreInventoryRebuildsFromLedger is the regression test for a
// restarted engine treating real, still-open positions as flat until new
// fills happened to repopulate an in-memory map that starts empty on every
// process start.
func TestRestoreInventoryRebuildsFromLedger(t *testing.T) {
	db, err := ledger.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	if err := db.UpsertWindow(ctx, ledger.WindowRow{
		MarketID: "btc-240m", Label: "BTC/240m", Asset: "BTC", IntervalSec: 14400, Expiry: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordFill(ctx, ledger.FillRow{
		Key: "test:1", TxHash: "a", MarketID: "btc-240m", Kind: "BUY_UP", Price: 0.5, Quantity: 35, Fair: 0.5,
		HasModelFair: true,
	}); err != nil {
		t.Fatal(err)
	}

	e := testEngine(t)
	e.Ledger = db

	// Before restoring, a fresh engine has no idea this position exists --
	// exactly the state a real restart would be in without this fix.
	if got := e.assetExposure("BTC"); got != 0 {
		t.Fatalf("setup: assetExposure(BTC) = %v before restore, want 0", got)
	}

	e.restoreInventory(ctx)

	if got := e.assetExposure("BTC"); got != 35 {
		t.Errorf("after restoreInventory, assetExposure(BTC) = %v, want 35 (the still-open ledger position)", got)
	}

	// The restored number must also reach the dashboard's persisted view --
	// restoring only in-memory state would leave the two disagreeing again.
	rows, err := db.Exposures(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Asset != "BTC" || rows[0].NetUp != 35 {
		t.Errorf("db.Exposures() = %+v, want one BTC row at 35", rows)
	}
}
