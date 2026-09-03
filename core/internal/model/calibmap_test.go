package model

import (
	"math"
	"math/rand"
	"testing"
)

// overconfident generates predictions with exactly the pathology measured on
// this venue: the model states p, but the outcome happens with a frequency
// pulled toward 0.5. Squashing in logit space reproduces the S-shaped
// reliability curve rather than a uniform offset, which is what a scalar sigma
// refit cannot repair.
func overconfident(n int, squash float64, seed int64) (ps []float64, wins []bool) {
	rng := rand.New(rand.NewSource(seed))
	ps = make([]float64, 0, n)
	wins = make([]bool, 0, n)
	for i := 0; i < n; i++ {
		p := 0.02 + 0.96*rng.Float64()
		true_ := 1 / (1 + math.Exp(-squash*math.Log(p/(1-p))))
		ps = append(ps, p)
		wins = append(wins, rng.Float64() < true_)
	}
	return ps, wins
}

// TestFitIsotonicImprovesCalibration is the claim the map has to earn: fitted
// on one sample and scored on a DIFFERENT one, it must beat the raw model.
// Fitting and scoring on the same rows would make any map look good.
func TestFitIsotonicImprovesCalibration(t *testing.T) {
	trainP, trainW := overconfident(6000, 0.55, 11)
	testP, testW := overconfident(6000, 0.55, 99)

	m, err := FitIsotonic(trainP, trainW, 150)
	if err != nil {
		t.Fatalf("FitIsotonic: %v", err)
	}

	mapped := make([]float64, len(testP))
	for i, p := range testP {
		mapped[i] = m.Apply(p)
	}
	raw := Brier(testP, testW)
	cal := Brier(mapped, testW)
	if cal >= raw {
		t.Errorf("out-of-sample Brier not improved: raw %.4f, calibrated %.4f", raw, cal)
	}
	t.Logf("out-of-sample Brier: raw %.4f -> calibrated %.4f (%.1f%% better)",
		raw, cal, 100*(1-cal/raw))
}

// TestFitIsotonicIsMonotone guards the property the whole design rests on. A
// non-monotone map would reorder our own beliefs, making a contract we think is
// worth more price lower than one we think is worth less.
func TestFitIsotonicIsMonotone(t *testing.T) {
	ps, wins := overconfident(4000, 0.6, 5)
	m, err := FitIsotonic(ps, wins, 100)
	if err != nil {
		t.Fatalf("FitIsotonic: %v", err)
	}
	prev := -1.0
	for x := 0.0; x <= 1.0; x += 0.001 {
		v := m.Apply(x)
		if v < prev-1e-12 {
			t.Fatalf("Apply is not monotone: Apply(%.3f)=%.6f after %.6f", x, v, prev)
		}
		if v < 0 || v > 1 {
			t.Fatalf("Apply(%.3f)=%.6f is outside [0,1]", x, v)
		}
		prev = v
	}
}

// TestMapDoesNotExtrapolateToCertainty is the safety property. The engine's
// losses came from acting on unsupported certainty, so beyond the fitted range
// the map must clamp, never continue toward 0 or 1.
func TestMapDoesNotExtrapolateToCertainty(t *testing.T) {
	m, err := NewMap([]Knot{
		{Raw: 0.10, Cal: 0.18, N: 500},
		{Raw: 0.50, Cal: 0.50, N: 500},
		{Raw: 0.95, Cal: 0.88, N: 500},
	})
	if err != nil {
		t.Fatalf("NewMap: %v", err)
	}
	if got := m.Apply(0.999); got != 0.88 {
		t.Errorf("Apply(0.999) = %.4f, want 0.88 (clamped to the last knot)", got)
	}
	if got := m.Apply(1.0); got != 0.88 {
		t.Errorf("Apply(1.0) = %.4f, want 0.88", got)
	}
	if got := m.Apply(0.0); got != 0.18 {
		t.Errorf("Apply(0.0) = %.4f, want 0.18 (clamped to the first knot)", got)
	}
	// Interpolation between knots.
	if got := m.Apply(0.30); math.Abs(got-0.34) > 1e-9 {
		t.Errorf("Apply(0.30) = %.6f, want 0.34 by linear interpolation", got)
	}
}

