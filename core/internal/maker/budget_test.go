package maker

import (
	"testing"
	"time"
)

func testParams() Params {
	p := DefaultParams()
	p.Size = 2
	p.MaxTakesPerMarket = 2
	p.MaxTakeNotional = 15
	p.TakeCooldown = 45 * time.Second
	return p
}

// TestTakeBudgetStopsTheEightFillCascade replays the shape of the 2026-09-03
// loss: one belief, held for four minutes, acted on every time the loop ticked.
// The old engine sent eight orders. The budget must stop after two.
func TestTakeBudgetStopsTheEightFillCascade(t *testing.T) {
	b := NewTakeBudget(testParams())
	t0 := time.Unix(1788414000, 0)

	const price, size = 0.94, 2.0
	var sent int
	// Eight loop ticks across four minutes, the book mispriced throughout.
	for i := 0; i < 8; i++ {
		now := t0.Add(time.Duration(i) * 30 * time.Second)
		if ok, _ := b.Allow(now, price, size); ok {
			b.Record(now, price, size)
			sent++
		}
	}
	if sent != 2 {
		t.Errorf("sent %d takes across the window, want 2 (was 8 before the budget)", sent)
	}
	takes, notional := b.Spent()
	if takes != 2 {
		t.Errorf("Spent() takes = %d, want 2", takes)
	}
	if want := 2 * price * size; notional != want {
		t.Errorf("Spent() notional = %.2f, want %.2f", notional, want)
	}
}

// TestTakeBudgetCooldown checks that a book which stays wrong for one tick
// cannot be crossed on the very next tick.
func TestTakeBudgetCooldown(t *testing.T) {
	b := NewTakeBudget(testParams())
	t0 := time.Unix(1788414000, 0)

	if ok, why := b.Allow(t0, 0.5, 2); !ok {
		t.Fatalf("first take refused: %s", why)
	}
	b.Record(t0, 0.5, 2)

	if ok, why := b.Allow(t0.Add(20*time.Second), 0.5, 2); ok {
		t.Error("take allowed 20s after the last one; cooldown is 45s")
	} else if why == "" {
		t.Error("refusal carried no reason; the log needs one")
	}
	if ok, why := b.Allow(t0.Add(46*time.Second), 0.5, 2); !ok {
		t.Errorf("take refused 46s after the last one: %s", why)
	}
}

// TestTakeBudgetNotionalCap checks the collateral bound binds independently of
// the take count: two cheap takes are fine, two expensive ones are not.
func TestTakeBudgetNotionalCap(t *testing.T) {
	p := testParams()
	p.MaxTakesPerMarket = 5
	p.TakeCooldown = 0
	b := NewTakeBudget(p)
	t0 := time.Unix(1788414000, 0)

	// 0.95 x 8 = 7.60 per take; the third would exceed 15.
	var sent int
	for i := 0; i < 5; i++ {
		now := t0.Add(time.Duration(i) * time.Minute)
		if ok, _ := b.Allow(now, 0.95, 8); ok {
			b.Record(now, 0.95, 8)
			sent++
		}
	}
	if sent != 1 {
		t.Errorf("sent %d takes, want 1 — 2x7.60 already exceeds the 15 cap", sent)
	}
}

// TestTakeBudgetRejectedIntentCostsNothing guards the Allow/Record split: a
// priced take that never reached the chain must not consume the window's risk.
func TestTakeBudgetRejectedIntentCostsNothing(t *testing.T) {
	b := NewTakeBudget(testParams())
	t0 := time.Unix(1788414000, 0)

	for i := 0; i < 3; i++ { // three priced intents, none of them sent
		if ok, _ := b.Allow(t0, 0.5, 2); !ok {
			t.Fatal("Allow refused before anything was recorded")
		}
	}
	if takes, notional := b.Spent(); takes != 0 || notional != 0 {
		t.Errorf("Spent() = (%d, %.2f), want (0, 0.00)", takes, notional)
	}
}

