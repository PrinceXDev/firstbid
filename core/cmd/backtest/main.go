// Command backtest replays every resolved window against the fair-value model
// using only information available at each moment, then reports calibration.
//
// This is the honesty check on the model: if predicted probabilities do not
// match realised frequencies, the model is wrong and no amount of live PnL
// storytelling fixes that.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"github.com/firstbid/core/internal/model"
	"github.com/firstbid/core/internal/venue"
)

// sample points as a fraction of the window already elapsed
var fractions = []float64{0.20, 0.40, 0.60, 0.80, 0.90}

type pred struct {
	p      float64
	actual bool
	asset  string
	frac   float64
	// inputs retained so sigma can be refitted without refetching
	spot, open, sigma, secsLeft float64
	expiry                      int64
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	gql, feed := venue.MainnetGQL, venue.PriceFeedMainnet
	rpc := "https://api.infra.mainnet.somnia.network"
	if os.Getenv("FB_NET") == "testnet" {
		rpc, gql, feed = venue.ShannonRPC, venue.ShannonGQL, venue.PriceFeedTestnet
	}
	c, err := venue.Dial(ctx, rpc, gql)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	since := time.Now().Add(-30 * 24 * time.Hour).Unix()
	fmt.Print("loading resolved windows... ")
	obs, err := c.BuildCalibrationSet(ctx, since, 60)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d\n", len(obs))

	// earliest window we have, bounded to what the price feed retains
	earliest := int64(math.MaxInt64)
	for _, o := range obs {
		if o.TradingStart > 0 && o.TradingStart < earliest {
			earliest = o.TradingStart
		}
	}
	fmt.Print("loading M1 index candles... ")
	series := map[string]venue.SpotSeries{}
	for _, a := range []string{"BTC", "ETH"} {
		cs, err := c.CandlesM1(ctx, feed, a, earliest, 40)
		if err != nil {
			log.Printf("%s candles: %v", a, err)
		}
		series[a] = venue.BuildSpotSeries(cs)
		fmt.Printf("%s=%d ", a, len(series[a]))
	}
	fmt.Println()

	var preds []pred
	var skipped int
	for _, o := range obs {
		s, ok := series[o.Asset]
		if !ok || o.TradingStart <= 0 || o.Expiry <= o.TradingStart {
			skipped++
			continue
		}
		sigma, ok := model.SigmaPerMin(o.Asset, o.IntervalSec)
		if !ok {
			skipped++
			continue
		}
		dur := float64(o.Expiry - o.TradingStart)
		for _, f := range fractions {
			t := o.TradingStart + int64(dur*f)
			spot, ok := s.At(t)
			if !ok {
				continue
			}
			open := venue.NormaliseTo(o.Open, spot)
			secsLeft := float64(o.Expiry - t)
			if secsLeft <= 0 {
				continue
			}
			preds = append(preds, pred{
				p:        model.FairValue(spot, open, sigma, secsLeft),
				actual:   o.Up,
				asset:    o.Asset,
				frac:     f,
				spot:     spot,
				open:     open,
				sigma:    sigma,
				secsLeft: secsLeft,
				expiry:   o.Expiry,
			})
		}
	}
	fmt.Printf("predictions: %d   (windows skipped for missing candles: %d)\n\n", len(preds), skipped)
	if len(preds) == 0 {
		fmt.Println("no overlap between resolved windows and retained candle history")
		return
	}

	fmt.Println("=== BEFORE RECALIBRATION ===")
	reliability(preds)
	fmt.Println()
	scores(preds)

	// Chronological split: fit the multiplier on the older half only, then
	// evaluate on the newer half the fit has never seen. Fitting and scoring on
	// the same rows would make any k look good.
	sort.Slice(preds, func(i, j int) bool { return preds[i].expiry < preds[j].expiry })
	cut := len(preds) / 2
	train, test := preds[:cut], preds[cut:]
	kTrain := fitK(train)
	fmt.Println()
	fmt.Printf("train/test split: %d train (older) / %d test (newer)", len(train), len(test))
	fmt.Println()
	fmt.Printf("k fitted on TRAIN only = %.3f", kTrain)
	fmt.Println()
	applyK(test, kTrain)
	fmt.Println()
	fmt.Println("=== OUT-OF-SAMPLE (test half, k from train) ===")
	reliability(test)
	fmt.Println()
	scores(test)

	k := fitK(preds)
	fmt.Println()
	fmt.Printf("fitted volatility multiplier k = %.3f  (sigma_effective = k * sigma_fitted)", k)
	fmt.Println()
	for i := range preds {
		preds[i].p = model.FairValue(preds[i].spot, preds[i].open, preds[i].sigma*k, preds[i].secsLeft)
	}

	fmt.Println("=== AFTER RECALIBRATION ===")
	reliability(preds)
	fmt.Println()
	scores(preds)
	fmt.Println()
	byFraction(preds)

	if out := os.Getenv("FB_EXPORT"); out != "" {
		if err := export(out, preds, test, kTrain); err != nil {
			log.Printf("export: %v", err)
		} else {
			fmt.Println()
			fmt.Printf("wrote %s", out)
			fmt.Println()
		}
	}
}

