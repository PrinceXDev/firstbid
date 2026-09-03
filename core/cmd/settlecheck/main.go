// Command settlecheck verifies we can read a settled market's outcome on-chain
// and that it agrees with the indexer.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	c, err := venue.Dial(ctx, venue.ShannonRPC, venue.ShannonGQL)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	// Recently finalized windows, straight from the indexer.
	var out struct {
		Market []struct {
			MarketID       string `json:"marketId"`
			Asset          string `json:"asset"`
			IntervalSec    string `json:"intervalSec"`
			WinningOutcome *int   `json:"winningOutcome"`
			Voided         bool   `json:"voided"`
		} `json:"Market"`
	}
	q := `query { Market(where:{marketType:{_eq:"BINARY"}, finalized:{_eq:true}},
	        order_by:{expiry:desc}, limit:8) {
	        marketId asset intervalSec winningOutcome voided } }`
	if err := c.GQL(ctx, q, nil, &out); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%-10s %-9s %-10s %-10s %s\n", "market", "indexer", "chain", "voided", "agree")
	agree, total := 0, 0
	for _, m := range out.Market {
		om, err := c.ReadMarket(ctx, venue.HashOf(m.MarketID))
		if err != nil {
			continue
		}
		st, err := c.ReadState(ctx, om.MarketAddr)
		if err != nil {
			continue
		}
		chainWinner := -1
		if st.IsResolved {
			if w, err := c.WinningOutcome(ctx, om.MarketAddr); err == nil {
				chainWinner = w
			}
		}
		idx := -1
		if m.WinningOutcome != nil {
			idx = *m.WinningOutcome
		}
		total++
		ok := chainWinner == idx
		if ok {
			agree++
		}
		fmt.Printf("%-10s %-9d %-10d %-10v %v\n",
			fmt.Sprintf("%s/%sm", m.Asset, m.IntervalSec), idx, chainWinner, st.IsVoided || m.Voided, ok)
	}
	fmt.Printf("\nchain agrees with indexer on %d/%d settled markets\n", agree, total)
}
