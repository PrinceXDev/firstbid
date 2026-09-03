// Command firstbid runs the quoting engine.
//
// Defaults to DRY RUN: it discovers live windows, prices them and logs the
// quotes it would place, without signing anything. Pass -live (and a funded
// FIRSTBID_PRIVATE_KEY) to actually rest orders on the book.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/firstbid/core/internal/ledger"
	"github.com/firstbid/core/internal/maker"
	"github.com/firstbid/core/internal/venue"
)

func main() {
	var (
		live    = flag.Bool("live", false, "sign and send orders (default: dry run)")
		net     = flag.String("net", "testnet", "testnet | mainnet")
		size    = flag.Float64("size", 5, "per-side order size in contracts")
		minHalf = flag.Float64("min-half-spread", 0.004, "floor on each side, in probability")
		dur     = flag.Duration("for", 0, "run for this long then stop (0 = until interrupted)")
		dbPath  = flag.String("db", "firstbid.db", "P&L ledger path (empty disables recording)")
	)
	flag.Parse()

	lg := log.New(os.Stdout, "", log.Ltime)

	rpc, gql, feed := venue.ShannonRPC, venue.ShannonGQL, venue.PriceFeedTestnet
	if *net == "mainnet" {
		rpc, gql, feed = "https://api.infra.mainnet.somnia.network", venue.MainnetGQL, venue.PriceFeedMainnet
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *dur > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *dur)
		defer cancel()
	}

	c, err := venue.Dial(ctx, rpc, gql)
	if err != nil {
		lg.Fatal(err)
	}
	defer c.Close()

	p := maker.DefaultParams()
	p.Size = *size
	p.MinHalfSpread = *minHalf

	var tr *venue.Trader
	if *live {
		if *net == "mainnet" {
			lg.Fatal("refusing to run live on mainnet: this is a hackathon build")
		}
		key := os.Getenv("FIRSTBID_PRIVATE_KEY")
		if key == "" {
			lg.Fatal("-live requires FIRSTBID_PRIVATE_KEY")
		}
		tr, err = venue.NewTrader(ctx, c, key)
		if err != nil {
			lg.Fatal(err)
		}
		lg.Printf("LIVE mode | signer %s", tr.From().Hex())
	} else {
		lg.Printf("DRY RUN | no key loaded, nothing will be signed")
	}

	var book *ledger.DB
	if *dbPath != "" {
		book, err = ledger.Open(*dbPath)
		if err != nil {
			lg.Fatalf("ledger: %v", err)
		}
		defer book.Close()
		lg.Printf("ledger: %s", *dbPath)
	}

	e := maker.New(c, tr, p, feed, lg)
	e.Ledger = book
	lg.Printf("net=%s size=%.1f min-half-spread=%.3f (round trip %.3f vs incumbent ~0.024)",
		*net, p.Size, p.MinHalfSpread, 2*p.MinHalfSpread)

	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				lg.Printf("stats %+v", e.Snapshot())
			}
		}
	}()

	_ = e.Run(ctx)
}