// reliability buckets predictions and compares them to realised frequency.
// A calibrated model tracks the diagonal.
func reliability(ps []pred) {
	const nb = 10
	type b struct {
		n    int
		sump float64
		hits int
	}
	buckets := make([]b, nb)
	for _, x := range ps {
		i := int(x.p * nb)
		if i >= nb {
			i = nb - 1
		}
		if i < 0 {
			i = 0
		}
		buckets[i].n++
		buckets[i].sump += x.p
		if x.actual {
			buckets[i].hits++
		}
	}
	fmt.Println("RELIABILITY  (predicted vs realised)")
	fmt.Printf("%-12s %7s %10s %10s %8s\n", "bucket", "n", "predicted", "realised", "err")
	for i, bk := range buckets {
		if bk.n == 0 {
			continue
		}
		pp := bk.sump / float64(bk.n)
		rr := float64(bk.hits) / float64(bk.n)
		fmt.Printf("%-12s %7d %10.3f %10.3f %+8.3f\n",
			fmt.Sprintf("%.1f-%.1f", float64(i)/nb, float64(i+1)/nb), bk.n, pp, rr, rr-pp)
	}
}

// scores compares the model's Brier score against always-0.5.
func scores(ps []pred) {
	var bm, b5 float64
	for _, x := range ps {
		a := 0.0
		if x.actual {
			a = 1.0
		}
		bm += (x.p - a) * (x.p - a)
		b5 += (0.5 - a) * (0.5 - a)
	}
	n := float64(len(ps))
	bm /= n
	b5 /= n
	fmt.Println("SKILL")
	fmt.Printf("  Brier (model)      : %.4f\n", bm)
	fmt.Printf("  Brier (always 0.5) : %.4f\n", b5)
	fmt.Printf("  skill score        : %+.2f%%   (positive = model beats a coin flip)\n", 100*(1-bm/b5))
}

func byFraction(ps []pred) {
	g := map[float64][]pred{}
	for _, x := range ps {
		g[x.frac] = append(g[x.frac], x)
	}
	var fs []float64
	for f := range g {
		fs = append(fs, f)
	}
	sort.Float64s(fs)
	fmt.Println("SKILL BY WINDOW PROGRESS")
	fmt.Printf("%-10s %8s %10s %10s\n", "elapsed", "n", "brier", "skill")
	for _, f := range fs {
		var bm, b5 float64
		for _, x := range g[f] {
			a := 0.0
			if x.actual {
				a = 1.0
			}
			bm += (x.p - a) * (x.p - a)
			b5 += (0.5 - a) * (0.5 - a)
		}
		n := float64(len(g[f]))
		fmt.Printf("%-10s %8d %10.4f %9.1f%%\n",
			fmt.Sprintf("%.0f%%", f*100), len(g[f]), bm/n, 100*(1-(bm/n)/(b5/n)))
	}
}

