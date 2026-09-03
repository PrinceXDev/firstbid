// Command sides dumps full depth for one market to pin down price semantics
// on the single shared book (Up and Down quoted against each other).
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := venue.Dial(ctx, venue.ShannonRPC, venue.ShannonGQL)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	live, err := c.DiscoverLive(ctx)
	if err != nil {
		log.Fatal(err)
	}
	shown := 0
	for _, im := range live {
		m, err := c.ReadMarket(ctx, im.ID())
		if err != nil {
			continue
		}
		st, err := c.ReadState(ctx, m.MarketAddr)
		if err != nil || st.Status != venue.StatusTrading {
			continue
		}
		bids, _ := c.BookLevels(ctx, m.Pool, true, 10)
		asks, _ := c.BookLevels(ctx, m.Pool, false, 10)
		if len(bids) == 0 || len(asks) == 0 {
			continue
		}
		fmt.Printf("\n%s/%dm  pool=%s  dec=%d\n", im.Asset, im.IntervalSecs()/60, m.Pool.Hex()[:10], im.QuoteDec)
		fmt.Println("  isBid=true (raw)          | isBid=false (raw)         | 1-ask")
		n := len(bids)
		if len(asks) > n {
			n = len(asks)
		}
		for i := 0; i < n; i++ {
			b, a, inv := "—", "—", "—"
			if i < len(bids) {
				b = fmt.Sprintf("%s x %s", d(bids[i].Price, im.QuoteDec), d(bids[i].Quantity, im.QuoteDec))
			}
			if i < len(asks) {
				a = fmt.Sprintf("%s x %s", d(asks[i].Price, im.QuoteDec), d(asks[i].Quantity, im.QuoteDec))
				inv = d(sub1(asks[i].Price, im.QuoteDec), im.QuoteDec)
			}
			fmt.Printf("  %-25s | %-25s | %s\n", b, a, inv)
		}
		shown++
		if shown >= 3 {
			break
		}
	}
}

func d(v *big.Int, dec int) string {
	if v == nil {
		return "—"
	}
	s := new(big.Float).SetInt(v)
	p := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(s, p).Float64()
	return fmt.Sprintf("%.3f", f)
}

func sub1(v *big.Int, dec int) *big.Int {
	one := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil)
	return new(big.Int).Sub(one, v)
}
