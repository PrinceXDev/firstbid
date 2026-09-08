// Command volscale asks whether Firstbid may honestly quote the cadences it
// currently refuses.
//
// The engine skips 240-minute and longer windows because `calibrate` fits sigma
// from RESOLVED VENUE HISTORY, and those cadences have not settled often enough
// on this venue to fit anything. The README states that refusal as a virtue,
// and as a refusal it is correct: extrapolating sqrt(t) four times past where it
// was tested is how the model became overconfident the first time.
//
// But "no resolved venue history" is not the same as "no evidence". Sigma is a
// property of the INDEX PRICE PROCESS, not of the venue's settlement log. The
// same 1-minute candles the backtest replays contain 30 days of BTC and ETH
// history, which is enough to measure two things the venue's own history cannot:
//
//  1. Whether sigma measured from price history agrees with sigma fitted from
//     settled windows. Two independent estimators of the same quantity, one of
//     which the engine already trades on. If they disagree, the venue fit is
//     picking up something other than diffusion and this whole line of argument
//     is dead.
//
//  2. Whether sqrt(t) actually holds in the price process out to long horizons.
//     Aggregating returns to k minutes and dividing by sqrt(k) must return the
//     same sigma/min at every k if the walk is driftless with independent
//     increments. Where it stops returning the same number is exactly where
//     extrapolation stops being licensed -- measured, not asserted.
//
// The command prints a per-cadence verdict and refuses to bless a horizon it
// does not have the independent observations to support. n is printed beside
// every figure for that reason: 30 days contains 27 non-overlapping 24-hour
// returns and well under one 45-day return, and a standard deviation from 27
// observations is not evidence of the same kind as one from 43,000.
//
// Non-overlapping windows only. Overlapping returns share increments, so their
// standard deviation looks reassuringly tight while carrying a fraction of the
// independent information -- the same family of mistake as reading a price 59
// seconds after the moment it claims to describe.
//
// Usage:
//
//	go run ./cmd/volscale                  # 30 days, BTC and ETH
//	go run ./cmd/volscale -days=14         # shorter sample
//	go run ./cmd/volscale -asset=BTC
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"github.com/firstbid/core/internal/model"
	"github.com/firstbid/core/internal/venue"
)

// horizons are the aggregation scales tested, in minutes. The first four are
// cadences the venue actually lists; 1m is the base measurement.
var horizons = []int{1, 5, 15, 60, 240, 1440}

// minIndependent is the fewest non-overlapping observations a horizon must have
// before its sigma is allowed to influence a verdict.
//
// 30 is not a magic number from statistics; it is the point below which the
// standard error of a standard deviation exceeds ~13% of the estimate, which is
// larger than the 12% gap between the two sigmas the README already ships and
// treats as a real regime difference. Below that, this command cannot tell a
// volatility regime from noise, and should say so instead of guessing.
const minIndependent = 30