// fitK finds the single volatility multiplier that minimises Brier score.
// One free parameter over 14k observations is recalibration, not overfitting:
// it corrects a scale error in sigma, it cannot invent signal.
func fitK(ps []pred) float64 {
	best, bestB := 1.0, math.Inf(1)
	for k := 0.20; k <= 2.0; k += 0.005 {
		var b float64
		for _, x := range ps {
			a := 0.0
			if x.actual {
				a = 1.0
			}
			p := model.FairValue(x.spot, x.open, x.sigma*k, x.secsLeft)
			b += (p - a) * (p - a)
		}
		if b < bestB {
			bestB, best = b, k
		}
	}
	return best
}

func applyK(ps []pred, k float64) {
	for i := range ps {
		ps[i].p = model.FairValue(ps[i].spot, ps[i].open, ps[i].sigma*k, ps[i].secsLeft)
	}
}

// Report is the backtest artifact the dashboard renders. Exporting it keeps the
// UI honest: it displays measured results rather than recomputing them live.
type Report struct {
	GeneratedAt string        `json:"generatedAt"`
	K           float64       `json:"k"`
	TrainN      int           `json:"trainN"`
	TestN       int           `json:"testN"`
	BrierModel  float64       `json:"brierModel"`
	BrierBase   float64       `json:"brierBaseline"`
	Skill       float64       `json:"skill"`
	Buckets     []BucketOut   `json:"buckets"`
	ByProgress  []ProgressOut `json:"byProgress"`
	Coverage    []CoverageOut `json:"coverage"`
}

type BucketOut struct {
	Lo        float64 `json:"lo"`
	Hi        float64 `json:"hi"`
	N         int     `json:"n"`
	Predicted float64 `json:"predicted"`
	Realised  float64 `json:"realised"`
}

type ProgressOut struct {
	Elapsed float64 `json:"elapsed"`
	N       int     `json:"n"`
	Brier   float64 `json:"brier"`
	Skill   float64 `json:"skill"`
}

type CoverageOut struct {
	Asset       string  `json:"asset"`
	SigmaPerMin float64 `json:"sigmaPerMin"`
	N           int     `json:"n"`
}

func export(path string, all, test []pred, k float64) error {
	const nb = 10
	type b struct {
		n    int
		sump float64
		hits int
	}
	buckets := make([]b, nb)
	for _, x := range test {
		i := int(x.p * nb)
		if i >= nb {
			i = nb - 1
		}
		if i < 0 {
			i = 0
		}
		buckets[i].n++
		buckets[i].sump += x.p
		if x.actual {
			buckets[i].hits++
		}
	}
	r := Report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		K:           k,
		TrainN:      len(all) - len(test),
		TestN:       len(test),
	}
	var bm, b5 float64
	for _, x := range test {
		a := 0.0
		if x.actual {
			a = 1.0
		}
		bm += (x.p - a) * (x.p - a)
		b5 += (0.5 - a) * (0.5 - a)
	}
	n := float64(len(test))
	r.BrierModel, r.BrierBase = bm/n, b5/n
	r.Skill = 100 * (1 - r.BrierModel/r.BrierBase)

	for i, bk := range buckets {
		if bk.n == 0 {
			continue
		}
		r.Buckets = append(r.Buckets, BucketOut{
			Lo: float64(i) / nb, Hi: float64(i+1) / nb, N: bk.n,
			Predicted: bk.sump / float64(bk.n),
			Realised:  float64(bk.hits) / float64(bk.n),
		})
	}

	g := map[float64][]pred{}
	for _, x := range test {
		g[x.frac] = append(g[x.frac], x)
	}
	var fs []float64
	for f := range g {
		fs = append(fs, f)
	}
	sort.Float64s(fs)
	for _, f := range fs {
		var m, base float64
		for _, x := range g[f] {
			a := 0.0
			if x.actual {
				a = 1.0
			}
			m += (x.p - a) * (x.p - a)
			base += (0.5 - a) * (0.5 - a)
		}
		cnt := float64(len(g[f]))
		r.ByProgress = append(r.ByProgress, ProgressOut{
			Elapsed: f * 100, N: len(g[f]),
			Brier: m / cnt, Skill: 100 * (1 - (m/cnt)/(base/cnt)),
		})
	}
	for _, v := range model.Coverage() {
		r.Coverage = append(r.Coverage, CoverageOut{v.Asset, v.SigmaPerMin, v.N})
	}

	blob, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, blob, 0o644)
}
