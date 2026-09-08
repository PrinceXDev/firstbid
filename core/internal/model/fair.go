// Package model prices binary event contracts.
//
// Empirically (n=6,665 resolved windows) these markets are a driftless random
// walk: P(Up) measured 0.4978, and sigma scales with sqrt(time) across cadences.
// So fair value is the terminal probability that a driftless walk finishes at or
// above where it started:
//
//	P(Up) = Phi( ln(spot/open) / (sigmaPerMin * sqrt(minutesRemaining)) )
//
// No directional forecast is made or implied. The edge is the spread, not the side.
package model

import "math"

// Vol is the fitted per-minute log-return volatility for one asset.
// Values from cmd/calibrate against mainnet history.
type Vol struct {
	Asset       string
	SigmaPerMin float64
	N           int
	// Source records WHERE this number came from, because the table now holds
	// two kinds of evidence and they are not interchangeable. N means "resolved
	// windows" for one and "non-overlapping price returns" for the other, and a
	// reader who conflates them will trust a horizon-scaled entry as though the
	// venue had settled it. Provenance travels with the number for the same
	// reason SpotObs carries an observable-at time: the expensive bug was not a
	// wrong value, it was a value whose origin nobody had written down.
	Source string
}

// Sources for Vol. A cadence is quotable under either, but only one of them
// means the venue itself has settled the window enough times to fit it.
const (
	// SourceResolvedWindows: fitted by cmd/calibrate from resolved venue
	// windows of exactly this cadence. The strongest evidence available.
	SourceResolvedWindows = "resolved-windows"

	// SourceHorizonScaled: no resolved venue history exists at this cadence, so
	// sigma is the same asset's resolved-window sigma scaled by the horizon
	// ratio cmd/volscale measured in the index price process. Weaker: it
	// assumes the price process the candles describe is the one the venue
	// settles against, which is checked but not settled-window-proven.
	SourceHorizonScaled = "horizon-scaled"
)

// Calibration is the volatility multiplier fitted by cmd/backtest, applied to
// the raw measured sigma in SigmaPerMin.
//
// HISTORY, because it matters: this constant was 0.530 for most of the build,
// fitted against a backtest whose spot lookup returned the close of the M1
// candle CONTAINING each decision time -- up to 59 seconds of look-ahead. With
// part of the future known, less diffusion remains to explain, so the fitter
// chose a sigma that was far too small and the model became badly overconfident
// in the tails. That is what produced the reported +59.5% skill, and it is what
// lost 37% of deployed capital live (edge +5.68, selection -41.44 over 53 fills).
//
// An earlier value of 0.530 was measured against a backtest that leaked future
// prices: the spot series was keyed by each minute's start but held that
// minute's CLOSE, so every sample saw up to 59 seconds ahead. Removing the leak
// raised the fitted multiplier substantially, which is to say the model had been
// systematically OVERCONFIDENT -- pushing probabilities too near 0 and 1, and
// therefore seeing mispricing that was not there.
//
// venue.SpotSeries is now strictly causal, and cmd/backtest defaults to
// replaying the same 1s PricePoint feed the engine trades, so replay and
// production cannot disagree about what was knowable when. See docs/AUTOPSY.md
// for the measurement and what it cost.
//
// k is REGIME-DEPENDENT, which is worth stating plainly because two honest
// measurements of it disagree:
//
//	720h of M1 candles, 15,485 predictions : 0.635 (older half) / 0.680 (full)
//	 96h of the 1s feed, 10,705 predictions : 0.710 (older half) / 0.720 (full)
//
// The candle sample is longer and averages more regimes; the points sample is
// the series the engine actually trades. We ship the HIGHER end, because a
// larger sigma means less confident probabilities, and overconfidence is the
// specific failure that cost 37% of deployed capital. Being under-confident
// only forgoes trades. At 0.710 on the traded feed no out-of-sample decile is
// off by more than 0.032; at 0.635 on candles the top four deciles are all
// still overconfident, the worst by 0.045.
//
// A single multiplier corrects a scale error, not a curve shape. An isotonic
// calibration map was fitted to correct the shape as well and did NOT survive
// its own out-of-sample test, so it is deliberately not shipped -- see
// calibmap.go. What the take rule uses instead is CalibrationError, the largest
// measured out-of-sample decile error.
const Calibration = 0.715

