// Package maker turns a calibrated fair value into two-sided quotes.
//
// The venue's outcome is empirically a fair coin (P(Up)=0.4978 over 10,209
// windows), so no directional view is taken. The edge is the spread, and a
// matched Up/Down pair always redeems to exactly 1.00 regardless of who wins.
package maker

import (
	"math"
	"time"
)

// Params bound how aggressive the quoter is allowed to be.
type Params struct {
	// MinHalfSpread is the floor on each side, in probability units.
	MinHalfSpread float64
	// MaxHalfSpread caps how wide we go when uncertainty spikes.
	MaxHalfSpread float64
	// UncertaintyMult scales model uncertainty into the quoted half-spread.
	UncertaintyMult float64
	// InventorySkew shifts quotes to shed a lopsided position, per contract held.
	InventorySkew float64
	// MaxInventory stops quoting the side that would deepen an oversized position.
	MaxInventory float64
	// MinSecondsLeft refuses to quote a window about to lock.
	MinSecondsLeft float64
	// MaxSpotAgeSec refuses to quote on a stale index price.
	MaxSpotAgeSec float64
	// CertaintyBound refuses to quote at the very edges, where a quote would be
	// degenerate rather than useful.
	CertaintyBound float64
	// TakeEdge is how far the book must be from fair value before we cross it.
	// Set above MinHalfSpread so we only take prices that are wrong by more than
	// our own uncertainty, never merely different.
	TakeEdge float64
	// Size is the per-side order size in contracts.
	Size float64

	// MaxTakesPerMarket caps how many times one window may be crossed. Takes
	// inside a window are perfectly correlated -- they settle against the same
	// outcome -- so N takes on one belief is one bet at N times the size.
	MaxTakesPerMarket int
	// MaxTakeNotional caps the collateral committed to taking in one window.
	MaxTakeNotional float64
	// TakeCooldown is the minimum gap between takes in the same window, so a
	// book that stays mispriced for a minute cannot be crossed every tick.
	TakeCooldown time.Duration
}

func DefaultParams() Params {
	return Params{
		MinHalfSpread:   0.004, // 0.8% round trip — a third of the ladder bots' 2.4%
		MaxHalfSpread:   0.060,
		UncertaintyMult: 1.15, // cover model uncertainty plus a margin
		InventorySkew:   0.0015,
		MaxInventory:    50,
		MinSecondsLeft:  20,
		MaxSpotAgeSec:   10,
		CertaintyBound:  0.005,
		TakeEdge:        0.020,
		Size:            5,

		// Sized from the 2026-09-03 post-mortem: the loss was not one bad take,
		// it was the same take eight times. Two crossings per window, at most
		// 15 collateral, no faster than one per 45s.
		MaxTakesPerMarket: 2,
		MaxTakeNotional:   15,
		TakeCooldown:      45 * time.Second,
	}
}

// Quote is a two-sided intent, expressed in YES terms.
type Quote struct {
	Fair    float64
	BidUp   float64 // buy Up at this probability
	AskUp   float64 // sell Up at this probability
	Half    float64
	SkipBid bool
	SkipAsk bool
	Reason  string
}

// Wide reports the round-trip spread we are quoting.
func (q Quote) Wide() float64 { return q.AskUp - q.BidUp }

// Compute derives a two-sided quote around fair value.
//
// The half-spread is driven by model uncertainty over the next requote interval,
// not by a fixed constant: near expiry the outcome is nearly determined and we
// can quote tight, while mid-window we must stand back. That is precisely what a
// static ladder cannot do.
func Compute(fair, uncertainty, inventory, secondsLeft, spotAge float64, p Params) Quote {
	q := Quote{Fair: fair}

	if secondsLeft < p.MinSecondsLeft {
		q.SkipBid, q.SkipAsk = true, true
		q.Reason = "window about to lock"
		return q
	}
	if spotAge > p.MaxSpotAgeSec {
		q.SkipBid, q.SkipAsk = true, true
		q.Reason = "stale index price"
		return q
	}

	if p.CertaintyBound > 0 && (fair < p.CertaintyBound || fair > 1-p.CertaintyBound) {
		q.SkipBid, q.SkipAsk = true, true
		q.Reason = "outcome effectively decided"
		return q
	}

	half := math.Max(p.MinHalfSpread, uncertainty*p.UncertaintyMult)
	half = math.Min(half, p.MaxHalfSpread)
	q.Half = half

	// Shift the whole quote against our inventory so fills flatten us.
	skew := inventory * p.InventorySkew
	mid := clamp(fair-skew, 0.001, 0.999)

	q.BidUp = clamp(mid-half, 0.001, 0.999)
	q.AskUp = clamp(mid+half, 0.001, 0.999)

	// Never deepen an oversized position.
	if inventory >= p.MaxInventory {
		q.SkipBid = true
		q.Reason = "long inventory cap"
	}
	if inventory <= -p.MaxInventory {
		q.SkipAsk = true
		q.Reason = "short inventory cap"
	}
	// A degenerate crossed or zero-width quote is never sent.
	if q.AskUp-q.BidUp < 2*p.MinHalfSpread*0.5 {
		q.SkipBid, q.SkipAsk = true, true
		q.Reason = "degenerate spread"
	}
	return q
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// Take is a decision to cross the book because it is mispriced against our
// calibrated fair value, rather than to rest behind it.
//
// On this venue there is no naked short, so a view is always expressed as a BUY:
// "Up is cheap" buys Up, and "Up is rich" buys Down at 1-price. Both are ordinary
// purchases of a fully collateralised outcome token.
type Take struct {
	BuyUp bool
	BuyDn bool
	// Price is the limit in YES terms. Verified empirically against the pool:
	// a BUY_NO at price P rests as an ASK at P in Up terms, so every one of the
	// four order kinds takes its price on the same YES scale. The docs' phrasing
	// ("a NO price is ONE - ticks(p)") reads the other way round.
	Price float64
	Edge  float64
	Why   string
}

// ShouldTake compares fair value against the live touch.
//
// bestBid/bestAsk are in Up terms; pass 0 for an empty side. `fair` must be the
// CALIBRATED probability and `residual` the calibration map's own error at that
// probability.
//
// The edge must clear three floors: a fixed minimum, the model's forward-looking
// uncertainty, and its measured calibration residual. The third one is the fix
// for the 2026-09-03 loss. The engine crossed at a believed 0.96 for a 0.03
// edge while its measured error at 0.96 was 0.14 -- the edge was noise wearing a
// confident number. An edge smaller than the model's own error is not an edge.
func ShouldTake(fair, uncertainty, residual, bestBid, bestAsk float64, p Params) Take {
	floor := math.Max(p.TakeEdge, math.Max(uncertainty, residual))

	// The book is offering Up below what we think it is worth: buy Up.
	if bestAsk > 0 && fair-bestAsk > floor {
		return Take{
			BuyUp: true, Price: bestAsk, Edge: fair - bestAsk,
			Why: "ask is below fair value",
		}
	}
	// The book is bidding Up above its worth, i.e. Down is cheap: buy Down.
	if bestBid > 0 && bestBid-fair > floor {
		return Take{
			// Still YES terms: to lift the Up bid we submit a BUY_NO priced at
			// that same YES level.
			BuyDn: true, Price: bestBid, Edge: bestBid - fair,
			Why: "bid is above fair value, so Down is cheap",
		}
	}
	return Take{}
}

func (t Take) Any() bool { return t.BuyUp || t.BuyDn }
