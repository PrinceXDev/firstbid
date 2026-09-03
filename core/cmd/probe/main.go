// Command probe verifies the pure-Go path end to end: discover live markets,
// gate on chain-truth status, and read the resting book straight from the pool.
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rpc, gql := venue.ShannonRPC, venue.ShannonGQL
	if os.Getenv("FB_NET") == "mainnet" {
		rpc, gql = "https://api.infra.mainnet.somnia.network", venue.MainnetGQL
	}
	c, err := venue.Dial(ctx, rpc, gql)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	chainID, err := c.Eth().ChainID(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("connected: chainId=%s\n\n", chainID)

	live, err := c.DiscoverLive(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("indexer: %d live binary markets\n\n", len(live))

	var tradable, empty, twoSided int
	now := time.Now().Unix()

	for i, im := range live {
		if i >= 12 {
			break
		}
		m, err := c.ReadMarket(ctx, im.ID())
		if err != nil {
			fmt.Printf("  ! %s: %v\n", im.Asset, err)
			continue
		}
		st, err := c.ReadState(ctx, m.MarketAddr)
		if err != nil {
			fmt.Printf("  ! %s state: %v\n", im.Asset, err)
			continue
		}
		label := fmt.Sprintf("%s/%dm", im.Asset, im.IntervalSecs()/60)
		if st.Status != venue.StatusTrading {
			fmt.Printf("  [skip %-8s] %s\n", st.Status, label)
			continue
		}
		tradable++

		bp, err := c.BookParams(ctx, m.Pool)
		if err != nil {
			fmt.Printf("  ! %s params: %v\n", label, err)
			continue
		}
		bids, err := c.BookLevels(ctx, m.Pool, true, 5)
		if err != nil {
			fmt.Printf("  ! %s bids: %v\n", label, err)
			continue
		}
		asks, err := c.BookLevels(ctx, m.Pool, false, 5)
		if err != nil {
			fmt.Printf("  ! %s asks: %v\n", label, err)
			continue
		}
		if len(bids) == 0 && len(asks) == 0 {
			empty++
		}
		if len(bids) > 0 && len(asks) > 0 {
			twoSided++
		}
		fmt.Printf("  %-10s t-%-5ds  %db/%da  tick=%s lot=%s  bestBid=%s bestAsk=%s\n",
			label, m.Expiry-uint64(now), len(bids), len(asks),
			bp.TickSize, bp.LotSize, prob(bids, im.QuoteDec), prob(asks, im.QuoteDec))
	}

	fmt.Printf("\n=== tradable=%d  empty books=%d  two-sided=%d\n", tradable, empty, twoSided)
}

// prob renders a raw price as a 0-1 probability at the collateral's scale.
func prob(l []venue.Level, dec int) string {
	if len(l) == 0 {
		return "—"
	}
	scale := new(big.Float).SetFloat64(1)
	for i := 0; i < dec; i++ {
		scale.Mul(scale, big.NewFloat(10))
	}
	f := new(big.Float).Quo(new(big.Float).SetInt(l[0].Price), scale)
	v, _ := f.Float64()
	return fmt.Sprintf("%.3f", v)
}
