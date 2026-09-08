package maker

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"

	"github.com/firstbid/core/internal/ledger"
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
