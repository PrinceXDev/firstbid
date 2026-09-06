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
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
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
	// window identifies the resolved market this row came from. Every fraction
	// of one window shares a single outcome, so the train/test boundary has to
	// fall BETWEEN windows: cutting through one would let the same outcome both
	// fit the calibration and score it.
	window int
}

func main() {
	feedMode := flag.String("feed", "points",
		"index-price series to replay: \"points\" (1s PricePoint, what the engine reads) or \"candles\" (M1)")
	window := flag.Duration("window", 0,
		"how much history to replay (default 24h for points, 720h for candles)")
	flag.Parse()

	if *feedMode != "points" && *feedMode != "candles" {
		log.Fatalf("-feed must be \"points\" or \"candles\", got %q", *feedMode)
	}
	if *window <= 0 {
		if *feedMode == "points" {
			*window = 24 * time.Hour
		} else {
			*window = 720 * time.Hour
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
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

	since := time.Now().Add(-*window).Unix()
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
	if earliest == int64(math.MaxInt64) {
		// No observation carried a trading start; fall back to the requested
		// window rather than handing MaxInt64 to the feed and to time.Unix.
		earliest = since
	}
	// Both builders stamp each observation with the time it became knowable, so
	// SpotSeries.At can never hand back a price from after the decision moment.
	// "points" replays the very table the live engine polls; "candles" is the
	// cheaper, coarser series kept for cross-checking.
	// Page caps are derived from the window actually requested rather than
	// hard-coded, so a longer -window cannot quietly return a shorter replay.
	// Both feeds page 1,000 rows at a time; the spare pages absorb gaps and the
	// time that passes between computing `earliest` and finishing the walk.
	span := time.Since(time.Unix(earliest, 0))
	if span < *window {
		span = *window
	}
	pointPages := int(span.Seconds()/1000) + 16  // ~1 point per second
	candlePages := int(span.Minutes()/1000) + 16 // 1 candle per minute

	fmt.Printf("loading index prices (%s, %s)... ", *feedMode, window.String())
	series := map[string]venue.SpotSeries{}
	for _, a := range []string{"BTC", "ETH"} {
		if *feedMode == "points" {
			ps, err := c.PricePoints(ctx, feed, a, earliest, pointPages)
			if err != nil {
				// A truncated series is not a degraded run, it is a different
				// run wearing this one's label: the newest history is missing
				// while every printed number still claims the full window.
				if errors.Is(err, venue.ErrPageCapReached) {
					log.Fatalf("%s price points: %v -- the replay would be silently short; "+
						"shorten -window or raise the page cap", a, err)
				}
				log.Printf("%s price points: %v", a, err)
			}
			series[a] = venue.BuildSpotSeriesFromPoints(ps)
		} else {
			cs, err := c.CandlesM1(ctx, feed, a, earliest, candlePages)
			if err != nil {
				if errors.Is(err, venue.ErrPageCapReached) {
					log.Fatalf("%s candles: %v -- the replay would be silently short; "+
						"shorten -window or raise the page cap", a, err)
				}
				log.Printf("%s candles: %v", a, err)
			}
			series[a] = venue.BuildSpotSeries(cs)
		}
		fmt.Printf("%s=%d ", a, series[a].Len())
	}
	fmt.Println()

	var preds []pred
	var skipped int
	for wi, o := range obs {
		s, ok := series[o.Asset]
		if !ok || o.TradingStart <= 0 || o.Expiry <= o.TradingStart {
			skipped++
			continue
		}
		// Raw, not calibrated: fitK reports the TOTAL multiplier, and
		// "BEFORE" below means the model as it ships (raw * model.Calibration).
		sigma, ok := model.SigmaPerMinRaw(o.Asset, o.IntervalSec)
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
				p:        model.FairValue(spot, open, sigma*model.Calibration, secsLeft),
				actual:   o.Up,
				asset:    o.Asset,
				frac:     f,
				spot:     spot,
				open:     open,
				sigma:    sigma,
				secsLeft: secsLeft,
				expiry:   o.Expiry,
				window:   wi,
			})
		}
	}
	fmt.Printf("predictions: %d   (windows skipped for missing candles: %d)\n\n", len(preds), skipped)
	if len(preds) == 0 {
		fmt.Println("no overlap between resolved windows and retained candle history")
		return
	}

	fmt.Printf("=== AS SHIPPED (k = model.Calibration = %.3f) ===\n", model.Calibration)
	reliability(preds)
	fmt.Println()
	scores(preds)

	// Chronological split: fit the multiplier on the older half only, then
	// evaluate on the newer half the fit has never seen. Fitting and scoring on
	// the same rows would make any k look good.
	//
	// The boundary is placed between WINDOWS, not between rows. Each window
	// contributes one row per sampled fraction, all carrying that window's
	// single outcome; a row-level cut at len/2 would put some of a window's
	// rows in train and the rest in test, so the same coin flip would both fit
	// the calibration and appear in the score that is supposed to hold it to
	// account. That is a leak, and a leak flatters exactly the number this
	// command exists to keep honest.
	sort.SliceStable(preds, func(i, j int) bool {
		if preds[i].expiry != preds[j].expiry {
			return preds[i].expiry < preds[j].expiry
		}
		return preds[i].window < preds[j].window
	})
	cut := splitAtWindowBoundary(preds, len(preds)/2)
	train, test := preds[:cut], preds[cut:]
	if len(train) == 0 || len(test) == 0 {
		fmt.Println()
		fmt.Println("not enough distinct resolved windows for a leak-free train/test split")
		return
	}
	kTrain := fitK(train)
	fmt.Println()
	fmt.Printf("train/test split: %d train (older) / %d test (newer), split on whole windows",
		len(train), len(test))
	fmt.Println()
	fmt.Printf("k fitted on TRAIN only = %.3f", kTrain)
	fmt.Println()
	applyK(test, kTrain)
	fmt.Println()
	fmt.Println("=== OUT-OF-SAMPLE (test half, k from train) ===")
	reliability(test)
	fmt.Println()
	scores(test)

	// ---- calibration map ----------------------------------------------
	// A single sigma multiplier moves the reliability curve; it cannot change
	// its shape. The honest curve is S-shaped, so residual overconfidence above
	// 0.5 survives any k. Fit a monotone map on the older half and score it on
	// the newer half, exactly as with k.
	trainP, trainW := shippedPairs(train)
	testP, testW := shippedPairs(test)

	minBin := len(trainP) / 12
	if minBin < 60 {
		minBin = 60
	}
	cmap, mapErr := model.FitIsotonic(trainP, trainW, minBin)
	if mapErr != nil {
		fmt.Println()
		fmt.Printf("calibration map: not fitted (%v)", mapErr)
		fmt.Println()
	} else {
		mappedTest := make([]float64, len(testP))
		for i, x := range testP {
			mappedTest[i] = cmap.Apply(x)
		}
		rawB := model.Brier(testP, testW)
		calB := model.Brier(mappedTest, testW)

		fmt.Println()
		fmt.Printf("=== CALIBRATION MAP (isotonic, %d knots, minBin=%d, fitted on TRAIN) ===",
			len(cmap.Knots()), minBin)
		fmt.Println()
		fmt.Printf("%-10s %8s %14s %10s\n", "raw p", "n", "-> calibrated", "residual")
		for _, kn := range cmap.Knots() {
			fmt.Printf("%-10.3f %8d %14.3f %10.3f\n", kn.Raw, kn.N, kn.Cal, kn.Raw-kn.Cal)
		}
		fmt.Println()
		fmt.Println("OUT-OF-SAMPLE reliability error per decile (0.0-0.1 .. 0.9-1.0)")
		reliabilityOf(testP, testW, "  raw   ")
		reliabilityOf(mappedTest, testW, "  mapped")
		fmt.Println()
		fmt.Printf("  Brier raw      : %.4f  (skill %+.2f%%)\n", rawB, 100*(1-rawB/0.25))
		fmt.Printf("  Brier mapped   : %.4f  (skill %+.2f%%)\n", calB, 100*(1-calB/0.25))
		// FB_MAP writes these knots straight into the source the engine prices
		// through, so the export is gated on the same test that decides whether
		// the map ships at all. Otherwise the documented regeneration command
		// would promote a rejected experiment into production, which is the
		// dishonesty this command exists to prevent.
		if calB < rawB {
			fmt.Printf("  the map earns its place: %.1f%% lower Brier out of sample\n",
				100*(1-calB/rawB))
			if err := exportMap(os.Getenv("FB_MAP"), cmap); err != nil {
				log.Printf("export map: %v", err)
			}
		} else {
			fmt.Printf("  the map does NOT improve out of sample; do not ship it\n")
			if os.Getenv("FB_MAP") != "" {
				fmt.Printf("  FB_MAP is set but NOT written: refusing to ship a map that lost its own test\n")
			}
		}
	}

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

