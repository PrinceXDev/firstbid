package venue

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
)

// UnclaimedRow is one wallet's surviving position on a settled market.
// A balance that still exists after finalization has not been redeemed:
// redeeming burns the outcome tokens.
type UnclaimedRow struct {
	Account      string `json:"account"`
	Balance      string `json:"balance"`
	OutcomeIndex int    `json:"outcomeIndex"`
	Market       struct {
		MarketID       string `json:"marketId"`
		Asset          string `json:"asset"`
		IntervalSec    string `json:"intervalSec"`
		Expiry         string `json:"expiry"`
		WinningOutcome *int   `json:"winningOutcome"`
		Voided         bool   `json:"voided"`
		QuoteDecimals  int    `json:"quoteDecimals"`
	} `json:"market"`
}

// Won reports whether this position is on the winning side (or a void, where
// both sides redeem at 0.5).
func (r UnclaimedRow) Won() bool {
	if r.Market.Voided {
		return true
	}
	return r.Market.WinningOutcome != nil && *r.Market.WinningOutcome == r.OutcomeIndex
}

// Human converts the raw balance to collateral units.
func (r UnclaimedRow) Human() float64 {
	b, ok := new(big.Int).SetString(r.Balance, 10)
	if !ok {
		return 0
	}
	den := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(r.Market.QuoteDecimals)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(b), den).Float64()
	if r.Market.Voided {
		return f * 0.5 // a void refunds 0.5 per contract, both sides
	}
	return f
}

const unclaimedQuery = `query Unclaimed($cut: numeric!, $limit: Int!, $offset: Int!) {
  OutcomeBalance(
    where: {
      balance: { _gt: "0" }
      market: { finalized: { _eq: true }, expiry: { _lt: $cut } }
    }
    order_by: { id: asc }
    limit: $limit
    offset: $offset
  ) {
    account balance outcomeIndex
    market { marketId asset intervalSec expiry winningOutcome voided quoteDecimals }
  }
}`

// ScanUnclaimed pages the full set of surviving positions on settled markets
// that expired before `cutoff`, so freshly-settled windows are not miscounted
// as abandoned.
func (c *Client) ScanUnclaimed(ctx context.Context, cutoff int64, maxPages int) ([]UnclaimedRow, error) {
	const page = 500
	var all []UnclaimedRow
	for i := 0; i < maxPages; i++ {
		var out struct {
			OutcomeBalance []UnclaimedRow `json:"OutcomeBalance"`
		}
		vars := map[string]any{"cut": cutoff, "limit": page, "offset": i * page}
		if err := c.gqlPost(ctx, unclaimedQuery, vars, &out); err != nil {
			return all, fmt.Errorf("page %d: %w", i, err)
		}
		all = append(all, out.OutcomeBalance...)
		if len(out.OutcomeBalance) < page {
			break
		}
	}
	return all, nil
}

func atoi64(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }
