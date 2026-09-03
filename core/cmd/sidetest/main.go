// Command sidetest determines empirically how the pool interprets the price of
// a BUY_NO order, by placing one at a distinctive level and finding it.
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/joho/godotenv"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	_ = godotenv.Load("../executor/.env", ".env")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	c, err := venue.Dial(ctx, venue.ShannonRPC, venue.ShannonGQL)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	tr, err := venue.NewTrader(ctx, c, os.Getenv("FIRSTBID_PRIVATE_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	live, _ := c.DiscoverLive(ctx)
	now := time.Now().Unix()
	for _, im := range live {
		if im.IntervalSecs() < 900 {
			continue
		}
		m, err := c.ReadMarket(ctx, im.ID())
		if err != nil {
			continue
		}
		st, err := c.ReadState(ctx, m.MarketAddr)
		if err != nil || st.Status != venue.StatusTrading || int64(m.Expiry)-now < 400 {
			continue
		}
		bp, _ := c.BookParams(ctx, m.Pool)
		if _, err := tr.EnsureApproval(ctx, venue.TestUSDC, m.Pool); err != nil {
			log.Fatal(err)
		}

		// Clear our own book presence so the reading is unambiguous.
		ids, _ := c.OwnOpenOrders(ctx, m.Pool, tr.From())
		for _, id := range ids {
			_, _ = tr.Cancel(ctx, m.Pool, id)
		}

		before := snap(ctx, c, m.Pool, im.QuoteDec)
		fmt.Printf("%s/%dm pool=%s\n", im.Asset, im.IntervalSecs()/60, m.Pool.Hex()[:10])
		fmt.Printf("before: bids%v asks%v\n", before.bids, before.asks)

		// A deliberately distinctive, non-crossing value.
		const p = 0.123
		fmt.Printf("\nplacing BUY_NO with price field = %.3f (raw %s)\n", p, venue.SnapPrice(p, bp.TickSize, im.QuoteDec))
		if _, err := tr.Place(ctx, venue.PlaceOrder{
			Pool: m.Pool, Kind: venue.BuyNo,
			Price:    venue.SnapPrice(p, bp.TickSize, im.QuoteDec),
			Quantity: venue.SnapQty(2, bp.LotSize, im.QuoteDec),
			Type:     venue.TypePostOnly,
			ExpireNs: uint64(time.Now().Add(4*time.Minute).Unix()) * 1_000_000_000,
		}); err != nil {
			fmt.Printf("  place failed: %v\n", err)
			return
		}
		time.Sleep(3 * time.Second)
		after := snap(ctx, c, m.Pool, im.QuoteDec)
		fmt.Printf("after : bids%v asks%v\n", after.bids, after.asks)
		fmt.Printf("\nIf it appeared as an ASK at %.3f, price is in YES terms (1-p).\n", 1-p)
		fmt.Printf("If it appeared as an ASK at %.3f, price is the NO price itself.\n", p)
		fmt.Printf("If it appeared as a BID at %.3f, BUY_NO rests on the bid side in NO terms.\n", p)
		return
	}
	log.Fatal("no suitable market")
}

type book struct{ bids, asks []float64 }

func snap(ctx context.Context, c *venue.Client, pool common.Address, dec int) book {
	var b book
	bids, _ := c.BookLevels(ctx, pool, true, 8)
	asks, _ := c.BookLevels(ctx, pool, false, 8)
	for _, l := range bids {
		b.bids = append(b.bids, round3(l.Price, dec))
	}
	for _, l := range asks {
		b.asks = append(b.asks, round3(l.Price, dec))
	}
	return b
}

func round3(v *big.Int, dec int) float64 {
	d := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), d).Float64()
	return f
}
