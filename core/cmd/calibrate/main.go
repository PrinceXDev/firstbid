// Command calibrate fits the fair-value model against every resolved window.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"time"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	rpc, gql := venue.ShannonRPC, venue.ShannonGQL
	if os.Getenv("FB_NET") != "testnet" {
		rpc, gql = "https://api.infra.mainnet.somnia.network", venue.MainnetGQL
	}
	c, err := venue.Dial(ctx, rpc, gql)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	since := time.Now().Add(-30 * 24 * time.Hour).Unix()
	obs, err := c.BuildCalibrationSet(ctx, since, 60)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("joined observations: %d\n\n", len(obs))
	if len(obs) == 0 {
		return
	}

	type key struct {
		asset string
		iv    int64
	}
	groups := map[key][]venue.Observation{}
	for _, o := range obs {
		groups[key{o.Asset, o.IntervalSec}] = append(groups[key{o.Asset, o.IntervalSec}], o)
	}

	var keys []key
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(groups[keys[i]]) > len(groups[keys[j]]) })

	fmt.Printf("%-12s %6s %8s %10s %10s %10s\n", "series", "n", "P(Up)", "sigma", "sigma/min", "|move|bps")
	for _, k := range keys {
		g := groups[k]
		if len(g) < 30 {
			continue
		}
		var ups int
		var rs []float64
		for _, o := range g {
			if o.Up {
				ups++
			}
			rs = append(rs, o.LogReturn())
		}
		sd := stddev(rs)
		perMin := sd / math.Sqrt(float64(k.iv)/60)
		fmt.Printf("%-12s %6d %8.4f %10.6f %10.6f %10.1f\n",
			fmt.Sprintf("%s/%dm", k.asset, k.iv/60), len(g), float64(ups)/float64(len(g)),
			sd, perMin, meanAbs(rs)*10000)
	}
}

func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var m float64
	for _, x := range xs {
		m += x
	}
	m /= float64(len(xs))
	var v float64
	for _, x := range xs {
		v += (x - m) * (x - m)
	}
	return math.Sqrt(v / float64(len(xs)-1))
}

func meanAbs(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += math.Abs(x)
	}
	return s / float64(len(xs))
}
