// Command volscale asks whether Firstbid may honestly quote the cadences it
// currently refuses.
//
// The engine skips 240-minute and longer windows because `calibrate` fits sigma
// from RESOLVED VENUE HISTORY, and those cadences have barely settled here. But
// "no resolved venue history" is not "no evidence": sigma is a property of the
// INDEX PRICE PROCESS, and 30 days of M1 candles can measure two things the
// venue's settlement log cannot.
//
//  1. Whether sigma from price history agrees with sigma fitted from settled
//     windows -- two independent estimators, one of which the engine already
//     trades on. Disagreement would mean the venue fit is measuring something
//     other than diffusion, and this whole argument is dead.
//
//  2. Whether sqrt(t) actually holds out to long horizons. Aggregating returns
//     to k minutes and dividing by sqrt(k) must give the same sigma/min at
//     every k. Where it stops doing so is where extrapolation stops being
//     licensed -- measured, not asserted.
//
// Non-overlapping returns only: overlapping ones share increments, so their
// standard deviation looks tight while carrying a fraction of the independent
// information. n is printed beside every figure, because a sigma from 27
// observations is not the same kind of evidence as one from 43,000.
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

// mainnetRPC matches the literal every other cmd in this repo uses.
const mainnetRPC = "https://api.infra.mainnet.somnia.network"

// minIndependent is the fewest non-overlapping observations a horizon needs
// before its sigma may influence a verdict.
//
// Below ~30 the standard error of a standard deviation exceeds ~13%, which is
// larger than the 12% gap between the two sigmas the README already treats as a
// real regime difference -- so below it we cannot tell regime from noise.
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

	// All three endpoints move together. CandlesM1 reads only the feed URL it
	// is handed, so a mainnet run that still dialled the Shannon testnet RPC
	// would fail on an endpoint it never reads from -- reporting "no candles"
	// for a mainnet feed that was healthy the whole time.
	rpc, gql, feed := venue.ShannonRPC, venue.ShannonGQL, venue.PriceFeedTestnet
	if *net == "mainnet" {
		rpc, gql, feed = mainnetRPC, venue.MainnetGQL, venue.PriceFeedMainnet
	}
	assets := []string{"BTC", "ETH"}
	if *asset != "" {
		assets = []string{*asset}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	c, err := venue.Dial(ctx, rpc, gql)
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
			// A page cap means the NEWEST candles are missing, so this is not
			// the sample that was asked for. Say so, then measure what arrived.
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
			// Two independent estimators of one quantity. Agreement is the
			// licence for everything below.
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
// Returns are taken between closes exactly k*60s apart, so a gap in the feed
// drops the return spanning it rather than stretching one across a longer
// interval -- which would inflate sigma at every horizon the gap touches.
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

// sigmaAt measures sigma/min from non-overlapping k-minute returns, and returns
// how many independent returns it used.
//
// Overlapping returns would multiply n by k while adding almost no information,
// making a 45-day horizon look measurable from 30 days of data.
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
	// Sigma about zero, not the sample mean: the model this feeds is explicitly
	// driftless, so subtracting a fitted mean would remove realised drift the
	// formula assumes is absent and understate what it must survive.
	var ss float64
	for _, r := range rets {
		ss += r * r
	}
	sd := math.Sqrt(ss / float64(len(rets)))
	return sd / math.Sqrt(float64(k)), len(rets)
}
