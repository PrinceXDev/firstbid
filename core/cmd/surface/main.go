// Command surface looks for profit that does not need our model to be right.
//
// Every take Firstbid makes depends on the model, and docs/AUTOPSY.md is what
// that dependency cost when the model was wrong. But DreamDEX runs several
// windows on one underlying at once, and they are all driven by the same spot
// process -- which forces a relation between their prices that holds under
// EVERY probability measure:
//
//	Two windows on the same asset expiring at the SAME second, with strikes
//	K1 < K2, must satisfy P(Up | K1) >= P(Up | K2). Up at the lower strike
//	needs less to happen, so it is worth at least as much, always.
//
// Buy Up(K1) below what Up(K2) can be sold for and the pair pays >= 0 at
// settlement for a negative entry cost. No view on sigma or direction.
//
// This command only MEASURES. It censuses the live book, counts which relations
// actually co-exist, and reports violations -- with the census first, because
// the thesis dies cheaply: if same-expiry pairs never co-exist here, there is
// nothing to trade and no violation arithmetic would rescue it. Two softer
// signals (crossed books, implied-sigma dispersion) are reported separately
// because they are NOT arbitrage. Every poll is appended to JSONL so the
// verdict can be recomputed rather than trusted.
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
	// Append, never truncate: runs are meant to be compared, not replaced.
	f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("open %s: %v", out, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	// Deadline bounds the run; each poll gets its own shorter context.
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
			// A gap in the record, not a reason to stop: a census that dies on
			// the first flaky RPC measures nothing.
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

// leg is one live window: what it pays on, when it settles, what it trades at.
//
// Bid and Ask are in Up terms, in probability units. Up and Down share one book
// quoted in Up terms, so buying Down at q is selling Up at 1-q -- one bid/ask
// pair is therefore a complete two-sided quote, and no Down book is read.
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
	// ImpliedSigma is sigma/min backed out of the mid, or 0 where the inversion
	// carries no information. Model-relative; never used in the hard verdict.
	ImpliedSigma float64 `json:"impliedSigma,omitempty"`
	// FittedSigma is what the model fitted for this series, 0 if it refuses to
	// quote the cadence at all. Those refusals are the interesting rows.
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
	// Size is depth on the thinner of the two levels. A gap with no size behind
	// it is a screenshot, not a trade.
	Size float64 `json:"size"`
	// Identical: same expiry AND exactly the same strike, so the two windows are
	// one contract under two marketIds and any gap is unambiguous.
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

// skip is one market the poll wanted and did not get, with the stage that
// failed. Recorded rather than swallowed: see snapshot.Complete.
type skip struct {
	MarketID string `json:"marketId"`
	Series   string `json:"series"`
	Stage    string `json:"stage"`
	Err      string `json:"err"`
}

type snapshot struct {
	At         int64                 `json:"at"`
	Block      uint64                `json:"block"`
	Legs       []leg                 `json:"legs"`
	Census     map[string]pairCensus `json:"census"`
	Violations []violation           `json:"violations"`
	Crossed    int                   `json:"crossed"`
	SpotAgeMS  int64                 `json:"spotAgeMs"`
	Fee        float64               `json:"fee"`

	// Discovered is how many live markets the indexer offered; Skips is every
	// one that a chain read then failed to deliver.
	Discovered int    `json:"discovered"`
	Skips      []skip `json:"skips,omitempty"`

	// Complete is false when a market was lost to a failed chain read.
	//
	// A transient RPC error can remove one side of every same-expiry pair,
	// leaving a poll that looks exactly like a venue with no cross-section. So
	// only complete polls may support the negative verdict. Partial polls still
	// contribute violations -- one observed is real whatever else was missed;
	// it is the ABSENCE of pairs that a partial read cannot establish.
	Complete bool `json:"complete"`
}

// takeSnapshot reads the entire live cross-section once, AT ONE BLOCK.
//
// The single block is what makes this a cross-section rather than a montage:
// ReadBook picks its own latest block per call, so eight markets read with
// eight calls can sample eight chain states, and comparing quotes that never
// coexisted manufactures an inconsistency that was never tradable. Same mistake
// ReadBook's own doc warns about, one level up -- two books instead of two
// sides.
//
// Status comes from the chain, not the indexer, whose status column trails by
// seconds. A "violation" against a window that has already locked is not a
// trade.
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

	// One block for every chain read below. Chosen after discovery so the block
	// is no older than the market list it is applied to.
	at, err := c.BlockNow(ctx)
	if err != nil {
		return nil, fmt.Errorf("block: %w", err)
	}

	now := time.Now().Unix()
	snap := &snapshot{
		At:         now,
		Block:      at.Uint64(),
		Census:     map[string]pairCensus{},
		Discovered: len(live),
		Complete:   true,
	}
	if sp, ok := spots["BTC"]; ok {
		snap.SpotAgeMS = sp.Age.Milliseconds()
	}
	// lose records a market the poll could not use. A chain failure clears
	// Complete; a market that is merely not comparable (no strike, not Trading)
	// does not -- that absence is a fact about the venue, not about our RPC.
	lose := func(im venue.IndexerMarket, stage string, err error, fatal bool) {
		s := skip{
			MarketID: im.MarketID,
			Series:   fmt.Sprintf("%s/%dm", im.Asset, im.IntervalSecs()/60),
			Stage:    stage,
		}
		if err != nil {
			s.Err = err.Error()
		}
		snap.Skips = append(snap.Skips, s)
		if fatal {
			snap.Complete = false
		}
	}

	for _, im := range live {
		sp, ok := spots[im.Asset]
		if !ok {
			lose(im, "no-spot", nil, false)
			continue
		}
		open, ok := opens[im.RowID]
		if !ok {
			// No resolved reference answer yet. The window has no line to beat
			// that we can read, so it has no strike and cannot be compared.
			lose(im, "no-strike", nil, false)
			continue
		}
		m, err := c.ReadMarket(ctx, im.ID())
		if err != nil {
			lose(im, "read-market", err, true)
			continue
		}
		st, err := c.ReadStateAt(ctx, m.MarketAddr, at)
		if err != nil {
			lose(im, "read-state", err, true)
			continue
		}
		if st.Status != venue.StatusTrading {
			lose(im, "not-trading", nil, false)
			continue
		}
		secsLeft := float64(int64(m.Expiry) - now)
		if secsLeft <= 0 {
			lose(im, "expired", nil, false)
			continue
		}

		bk, err := c.ReadBookAt(ctx, m.Pool, depth, at)
		if err != nil {
			lose(im, "read-book", err, true)
			continue
		}
		bid := lvlPx(bk.BestBid(), im.QuoteDec)
		ask := lvlPx(bk.BestAsk(), im.QuoteDec)
		bidQty := qtyAt(bk.Bids, im.QuoteDec)
		askQty := qtyAt(bk.Asks, im.QuoteDec)
		if bid <= 0 && ask <= 0 {
			// Completely empty. 80.8% of this venue's markets never trade, so
			// this is the common case and not an error.
			lose(im, "empty-book", nil, false)
			continue
		}
		// One-sided books are KEPT. A vertical needs only the lower strike's ask
		// and the higher strike's bid, so an ask-only and a bid-only market at
		// the same expiry are together a complete trade. Dropping them would
		// also hide the pair from the census -- letting the tool prove its own
		// null result.

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
			BidQty:      bidQty,
			AskQty:      askQty,
			// Needs BOTH sides: bid > ask with ask == 0 would call every
			// bid-only market a free intra-market box.
			Crossed: bid > 0 && ask > 0 && bid > ask,
		}
		if l.Crossed {
			snap.Crossed++
		}
		if s, ok := model.SigmaPerMin(im.Asset, im.IntervalSecs()); ok {
			l.FittedSigma = s
		}
		// Only a two-sided book has a mid; inverting half a quote would report
		// it as a market price.
		if bid > 0 && ask > 0 {
			l.ImpliedSigma = impliedSigma(sp.Price, strike, (bid+ask)/2, secsLeft)
		}
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

// strikeEps groups strikes for the CENSUS. The oracle's fixed-point scale
// varies by question, so identical strikes can differ in the last bits. One
// part in 100,000 is tighter than any in-window price move, looser than noise.
// It does not license a trade -- see the reverse direction in scanAsset.
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
			// nearStrike groups the census; exactStrike licenses the reverse
			// trade. They are deliberately different tests -- see below.
			nearStrike := relClose(a.Strike, b.Strike, strikeEps)
			exactStrike := a.Strike == b.Strike
			sameStrike := nearStrike

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
			if v, ok := verticalArb(lo, hi, exactStrike); ok {
				out = append(out, v)
			}
			// The REVERSE direction needs EXACT equality, not the tolerance.
			// P(Up|K_lo) >= P(Up|K_hi) collapses to equality only when both
			// settle against the same threshold; under near-equality the
			// reverse pair pays -1 whenever spot lands BETWEEN the strikes --
			// a directional bet, not an arbitrage.
			if exactStrike {
				if v, ok := verticalArb(hi, lo, true); ok {
					out = append(out, v)
				}
			}
		}
	}
	return cen, out
}

