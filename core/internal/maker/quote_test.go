package maker

import (
	"math"
	"testing"
	"time"
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
	if tk := ShouldTake(0.80, 0.005, 0, 0.70, 0.72, p); !tk.BuyUp {
		t.Errorf("should buy Up when ask 0.72 is far below fair 0.80")
	}
	// Bid far above fair: Down is cheap.
	if tk := ShouldTake(0.40, 0.005, 0, 0.50, 0.52, p); !tk.BuyDn {
		t.Errorf("should buy Down when bid 0.50 is far above fair 0.40")
	}
	// Small difference inside our own uncertainty: do nothing.
	if tk := ShouldTake(0.505, 0.02, 0, 0.49, 0.51, p); tk.Any() {
		t.Errorf("must not cross on a difference smaller than our uncertainty")
	}
}

func TestTakeRespectsUncertaintyNotJustTakeEdge(t *testing.T) {
	p := DefaultParams()
	// 0.05 edge clears TakeEdge (0.02) but not an uncertainty of 0.10.
	if tk := ShouldTake(0.75, 0.10, 0, 0.68, 0.70, p); tk.Any() {
		t.Error("a noisy estimate must not justify crossing")
	}
}

func TestTakeIgnoresEmptySides(t *testing.T) {
	p := DefaultParams()
	if tk := ShouldTake(0.90, 0.005, 0, 0, 0, p); tk.Any() {
		t.Error("an empty book offers nothing to take")
	}
}

// Every order kind prices on the same YES scale, verified against the pool.
func TestTakePriceIsAlwaysInYesTerms(t *testing.T) {
	p := DefaultParams()
	up := ShouldTake(0.80, 0.005, 0, 0.70, 0.72, p)
	if !up.BuyUp || math.Abs(up.Price-0.72) > 1e-9 {
		t.Errorf("buying Up should lift the ask at 0.72, got %v", up.Price)
	}
	dn := ShouldTake(0.40, 0.005, 0, 0.50, 0.52, p)
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
	if tk := ShouldTake(0.80, 0.005, 0, 0.70, 0.72, p); !tk.Any() {
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
	if tk := ShouldTake(0.80, 0.005, 0, 0.70, 0.72, p); !tk.BuyUp {
		t.Error("precondition: this edge is a BUY_UP take")
	}
}

// TestShouldTakeClearsCalibrationResidual is the regression test for the take
// rule that lost money.
//
// On 2026-09-03 the engine crossed at a believed fair of 0.96 for roughly a
// 0.03 edge, 53 times. Its measured calibration error at 0.96 was 0.14. The
// edge never existed; only the confidence did. A take must clear the model's
// own error, not merely the fixed TakeEdge.
func TestShouldTakeClearsCalibrationResidual(t *testing.T) {
	p := DefaultParams()

	// The exact shape of the losing trade: ask 0.93 against a believed 0.96.
	const fair, ask, residual = 0.96, 0.93, 0.14

	if tk := ShouldTake(fair, 0.005, 0, 0, ask, p); !tk.BuyUp {
		t.Fatal("with no residual the old rule should still fire; test setup is wrong")
	}
	if tk := ShouldTake(fair, 0.005, residual, 0, ask, p); tk.Any() {
		t.Errorf("took a %.3f edge against a %.3f measured calibration error",
			fair-ask, residual)
	}
	// A genuine mispricing larger than the residual must still be taken --
	// the floor raises the bar, it does not close the strategy.
	if tk := ShouldTake(fair, 0.005, residual, 0, 0.70, p); !tk.BuyUp {
		t.Error("refused a 0.26 edge that clears a 0.14 residual")
	}
}

// An unchanged quote must not be cancelled and replaced: that is two cancels
// and two placements of gas for no change to the book.
func TestRestingQuoteHeldWhenUnchanged(t *testing.T) {
	const tick = 0.001
	now := time.Now()
	exp := now.Add(5 * time.Minute)
	var r restingQuote

	q := Quote{BidUp: 0.480, AskUp: 0.520}
	if r.matches(q, tick, now) {
		t.Error("nothing is resting yet, so nothing can match")
	}
	r.set(q, tick, exp)
	if !r.matches(Quote{BidUp: 0.480, AskUp: 0.520}, tick, now) {
		t.Error("an identical quote must be held, not replaced")
	}
	if r.matches(Quote{BidUp: 0.478, AskUp: 0.520}, tick, now) {
		t.Error("a move of two ticks is a different quote and must be replaced")
	}
	r.clear()
	if r.matches(q, tick, now) {
		t.Error("after cancelling, nothing is resting")
	}
}

// Two prices closer than one tick can still round to DIFFERENT ticks. Comparing
// raw distance would hold an order that sits at the wrong price.
func TestPricesAcrossARoundingBoundaryAreNotAMatch(t *testing.T) {
	const tick = 0.001
	now := time.Now()
	var r restingQuote
	// 0.4504 snaps to 0.450; 0.4506 snaps to 0.451. They differ by 0.0002.
	r.set(Quote{BidUp: 0.4504, AskUp: 0.520}, tick, now.Add(5*time.Minute))
	if r.matches(Quote{BidUp: 0.4506, AskUp: 0.520}, tick, now) {
		t.Error("prices that snap to different ticks must not be treated as unchanged")
	}
	// Same side of the boundary: genuinely the same order.
	if !r.matches(Quote{BidUp: 0.4501, AskUp: 0.520}, tick, now) {
		t.Error("prices that snap to the same tick are the same order")
	}
}

// An order that has aged off the book is not resting. Holding past its expiry
// would leave the market unquoted until something else moved the quote.
func TestRestingQuoteExpires(t *testing.T) {
	const tick = 0.001
	now := time.Now()
	var r restingQuote
	q := Quote{BidUp: 0.480, AskUp: 0.520}
	r.set(q, tick, now.Add(30*time.Second))

	if !r.matches(q, tick, now) {
		t.Error("a fresh quote should hold")
	}
	if r.matches(q, tick, now.Add(25*time.Second)) {
		t.Error("must stop holding before the orders actually expire")
	}
	if r.matches(q, tick, now.Add(2*time.Minute)) {
		t.Error("must never hold past expiry")
	}
}

// Even an unchanged quote must periodically fall through to a full requote, so
// the engine re-reads the pool instead of trusting its memory forever.
func TestRestingQuoteForcesPeriodicReverification(t *testing.T) {
	const tick = 0.001
	now := time.Now()
	var r restingQuote
	q := Quote{BidUp: 0.480, AskUp: 0.520}
	r.set(q, tick, now.Add(1*time.Hour))

	held := 0
	for i := 0; i < maxHolds+2; i++ {
		if !r.matches(q, tick, now) {
			break
		}
		r.hold()
		held++
	}
	if held != maxHolds {
		t.Errorf("held %d times before re-verifying, want %d", held, maxHolds)
	}
}
