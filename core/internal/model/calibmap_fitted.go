// Placeholder until cmd/backtest generates the fitted map.
//
// Regenerate:
//
//	FB_MAP=internal/model/calibmap_fitted.go go run ./cmd/backtest
//
// That command writes this file only when the fitted map BEATS the raw
// probabilities on the held-out half. A map that loses its own out-of-sample
// test is printed and discarded, so following the regeneration instruction can
// never turn a rejected experiment into the source the engine prices through.
// If this file is still a placeholder, that is a result, not a missing step.
//
// An empty knot set makes CalibrationMap the identity, so an unfitted build
// prices exactly as it did before rather than refusing to quote.
package model

var fittedKnots = []Knot{}
