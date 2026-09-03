package venue

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	PriceFeedMainnet = "https://price-feed.prd.oracle.somnia.host/v1/graphql"
	PriceFeedTestnet = "https://price-feed.dev.oracle.somnia.host/v1/graphql"
	priceFeedDec     = 18
)

type pricePoint struct {
	Base        string `json:"base"`
	Quote       string `json:"quote"`
	Spot        string `json:"spot"`
	Mark        string `json:"mark"`
	UpdatedAtMs string `json:"updatedAtMs"`
}

// Spot is a live index price with its age, so a stale feed can never silently
// price an order.
type Spot struct {
	Asset string
	Price float64
	Age   time.Duration
}

const spotQ = `query { PricePoint(order_by: {blockTimestamp: desc}, limit: 400) {
  base quote spot mark updatedAtMs
} }`

// Spots returns the freshest index price per asset, keyed by base symbol.
func (c *Client) Spots(ctx context.Context, feedURL string) (map[string]Spot, error) {
	var out struct {
		PricePoint []pricePoint `json:"PricePoint"`
	}
	if err := gqlAt(ctx, feedURL, spotQ, nil, &out); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	res := map[string]Spot{}
	for _, p := range out.PricePoint {
		base := strings.ToUpper(p.Base)
		if _, seen := res[base]; seen {
			continue // rows are newest-first, so the first is freshest
		}
		v, err := strconv.ParseFloat(p.Spot, 64)
		if err != nil {
			continue
		}
		ms, _ := strconv.ParseInt(p.UpdatedAtMs, 10, 64)
		res[base] = Spot{
			Asset: base,
			Price: v / 1e18,
			Age:   time.Duration(now-ms) * time.Millisecond,
		}
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("price feed returned no points")
	}
	return res, nil
}

// ---- opening prices for live markets ------------------------------------

const openingQ = `query($ids: [String!]) {
  MarketReferenceLink(where: {market_id: {_in: $ids}}) { market_id referenceQuestionId pending }
}`

const answerByQIDQ = `query($qids: [numeric!]) {
  OracleAnswer(where: {oracleQuestionId: {_in: $qids}}) { oracleQuestionId numericValue voided }
}`

// OpeningPrices resolves each market's "line to beat" — the reference question's
// answer. Reference-mode markets carry strike=0, so this is the only source.
func (c *Client) OpeningPrices(ctx context.Context, marketRowIDs []string) (map[string]float64, error) {
	if len(marketRowIDs) == 0 {
		return map[string]float64{}, nil
	}
	var links struct {
		MarketReferenceLink []refLink `json:"MarketReferenceLink"`
	}
	if err := c.gqlPost(ctx, openingQ, map[string]any{"ids": marketRowIDs}, &links); err != nil {
		return nil, err
	}
	qids := make([]string, 0, len(links.MarketReferenceLink))
	byMarket := map[string]string{}
	for _, l := range links.MarketReferenceLink {
		if l.RefQID.Empty() {
			continue
		}
		byMarket[l.MarketID] = l.RefQID.String()
		qids = append(qids, l.RefQID.String())
	}
	if len(qids) == 0 {
		return map[string]float64{}, nil
	}
	var ans struct {
		OracleAnswer []oracleAnswer `json:"OracleAnswer"`
	}
	if err := c.gqlPost(ctx, answerByQIDQ, map[string]any{"qids": qids}, &ans); err != nil {
		return nil, err
	}
	val := map[string]float64{}
	for _, a := range ans.OracleAnswer {
		if a.Voided || a.NumericValue.Empty() {
			continue
		}
		if v, ok := a.NumericValue.Float(); ok {
			val[a.OracleQID.String()] = v
		}
	}
	out := map[string]float64{}
	for m, q := range byMarket {
		if v, ok := val[q]; ok {
			out[m] = v
		}
	}
	return out, nil
}

// ---- historical candles (for backtesting) --------------------------------

type FeedCandle struct {
	Base        string `json:"base"`
	BucketStart numStr `json:"bucketStart"`
	Open        numStr `json:"open"`
	Close       numStr `json:"close"`
}

const candlesQ = `query($base: String!, $from: numeric!, $a: numeric!) {
  Candle(where: {base:{_eq:$base}, resolution:{_eq:"M1"}, bucketStart:{_gte:$from, _gt:$a}},
         order_by:{bucketStart:asc}, limit:1000) {
    base bucketStart open close
  }
}`

// CandlesM1 returns 1-minute index candles for one asset, newest-forward,
// paged on bucketStart so deep history stays cheap.
func (c *Client) CandlesM1(ctx context.Context, feedURL, base string, from int64, maxPages int) ([]FeedCandle, error) {
	var all []FeedCandle
	cursor := from - 1
	for i := 0; i < maxPages; i++ {
		var out struct {
			Candle []FeedCandle `json:"Candle"`
		}
		vars := map[string]any{"base": base, "from": from, "a": cursor}
		if err := gqlAt(ctx, feedURL, candlesQ, vars, &out); err != nil {
			return all, err
		}
		if len(out.Candle) == 0 {
			return all, nil
		}
		all = append(all, out.Candle...)
		last := out.Candle[len(out.Candle)-1]
		v, _ := strconv.ParseInt(last.BucketStart.String(), 10, 64)
		cursor = v
		if len(out.Candle) < 1000 {
			return all, nil
		}
	}
	return all, nil
}

// ---- replay series -------------------------------------------------------

