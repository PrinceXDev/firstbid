// Command surface answers one question, and is built to answer it "no".
//
// Firstbid prices each window against a model. Every take therefore depends on
// the model being right, and docs/AUTOPSY.md is what that dependency cost when
// it was wrong. The obvious next question is whether this venue offers trades
// that are profitable regardless of whether our model is right.
//
// It might. DreamDEX runs several windows on ONE underlying at the same time --
// ob.json shows 900s, 3600s, 14400s and 86400s cadences co-existing for both
// BTC and ETH -- and those windows are not independent. They are all functions
// of the same spot process. That imposes relations between their prices which
// hold under EVERY probability measure, so a violation is riskless profit
// rather than a view.
//
// The relation this command measures is the vertical one, because it is the
// only one that is genuinely model-free:
//
//	Two windows on the same asset expiring at the SAME second, with strikes
//	K1 < K2, must satisfy P(Up | K1) >= P(Up | K2). Up at the lower strike
//	needs less to happen, so it is worth at least as much, always.
//
// If the book lets us buy Up(K1) for less than we can sell Up(K2), the pair
// pays >= 0 at settlement and cost < 0 to enter. That is locked, and no belief
// about sigma or direction enters the argument.
//
// # WHAT THIS COMMAND DELIBERATELY DOES NOT DO
//
// It does not trade, and it does not fit anything. It records a census. The
// thesis has a specific, cheap way to die: if same-expiry pairs almost never
// co-exist on this venue, there is nothing to measure and the strategy is
// vapour. The census is printed before the violations for exactly that reason
// -- a pair count near zero is the finding, and no amount of violation
// arithmetic afterwards would rescue it.
//
// It also reports two softer signals, clearly separated from the hard one
// because they are NOT arbitrage and must never be presented as such:
//
//   - crossed books, which are the intra-market box (Up + Down < 1) and, on a
//     single-block read, usually mean the book really was crossed rather than
//     that we straddled a block boundary.
//   - dispersion of the sigma implied by each cadence's mid. The README
//     measured sigma/min to be stable across a 12x range of window lengths, so
//     wide dispersion is a mispricing signal -- but reading it requires
//     believing the diffusion model, which is the dependency we are trying to
//     escape.
//
// Every poll is written to JSONL so the verdict can be recomputed from the
// record instead of trusted, and so a later run can be compared against this
// one rather than replacing it.
//
// Usage:
//
//	go run ./cmd/surface                       # 30 minutes, 5s polls
//	go run ./cmd/surface -for=4h -poll=10s     # an overnight census
//	go run ./cmd/surface -for=0                # one poll, then exit
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/firstbid/core/internal/model"
	"github.com/firstbid/core/internal/venue"
)

