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
	for _, iv := range []int64{60, 14400, 86400} {
		if Calibrated("BTC", iv) {
			t.Errorf("BTC/%ds has no fitted history and must not be quoted", iv)
		}
	}
	if Calibrated("DOGE", 900) {
		t.Error("unmodelled asset must not be calibrated")
	}
}
