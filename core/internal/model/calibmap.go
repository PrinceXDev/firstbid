// Calibration mapping -- built, measured, and DELIBERATELY NOT SHIPPED.
//
// The reasoning that motivated this file: FairValue is a diffusion model with
// one scale parameter, and scaling sigma can move a reliability curve but not
// change its shape. Measured against the leaked backtest's constant
// (k = 0.530) the honest curve was strongly S-shaped -- under-confident below
// 0.4, over-confident above, worst in the tail the take rule likes best -- so a
// monotone map looked like the right tool.
//
// It was not. Once the look-ahead was removed and sigma refitted honestly
// (k = 0.715), the S-shape largely disappeared: no out-of-sample decile is off
// by more than 0.032. Fitting an isotonic map on the older half and scoring it
// on the newer half then makes calibration WORSE -- Brier 0.1357 raw against
// 0.1368 mapped, over 5,353 held-out predictions. With 12 knots of 446
// observations each, the map was fitting the training half's noise.
//
// So fittedKnots is empty, CalibrationMap() is the identity, and the scalar
// refit stands on its own. The machinery stays because it is the correct tool
// if a larger sample ever shows real curvature, and because the negative result
// is worth keeping: the map has to earn its place out of sample, and this one
// did not. cmd/backtest prints that verdict on every run.
//
// What DID survive is CalibrationError -- the measured bound a take must clear.
// See docs/AUTOPSY.md.
package model

import (
	"fmt"
	"math"
	"sort"
)

// Knot is one fitted point of the calibration map: a raw model probability and
// the frequency actually observed around it.
type Knot struct {
	Raw float64 `json:"raw"`
	Cal float64 `json:"cal"`
	N   int     `json:"n"`
}

// Map is a piecewise-linear monotone calibration map.
//
// Outside the fitted range it clamps to the nearest knot rather than
// extrapolating. That is deliberate: the whole failure this package is
// recovering from came from claiming certainty the data did not support, and
// extrapolating a calibration curve toward 1.0 would do it again.
type Map struct {
	knots []Knot
}

// NewMap validates and returns a calibration map. Knots must be sorted by Raw
// and non-decreasing in Cal; a map that fails either is rejected rather than
// silently repaired, because a non-monotone map would reorder our own beliefs.
func NewMap(ks []Knot) (Map, error) {
	if len(ks) < 2 {
		return Map{}, fmt.Errorf("calibration map needs at least 2 knots, got %d", len(ks))
	}
	for i, k := range ks {
		if k.Raw < 0 || k.Raw > 1 || k.Cal < 0 || k.Cal > 1 {
			return Map{}, fmt.Errorf("knot %d out of [0,1]: %+v", i, k)
		}
		if i > 0 {
			if k.Raw <= ks[i-1].Raw {
				return Map{}, fmt.Errorf("knot %d raw %.4f not increasing after %.4f",
					i, k.Raw, ks[i-1].Raw)
			}
			if k.Cal < ks[i-1].Cal {
				return Map{}, fmt.Errorf("knot %d cal %.4f decreases after %.4f",
					i, k.Cal, ks[i-1].Cal)
			}
		}
	}
	out := make([]Knot, len(ks))
	copy(out, ks)
	return Map{knots: out}, nil
}

// Empty reports whether this map is the zero value, i.e. no calibration.
func (m Map) Empty() bool { return len(m.knots) == 0 }

// Knots returns the fitted points, for display and export.
func (m Map) Knots() []Knot {
	out := make([]Knot, len(m.knots))
	copy(out, m.knots)
	return out
}

// Apply maps a raw model probability to its calibrated value. An empty map is
// the identity, so an uncalibrated build behaves as before rather than refusing.
func (m Map) Apply(p float64) float64 {
	if len(m.knots) == 0 {
		return p
	}
	if p <= m.knots[0].Raw {
		return m.knots[0].Cal
	}
	last := m.knots[len(m.knots)-1]
	if p >= last.Raw {
		return last.Cal
	}
	i := sort.Search(len(m.knots), func(i int) bool { return m.knots[i].Raw > p })
	lo, hi := m.knots[i-1], m.knots[i]
	span := hi.Raw - lo.Raw
	if span <= 0 {
		return lo.Cal
	}
	return lo.Cal + (hi.Cal-lo.Cal)*(p-lo.Raw)/span
}

// Residual is how far the raw estimate sits from its calibrated value at p:
// the model's own measured error, in probability units.
//
// This is the floor on the edge required to act. At p = 0.97 the engine used to
// see a 0.03 edge as decisive, while its measured error at that probability was
// larger than 0.03 -- so the edge was noise wearing a confident number. Any
// take must clear this.
func (m Map) Residual(p float64) float64 { return math.Abs(p - m.Apply(p)) }