// TestTakeBudgetNilIsPermissive keeps the zero value usable, so a caller that
// has not opted in behaves exactly as before rather than silently refusing
// every order.
func TestTakeBudgetNilIsPermissive(t *testing.T) {
	var b *TakeBudget
	if ok, _ := b.Allow(time.Now(), 0.5, 2); !ok {
		t.Error("nil budget refused a take; it must be permissive")
	}
	b.Record(time.Now(), 0.5, 2) // must not panic
	if takes, notional := b.Spent(); takes != 0 || notional != 0 {
		t.Errorf("nil Spent() = (%d, %.2f), want (0, 0.00)", takes, notional)
	}
}

// TestDefaultParamsBoundTakeRisk asserts the shipped defaults are actually
// bounded. A zero here would silently restore the old behaviour.
func TestDefaultParamsBoundTakeRisk(t *testing.T) {
	p := DefaultParams()
	if p.MaxTakesPerMarket <= 0 {
		t.Error("MaxTakesPerMarket is unbounded by default")
	}
	if p.MaxTakeNotional <= 0 {
		t.Error("MaxTakeNotional is unbounded by default")
	}
	if p.TakeCooldown <= 0 {
		t.Error("TakeCooldown is zero by default")
	}
	if p.MaxAssetExposure <= 0 {
		t.Error("MaxAssetExposure is unbounded by default")
	}
	if worst := float64(p.MaxTakesPerMarket) * p.Size; worst > p.MaxTakeNotional {
		t.Logf("note: take count allows %.0f contracts, notional cap binds first at %.2f",
			worst, p.MaxTakeNotional)
	}
}

// TestExposureAllowsBlocksTwoWindowsAgreeing replays the shape docs/COVERAGE.md
// warns about: BTC/60m and BTC/240m are both live simultaneously, and nothing
// stops them making the same correlated bet independently, each within its
// own per-window MaxInventory. The cross-window cap must catch what the
// per-window one cannot see.
func TestExposureAllowsBlocksTwoWindowsAgreeing(t *testing.T) {
	const cap = 60.0
	var asset float64 // aggregate BTC exposure across both windows

	// BTC/60m takes Up up to its own 50-contract window cap; each individual
	// take is comfortably inside that, and inside the 60 asset cap too.
	ok, after := ExposureAllows(asset, 40, cap)
	if !ok {
		t.Fatalf("first window's 40 refused, want allowed (well under cap=%.0f)", cap)
	}
	asset = after

	// BTC/240m now tries to add 40 MORE of the same side. Per-window inventory
	// caps have no opinion about this — it is a different market — but the
	// asset is already at 40, and 40+40=80 exceeds 60.
	ok, after = ExposureAllows(asset, 40, cap)
	if ok {
		t.Fatalf("second window's agreeing 40 allowed; aggregate would reach %.0f > cap %.0f", after, cap)
	}
	if after != 80 {
		t.Errorf("prospective = %.0f, want 80 (the number a refusal log should show)", after)
	}

	// A SMALLER add, or one on the opposite side, must still be allowed —
	// the cap bounds the aggregate, not activity on the asset at all.
	if ok, after := ExposureAllows(asset, 15, cap); !ok {
		t.Errorf("40+15=55 refused, want allowed under cap=%.0f (got prospective %.0f)", cap, after)
	}
	if ok, after := ExposureAllows(asset, -40, cap); !ok {
		t.Errorf("an offsetting take (Down) refused; it flattens exposure to %.0f, should never be blocked", after)
	}
}

// TestExposureAllowsCapDisabledAtZero keeps a zero cap permissive rather than
// refusing every take, matching TakeBudget's nil-is-permissive convention.
func TestExposureAllowsCapDisabledAtZero(t *testing.T) {
	if ok, _ := ExposureAllows(1000, 500, 0); !ok {
		t.Error("cap=0 must disable the check, not refuse everything")
	}
}