// verticalArb tests buying Up on `lo` and selling Up on `hi`. The pair pays
// Up(K_lo) - Up(K_hi): 1 if spot finishes between the strikes, else 0, never
// negative. So any positive bid_hi - ask_lo is profit locked at settlement.
func verticalArb(lo, hi leg, identical bool) (violation, bool) {
	// Require the two sides this trade actually consumes, with size behind
	// them. A one-sided leg can still complete the pair, so the check belongs
	// here -- but an absent side reads as 0.0, which would otherwise look like
	// the cheapest ask on the venue.
	if lo.Ask <= 0 || hi.Bid <= 0 || lo.AskQty <= 0 || hi.BidQty <= 0 {
		return violation{}, false
	}
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

// impliedSigma inverts FairValue: sigma = ln(spot/strike) / (Phi^-1(mid)*sqrt(m)).
//
// Near mid = 0.5 the inverse normal goes to zero, and near spot = strike so does
// the numerator. Both return 0 rather than a huge number, because a sigma of 40
// in the record reads as a signal when it is really a division by nothing.
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
	// sigma dispersion per poll, relative to fitted so assets are comparable.
	disp []float64
	// uncalibrated legs are windows the engine currently refuses to quote.
	uncalibrated int

	// completePolls is the ONLY denominator the negative verdict may use.
	// skipped tallies lost markets by stage, so a run that quietly degraded
	// says so instead of reporting a clean null result.
	completePolls int
	skipped       map[string]int
	// completeSameExpiry counts model-free pairs seen in complete polls only.
	completeSameExpiry int
}

