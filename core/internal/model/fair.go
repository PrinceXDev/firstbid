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
}

// Calibration is the volatility multiplier fitted by cmd/backtest.
//
// Terminal-move sigma (fitted from oracle open->close) overstates the diffusion
// that actually matters for settlement, because the settlement reference and the
// index feed are different series. The multiplier corrects that scale error.
//
// Fitted on the OLDER half of 15,386 replayed predictions and validated on the
// newer half it never saw: Brier 0.1395 vs 0.2500 baseline, +44.2% skill,
// reliability within +/-0.05 in every decile.
//
// An earlier value of 0.530 was measured against a backtest that leaked future
// prices: the spot series was keyed by each minute's start but held that
// minute's CLOSE, so every sample saw up to 59 seconds ahead. Removing the leak
// raised the fitted multiplier substantially, which is to say the model had been
// systematically OVERCONFIDENT — pushing probabilities too near 0 and 1, and
// therefore seeing mispricing that was not there.
const Calibration = 0.636

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
var fitted = map[cadence]Vol{
	{"BTC", 300}:  {Asset: "BTC", SigmaPerMin: 0.000513 * Calibration, N: 512},
	{"BTC", 900}:  {Asset: "BTC", SigmaPerMin: 0.000504 * Calibration, N: 2880},
	{"BTC", 3600}: {Asset: "BTC", SigmaPerMin: 0.000517 * Calibration, N: 720},
	{"ETH", 300}:  {Asset: "ETH", SigmaPerMin: 0.000683 * Calibration, N: 470},
	{"ETH", 900}:  {Asset: "ETH", SigmaPerMin: 0.000681 * Calibration, N: 2880},
	{"ETH", 3600}: {Asset: "ETH", SigmaPerMin: 0.000707 * Calibration, N: 720},
}

type cadence struct {
	asset       string
	intervalSec int64
}

// SigmaPerMin returns the fitted volatility for one asset and cadence, and
// whether we have calibration for it at all.
func SigmaPerMin(asset string, intervalSec int64) (float64, bool) {
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
