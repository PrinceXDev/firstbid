package venue

import (
	"context"
	"fmt"
)

// ---- calibration dataset ------------------------------------------------
// A resolved window gives us one observation: where the asset opened, where it
// closed, and which side won. Across thousands of windows that is the empirical
// distribution a fair-value model has to match.

type calMarket struct {
	ID             string `json:"id"`
	MarketID       string `json:"marketId"`
	Asset          string `json:"asset"`
	IntervalSec    string `json:"intervalSec"`
	Expiry         string `json:"expiry"`
	TradingStart   string `json:"tradingStart"`
	WinningOutcome *int   `json:"winningOutcome"`
	OracleQID      numStr `json:"oracleQuestionId"`
	TradeCount     string `json:"tradeCount"`
}

type refLink struct {
	MarketID string `json:"market_id"`
	RefQID   numStr `json:"referenceQuestionId"`
	Pending  bool   `json:"pending"`
}

type oracleAnswer struct {
	OracleQID    numStr `json:"oracleQuestionId"`
	NumericValue numStr `json:"numericValue"`
	Voided       bool   `json:"voided"`
}

// Observation is one resolved window, joined and ready to fit.
type Observation struct {
	Asset        string
	IntervalSec  int64
	Open         float64
	Close        float64
	Up           bool // close >= open
	Traded       bool
	Expiry       int64
	TradingStart int64
}

// LogReturn is the window's realised move. Fair value depends on the size of
// this move relative to how far price still has to travel, never on its sign.
func (o Observation) LogReturn() float64 {
	if o.Open <= 0 || o.Close <= 0 {
		return 0
	}
	return ln(o.Close / o.Open)
}

const calMarketsQ = `query($a: String!, $since: numeric!) {
  Market(where: {marketType:{_eq:"BINARY"}, finalized:{_eq:true}, voided:{_eq:false},
                 id:{_gt:$a}, expiry:{_gte:$since}},
         order_by:{id:asc}, limit:1000) {
    id marketId asset intervalSec expiry tradingStart winningOutcome oracleQuestionId tradeCount
  }
}`

const refLinksQ = `query($a: String!, $since: numeric!) {
  MarketReferenceLink(where: {id:{_gt:$a}, timestamp:{_gte:$since}}, order_by:{id:asc}, limit:1000) {
    id market_id referenceQuestionId pending
  }
}`

const answersQ = `query($a: String!, $since: numeric!) {
  OracleAnswer(where: {id:{_gt:$a}, resolvedAt:{_gte:$since}}, order_by:{id:asc}, limit:1000) {
    id oracleQuestionId numericValue voided
  }
}`

// BuildCalibrationSet pages three tables and joins them into observations.
// `since` bounds the scan to the period the price feed still retains candles
// for; without it, id-ascending paging returns the OLDEST markets, which have
// no candle coverage and silently yield an empty backtest.
func (c *Client) BuildCalibrationSet(ctx context.Context, since int64, maxPages int) ([]Observation, error) {
	// 1. resolved markets
	// The opening answer lands at window start, before the window's own expiry;
	// widen the answer/link scan so those are never cut off.
	lead := since - 86400
	var markets []calMarket
	if err := pageAll(ctx, c, calMarketsQ, "Market", maxPages, &markets, since); err != nil {
		return nil, fmt.Errorf("markets: %w", err)
	}
	// 2. market -> reference (opening) question
	var links []refLink
	if err := pageAll(ctx, c, refLinksQ, "MarketReferenceLink", maxPages, &links, lead); err != nil {
		return nil, fmt.Errorf("reflinks: %w", err)
	}
	// 3. question -> answer
	var answers []oracleAnswer
	if err := pageAll(ctx, c, answersQ, "OracleAnswer", maxPages, &answers, lead); err != nil {
		return nil, fmt.Errorf("answers: %w", err)
	}

	ansByQID := make(map[string]float64, len(answers))
	for _, a := range answers {
		if a.Voided || a.NumericValue.Empty() {
			continue
		}
		if v, ok := a.NumericValue.Float(); ok {
			ansByQID[a.OracleQID.String()] = v
		}
	}
	refByMarket := make(map[string]string, len(links))
	for _, l := range links {
		if !l.RefQID.Empty() && !l.Pending {
			refByMarket[l.MarketID] = l.RefQID.String()
		}
	}

	out := make([]Observation, 0, len(markets))
	for _, m := range markets {
		if m.OracleQID.Empty() || m.WinningOutcome == nil {
			continue
		}
		closeV, ok := ansByQID[m.OracleQID.String()]
		if !ok {
			continue
		}
		refQ, ok := refByMarket[m.ID]
		if !ok {
			continue
		}
		openV, ok := ansByQID[refQ]
		if !ok || openV <= 0 {
			continue
		}
		out = append(out, Observation{
			Asset:        m.Asset,
			IntervalSec:  atoi64(m.IntervalSec),
			Open:         openV,
			Close:        closeV,
			Up:           *m.WinningOutcome == 0,
			Traded:       atoi64(m.TradeCount) > 0,
			Expiry:       atoi64(m.Expiry),
			TradingStart: atoi64(m.TradingStart),
		})
	}
	return out, nil
}