// TestResidualIsTheEdgeFloor checks the quantity the take rule consumes. At the
// probability where the engine traded, the residual must exceed the 0.020 edge
// it used to consider decisive -- that inequality is the whole reason the old
// take rule lost money.
func TestResidualIsTheEdgeFloor(t *testing.T) {
	m, err := NewMap([]Knot{
		{Raw: 0.50, Cal: 0.50, N: 500},
		{Raw: 0.96, Cal: 0.82, N: 300}, // the measured tail on this venue
	})
	if err != nil {
		t.Fatalf("NewMap: %v", err)
	}
	got := m.Residual(0.96)
	if math.Abs(got-0.14) > 1e-9 {
		t.Errorf("Residual(0.96) = %.4f, want 0.14", got)
	}
	if got <= 0.020 {
		t.Errorf("Residual(0.96) = %.4f does not exceed the old 0.020 take edge; "+
			"the take rule would still fire on noise", got)
	}
	if r := m.Residual(0.50); r != 0 {
		t.Errorf("Residual(0.50) = %.4f, want 0 where the model is calibrated", r)
	}
}

// TestEmptyMapIsIdentity keeps an uncalibrated build behaving exactly as before
// rather than refusing every quote.
func TestEmptyMapIsIdentity(t *testing.T) {
	var m Map
	if !m.Empty() {
		t.Error("zero Map does not report Empty")
	}
	for _, p := range []float64{0, 0.13, 0.5, 0.87, 1} {
		if got := m.Apply(p); got != p {
			t.Errorf("Apply(%.2f) = %.2f on an empty map, want identity", p, got)
		}
		if r := m.Residual(p); r != 0 {
			t.Errorf("Residual(%.2f) = %.4f on an empty map, want 0", p, r)
		}
	}
}

// TestNewMapRejectsBadInput checks the validator refuses rather than repairs.
func TestNewMapRejectsBadInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		knots []Knot
	}{
		{"too few", []Knot{{Raw: 0.5, Cal: 0.5}}},
		{"raw not increasing", []Knot{{Raw: 0.5, Cal: 0.4}, {Raw: 0.5, Cal: 0.6}}},
		{"cal decreasing", []Knot{{Raw: 0.2, Cal: 0.6}, {Raw: 0.8, Cal: 0.3}}},
		{"out of range", []Knot{{Raw: 0.2, Cal: 0.6}, {Raw: 1.4, Cal: 0.9}}},
	} {
		if _, err := NewMap(tc.knots); err == nil {
			t.Errorf("%s: NewMap accepted invalid knots", tc.name)
		}
	}
}

// TestFitIsotonicRespectsMinBin makes sure no knot is fitted from a handful of
// observations. A knot with n=3 is a rumour, not a calibration.
func TestFitIsotonicRespectsMinBin(t *testing.T) {
	ps, wins := overconfident(3000, 0.6, 21)
	const minBin = 250
	m, err := FitIsotonic(ps, wins, minBin)
	if err != nil {
		t.Fatalf("FitIsotonic: %v", err)
	}
	for i, k := range m.Knots() {
		if k.N < minBin {
			t.Errorf("knot %d fitted from n=%d, below minBin=%d", i, k.N, minBin)
		}
	}
}

// TestShippedMapIsEitherEmptyOrValid guards the generated file. An invalid map
// degrades to the identity at runtime rather than panicking, so without this
// test a bad regeneration would silently disable calibration.
func TestShippedMapIsEitherEmptyOrValid(t *testing.T) {
	if len(fittedKnots) == 0 {
		if !CalibrationMap().Empty() {
			t.Error("no fitted knots, but CalibrationMap() is not empty")
		}
		return
	}
	if err := CalibrationMapErr(); err != nil {
		t.Fatalf("shipped knots are invalid: %v", err)
	}
	if CalibrationMap().Empty() {
		t.Error("shipped knots present but CalibrationMap() is empty")
	}
}

// TestEdgeFloorNeverBelowMeasuredError is the property the take rule depends on.
// Whatever happens to the map -- shipped, empty, or replaced -- the floor can
// never fall below the calibration error we actually measured.
func TestEdgeFloorNeverBelowMeasuredError(t *testing.T) {
	for p := 0.0; p <= 1.0; p += 0.01 {
		if got := EdgeFloor(p); got < CalibrationError {
			t.Fatalf("EdgeFloor(%.2f) = %.4f, below the measured %.4f", p, got, CalibrationError)
		}
	}
}

// TestCalibrationErrorExceedsDefaultTakeEdge is the whole point of the constant.
// The old rule crossed on a 0.020 edge; if the measured error is larger than
// that, 0.020 was never a threshold at all.
func TestCalibrationErrorExceedsDefaultTakeEdge(t *testing.T) {
	const oldTakeEdge = 0.020
	if CalibrationError <= oldTakeEdge {
		t.Errorf("CalibrationError %.3f does not exceed the old %.3f take edge; "+
			"the floor would change nothing", CalibrationError, oldTakeEdge)
	}
}