// fitted holds per-minute volatility measured per (asset, cadence) by
// cmd/calibrate against resolved mainnet windows.
//
// sigma/min is strikingly stable within an asset across cadences
// (BTC: 0.000513 at 5m, 0.000504 at 15m, 0.000517 at 60m), which is exactly the
// sqrt(t) scaling the model assumes -- measured, not asserted.
//
// Cadences absent from this table have NO resolved history to fit against, so
// the engine refuses to quote them rather than extrapolating. Quoting a market
// we have not validated is how a maker donates money.
// These are RAW measured values. Calibration is applied in SigmaPerMin, never
// baked in here -- when it was baked in, cmd/backtest's fitter multiplied a
// second time and its reported k meant something different from what it looked
// like, which hid the overconfidence for days.
var fitted = map[cadence]Vol{
	{"BTC", 300}:  {Asset: "BTC", SigmaPerMin: 0.000513, N: 512, Source: SourceResolvedWindows},
	{"BTC", 900}:  {Asset: "BTC", SigmaPerMin: 0.000504, N: 2880, Source: SourceResolvedWindows},
	{"BTC", 3600}: {Asset: "BTC", SigmaPerMin: 0.000517, N: 720, Source: SourceResolvedWindows},
	{"ETH", 300}:  {Asset: "ETH", SigmaPerMin: 0.000683, N: 470, Source: SourceResolvedWindows},
	{"ETH", 900}:  {Asset: "ETH", SigmaPerMin: 0.000681, N: 2880, Source: SourceResolvedWindows},
	{"ETH", 3600}: {Asset: "ETH", SigmaPerMin: 0.000707, N: 720, Source: SourceResolvedWindows},

	// BTC/240m -- the first entry NOT fitted from resolved venue windows.
	//
	// WHY IT IS HERE. Refusing this cadence cost more than it saved. The venue
	// reshaped into few, long, heavily traded windows, and a census of the live
	// book (cmd/surface) found 187 of 191 trades and ~99% of quote volume on
	// the 240m, 1440m and 64800m cadences this table refused, resting ~250x the
	// depth of the 60m books it accepts. The refusal was not wrong to make; the
	// REASON was too strong. "No resolved venue history" was read as "no
	// evidence", but sigma is a property of the index price process, not of the
	// venue's settlement log.
	//
	// WHY THIS NUMBER. cmd/volscale measures sigma/min from 30 days of M1
	// candles using non-overlapping returns, and two independent estimators
	// agree: realised 1-minute sigma is 0.000530 against this table's
	// resolved-window 0.000504, a ratio of 1.053. At a 240-minute aggregation
	// realised sigma/min is 0.000589, i.e. 1.111x the 1-minute measurement over
	// n=177 independent 4-hour returns.
	//
	// Only that RATIO is imported, never the absolute level:
	//
	//	0.000504 (BTC/900 resolved) * 1.111 = 0.000560
	//
	// Importing 0.000589 directly would hand Calibration a number it was never
	// fitted against -- k=0.715 was fitted to correct RESOLVED-WINDOW sigma on
	// the 5m-60m cadences -- and the result would be corrected twice, which is
	// the precise hazard the note above this table warns about. Scaling the
	// resolved-window anchor keeps the whole existing calibration chain intact
	// and imports only the part cmd/volscale actually validated: how sigma
	// moves BETWEEN horizons.
	//
	// The deviation is POSITIVE, so this sigma is larger than sqrt(t) from the
	// 15m fit implies, and the probabilities it yields are less confident.
	// That is the safe direction; overconfidence is what cost 37%.
	{"BTC", 14400}: {Asset: "BTC", SigmaPerMin: 0.000560, N: 177, Source: SourceHorizonScaled},

	// WHAT IS DELIBERATELY ABSENT, and why the same measurement excludes it:
	//
	//	ETH/240m   n=177, +18.2% -- sqrt(t) breaks. The identical measurement
	//	           that blessed BTC/240m refuses this one. Passing it anyway
	//	           would make cmd/volscale a rubber stamp rather than a test.
	//	*/1440m    n=27 -- 30 days holds 27 independent 24-hour returns, below
	//	           the point where a regime can be told from noise.
	//	*/64800m   under one independent 45-day return in 30 days of history.
	//	           This cadence rests the DEEPEST book on the venue and is still
	//	           refused: it is not short of a model, it is short of evidence.
	//
	// Extending coverage further needs a longer sample, not a looser tolerance.
}

