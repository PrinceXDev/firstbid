package main

import (
	"sort"
	"testing"
)

// buildPreds fakes `nWindows` resolved markets, each contributing one row per
// sampled fraction, exactly as the replay loop does.
func buildPreds(nWindows int) []pred {
	var ps []pred
	for w := 0; w < nWindows; w++ {
		for _, f := range fractions {
			ps = append(ps, pred{
				frac:   f,
				expiry: int64(1000 + w*300),
				window: w,
				actual: w%2 == 0,
			})
		}
	}
	return ps
}

// TestSplitNeverCutsAWindow is the property the out-of-sample number depends
// on. Every row of one window carries that window's single outcome, so a
// boundary drawn through a window would let the same coin flip both fit the
// calibration and score it -- which is how a leak flatters the very figure the
// backtest exists to keep honest.
func TestSplitNeverCutsAWindow(t *testing.T) {
	// 2141 windows * 5 rows = 10705 rows, and 10705/2 = 5352 lands mid-window:
	// the exact split the published counts revealed.
	for _, n := range []int{2, 3, 7, 100, 2141} {
		ps := buildPreds(n)
		cut := splitAtWindowBoundary(ps, len(ps)/2)
		train, test := ps[:cut], ps[cut:]

		if len(train) == 0 || len(test) == 0 {
			t.Fatalf("n=%d: split produced an empty side (%d/%d)", n, len(train), len(test))
		}
		seen := map[int]bool{}
		for _, p := range train {
			seen[p.window] = true
		}
		for _, p := range test {
			if seen[p.window] {
				t.Fatalf("n=%d: window %d appears in both train and test", n, p.window)
			}
		}
		if cut%len(fractions) != 0 {
			t.Errorf("n=%d: cut %d is not a whole number of windows", n, cut)
		}
	}
}

// TestSplitStaysNearTheMidpoint checks the boundary is nudged, not relocated:
// a leak-free split is only useful if both halves remain large enough to say
// anything.
func TestSplitStaysNearTheMidpoint(t *testing.T) {
	ps := buildPreds(2141)
	want := len(ps) / 2
	cut := splitAtWindowBoundary(ps, want)
	if d := cut - want; d < 0 || d >= len(fractions) {
		t.Errorf("cut %d moved %d rows from the midpoint %d; want a move inside one window",
			cut, d, want)
	}
}

// TestSplitHandlesASingleWindow: with nothing to split between, the boundary
// must collapse to an empty side so main() can refuse the run, rather than
// inventing a partition that leaks.
func TestSplitHandlesASingleWindow(t *testing.T) {
	ps := buildPreds(1)
	if cut := splitAtWindowBoundary(ps, len(ps)/2); cut != 0 && cut != len(ps) {
		t.Errorf("single window split at %d; want 0 or %d", cut, len(ps))
	}
}

// TestSortKeepsWindowsContiguous guards the precondition splitAtWindowBoundary
// relies on: windows that expire at the same second (BTC and ETH share a
// cadence) must not interleave.
func TestSortKeepsWindowsContiguous(t *testing.T) {
	var ps []pred
	for w := 0; w < 4; w++ {
		for _, f := range fractions {
			ps = append(ps, pred{frac: f, expiry: 5000, window: w})
		}
	}
	// Shuffle deterministically by frac so the sort has work to do.
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].frac < ps[j].frac })
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].expiry != ps[j].expiry {
			return ps[i].expiry < ps[j].expiry
		}
		return ps[i].window < ps[j].window
	})
	for i := 1; i < len(ps); i++ {
		if ps[i].window < ps[i-1].window {
			t.Fatalf("windows out of order at %d", i)
		}
	}
	seenEnd := map[int]bool{}
	for i := 1; i < len(ps); i++ {
		if ps[i].window != ps[i-1].window {
			if seenEnd[ps[i].window] {
				t.Fatalf("window %d is not contiguous", ps[i].window)
			}
			seenEnd[ps[i-1].window] = true
		}
	}
}
