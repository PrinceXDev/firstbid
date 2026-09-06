package maker

import (
	"math"
	"testing"
	"time"

	"github.com/firstbid/core/internal/venue"
)

// TestCollateralCostIsWhatTheSideActuallyPays pins the YES-axis conversion the
// risk budget depends on. Charging a Down take the YES price is what let one
// window's budget report far more spent than it had, so this is checked at the
// prices where the two numbers differ most.
func TestCollateralCostIsWhatTheSideActuallyPays(t *testing.T) {
	cases := []struct {
		kind venue.OrderKind
		yes  float64
		want float64
	}{
		{venue.BuyYes, 0.90, 0.90},
		{venue.BuyYes, 0.10, 0.10},
		{venue.BuyNo, 0.90, 0.10},
		{venue.BuyNo, 0.10, 0.90},
		{venue.SellNo, 0.75, 0.25},
	}
	for _, c := range cases {
		if got := collateralCost(c.kind, c.yes); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("collateralCost(%v, %.2f) = %.4f, want %.4f",
				c.kind, c.yes, got, c.want)
		}
	}
}

// TestDownTakeIsNotOvercharged is the budget-level statement of the same rule.
// At a YES price of 0.90 a Down take costs 0.10 per contract; charging it 0.90
// would exhaust a 15-unit notional cap after two takes instead of the full
// per-window allowance, silently turning a risk limit into a spending bug.
func TestDownTakeIsNotOvercharged(t *testing.T) {
	p := DefaultParams()
	p.TakeCooldown = 0
	p.MaxTakesPerMarket = 20
	p.MaxTakeNotional = 15
	p.Size = 2

	b := NewTakeBudget(p)
	const yes = 0.90
	cost := collateralCost(venue.BuyNo, yes)

	now := time.Now()
	takes := 0
	for {
		ok, _ := b.Allow(now, cost, p.Size)
		if !ok {
			break
		}
		b.Record(now, cost, p.Size)
		takes++
	}
	// 0.10 * 2 = 0.20 per take against a 15.00 cap, so 20 takes cost 4.00 and
	// the take-COUNT cap is what should stop us. Charged the YES price instead,
	// each take would book 1.80 and the notional cap would bite after 8.
	if takes != p.MaxTakesPerMarket {
		t.Fatalf("Down takes at YES %.2f stopped after %d, want %d: the budget is "+
			"being charged the YES price rather than the %.2f actually paid",
			yes, takes, p.MaxTakesPerMarket, cost)
	}
	if _, notional := b.Spent(); math.Abs(notional-float64(takes)*cost*p.Size) > 1e-9 {
		t.Errorf("recorded notional %.4f does not match collateral paid", notional)
	}
}
