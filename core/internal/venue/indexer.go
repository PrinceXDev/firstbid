package venue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Discovery only. Chain is truth for anything we trade on — the indexer lags by
// seconds and its `status` column trails the timestamp-derived on-chain state.
type IndexerMarket struct {
	RowID        string  `json:"id"`
	MarketID     string  `json:"marketId"`
	Asset        string  `json:"asset"`
	IntervalSec  string  `json:"intervalSec"`
	TradingStart string  `json:"tradingStart"`
	Expiry       string  `json:"expiry"`
	PoolAddress  string  `json:"poolAddress"`
	QuoteDec     int     `json:"quoteDecimals"`
	TradeCount   string  `json:"tradeCount"`
	LastPrice    *string `json:"lastPrice"`
	OracleQID    *string `json:"oracleQuestionId"`
	CumQuoteVol  string  `json:"cumulativeQuoteVolume"`
}

func (m IndexerMarket) ID() common.Hash   { return common.HexToHash(m.MarketID) }
func (m IndexerMarket) ExpiryUnix() int64 { v, _ := strconv.ParseInt(m.Expiry, 10, 64); return v }
func (m IndexerMarket) IntervalSecs() int64 {
	v, _ := strconv.ParseInt(m.IntervalSec, 10, 64)
	return v
}

const liveMarketsQuery = `query Live($now: numeric!) {
  Market(
    where: { marketType: {_eq: "BINARY"}, finalized: {_eq: false}, expiry: {_gt: $now} }
    order_by: { expiry: asc }
    limit: 80
  ) {
    id marketId asset intervalSec tradingStart expiry poolAddress
    quoteDecimals tradeCount lastPrice oracleQuestionId cumulativeQuoteVolume
  }
}`

func (c *Client) gqlPost(ctx context.Context, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gql, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("indexer: %w", err)
	}
	defer resp.Body.Close()

	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("indexer decode: %w", err)
	}
	if len(env.Errors) > 0 {
		return fmt.Errorf("indexer: %s", env.Errors[0].Message)
	}
	// An empty result always means "no rows", never "request failed" — errors throw above.
	return json.Unmarshal(env.Data, out)
}

// DiscoverLive lists binary markets whose window has not yet expired.
func (c *Client) DiscoverLive(ctx context.Context) ([]IndexerMarket, error) {
	var out struct {
		Market []IndexerMarket `json:"Market"`
	}
	now := time.Now().Unix()
	if err := c.gqlPost(ctx, liveMarketsQuery, map[string]any{"now": now}, &out); err != nil {
		return nil, err
	}
	return out.Market, nil
}

// ---- our own fills, from the indexer ------------------------------------

// UserFill is one execution against our address, as the indexer recorded it.
//
// The submission receipt only shows fills that happened in that same
// transaction. A post-only order that rests and is filled later by a
// counterparty's transaction produces no log we ever see, so a maker that
// records only its own receipts under-reports exactly the fills it exists to
// generate. This is the query that closes that gap.
type UserFill struct {
	ID         string `json:"id"`
	TxHash     string `json:"txHash"`
	MarketID   string `json:"market_id"`
	Maker      string `json:"maker"`
	Taker      string `json:"taker"`
	FillPrice  string `json:"fillPrice"`
	Quantity   string `json:"quantity"`
	Timestamp  string `json:"timestamp"`
	TakerIsBid bool   `json:"takerIsBid"`
	Market     struct {
		QuoteDecimals int    `json:"quoteDecimals"`
		Asset         string `json:"asset"`
	} `json:"market"`
}

const userFillsQuery = `query Fills($who: String!, $since: numeric!) {
  Fill(
    where: { timestamp: {_gte: $since},
             _or: [{maker: {_eq: $who}}, {taker: {_eq: $who}}] }
    order_by: { timestamp: desc }
    limit: 500
  ) {
    id txHash market_id maker taker fillPrice quantity timestamp takerIsBid
    market { quoteDecimals asset }
  }
}`

// UserFills returns every fill involving `account` since `since` (unix seconds).
func (c *Client) UserFills(ctx context.Context, account string, since int64) ([]UserFill, error) {
	var out struct {
		Fill []UserFill `json:"Fill"`
	}
	vars := map[string]any{"who": strings.ToLower(account), "since": since}
	if err := c.gqlPost(ctx, userFillsQuery, vars, &out); err != nil {
		return nil, err
	}
	return out.Fill, nil
}
