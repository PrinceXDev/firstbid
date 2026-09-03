package venue

import (
	"context"
	"fmt"
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

// SpotAt indexes candles by minute for O(1) historical lookups.
type SpotSeries map[int64]float64

func BuildSpotSeries(cs []FeedCandle) SpotSeries {
	s := make(SpotSeries, len(cs))
	for _, c := range cs {
		t, err := strconv.ParseInt(c.BucketStart.String(), 10, 64)
		if err != nil {
			continue
		}
		v, ok := c.Close.Float()
		if !ok {
			continue
		}
		s[t/60] = v / 1e18
	}
	return s
}

// At returns the index price at time t, tolerating small gaps in the feed.
func (s SpotSeries) At(t int64) (float64, bool) {
	m := t / 60
	for back := int64(0); back <= 5; back++ {
		if v, ok := s[m-back]; ok {
			return v, true
		}
	}
	return 0, false
}
