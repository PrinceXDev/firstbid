package maker

import (
	"fmt"
	"time"
)

// TakeBudget is the per-window risk budget for crossing the book.
//
// It exists because ShouldTake consulted neither inventory nor history. On
// 2026-09-03, in a single BTC/5m window, one overconfident belief was acted on
// eight times in four minutes: eight fills, all the same side, all the same
// reasoning, all wrong together. A 2-contract idea became 16 contracts of
// perfectly correlated loss.
//
// Correlation is the point. Repeating a take is not diversification -- within
// one window every fill settles against the same outcome, so N takes on one
// belief is one bet at N times the size. The ledger reported 53 fills; the
// effective sample was 11 markets, and reading the first number as the second
// is what made a broken model look merely unlucky.
//
// A TakeBudget is owned by exactly one marketLoop goroutine and is not safe for
// concurrent use, which matches how the rest of the engine scopes state.
type TakeBudget struct {
	maxTakes    int
	maxNotional float64
	cooldown    time.Duration

	takes    int
	notional float64
	last     time.Time
}

// NewTakeBudget returns the budget for one window.
func NewTakeBudget(p Params) *TakeBudget {
	return &TakeBudget{
		maxTakes:    p.MaxTakesPerMarket,
		maxNotional: p.MaxTakeNotional,
		cooldown:    p.TakeCooldown,
	}
}

// Allow reports whether one more take of `size` contracts at `price` is within
// budget, and if not, why. The reason is logged rather than swallowed: what the
// engine refuses to do is as much a result as what it does.
//
// `price` is COLLATERAL per contract, not the YES-axis wire price: a BUY_DN at
// YES price p costs 1-p, and the caller converts before calling.
func (b *TakeBudget) Allow(now time.Time, price, size float64) (bool, string) {
	if b == nil {
		return true, ""
	}
	if b.maxTakes > 0 && b.takes >= b.maxTakes {
		return false, fmt.Sprintf("take budget spent (%d/%d in this window)", b.takes, b.maxTakes)
	}
	if !b.last.IsZero() && b.cooldown > 0 {
		if waited := now.Sub(b.last); waited < b.cooldown {
			return false, fmt.Sprintf("take cooldown (%.0fs of %.0fs)",
				waited.Seconds(), b.cooldown.Seconds())
		}
	}
	if cost := price * size; b.maxNotional > 0 && b.notional+cost > b.maxNotional {
		return false, fmt.Sprintf("take notional cap (%.2f + %.2f > %.2f)",
			b.notional, cost, b.maxNotional)
	}
	return true, ""
}

// Record books a take against the budget. Call it only when an order was
// actually sent, so a rejected intent does not consume the window's risk.
// `price` is collateral per contract, as in Allow.
func (b *TakeBudget) Record(now time.Time, price, size float64) {
	if b == nil {
		return
	}
	b.takes++
	b.notional += price * size
	b.last = now
}

// Spent reports what the window has used, for logging and the dashboard.
func (b *TakeBudget) Spent() (takes int, notional float64) {
	if b == nil {
		return 0, 0
	}
	return b.takes, b.notional
}
