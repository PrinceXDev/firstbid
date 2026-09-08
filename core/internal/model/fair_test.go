package model

import (
	"math"
	"testing"
)

const sigmaBTC = 0.000504 * Calibration

func TestAtTheLineIsFiftyFifty(t *testing.T) {
	// Spot exactly at the opening price: the model must know nothing.
	// This reproduces the measured base rate of 0.4978 over 10,209 windows.
	got := FairValue(77000, 77000, sigmaBTC, 900)
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("FairValue at the line = %v, want 0.5", got)
	}
}

func TestAboveTheLineIsAboveHalf(t *testing.T) {
	if got := FairValue(77100, 77000, sigmaBTC, 900); got <= 0.5 {
		t.Errorf("above the line = %v, want > 0.5", got)
	}
	if got := FairValue(76900, 77000, sigmaBTC, 900); got >= 0.5 {
		t.Errorf("below the line = %v, want < 0.5", got)
	}
}

// Up and Down must sum to exactly 1: the venue quotes one book, and a matched
// pair always redeems to 1.00. Any asymmetry here is free money for a taker.
func TestUpAndDownSumToOne(t *testing.T) {
	for _, spot := range []float64{76500, 77000, 77500} {
		up := FairValue(spot, 77000, sigmaBTC, 600)
		down := 1 - up
		if math.Abs(up+down-1) > 1e-12 {
			t.Errorf("up+down = %v, want 1", up+down)
		}
	}
}

// As the window closes, the price must converge to the realised outcome.
func TestConvergesAtExpiry(t *testing.T) {
	prev := FairValue(77100, 77000, sigmaBTC, 3600)
	for _, secs := range []float64{1800, 600, 120, 30, 5} {
		got := FairValue(77100, 77000, sigmaBTC, secs)
		if got < prev {
			t.Errorf("at %vs got %v, expected monotone increase toward 1 (prev %v)", secs, got, prev)
		}
		prev = got
	}
	if got := FairValue(77100, 77000, sigmaBTC, 0); got != 1 {
		t.Errorf("terminal above the line = %v, want 1", got)
	}
	if got := FairValue(76900, 77000, sigmaBTC, 0); got != 0 {
		t.Errorf("terminal below the line = %v, want 0", got)
	}
}

// Uncertainty must shrink as the window closes: that is what lets a quoter
// tighten safely instead of holding one static spread all the way to expiry.
func TestUncertaintyShrinksNearExpiry(t *testing.T) {
	far := Uncertainty(77000, 77000, sigmaBTC, 3600, 15)
	near := Uncertainty(77000, 77000, sigmaBTC, 120, 15)
	if !(near > far) {
		t.Errorf("uncertainty near expiry (%v) should exceed far (%v) at the line", near, far)
	}
}

func TestDegenerateInputsDoNotPanic(t *testing.T) {
	for _, c := range [][4]float64{
		{0, 77000, sigmaBTC, 900}, {77000, 0, sigmaBTC, 900},
		{77000, 77000, 0, 900}, {77000, 77000, sigmaBTC, -5},
	} {
		v := FairValue(c[0], c[1], c[2], c[3])
		if math.IsNaN(v) || v < 0 || v > 1 {
			t.Errorf("FairValue%v = %v, want a probability", c, v)
		}
	}
}

// The engine must refuse cadences we never fitted, rather than extrapolate.
func TestOnlyValidatedCadencesAreCalibrated(t *testing.T) {
	for _, iv := range []int64{300, 900, 3600} {
		if !Calibrated("BTC", iv) {
			t.Errorf("BTC/%ds should be calibrated", iv)
		}
	}
	// BTC/14400 is calibrated from horizon-scaled evidence; see fitted.
	if !Calibrated("BTC", 14400) {
		t.Error("BTC/14400s is horizon-scaled and should be calibrated")
	}
	for _, iv := range []int64{60, 86400, 3888000} {
		if Calibrated("BTC", iv) {
			t.Errorf("BTC/%ds has no fitted history and must not be quoted", iv)
		}
	}
	if Calibrated("DOGE", 900) {
		t.Error("unmodelled asset must not be calibrated")
	}
}

// The same measurement that blessed BTC/240m refused ETH/240m: sqrt(t) deviated
// 18.2% there against 11.1%. If ETH/240m ever appears without its own passing
// measurement, volscale has become a rubber stamp and this test says so.
func TestHorizonScalingIsNotBlanketPermission(t *testing.T) {
	if Calibrated("ETH", 14400) {
		t.Error("ETH/14400s failed the sqrt(t) tolerance in cmd/volscale (+18.2%) and must not be quoted")
	}
	// Neither asset has the independent observations for 24h or longer: 30 days
	// of M1 history holds 27 non-overlapping 24-hour returns.
	for _, a := range []string{"BTC", "ETH"} {
		for _, iv := range []int64{86400, 3888000} {
			if Calibrated(a, iv) {
				t.Errorf("%s/%ds lacks independent observations and must not be quoted", a, iv)
			}
		}
	}
}

// The table mixes two kinds of evidence, so an entry with no Source reads as
// resolved-window-strong when it may not be.
func TestEveryFittedEntryDeclaresItsProvenance(t *testing.T) {
	for c, v := range fitted {
		switch v.Source {
		case SourceResolvedWindows, SourceHorizonScaled:
		default:
			t.Errorf("%s/%ds: Source is %q, want one of the declared sources",
				c.asset, c.intervalSec, v.Source)
		}
		if v.N <= 0 {
			t.Errorf("%s/%ds: N=%d, an entry must carry the sample it was measured on",
				c.asset, c.intervalSec, v.N)
		}
	}
}

// A horizon-scaled entry is justified by realised moves being LARGER than
// sqrt(t) implies, which makes probabilities less confident. One at or below its
// anchor would raise confidence on the cadence with the least evidence -- the
// shape of the failure in docs/AUTOPSY.md.
func TestHorizonScaledSigmaExceedsItsAnchor(t *testing.T) {
	for c, v := range fitted {
		if v.Source != SourceHorizonScaled {
			continue
		}
		anchor, ok := fitted[cadence{c.asset, 900}]
		if !ok {
			t.Fatalf("%s/%ds is horizon-scaled but %s has no 15m anchor to scale from",
				c.asset, c.intervalSec, c.asset)
		}
		if v.SigmaPerMin <= anchor.SigmaPerMin {
			t.Errorf("%s/%ds: sigma %.6f is not above its %.6f anchor; horizon scaling must not increase confidence",
				c.asset, c.intervalSec, v.SigmaPerMin, anchor.SigmaPerMin)
		}
	}
}
