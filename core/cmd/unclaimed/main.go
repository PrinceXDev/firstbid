// Command unclaimed measures winnings that settled but were never redeemed.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	rpc, gql, net := venue.ShannonRPC, venue.ShannonGQL, "testnet"
	if os.Getenv("FB_NET") == "mainnet" {
		rpc, gql, net = "https://api.infra.mainnet.somnia.network", venue.MainnetGQL, "mainnet"
	}
	c, err := venue.Dial(ctx, rpc, gql)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	cutoff := time.Now().Add(-1 * time.Hour).Unix()
	rows, err := c.ScanUnclaimed(ctx, cutoff, 40)
	if err != nil {
		log.Printf("(partial) %v", err)
	}

	var winners, losers int
	var total float64
	byAcct := map[string]float64{}
	oldest := time.Now().Unix()

	for _, r := range rows {
		if !r.Won() {
			losers++
			continue
		}
		winners++
		v := r.Human()
		total += v
		byAcct[r.Account] += v
		if e := parse(r.Market.Expiry); e > 0 && e < oldest {
			oldest = e
		}
	}

	fmt.Printf("network: %s   settled >1h ago   rows scanned: %d\n", net, len(rows))
	fmt.Printf("  unredeemed WINNING positions : %d\n", winners)
	fmt.Printf("  worthless losing positions   : %d\n", losers)
	fmt.Printf("  distinct wallets owed        : %d\n", len(byAcct))
	fmt.Printf("  TOTAL UNREDEEMED             : %.2f\n", total)
	if winners > 0 {
		fmt.Printf("  oldest unclaimed settled     : %s ago\n",
			time.Since(time.Unix(oldest, 0)).Truncate(time.Minute))
	}

	type kv struct {
		a string
		v float64
	}
	var top []kv
	for a, v := range byAcct {
		top = append(top, kv{a, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })
	fmt.Println("  largest balances owed:")
	for i, t := range top {
		if i >= 5 {
			break
		}
		fmt.Printf("    %s  %.2f\n", t.a, t.v)
	}
}

func parse(s string) int64 {
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v
}