// FitIsotonic fits a monotone calibration map from replayed predictions.
//
// Observations are grouped into equal-count bins of at least minBin, then the
// bin frequencies are made monotone by pool-adjacent-violators. Equal-count
// binning matters: these predictions pile up in the tails, so equal-WIDTH bins
// would fit the middle from a handful of points and the tail from thousands.
func FitIsotonic(ps []float64, wins []bool, minBin int) (Map, error) {
	if len(ps) != len(wins) {
		return Map{}, fmt.Errorf("mismatched inputs: %d probabilities, %d outcomes",
			len(ps), len(wins))
	}
	if minBin < 2 {
		minBin = 2
	}
	if len(ps) < 2*minBin {
		return Map{}, fmt.Errorf("need at least %d observations to fit, got %d",
			2*minBin, len(ps))
	}

	type obs struct {
		p float64
		y float64
	}
	all := make([]obs, len(ps))
	for i := range ps {
		y := 0.0
		if wins[i] {
			y = 1.0
		}
		all[i] = obs{ps[i], y}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].p < all[j].p })

	// Equal-count bins.
	type bin struct {
		sumP, sumY float64
		n          int
	}
	var bins []bin
	for i := 0; i < len(all); i += minBin {
		j := i + minBin
		if j > len(all) {
			j = len(all)
		}
		// Fold a short trailing bin into its predecessor.
		if j-i < minBin && len(bins) > 0 {
			b := &bins[len(bins)-1]
			for _, o := range all[i:j] {
				b.sumP += o.p
				b.sumY += o.y
				b.n++
			}
			break
		}
		var b bin
		for _, o := range all[i:j] {
			b.sumP += o.p
			b.sumY += o.y
			b.n++
		}
		bins = append(bins, b)
	}
	if len(bins) < 2 {
		return Map{}, fmt.Errorf("binning produced %d bins; lower minBin", len(bins))
	}

	// Pool adjacent violators: merge any bin whose frequency dips below its
	// predecessor, until the sequence is non-decreasing.
	type blk struct {
		sumP, sumY float64
		n          int
	}
	var st []blk
	for _, b := range bins {
		cur := blk{b.sumP, b.sumY, b.n}
		for len(st) > 0 {
			prev := st[len(st)-1]
			if prev.sumY/float64(prev.n) <= cur.sumY/float64(cur.n) {
				break
			}
			cur = blk{prev.sumP + cur.sumP, prev.sumY + cur.sumY, prev.n + cur.n}
			st = st[:len(st)-1]
		}
		st = append(st, cur)
	}

	knots := make([]Knot, 0, len(st))
	for _, b := range st {
		knots = append(knots, Knot{
			Raw: b.sumP / float64(b.n),
			Cal: b.sumY / float64(b.n),
			N:   b.n,
		})
	}
	// Pooling can leave two blocks with an identical mean probability; NewMap
	// requires strictly increasing Raw, so nudge duplicates apart.
	for i := 1; i < len(knots); i++ {
		if knots[i].Raw <= knots[i-1].Raw {
			knots[i].Raw = math.Nextafter(knots[i-1].Raw, 1)
		}
	}
	return NewMap(knots)
}

// Brier is the mean squared error of a set of probabilities against outcomes.
// Reported next to the always-0.5 baseline, it is the one number that says
// whether a change helped.
func Brier(ps []float64, wins []bool) float64 {
	if len(ps) == 0 {
		return 0
	}
	var s float64
	for i, p := range ps {
		y := 0.0
		if wins[i] {
			y = 1.0
		}
		s += (p - y) * (p - y)
	}
	return s / float64(len(ps))
}

// ---- the shipped map -----------------------------------------------------

// calMap is built once from the generated knots. A map that fails validation
// degrades to the identity rather than panicking: refusing to price at all
// would be a worse failure in a live engine than pricing uncalibrated, and
// TestShippedMapIsValid fails the build instead.
var calMap, calMapErr = NewMap(fittedKnots)

// CalibrationMap returns the fitted map the engine prices through.
func CalibrationMap() Map {
	if calMapErr != nil {
		return Map{}
	}
	return calMap
}

// CalibrationMapErr reports why the shipped map was rejected, if it was. An
// empty knot set is a legitimate "not fitted yet" and reports an error here.
func CalibrationMapErr() error { return calMapErr }

// CalibrationError is the largest out-of-sample per-decile calibration error
// measured at the shipped Calibration, rounded up.
//
// Measured 2026-09-03, k fitted on the older half and scored on the newer, on
// two independent replay series:
//
//	 96h of the 1s index feed, 10,705 predictions : worst decile 0.032
//	720h of M1 candles,        15,485 predictions : worst decile 0.045 (0.8-0.9)
//
// The bound takes the LARGER. The engine prices off the 1s feed, but takes
// cluster in the upper deciles, which is exactly where the longer candle sample
// shows the bigger error -- so using the tighter number would put the floor
// below the error it is meant to cover.
//
// This is the honest floor on the edge required to cross a spread. The trade
// that lost 37% was a 0.03 edge taken on a belief whose error was larger than
// the edge -- so the edge was noise wearing a confident number. A fixed measured
// bound is used rather than a fitted per-probability residual because the fitted
// map did not survive its own out-of-sample test (see below).
const CalibrationError = 0.045

// EdgeFloor is the minimum edge that means anything at probability p: the
// model's measured calibration error, widened by the fitted map's own residual
// wherever a validated map is shipped.
func EdgeFloor(p float64) float64 {
	return math.Max(CalibrationError, CalibrationMap().Residual(p))
}
