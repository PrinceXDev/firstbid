// Command mybook shows the live book with our own resting orders marked.
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/joho/godotenv"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	_ = godotenv.Load("../executor/.env", ".env")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := venue.Dial(ctx, venue.ShannonRPC, venue.ShannonGQL)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	key, _ := crypto.HexToECDSA(os.Getenv("FIRSTBID_PRIVATE_KEY")[2:])
	me := crypto.PubkeyToAddress(key.PublicKey)
	fmt.Printf("us: %s\n", me.Hex())

	live, _ := c.DiscoverLive(ctx)
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
		mine, err := c.OwnOpenOrders(ctx, m.Pool, me)
		if err != nil || len(mine) == 0 {
			continue
		}
		bids, _ := c.BookLevels(ctx, m.Pool, true, 8)
		asks, _ := c.BookLevels(ctx, m.Pool, false, 8)
		fmt.Printf("\n%s/%dm   our resting orders: %d\n", im.Asset, im.IntervalSecs()/60, len(mine))
		fmt.Println("   BIDS (buy Up)          ASKS (sell Up)")
		n := max(len(bids), len(asks))
		for i := 0; i < n; i++ {
			l, r := "                    ", ""
			if i < len(bids) {
				l = fmt.Sprintf("   %.3f x %-8.1f", f(bids[i].Price, im.QuoteDec), f(bids[i].Quantity, im.QuoteDec))
			}
			if i < len(asks) {
				r = fmt.Sprintf("   %.3f x %-8.1f", f(asks[i].Price, im.QuoteDec), f(asks[i].Quantity, im.QuoteDec))
			}
			fmt.Printf("%s %s\n", l, r)
		}
		if len(bids) > 0 && len(asks) > 0 {
			fmt.Printf("   spread: %.3f\n", f(asks[0].Price, im.QuoteDec)-f(bids[0].Price, im.QuoteDec))
		}
		shown++
		if shown >= 3 {
			break
		}
	}
	if shown == 0 {
		fmt.Println("\nno markets currently carry our orders (they expire on their own)")
	}
}

func f(v *big.Int, dec int) float64 {
	d := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	x, _ := new(big.Float).Quo(new(big.Float).SetInt(v), d).Float64()
	return x
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