func newAggregate(fee float64) *aggregate {
	return &aggregate{fee: fee, skipped: map[string]int{}}
}

func (a *aggregate) add(s *snapshot) {
	a.polls++
	a.legTotal += len(s.Legs)
	if len(s.Legs) > a.legMax {
		a.legMax = len(s.Legs)
	}
	a.crossed += s.Crossed
	if s.Complete {
		a.completePolls++
	}
	for _, sk := range s.Skips {
		a.skipped[sk.Stage]++
	}

	var anySameExpiry bool
	for _, c := range s.Census {
		a.census.SameExpiry += c.SameExpiry
		a.census.Identical += c.Identical
		a.census.SameStrikeDiffExpiry += c.SameStrikeDiffExpiry
		a.census.Disjoint += c.Disjoint
		if c.SameExpiry > 0 {
			anySameExpiry = true
		}
		if s.Complete {
			a.completeSameExpiry += c.SameExpiry
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

// report prints the census first and the verdict last. The verdict is allowed
// to say no, and to say "inconclusive".
func (a *aggregate) report() {
	if a.polls == 0 {
		fmt.Println("no successful polls; nothing measured")
		return
	}
	fmt.Println("CENSUS -- does the cross-section even exist?")
	fmt.Printf("  polls                                : %d\n", a.polls)
	fmt.Printf("  complete polls (read every market)   : %d (%.1f%%)\n",
		a.completePolls, 100*float64(a.completePolls)/float64(a.polls))
	if len(a.skipped) > 0 {
		stages := make([]string, 0, len(a.skipped))
		for st := range a.skipped {
			stages = append(stages, st)
		}
		sort.Strings(stages)
		fmt.Print("  markets skipped, by stage            : ")
		for i, st := range stages {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s=%d", st, a.skipped[st])
		}
		fmt.Println()
		fmt.Println("    (read-market / read-state / read-book are RPC failures and make a")
		fmt.Println("     poll incomplete; the others are facts about the venue and do not)")
	}
	fmt.Printf("  legs per poll, >=1 tradable side     : %.1f / %d\n",
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
	case a.completePolls == 0:
		// The negative result is the one this tool most wants to report, which
		// is exactly why a degraded sample may not support it.
		fmt.Printf("  INCONCLUSIVE. Not one of %d polls read every market it discovered, so\n", a.polls)
		fmt.Println("  the absence of same-expiry pairs cannot be distinguished from markets")
		fmt.Println("  lost to failed chain reads. Fix the RPC path and re-run before drawing")
		fmt.Println("  any conclusion; the skip table above says which stage failed.")
	case a.completeSameExpiry == 0:
		fmt.Printf("  KILLED. Across %s, no two live windows on one asset ever shared\n", plural(a.completePolls, "complete poll"))
		fmt.Println("  an expiry, so the model-free vertical rule has nothing to apply to.")
		fmt.Println("  Do not build the arbitrage taker.")
		fhint()
	case a.netHits == 0:
		fmt.Printf("  KILLED as a taker. %d same-expiry pairs existed in complete polls and\n", a.completeSameExpiry)
		fmt.Println("  none was mispriced past the fee. The relation holds, which means the")
		fmt.Println("  venue is already consistent where it is comparable -- no free money.")
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

// fhint leaves a negative verdict with the next move, not just a dead end.
func fhint() {
	fmt.Println("  The surviving idea is the surface as a REFERENCE PRICE: derive a quote")
	fmt.Println("  for every listed market from the few that trade, which needs consistency")
	fmt.Println("  to hold rather than to be violated.")
}

// plural renders "1 complete poll" and "93 complete polls". A verdict is the
// most-read line this command prints, and it should not read like a stack trace.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
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
// the collateral's decimals (6 testnet, 18 mainnet), so read it per market.
func lvlPx(p *big.Int, dec int) float64 {
	if p == nil {
		return 0
	}
	den := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(p), den).Float64()
	return f
}

// qtyAt is size resting on the touch, so violations carry depth, not just a gap.
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

// probit is the inverse normal CDF: Acklam's approximation plus one Halley
// step, accurate to ~1e-15 -- far tighter than anything downstream needs.
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
	// Halley step via the same erfc the model's normCDF uses, so the inverse
	// stays consistent with the function it inverts.
	e := 0.5*math.Erfc(-x/math.Sqrt2) - p
	u := e * math.Sqrt(2*math.Pi) * math.Exp(x*x/2)
	return x - u/(1+x*u/2)
}