func main() {
	var (
		poll   = flag.Duration("poll", 5*time.Second, "seconds between snapshots")
		runFor = flag.Duration("for", 30*time.Minute, "total measurement duration; 0 means a single poll")
		depth  = flag.Uint64("depth", 5, "book levels to read per side")
		outPth = flag.String("out", "", "JSONL output path (default docs/surface-census.jsonl)")
		fee    = flag.Float64("fee", 0.0, "round-trip cost in probability units, subtracted from every gross gap")
		net    = flag.String("net", "testnet", "testnet|mainnet")
		quiet  = flag.Bool("quiet", false, "suppress the per-poll line; print only the summary")
	)
	flag.Parse()

	rpc, gql, feed := venue.ShannonRPC, venue.ShannonGQL, venue.PriceFeedTestnet
	if *net == "mainnet" {
		rpc, gql, feed = "https://api.infra.mainnet.somnia.network", venue.MainnetGQL, venue.PriceFeedMainnet
	}

	out := *outPth
	if out == "" {
		out = filepath.Join("..", "docs", "surface-census.jsonl")
	}
	// Append, never truncate. A census whose earlier runs are silently
	// overwritten cannot be compared against itself, and comparing runs is the
	// whole point of writing it down.
	f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("open %s: %v", out, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	// The deadline bounds the whole census; each poll gets its own shorter
	// context so one unresponsive RPC cannot consume the entire run.
	root := context.Background()
	deadline := time.Now().Add(*runFor)

	c, err := venue.Dial(root, rpc, gql)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	fmt.Printf("surface census -> %s\n", out)
	fmt.Printf("net=%s poll=%v for=%v depth=%d fee=%.4f\n\n", *net, *poll, *runFor, *depth, *fee)
	if !*quiet {
		fmt.Printf("%-8s %5s %5s %6s %6s %6s %6s %8s %8s\n",
			"time", "legs", "xpry", "same", "diff", "viol", "cross", "grossmax", "netmax")
	}

	agg := newAggregate(*fee)
	for {
		pctx, cancel := context.WithTimeout(root, 90*time.Second)
		snap, err := takeSnapshot(pctx, c, feed, *depth)
		cancel()
		if err != nil {
			// A failed poll is a gap in the record, not a reason to stop: the
			// indexer and RPC both flake, and a census that dies on the first
			// timeout measures nothing.
			log.Printf("poll failed: %v", err)
		} else {
			snap.Fee = *fee
			agg.add(snap)
			if err := enc.Encode(snap); err != nil {
				log.Printf("write: %v", err)
			}
			if !*quiet {
				printPoll(snap, *fee)
			}
		}
		if time.Now().Add(*poll).After(deadline) {
			break
		}
		time.Sleep(*poll)
	}

	fmt.Println()
	agg.report()
}

// ---- one snapshot of the whole live cross-section -------------------------

// leg is one live window reduced to the only things a relative-value check
// needs: what it pays on, when it settles, and what it can be traded at.
//
// Bid and Ask are in Up terms and in probability units. Up and Down share a
// single book quoted in Up terms, so buying Down at q is selling Up at 1-q --
// which is why one Up-terms bid/ask pair is a complete two-sided tradable
// quote and no separate Down book is read.
type leg struct {
	Series      string  `json:"series"`
	RowID       string  `json:"rowId"`
	MarketID    string  `json:"marketId"`
	Asset       string  `json:"asset"`
	IntervalSec int64   `json:"intervalSec"`
	Expiry      int64   `json:"expiry"`
	SecsLeft    float64 `json:"secsLeft"`
	Strike      float64 `json:"strike"`
	Spot        float64 `json:"spot"`
	Bid         float64 `json:"bid"`
	Ask         float64 `json:"ask"`
	BidQty      float64 `json:"bidQty"`
	AskQty      float64 `json:"askQty"`
	Crossed     bool    `json:"crossed"`
	// ImpliedSigma is sigma/min backed out of the mid, or 0 when the mid is
	// too close to 0.5 (or spot too close to strike) for the inversion to carry
	// information. Model-relative, and never used in the hard verdict.
	ImpliedSigma float64 `json:"impliedSigma,omitempty"`
	// FittedSigma is what calibrate fitted for this series, 0 if uncalibrated.
	// The 14400s and 86400s cadences have no resolved history, so the engine
	// refuses to quote them -- they are exactly the windows a relative-value
	// price would reach that the model cannot.
	FittedSigma float64 `json:"fittedSigma,omitempty"`
}

// violation is one model-free vertical inconsistency, expressed as the trade
// that captures it.
type violation struct {
	Expiry   int64   `json:"expiry"`
	Asset    string  `json:"asset"`
	BuyUp    string  `json:"buyUp"`  // series of the lower strike, bought at its ask
	SellUp   string  `json:"sellUp"` // series of the higher strike, sold at its bid
	StrikeLo float64 `json:"strikeLo"`
	StrikeHi float64 `json:"strikeHi"`
	Ask      float64 `json:"ask"`
	Bid      float64 `json:"bid"`
	// Gross is bid - ask: profit per unit locked at settlement, before costs.
	Gross float64 `json:"gross"`
	// Size is the depth available on the thinner of the two levels. A gap with
	// no size behind it is a screenshot, not a trade.
	Size float64 `json:"size"`
	// Identical marks the degenerate case: same expiry AND same strike, so the
	// two windows are the same contract under different marketIds and any gap
	// between them is unambiguous.
	Identical bool `json:"identical"`
}

// pairCensus counts which relations actually co-exist. This is the number that
// decides whether the strategy exists at all.
type pairCensus struct {
	// SameExpiry pairs are the only ones the model-free rule applies to.
	SameExpiry int `json:"sameExpiry"`
	// Identical: same expiry and same strike -- the strongest case.
	Identical int `json:"identical"`
	// SameStrike, different expiry: no model-free inequality holds here for a
	// driftless walk, so these are counted but never scored as arbitrage.
	SameStrikeDiffExpiry int `json:"sameStrikeDiffExpiry"`
	// Disjoint: different strike and different expiry. Only reachable with the
	// model, which is the dependency this command exists to avoid.
	Disjoint int `json:"disjoint"`
}

type snapshot struct {
	At         int64                 `json:"at"`
	Legs       []leg                 `json:"legs"`
	Census     map[string]pairCensus `json:"census"`
	Violations []violation           `json:"violations"`
	Crossed    int                   `json:"crossed"`
	SpotAgeMS  int64                 `json:"spotAgeMs"`
	Fee        float64               `json:"fee"`
}

// takeSnapshot reads the entire live cross-section once.
//
// Chain is truth for everything traded on: the indexer's status column trails
// the timestamp-derived on-chain state, so every leg is confirmed Trading via
// ReadState before it is allowed to contribute a price. A leg admitted on the
// indexer's word could be Locked, and a "violation" against a locked window is
// not tradable.
func takeSnapshot(ctx context.Context, c *venue.Client, feed string, depth uint64) (*snapshot, error) {
	spots, err := c.Spots(ctx, feed)
	if err != nil {
		return nil, fmt.Errorf("spots: %w", err)
	}
	live, err := c.DiscoverLive(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	ids := make([]string, 0, len(live))
	for _, m := range live {
		ids = append(ids, m.RowID)
	}
	opens, err := c.OpeningPrices(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("opens: %w", err)
	}

	now := time.Now().Unix()
	snap := &snapshot{At: now, Census: map[string]pairCensus{}}
	if sp, ok := spots["BTC"]; ok {
		snap.SpotAgeMS = sp.Age.Milliseconds()
	}

	for _, im := range live {
		sp, ok := spots[im.Asset]
		if !ok {
			continue
		}
		open, ok := opens[im.RowID]
		if !ok {
			// No resolved reference answer yet. The window has no line to beat
			// that we can read, so it has no strike and cannot be compared.
			continue
		}
		m, err := c.ReadMarket(ctx, im.ID())
		if err != nil {
			continue
		}
		st, err := c.ReadState(ctx, m.MarketAddr)
		if err != nil || st.Status != venue.StatusTrading {
			continue
		}
		secsLeft := float64(int64(m.Expiry) - now)
		if secsLeft <= 0 {
			continue
		}

		// Both sides at ONE block. Two unpinned reads can straddle a block
		// boundary and manufacture a crossed book that never existed -- and a
		// phantom cross would be recorded here as a free intra-market box.
		bk, err := c.ReadBook(ctx, m.Pool, depth)
		if err != nil {
			continue
		}
		bid := lvlPx(bk.BestBid(), im.QuoteDec)
		ask := lvlPx(bk.BestAsk(), im.QuoteDec)
		if bid <= 0 || ask <= 0 {
			// One-sided or empty. 80.8% of this venue's markets never trade, so
			// this is the common case and not an error -- but a leg that can
			// only be traded in one direction cannot close a two-leg box.
			continue
		}

		strike := venue.NormaliseTo(open, sp.Price)
		l := leg{
			Series:      fmt.Sprintf("%s/%dm", im.Asset, im.IntervalSecs()/60),
			RowID:       im.RowID,
			MarketID:    im.MarketID,
			Asset:       im.Asset,
			IntervalSec: im.IntervalSecs(),
			Expiry:      int64(m.Expiry),
			SecsLeft:    secsLeft,
			Strike:      strike,
			Spot:        sp.Price,
			Bid:         bid,
			Ask:         ask,
			BidQty:      qtyAt(bk.Bids, im.QuoteDec),
			AskQty:      qtyAt(bk.Asks, im.QuoteDec),
			Crossed:     bid > ask,
		}
		if l.Crossed {
			snap.Crossed++
		}
		if s, ok := model.SigmaPerMin(im.Asset, im.IntervalSecs()); ok {
			l.FittedSigma = s
		}
		l.ImpliedSigma = impliedSigma(sp.Price, strike, (bid+ask)/2, secsLeft)
		snap.Legs = append(snap.Legs, l)
	}

	// Census and violations are computed per asset: windows on different
	// underlyings share no state and no relation.
	byAsset := map[string][]leg{}
	for _, l := range snap.Legs {
		byAsset[l.Asset] = append(byAsset[l.Asset], l)
	}
	for asset, ls := range byAsset {
		cen, vs := scanAsset(ls)
		snap.Census[asset] = cen
		snap.Violations = append(snap.Violations, vs...)
	}
	sort.Slice(snap.Violations, func(i, j int) bool {
		return snap.Violations[i].Gross > snap.Violations[j].Gross
	})
	return snap, nil
}

// strikeEps is how close two strikes must be to count as the same contract.
// Strikes are index prices in the tens of thousands, and the oracle's answer is
// a fixed-point value whose scale varies by question, so an exact float compare
// would miss genuine duplicates. One part in 100,000 is far tighter than any
// price move inside a window and far looser than float noise.
const strikeEps = 1e-5

// scanAsset counts the relations present among one asset's live windows and
// returns every model-free vertical violation among them.
func scanAsset(ls []leg) (pairCensus, []violation) {
	var cen pairCensus
	var out []violation

	for i := 0; i < len(ls); i++ {
		for j := i + 1; j < len(ls); j++ {
			a, b := ls[i], ls[j]
			sameExpiry := a.Expiry == b.Expiry
			sameStrike := relClose(a.Strike, b.Strike, strikeEps)

			switch {
			case sameExpiry && sameStrike:
				cen.Identical++
				cen.SameExpiry++
			case sameExpiry:
				cen.SameExpiry++
			case sameStrike:
				cen.SameStrikeDiffExpiry++
				continue
			default:
				cen.Disjoint++
				continue
			}

			// Same expiry from here. Order the pair so lo is the strike that
			// needs less to happen, and is therefore worth at least as much.
			lo, hi := a, b
			if hi.Strike < lo.Strike {
				lo, hi = hi, lo
			}

			// Buy the cheap one, sell the dear one. Selling Up on `hi` means
			// resting a Buy Down at 1-bid that crosses hi's bid; the pool mints
			// the pair, so no inventory is needed to be short. That mechanism
			// is real on this venue -- MINT_A_PAIR was 22-42% of all fills.
			if v, ok := verticalArb(lo, hi, sameStrike); ok {
				out = append(out, v)
			}
			// When the strikes are identical the inequality is an equality, so
			// the reverse direction is an arbitrage too and must be checked.
			if sameStrike {
				if v, ok := verticalArb(hi, lo, true); ok {
					out = append(out, v)
				}
			}
		}
	}
	return cen, out
}

// verticalArb tests buying Up on `lo` and selling Up on `hi`.
//
// The payoff of that pair is Up(K_lo) - Up(K_hi), which is 1 when spot finishes
// between the strikes and 0 otherwise -- never negative, because lo needs less
// to happen than hi. So any positive bid_hi - ask_lo is profit locked at
// settlement under every probability measure.
func verticalArb(lo, hi leg, identical bool) (violation, bool) {
	gross := hi.Bid - lo.Ask
	if gross <= 0 {
		return violation{}, false
	}
	return violation{
		Expiry:    lo.Expiry,
		Asset:     lo.Asset,
		BuyUp:     lo.Series,
		SellUp:    hi.Series,
		StrikeLo:  lo.Strike,
		StrikeHi:  hi.Strike,
		Ask:       lo.Ask,
		Bid:       hi.Bid,
		Gross:     gross,
		Size:      math.Min(lo.AskQty, hi.BidQty),
		Identical: identical,
	}, true
}

// impliedSigma inverts FairValue for sigma/min.
//
// FairValue is Phi(ln(spot/strike) / (sigma*sqrt(minutes))), so
// sigma = ln(spot/strike) / (Phi^-1(mid) * sqrt(minutes)). Near mid = 0.5 the
// inverse normal goes to zero and the quotient explodes; near spot = strike the
// numerator does the same. Both are returned as 0 rather than as a huge number,
// because a sigma of 40 in the record would be read as a signal when it is
// really a division by almost nothing.
func impliedSigma(spot, strike, mid, secsLeft float64) float64 {
	if spot <= 0 || strike <= 0 || secsLeft <= 0 {
		return 0
	}
	if mid <= 0.01 || mid >= 0.99 {
		return 0
	}
	z := probit(mid)
	if math.Abs(z) < 0.15 {
		return 0
	}
	num := math.Log(spot / strike)
	if math.Abs(num) < 1e-9 {
		return 0
	}
	s := num / (z * math.Sqrt(secsLeft/60))
	if s <= 0 || math.IsNaN(s) || math.IsInf(s, 0) {
		return 0
	}
	return s
}

// ---- aggregation across the whole run -------------------------------------

type aggregate struct {
	fee                 float64
	polls               int
	legTotal            int
	legMax              int
	census              pairCensus
	pollsWithSameExpiry int
	grossHits           int
	netHits             int
	bestGross           violation
	crossed             int
	// sigma dispersion per poll: max - min of the defined implied sigmas,
	// relative to the fitted value, so the numbers are comparable across assets.
	disp []float64
	// uncalibrated legs are windows the engine currently refuses to quote.
	uncalibrated int
}

func newAggregate(fee float64) *aggregate { return &aggregate{fee: fee} }

func (a *aggregate) add(s *snapshot) {
	a.polls++
	a.legTotal += len(s.Legs)
	if len(s.Legs) > a.legMax {
		a.legMax = len(s.Legs)
	}
	a.crossed += s.Crossed

	var anySameExpiry bool
	for _, c := range s.Census {
		a.census.SameExpiry += c.SameExpiry
		a.census.Identical += c.Identical
		a.census.SameStrikeDiffExpiry += c.SameStrikeDiffExpiry
		a.census.Disjoint += c.Disjoint
		if c.SameExpiry > 0 {
			anySameExpiry = true
		}
	}
	if anySameExpiry {
		a.pollsWithSameExpiry++
	}

	for _, v := range s.Violations {
		a.grossHits++
		if v.Gross-a.fee > 0 {
			a.netHits++
		}
		if v.Gross > a.bestGross.Gross {
			a.bestGross = v
		}
	}

	// Implied-sigma dispersion, per asset, as a fraction of the fitted sigma.
	byAsset := map[string][]float64{}
	fitted := map[string]float64{}
	for _, l := range s.Legs {
		if l.FittedSigma == 0 {
			a.uncalibrated++
			continue
		}
		if l.ImpliedSigma == 0 {
			continue
		}
		byAsset[l.Asset] = append(byAsset[l.Asset], l.ImpliedSigma)
		fitted[l.Asset] = l.FittedSigma
	}
	for asset, xs := range byAsset {
		if len(xs) < 2 || fitted[asset] == 0 {
			continue
		}
		lo, hi := xs[0], xs[0]
		for _, x := range xs {
			lo = math.Min(lo, x)
			hi = math.Max(hi, x)
		}
		a.disp = append(a.disp, (hi-lo)/fitted[asset])
	}
}

// report prints the census first and the verdict last, and the verdict is
// allowed to say no.
func (a *aggregate) report() {
	if a.polls == 0 {
		fmt.Println("no successful polls; nothing measured")
		return
	}
	fmt.Println("CENSUS -- does the cross-section even exist?")
	fmt.Printf("  polls                                : %d\n", a.polls)
	fmt.Printf("  two-sided legs per poll (mean / max) : %.1f / %d\n",
		float64(a.legTotal)/float64(a.polls), a.legMax)
	fmt.Printf("  polls with >=1 same-expiry pair      : %d (%.1f%%)\n",
		a.pollsWithSameExpiry, 100*float64(a.pollsWithSameExpiry)/float64(a.polls))
	fmt.Printf("  same-expiry pairs (model-free)       : %d\n", a.census.SameExpiry)
	fmt.Printf("    of which identical strike          : %d\n", a.census.Identical)
	fmt.Printf("  same-strike, different expiry        : %d  (no model-free rule; not scored)\n", a.census.SameStrikeDiffExpiry)
	fmt.Printf("  disjoint pairs                       : %d  (model-only; not scored)\n", a.census.Disjoint)
	fmt.Printf("  uncalibrated legs seen               : %d  (240m/1440m -- the engine refuses these today)\n", a.uncalibrated)

	fmt.Println()
	fmt.Println("HARD SIGNAL -- model-free vertical violations")
	fmt.Printf("  gross violations                     : %d\n", a.grossHits)
	fmt.Printf("  net of fee=%.4f                      : %d\n", a.fee, a.netHits)
	if a.bestGross.Gross > 0 {
		v := a.bestGross
		fmt.Printf("  largest: buy %s @%.3f / sell %s @%.3f -> %.4f on size %.2f%s\n",
			v.BuyUp, v.Ask, v.SellUp, v.Bid, v.Gross, v.Size,
			map[bool]string{true: " (identical strike)"}[v.Identical])
	}

	fmt.Println()
	fmt.Println("SOFT SIGNAL -- implied-sigma dispersion across cadences (model-relative)")
	if len(a.disp) == 0 {
		fmt.Println("  not measurable: fewer than two calibrated legs with an invertible mid")
	} else {
		sort.Float64s(a.disp)
		fmt.Printf("  observations %d   median %.2fx fitted sigma   p90 %.2fx   max %.2fx\n",
			len(a.disp), a.disp[len(a.disp)/2], a.disp[(len(a.disp)*9)/10], a.disp[len(a.disp)-1])
	}
	fmt.Printf("  crossed books observed               : %d (intra-market box; single-block reads)\n", a.crossed)

	fmt.Println()
	fmt.Println("VERDICT")
	switch {
	case a.census.SameExpiry == 0:
		fmt.Println("  KILLED. No two live windows on one asset ever shared an expiry, so the")
		fmt.Println("  model-free vertical rule has nothing to apply to. Do not build the")
		fhint()
	case a.netHits == 0:
		fmt.Printf("  KILLED as a taker. %d same-expiry pairs existed and none was mispriced\n", a.census.SameExpiry)
		fmt.Println("  past the fee. The relation holds, which means the venue is already")
		fmt.Println("  consistent where it is comparable -- there is no free money here.")
		fhint()
	case a.netHits < a.polls/20:
		fmt.Printf("  MARGINAL. %d net violations across %d polls is too rare to be a\n", a.netHits, a.polls)
		fmt.Println("  strategy on its own, but is enough to justify the surface as a pricing")
		fmt.Println("  service that quotes the windows the model refuses.")
	default:
		fmt.Printf("  ALIVE. %d net violations across %d polls, on real depth. The two-leg\n", a.netHits, a.polls)
		fmt.Println("  box is worth building -- with atomic-or-nothing execution, because a")
		fmt.Println("  one-legged box is exactly the naked directional position AUTOPSY.md")
		fmt.Println("  was about.")
	}
}

// fhint states the fallback once, so a negative verdict still leaves the reader
// with the next move rather than just a dead end.
func fhint() {
	fmt.Println("  arbitrage taker. The surviving idea is the surface as a REFERENCE PRICE:")
	fmt.Println("  derive a quote for all ~770 daily markets from the few that trade, which")
	fmt.Println("  needs consistency to hold, not to be violated.")
}

func printPoll(s *snapshot, fee float64) {
	var same, ident, diff int
	for _, c := range s.Census {
		same += c.SameExpiry
		ident += c.Identical
		diff += c.Disjoint
	}
	var gmax, nmax float64
	var nets int
	for _, v := range s.Violations {
		gmax = math.Max(gmax, v.Gross)
		if n := v.Gross - fee; n > nmax {
			nmax = n
		}
		if v.Gross-fee > 0 {
			nets++
		}
	}
	fmt.Printf("%-8s %5d %5d %6d %6d %6d %6d %8.4f %8.4f\n",
		time.Unix(s.At, 0).Format("15:04:05"),
		len(s.Legs), same, ident, diff, nets, s.Crossed, gmax, nmax)
}

// ---- small helpers --------------------------------------------------------

// lvlPx converts a raw book price to probability units. The grid scales with
// the collateral's decimals -- 6 on testnet, 18 on mainnet -- so the divisor is
// read per market and never hardcoded.
func lvlPx(p *big.Int, dec int) float64 {
	if p == nil {
		return 0
	}
	den := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(p), den).Float64()
	return f
}

// qtyAt is the size resting on the touch. A price with no size behind it cannot
// be traded, so the violation record carries depth alongside the gap.
func qtyAt(ls []venue.Level, dec int) float64 {
	if len(ls) == 0 || ls[0].Quantity == nil {
		return 0
	}
	return lvlPx(ls[0].Quantity, dec)
}

func relClose(a, b, eps float64) bool {
	if a == b {
		return true
	}
	m := math.Max(math.Abs(a), math.Abs(b))
	if m == 0 {
		return true
	}
	return math.Abs(a-b)/m < eps
}

// probit is the inverse normal CDF (Acklam's rational approximation, refined by
// one Halley step). Accurate to roughly 1e-15 over the range that matters here,
// which is far tighter than the 0.045 calibration error anything downstream
// respects.
func probit(p float64) float64 {
	if p <= 0 || p >= 1 {
		return math.NaN()
	}
	a := [6]float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02,
		1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := [5]float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02,
		6.680131188771972e+01, -1.328068155288572e+01}
	c := [6]float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00,
		-2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := [4]float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00,
		3.754408661907416e+00}
	const plow = 0.02425
	var x float64
	switch {
	case p < plow:
		q := math.Sqrt(-2 * math.Log(p))
		x = (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p > 1-plow:
		q := math.Sqrt(-2 * math.Log(1-p))
		x = -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	default:
		q := p - 0.5
		r := q * q
		x = (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	}
	// One Halley refinement, using the same erfc the model's normCDF uses so
	// the inverse is consistent with the forward function it inverts.
	e := 0.5*math.Erfc(-x/math.Sqrt2) - p
	u := e * math.Sqrt(2*math.Pi) * math.Exp(x*x/2)
	return x - u/(1+x*u/2)
}