type cadence struct {
	asset       string
	intervalSec int64
}

// SigmaPerMin returns the calibrated volatility the engine prices with: the raw
// measured value scaled by Calibration.
func SigmaPerMin(asset string, intervalSec int64) (float64, bool) {
	v, ok := fitted[cadence{asset, intervalSec}]
	return v.SigmaPerMin * Calibration, ok
}

// SigmaPerMinRaw returns the volatility as measured by cmd/calibrate, before
// any multiplier. Fitters must use this, so the k they report is the total
// multiplier rather than a correction on top of an existing one.
func SigmaPerMinRaw(asset string, intervalSec int64) (float64, bool) {
	v, ok := fitted[cadence{asset, intervalSec}]
	return v.SigmaPerMin, ok
}

// Calibrated reports whether this market is one we have validated a model for.
func Calibrated(asset string, intervalSec int64) bool {
	_, ok := fitted[cadence{asset, intervalSec}]
	return ok
}

// Coverage lists every calibrated series, for display and for tests.
func Coverage() []Vol {
	out := make([]Vol, 0, len(fitted))
	for _, v := range fitted {
		v.SigmaPerMin *= Calibration
		out = append(out, v)
	}
	return out
}

// FairValue is the model's probability that the window closes at or above its
// opening price. secondsLeft is time to expiry; spot and open are index prices.
func FairValue(spot, open, sigmaPerMin float64, secondsLeft float64) float64 {
	switch {
	case open <= 0 || spot <= 0 || sigmaPerMin <= 0:
		return 0.5
	case secondsLeft <= 0:
		// Terminal: the comparison is already decided.
		if spot >= open {
			return 1
		}
		return 0
	}
	minutes := secondsLeft / 60
	denom := sigmaPerMin * math.Sqrt(minutes)
	if denom <= 0 {
		return 0.5
	}
	return normCDF(math.Log(spot/open) / denom)
}

// Uncertainty is how much the fair value could move over the next `horizon`
// seconds purely from price diffusion. It is the honest floor on how tight a
// two-sided quote can be without being picked off by ordinary noise.
func Uncertainty(spot, open, sigmaPerMin, secondsLeft, horizon float64) float64 {
	if secondsLeft <= 0 {
		return 0
	}
	base := FairValue(spot, open, sigmaPerMin, secondsLeft)
	step := sigmaPerMin * math.Sqrt(horizon/60)
	up := FairValue(spot*math.Exp(step), open, sigmaPerMin, secondsLeft-horizon)
	dn := FairValue(spot*math.Exp(-step), open, sigmaPerMin, secondsLeft-horizon)
	return math.Max(math.Abs(up-base), math.Abs(base-dn))
}

// normCDF is the standard normal CDF via the error function.
func normCDF(x float64) float64 { return 0.5 * math.Erfc(-x/math.Sqrt2) }