// SpotObs is one index-price observation, stamped with the wall-clock time at
// which that price became OBSERVABLE -- not the time the bucket it came from
// opened.
//
// That distinction is the entire reason this type exists. An M1 candle's close
// is not knowable until the bucket ends, so pricing a decision made mid-bucket
// with that close leaks up to 59 seconds of the future into the prediction. At
// BTC's fitted volatility that is roughly 1 sigma, which near expiry removes a
// large share of the uncertainty the model exists to estimate: a backtest built
// that way fits a volatility far too small, because part of the diffusion it is
// meant to forecast has already happened.
type SpotObs struct {
	At    int64 // unix seconds at which this price was observable
	Price float64
}

// SpotSeries is a time-ordered index-price history for one asset, built for
// replay. Lookups are strictly causal: At(t) can only return a price that was
// already observable at t.
type SpotSeries struct {
	obs      []SpotObs // ascending by At, one entry per timestamp
	maxStale int64     // refuse to serve an observation older than this
}

// candleObsLag is how long after a bucket opens that its close is knowable.
const candleObsLag = 60

// BuildSpotSeries turns M1 candles into a causal replay series.
//
// Each candle yields TWO observations, because each end is knowable at a
// different moment: the OPEN is the price at bucketStart, and the CLOSE is not
// knowable until bucketStart+60. Emitting both doubles the replay's resolution
// while keeping every point strictly causal -- using only the open would throw
// away a minute of real information, and using only the close would ignore a
// price that was already public.
func BuildSpotSeries(cs []FeedCandle) SpotSeries {
	obs := make([]SpotObs, 0, 2*len(cs))
	for _, c := range cs {
		t, err := strconv.ParseInt(c.BucketStart.String(), 10, 64)
		if err != nil {
			continue
		}
		if v, ok := c.Open.Float(); ok {
			obs = append(obs, SpotObs{At: t, Price: v / 1e18})
		}
		if v, ok := c.Close.Float(); ok {
			obs = append(obs, SpotObs{At: t + candleObsLag, Price: v / 1e18})
		}
	}
	// Tolerate a few missing buckets, plus the bucket length itself.
	return newSpotSeries(obs, 5*60+candleObsLag)
}

// BuildSpotSeriesFromPoints turns raw index-price points into a causal replay
// series. This is the table the live engine polls, so a backtest built on it
// and the engine cannot disagree about what was knowable when. Prefer it.
func BuildSpotSeriesFromPoints(ps []SpotObs) SpotSeries {
	return newSpotSeries(ps, 30)
}

func newSpotSeries(obs []SpotObs, maxStale int64) SpotSeries {
	sort.Slice(obs, func(i, j int) bool { return obs[i].At < obs[j].At })
	out := obs[:0]
	for i, o := range obs {
		if i > 0 && o.At == out[len(out)-1].At {
			out[len(out)-1] = o // last write for a timestamp wins
			continue
		}
		out = append(out, o)
	}
	return SpotSeries{obs: out, maxStale: maxStale}
}

// Len reports how many observations the series holds.
func (s SpotSeries) Len() int { return len(s.obs) }

// Span reports the first and last observation times, or (0,0) when empty.
func (s SpotSeries) Span() (int64, int64) {
	if len(s.obs) == 0 {
		return 0, 0
	}
	return s.obs[0].At, s.obs[len(s.obs)-1].At
}

// At returns the most recent index price observable at or before t, and whether
// one exists within the series' staleness tolerance.
//
// It finds the last observation with At <= t. Returning anything stamped after
// t would be look-ahead, which this type exists to make impossible.
func (s SpotSeries) At(t int64) (float64, bool) {
	i := sort.Search(len(s.obs), func(i int) bool { return s.obs[i].At > t }) - 1
	if i < 0 {
		return 0, false
	}
	if t-s.obs[i].At > s.maxStale {
		return 0, false
	}
	return s.obs[i].Price, true
}

// ---- raw index-price history (the series the engine trades) --------------

type pricePointRow struct {
	ID             string `json:"id"`
	Spot           numStr `json:"spot"`
	BlockTimestamp numStr `json:"blockTimestamp"`
}

const pricePointsQ = `query($base: String!, $from: numeric!, $a: String!) {
  PricePoint(where: {base:{_eq:$base}, blockTimestamp:{_gte:$from}, id:{_gt:$a}},
             order_by:{id:asc}, limit:1000) {
    id spot blockTimestamp
  }
}`

// PricePoints returns raw index-price observations for one asset since `from`,
// paged on id. This is the same table Spots() reads live, at roughly one point
// per second, which is why replaying it removes a whole class of replay bias.
func (c *Client) PricePoints(ctx context.Context, feedURL, base string, from int64, maxPages int) ([]SpotObs, error) {
	var all []SpotObs
	cursor := ""
	for i := 0; i < maxPages; i++ {
		var out struct {
			PricePoint []pricePointRow `json:"PricePoint"`
		}
		vars := map[string]any{"base": base, "from": from, "a": cursor}
		if err := gqlAt(ctx, feedURL, pricePointsQ, vars, &out); err != nil {
			return all, err
		}
		if len(out.PricePoint) == 0 {
			return all, nil
		}
		for _, p := range out.PricePoint {
			ts, err := strconv.ParseInt(p.BlockTimestamp.String(), 10, 64)
			if err != nil {
				continue
			}
			v, ok := p.Spot.Float()
			if !ok {
				continue
			}
			all = append(all, SpotObs{At: ts, Price: v / 1e18})
		}
		cursor = out.PricePoint[len(out.PricePoint)-1].ID
		if len(out.PricePoint) < 1000 {
			return all, nil
		}
	}
	return all, nil
}