// splitAtWindowBoundary moves `want` to the nearest index where the window
// changes, so no resolved market straddles the train/test boundary.
//
// ps must already be grouped by window (the chronological sort above keeps each
// window's rows contiguous). It walks forward to the next boundary, and if that
// would leave nothing to test on, walks backward instead.
func splitAtWindowBoundary(ps []pred, want int) int {
	if want <= 0 || want >= len(ps) {
		return want
	}
	fwd := want
	for fwd < len(ps) && ps[fwd].window == ps[fwd-1].window {
		fwd++
	}
	if fwd < len(ps) {
		return fwd
	}
	back := want
	for back > 0 && ps[back].window == ps[back-1].window {
		back--
	}
	return back
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

// export writes the artifact the dashboard renders.
//
// Every probability here is RECOMPUTED from each prediction's stored inputs at
// the given k, never read from pred.p. The in-place refits in main() share a
// backing array with `test`, so reading pred.p would export buckets computed at
// the full-set k while labelling them with the train-fitted one -- a number that
// does not match its own claim, which is the exact failure this whole build is
// recovering from.
func export(path string, all, test []pred, k float64) error {
	const nb = 10
	type b struct {
		n    int
		sump float64
		hits int
	}
	at := func(x pred) float64 {
		return model.FairValue(x.spot, x.open, x.sigma*k, x.secsLeft)
	}
	buckets := make([]b, nb)
	for _, x := range test {
		i := int(at(x) * nb)
		if i >= nb {
			i = nb - 1
		}
		if i < 0 {
			i = 0
		}
		buckets[i].n++
		buckets[i].sump += at(x)
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
		p := at(x)
		bm += (p - a) * (p - a)
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
			p := at(x)
			m += (p - a) * (p - a)
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

// shippedPairs recomputes the probabilities the ENGINE would have produced --
// raw sigma scaled by model.Calibration -- from each prediction's stored
// inputs. Recomputing rather than reading pred.p keeps this immune to the
// in-place refits above.
func shippedPairs(ps []pred) ([]float64, []bool) {
	out := make([]float64, len(ps))
	win := make([]bool, len(ps))
	for i, x := range ps {
		out[i] = model.FairValue(x.spot, x.open, x.sigma*model.Calibration, x.secsLeft)
		win[i] = x.actual
	}
	return out, win
}

// reliabilityOf prints one compact row of per-decile calibration error.
func reliabilityOf(ps []float64, wins []bool, tag string) {
	const nb = 10
	var n [nb]int
	var sp, hit [nb]float64
	for i, p := range ps {
		b := int(p * nb)
		if b >= nb {
			b = nb - 1
		}
		if b < 0 {
			b = 0
		}
		n[b]++
		sp[b] += p
		if wins[i] {
			hit[b]++
		}
	}
	fmt.Printf("%s ", tag)
	for b := 0; b < nb; b++ {
		if n[b] == 0 {
			fmt.Printf("  %4s", "-")
			continue
		}
		fmt.Printf(" %+5.3f", hit[b]/float64(n[b])-sp[b]/float64(n[b]))
	}
	fmt.Println()
}

// exportMap writes the fitted knots as compilable Go, so the map ships as
// reviewed source rather than a file the binary must find at runtime.
func exportMap(path string, m model.Map) error {
	if path == "" {
		return nil
	}
	var b strings.Builder
	b.WriteString("// Code generated by cmd/backtest -- DO NOT EDIT BY HAND.\n")
	b.WriteString("// Regenerate:\n")
	b.WriteString("//   FB_MAP=internal/model/calibmap_fitted.go go run ./cmd/backtest\n")
	b.WriteString("//\n")
	b.WriteString("// Isotonic calibration map fitted on the older half of replayed\n")
	b.WriteString("// predictions against the 1s index feed the engine trades, and scored\n")
	b.WriteString("// on the newer half it never saw.\n\n")
	b.WriteString("package model\n\n")
	b.WriteString("var fittedKnots = []Knot{\n")
	for _, k := range m.Knots() {
		fmt.Fprintf(&b, "	{Raw: %.6f, Cal: %.6f, N: %d},\n", k.Raw, k.Cal, k.N)
	}
	b.WriteString("}\n")
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
