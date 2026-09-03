// Command firstorder places one real post-only order and reads it back off the
// book. This is the end-to-end proof that pure Go can trade this venue.
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

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

	live, err := c.DiscoverLive(ctx)
	if err != nil {
		log.Fatal(err)
	}

	// Pick a Trading window with real headroom so it cannot lock mid-send.
	now := time.Now().Unix()
	for _, im := range live {
		m, err := c.ReadMarket(ctx, im.ID())
		if err != nil {
			continue
		}
		st, err := c.ReadState(ctx, m.MarketAddr)
		if err != nil || st.Status != venue.StatusTrading {
			continue
		}
		if int64(m.Expiry)-now < 300 {
			continue
		}
		label := fmt.Sprintf("%s/%dm", im.Asset, im.IntervalSecs()/60)
		fmt.Printf("market %s  pool=%s  t-%ds\n", label, m.Pool.Hex(), int64(m.Expiry)-now)

		bp, err := c.BookParams(ctx, m.Pool)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("grid: tick=%s lot=%s min=%s (dec=%d)\n", bp.TickSize, bp.LotSize, bp.MinQuantity, im.QuoteDec)

		bids, _ := c.BookLevels(ctx, m.Pool, true, 3)
		asks, _ := c.BookLevels(ctx, m.Pool, false, 3)
		fmt.Printf("book before: %d bids / %d asks  best=%s/%s\n",
			len(bids), len(asks), show(bids, im.QuoteDec), show(asks, im.QuoteDec))

		did, err := tr.EnsureApproval(ctx, venue.TestUSDC, m.Pool)
		if err != nil {
			log.Fatalf("approve: %v", err)
		}
		fmt.Printf("approval: %s\n", map[bool]string{true: "granted now", false: "already in place"}[did])

		// Rest well below the touch so post-only cannot cross.
		target := 0.100
		if len(bids) > 0 {
			if b := toF(bids[0].Price, im.QuoteDec); b > 0.15 {
				target = b - 0.05
			}
		}
		price := venue.SnapPrice(target, bp.TickSize, im.QuoteDec)
		qty := venue.SnapQty(2, bp.LotSize, im.QuoteDec)
		fmt.Printf("placing POST_ONLY BUY_UP  price=%.3f (raw %s)  qty=2 (raw %s)\n",
			target, price, qty)

		rcpt, err := tr.Place(ctx, venue.PlaceOrder{
			Pool: m.Pool, Kind: venue.BuyYes, Price: price, Quantity: qty,
			Type: venue.TypePostOnly,
		})
		if err != nil {
			log.Fatalf("place failed: %v", err)
		}
		fmt.Printf("ORDER SENT  tx=%s  gasUsed=%d  block=%d\n",
			rcpt.TxHash.Hex(), rcpt.GasUsed, rcpt.BlockNumber)

		time.Sleep(3 * time.Second)
		bids2, _ := c.BookLevels(ctx, m.Pool, true, 6)
		fmt.Printf("book after : %d bids  levels=%s\n", len(bids2), all(bids2, im.QuoteDec))
		return
	}
	log.Fatal("no tradable market with headroom found")
}

func toF(v *big.Int, dec int) float64 {
	d := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), d).Float64()
	return f
}

func show(l []venue.Level, dec int) string {
	if len(l) == 0 {
		return "-"
	}
	return fmt.Sprintf("%.3f", toF(l[0].Price, dec))
}

func all(l []venue.Level, dec int) string {
	s := ""
	for _, x := range l {
		s += fmt.Sprintf(" %.3f x%.1f", toF(x.Price, dec), toF(x.Quantity, dec))
	}
	return s
}
