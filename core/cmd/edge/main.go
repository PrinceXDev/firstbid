// Command edge prints, for every live window, what the model thinks the
// contract is worth against what the book is actually quoting.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"time"

	"github.com/firstbid/core/internal/model"
	"github.com/firstbid/core/internal/venue"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	rpc, gql, feed := venue.ShannonRPC, venue.ShannonGQL, venue.PriceFeedTestnet
	if os.Getenv("FB_NET") == "mainnet" {
		rpc, gql, feed = "https://api.infra.mainnet.somnia.network", venue.MainnetGQL, venue.PriceFeedMainnet
	}
	c, err := venue.Dial(ctx, rpc, gql)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	spots, err := c.Spots(ctx, feed)
	if err != nil {
		log.Fatal(err)
	}
	live, err := c.DiscoverLive(ctx)
	if err != nil {
		log.Fatal(err)
	}
	ids := make([]string, 0, len(live))
	for _, m := range live {
		ids = append(ids, m.RowID)
	}
	opens, err := c.OpeningPrices(ctx, ids)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%-10s %7s %10s %10s %7s %7s  %-13s %7s  %s\n",
		"series", "t-left", "spot", "open", "fair", "unc", "book", "spread", "verdict")

	now := time.Now().Unix()
	var edges int
	for _, im := range live {
		sp, ok := spots[im.Asset]
		if !ok {
			continue
		}
		open, ok := opens[im.RowID]
		if !ok {
			continue
		}
		sigma, ok := model.SigmaPerMin(im.Asset, im.IntervalSecs())
		if !ok {
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

		// The oracle's numericValue is scaled; normalise to the spot's scale.
		openPx := normalise(open, sp.Price)
		fair := model.FairValue(sp.Price, openPx, sigma, secsLeft)
		unc := model.Uncertainty(sp.Price, openPx, sigma, secsLeft, 15)

		bids, _ := c.BookLevels(ctx, m.Pool, true, 1)
		asks, _ := c.BookLevels(ctx, m.Pool, false, 1)
		bid, ask := px(bids, im.QuoteDec), px(asks, im.QuoteDec)

		book, spread, verdict := "—", math.NaN(), ""
		if bid > 0 && ask > 0 {
			book = fmt.Sprintf("%.3f/%.3f", bid, ask)
			spread = ask - bid
			switch {
			case fair < bid:
				verdict = fmt.Sprintf("SELL rich by %.3f", bid-fair)
				edges++
			case fair > ask:
				verdict = fmt.Sprintf("BUY cheap by %.3f", fair-ask)
				edges++
			default:
				verdict = fmt.Sprintf("inside; we could quote ±%.3f", math.Max(unc, 0.002))
			}
		} else if bid > 0 || ask > 0 {
			book = fmt.Sprintf("%.3f/%.3f", bid, ask)
			verdict = "one-sided"
		} else {
			verdict = "EMPTY — nothing to take"
		}

		fmt.Printf("%-10s %6.0fs %10.2f %10.2f %7.3f %7.3f  %-13s %7s  %s\n",
			fmt.Sprintf("%s/%dm", im.Asset, im.IntervalSecs()/60), secsLeft,
			sp.Price, openPx, fair, unc, book, fmtf(spread), verdict)
	}
	fmt.Printf("\nspot age: %v   markets with model/book disagreement: %d\n", spots["BTC"].Age, edges)
}

// normalise rescales the oracle's fixed-point answer onto the price feed's units.
// The oracle's numericValue scale VARIES BY QUESTION (observed 1e2 and 1e4 on
// mainnet), so the divisor cannot be hardcoded. Anchoring to live spot within a
// 3x band is safe: no supported asset moves 3x inside a single window.
func normalise(oracleVal, ref float64) float64 {
	if ref <= 0 || oracleVal <= 0 {
		return oracleVal
	}
	v := oracleVal
	for v > ref*3 {
		v /= 10
	}
	for v < ref/3 {
		v *= 10
	}
	return v
}

func px(l []venue.Level, dec int) float64 {
	if len(l) == 0 || l[0].Price == nil {
		return 0
	}
	den := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(l[0].Price), den).Float64()
	return f
}

func fmtf(v float64) string {
	if math.IsNaN(v) {
		return "—"
	}
	return fmt.Sprintf("%.3f", v)
}
