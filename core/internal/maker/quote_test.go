package maker

import (
	"math"
	"testing"
)

func TestQuoteStraddlesFairValue(t *testing.T) {
	p := DefaultParams()
	q := Compute(0.62, 0.01, 0, 600, 1, p)
	if q.SkipBid || q.SkipAsk {
		t.Fatalf("unexpected skip: %s", q.Reason)
	}
	if !(q.BidUp < q.Fair && q.Fair < q.AskUp) {
		t.Errorf("quote %v/%v does not straddle fair %v", q.BidUp, q.AskUp, q.Fair)
	}
}

// The whole thesis: we must quote inside the ~0.024 ladder-bot spread.
func TestQuotesInsideTheIncumbentSpread(t *testing.T) {
	p := DefaultParams()
	q := Compute(0.50, 0.003, 0, 600, 1, p)
	const incumbent = 0.024
	if q.Wide() >= incumbent {
		t.Errorf("quoted %.4f wide, need tighter than the incumbent %.3f", q.Wide(), incumbent)
	}
}

func TestRefusesStaleSpotAndClosingWindow(t *testing.T) {
	p := DefaultParams()
	if q := Compute(0.5, 0.01, 0, 5, 1, p); !q.SkipBid || !q.SkipAsk {
		t.Error("must not quote a window about to lock")
	}
	if q := Compute(0.5, 0.01, 0, 600, 30, p); !q.SkipBid || !q.SkipAsk {
		t.Error("must not quote on a stale index price")
	}
}

func TestInventorySkewPushesQuotesToFlatten(t *testing.T) {
	p := DefaultParams()
	flat := Compute(0.50, 0.005, 0, 600, 1, p)
	long := Compute(0.50, 0.005, 20, 600, 1, p)
	if !(long.BidUp < flat.BidUp && long.AskUp < flat.AskUp) {
		t.Errorf("a long position must lower both quotes: flat %v/%v long %v/%v",
			flat.BidUp, flat.AskUp, long.BidUp, long.AskUp)
	}
}

func TestInventoryCapsStopDeepeningPosition(t *testing.T) {
	p := DefaultParams()
	if q := Compute(0.5, 0.005, p.MaxInventory, 600, 1, p); !q.SkipBid {
		t.Error("at the long cap we must stop bidding")
	}
	if q := Compute(0.5, 0.005, -p.MaxInventory, 600, 1, p); !q.SkipAsk {
		t.Error("at the short cap we must stop offering")
	}
}

func TestQuotesStayWithinProbabilityBounds(t *testing.T) {
	p := DefaultParams()
	for _, fair := range []float64{0.001, 0.02, 0.5, 0.98, 0.999} {
		q := Compute(fair, 0.05, 0, 600, 1, p)
		if q.BidUp < 0 || q.AskUp > 1 || math.IsNaN(q.BidUp) || math.IsNaN(q.AskUp) {
			t.Errorf("fair %v produced out-of-range quote %v/%v", fair, q.BidUp, q.AskUp)
		}
	}
}

func TestTakesOnlyWhenBookIsClearlyWrong(t *testing.T) {
	p := DefaultParams()
	// Ask far below fair: buy Up.
	if tk := ShouldTake(0.80, 0.005, 0.70, 0.72, p); !tk.BuyUp {
		t.Errorf("should buy Up when ask 0.72 is far below fair 0.80")
	}
	// Bid far above fair: Down is cheap.
	if tk := ShouldTake(0.40, 0.005, 0.50, 0.52, p); !tk.BuyDn {
		t.Errorf("should buy Down when bid 0.50 is far above fair 0.40")
	}
	// Small difference inside our own uncertainty: do nothing.
	if tk := ShouldTake(0.505, 0.02, 0.49, 0.51, p); tk.Any() {
		t.Errorf("must not cross on a difference smaller than our uncertainty")
	}
}

func TestTakeRespectsUncertaintyNotJustTakeEdge(t *testing.T) {
	p := DefaultParams()
	// 0.05 edge clears TakeEdge (0.02) but not an uncertainty of 0.10.
	if tk := ShouldTake(0.75, 0.10, 0.68, 0.70, p); tk.Any() {
		t.Error("a noisy estimate must not justify crossing")
	}
}

func TestTakeIgnoresEmptySides(t *testing.T) {
	p := DefaultParams()
	if tk := ShouldTake(0.90, 0.005, 0, 0, p); tk.Any() {
		t.Error("an empty book offers nothing to take")
	}
}

// Every order kind prices on the same YES scale, verified against the pool.
func TestTakePriceIsAlwaysInYesTerms(t *testing.T) {
	p := DefaultParams()
	up := ShouldTake(0.80, 0.005, 0.70, 0.72, p)
	if !up.BuyUp || math.Abs(up.Price-0.72) > 1e-9 {
		t.Errorf("buying Up should lift the ask at 0.72, got %v", up.Price)
	}
	dn := ShouldTake(0.40, 0.005, 0.50, 0.52, p)
	if !dn.BuyDn || math.Abs(dn.Price-0.50) > 1e-9 {
		t.Errorf("buying Down should hit the bid at 0.50 in YES terms, got %v", dn.Price)
	}
}

// Taking is a WRITE, so it must answer to the same pre-signing gates as a
// quote. These assert the conditions the engine now checks before crossing.
func TestGatesThatMustAlsoBlockATake(t *testing.T) {
	p := DefaultParams()

	// A large edge exists, but the index price is stale.
	stale := Compute(0.80, 0.005, 0, 600, 30, p)
	if !stale.SkipBid || !stale.SkipAsk {
		t.Fatal("stale spot must skip both sides")
	}
	if tk := ShouldTake(0.80, 0.005, 0.70, 0.72, p); !tk.Any() {
		t.Fatal("precondition: an edge should exist here")
	}
	// The engine consults both, so a stale feed blocks the take.

	// A large edge exists, but the window is about to lock.
	closing := Compute(0.80, 0.005, 0, 5, 1, p)
	if !closing.SkipBid || !closing.SkipAsk {
		t.Error("an imminent lock must skip both sides")
	}

	// At the long cap, the bid side is skipped, which must also stop a BUY_UP take.
	capped := Compute(0.80, 0.005, p.MaxInventory, 600, 1, p)
	if !capped.SkipBid {
		t.Error("the long inventory cap must skip the bid side")
	}
	if tk := ShouldTake(0.80, 0.005, 0.70, 0.72, p); !tk.BuyUp {
		t.Error("precondition: this edge is a BUY_UP take")
	}
}
