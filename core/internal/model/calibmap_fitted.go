// Placeholder until cmd/backtest generates the fitted map.
//
// Regenerate:
//
//	FB_MAP=internal/model/calibmap_fitted.go go run ./cmd/backtest
//
// An empty knot set makes CalibrationMap the identity, so an unfitted build
// prices exactly as it did before rather than refusing to quote.
package model

var fittedKnots = []Knot{}