func main() {
	var (
		days   = flag.Int("days", 30, "days of M1 history to measure")
		asset  = flag.String("asset", "", "single asset (BTC|ETH); default both")
		net    = flag.String("net", "testnet", "testnet|mainnet")
		pages  = flag.Int("pages", 60, "max 1000-row pages per asset")
		tolPct = flag.Float64("tol", 15, "max %% deviation from base sigma for a horizon to be blessed")
	)
	flag.Parse()

	feed := venue.PriceFeedTestnet
	if *net == "mainnet" {
		feed = venue.PriceFeedMainnet
	}
	assets := []string{"BTC", "ETH"}
	if *asset != "" {
		assets = []string{*asset}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	c, err := venue.Dial(ctx, venue.ShannonRPC, venue.ShannonGQL)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	from := time.Now().Add(-time.Duration(*days) * 24 * time.Hour).Unix()
	fmt.Printf("volscale: %d days of M1 index candles, non-overlapping returns only\n", *days)
	fmt.Printf("blessing a horizon requires n >= %d and |dev| <= %.0f%%\n\n", minIndependent, *tolPct)

	var anyBlessedBeyond60 bool
	for _, a := range assets {
		cs, err := c.CandlesM1(ctx, feed, a, from, *pages)
		if err != nil {
			// A page cap reached mid-history means the NEWEST candles are
			// missing, so the sample is not the one that was asked for. Report
			// it and measure what arrived rather than pretending otherwise.
			log.Printf("%s: %v", a, err)
		}
		obs := toObs(cs)
		if len(obs) < 500 {
			fmt.Printf("%s: only %d usable candles; not measurable\n\n", a, len(obs))
			continue
		}
		span := float64(obs[len(obs)-1].t-obs[0].t) / 86400

		base, nBase := sigmaAt(obs, 1)
		fitted, hasFit := model.SigmaPerMinRaw(a, 900)

		fmt.Printf("%s -- %d candles spanning %.1f days\n", a, len(obs), span)
		fmt.Printf("  realised sigma/min (1m, n=%d) : %.6f\n", nBase, base)
		if hasFit {
			// The venue fit and the price-history fit are independent estimators
			// of the same quantity. Agreement is the licence for everything
			// below; disagreement would mean the venue fit is measuring
			// something that is not the diffusion of this price series.
			fmt.Printf("  venue-fitted sigma/min (15m)  : %.6f   ratio %.3f\n",
				fitted, base/fitted)
		} else {
			fmt.Printf("  venue-fitted sigma/min (15m)  : none\n")
		}

		fmt.Printf("\n  %-8s %8s %12s %9s  %s\n", "horizon", "n", "sigma/min", "dev", "verdict")
		for _, k := range horizons {
			s, n := sigmaAt(obs, k)
			if n == 0 || s == 0 {
				fmt.Printf("  %6dm %8d %12s %9s  no non-overlapping returns\n", k, n, "-", "-")
				continue
			}
			dev := 100 * (s/base - 1)
			var verdict string
			switch {
			case n < minIndependent:
				verdict = fmt.Sprintf("REFUSE - n<%d, cannot tell regime from noise", minIndependent)
			case math.Abs(dev) > *tolPct:
				verdict = fmt.Sprintf("REFUSE - sqrt(t) breaks by %.0f%%", dev)
			case k > 60:
				verdict = "QUOTABLE - sqrt(t) holds; widen sigma to the measured value"
				anyBlessedBeyond60 = true
			default:
				verdict = "already quoted"
			}
			fmt.Printf("  %6dm %8d %12.6f %+8.1f%%  %s\n", k, n, s, dev, verdict)
		}
		fmt.Println()
	}

	fmt.Println("READING THIS")
	fmt.Println("  A horizon whose sigma/min matches the 1-minute measurement is one where")
	fmt.Println("  sqrt(t) is not an assumption but an observation, and where a window may")
	fmt.Println("  be priced with the sigma measured AT that horizon -- never with the")
	fmt.Println("  1-minute value, which would understate it.")
	fmt.Println()
	fmt.Println("  A positive deviation means realised moves are LARGER than sqrt(t)")
	fmt.Println("  predicts, so pricing with the measured value yields LESS confident")
	fmt.Println("  probabilities. That is the safe direction: under-confidence forgoes")
	fmt.Println("  trades, over-confidence is what cost 37% of deployed capital.")
	if !anyBlessedBeyond60 {
		fmt.Println()
		fmt.Println("  Nothing beyond 60m was blessed. The engine's current refusal stands,")
		fmt.Println("  and extending coverage needs a longer sample, not a looser tolerance.")
		os.Exit(0)
	}
	fmt.Println()
	fmt.Println("  Horizons marked QUOTABLE can be added to internal/model with the sigma")
	fmt.Println("  measured at that horizon. Ones marked REFUSE stay refused: 30 days")
	fmt.Println("  contains 27 independent 24-hour returns and under one 45-day return, so")
	fmt.Println("  the long cadences are not short of a model, they are short of evidence.")
}

// ---- measurement ----------------------------------------------------------

type obsPoint struct {
	t  int64
	px float64
}

// toObs converts feed candles into a clean, ascending, deduplicated series.
//
// The candle's CLOSE is used, and every return below is taken between two
// closes exactly k*60 seconds apart. A gap in the feed therefore drops the
// return that spans it rather than silently stretching one return across a
// longer interval, which would inflate sigma at every horizon that gap touches.
func toObs(cs []venue.FeedCandle) []obsPoint {
	out := make([]obsPoint, 0, len(cs))
	for _, c := range cs {
		bs, ok := c.BucketStart.Float()
		if !ok {
			continue
		}
		px, ok := c.Close.Float()
		if !ok || px <= 0 {
			continue
		}
		// The feed reports 1e18-scaled prices; the absolute scale is irrelevant
		// to a log return, but keeping it human makes the series debuggable.
		out = append(out, obsPoint{t: int64(bs), px: px / 1e18})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].t < out[j].t })
	// Deduplicate on timestamp: a paged feed can repeat a boundary row, and a
	// duplicated close produces a spurious zero return that drags sigma down.
	ded := out[:0]
	var last int64 = -1
	for _, o := range out {
		if o.t == last {
			continue
		}
		ded = append(ded, o)
		last = o.t
	}
	return ded
}

// sigmaAt measures sigma per minute using non-overlapping k-minute returns,
// and returns the count of independent returns it used.
//
// Overlapping returns would multiply n by k while adding almost no independent
// information. Reporting that inflated n beside a tight standard deviation
// would make a 45-day horizon look measurable from 30 days of data, which is
// the specific false confidence this command exists to prevent.
func sigmaAt(obs []obsPoint, k int) (float64, int) {
	if k <= 0 || len(obs) <= k {
		return 0, 0
	}
	want := int64(60 * k)
	var rets []float64
	// Step by k, not by 1: consecutive windows must not share increments.
	for i := 0; i+k < len(obs); i += k {
		a, b := obs[i], obs[i+k]
		if b.t-a.t != want || a.px <= 0 || b.px <= 0 {
			continue
		}
		rets = append(rets, math.Log(b.px/a.px))
	}
	if len(rets) < 2 {
		return 0, len(rets)
	}
	// Population sigma about zero, not about the sample mean: the model this
	// feeds is explicitly driftless, so subtracting a fitted mean would remove
	// realised drift the pricing formula assumes is not there, and understate
	// the dispersion the formula must survive.
	var ss float64
	for _, r := range rets {
		ss += r * r
	}
	sd := math.Sqrt(ss / float64(len(rets)))
	return sd / math.Sqrt(float64(k)), len(rets)
}
