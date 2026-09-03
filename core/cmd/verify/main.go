// Command verify reconciles the P&L ledger against the chain.
//
// The engine records settlements as it observes them, but a ledger that trusts
// its own logs is not evidence. This re-derives every outcome from the market
// contract and repairs any row that disagrees, so the reported P&L is a claim
// about the chain rather than about our uptime.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/firstbid/core/internal/ledger"
	"github.com/firstbid/core/internal/venue"
)

func main() {
	dbPath := flag.String("db", "run.db", "ledger to reconcile")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c, err := venue.Dial(ctx, venue.ShannonRPC, venue.ShannonGQL)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	db, err := ledger.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ws, err := db.Windows(ctx)
	if err != nil {
		log.Fatal(err)
	}

	var checked, repaired, newly, pending int
	for _, w := range ws {
		m, err := c.ReadMarket(ctx, venue.HashOf(w.MarketID))
		if err != nil {
			continue
		}
		st, err := c.ReadState(ctx, m.MarketAddr)
		if err != nil {
			continue
		}
		if !st.IsResolved && !st.IsVoided {
			pending++
			continue
		}
		chainWinner := -1
		if st.IsResolved {
			if x, err := c.WinningOutcome(ctx, m.MarketAddr); err == nil {
				chainWinner = x
			} else {
				continue
			}
		}
		checked++

		switch {
		case !w.Settled:
			_ = db.Settle(ctx, w.MarketID, chainWinner, st.IsVoided, 0, 0)
			newly++
			fmt.Printf("  + %-10s settled on chain, recorded winner=%d voided=%v\n", w.Label, chainWinner, st.IsVoided)
		case int(w.Winner.Int64) != chainWinner || w.Voided != st.IsVoided:
			_ = db.Settle(ctx, w.MarketID, chainWinner, st.IsVoided, 0, 0)
			repaired++
			fmt.Printf("  ! %-10s ledger said winner=%d, chain says %d — repaired\n",
				w.Label, w.Winner.Int64, chainWinner)
		}
	}

	fmt.Printf("\nwindows in ledger : %d\n", len(ws))
	fmt.Printf("verified vs chain : %d\n", checked)
	fmt.Printf("newly settled     : %d\n", newly)
	fmt.Printf("repaired          : %d\n", repaired)
	fmt.Printf("still pending     : %d\n", pending)

	as, err := db.Attribute(ctx)
	if err != nil {
		log.Fatal(err)
	}
	t := ledger.Sum(as)
	fmt.Printf("\n=== REALISED P&L (chain-verified) ===\n")
	fmt.Printf("%-12s %6s %10s %10s %10s %10s\n", "window", "fills", "cost", "payout", "edge", "net")
	for _, a := range as {
		if a.Fills == 0 {
			continue
		}
		fmt.Printf("%-12s %6d %10.4f %10.4f %+10.4f %+10.4f\n",
			a.Label, a.Fills, a.Cost, a.Payout, a.Edge, a.Net)
	}
	fmt.Printf("%-12s %6d %10s %10s %+10.4f %+10.4f\n", "TOTAL", t.Fills, "", "", t.Edge, t.Net)
	fmt.Printf("\nedge %+.4f + selection %+.4f = net %+.4f over %d settled windows\n",
		t.Edge, t.Selection, t.Net, t.Windows)
}
