package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c, _ := venue.Dial(ctx, "https://api.infra.mainnet.somnia.network", venue.MainnetGQL)
	defer c.Close()

	since := time.Now().Add(-30 * 24 * time.Hour).Unix()
	obs, err := c.BuildCalibrationSet(ctx, since, 60)
	if err != nil {
		log.Fatal(err)
	}
	var ts []int64
	zero := 0
	for _, o := range obs {
		if o.TradingStart == 0 {
			zero++
			continue
		}
		ts = append(ts, o.TradingStart)
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	fmt.Printf("observations=%d  tradingStart==0: %d\n", len(obs), zero)
	if len(ts) > 0 {
		fmt.Printf("window tradingStart range: %s -> %s\n",
			time.Unix(ts[0], 0).UTC().Format(time.RFC3339),
			time.Unix(ts[len(ts)-1], 0).UTC().Format(time.RFC3339))
	}

	earliest := int64(math.MaxInt64)
	for _, o := range obs {
		if o.TradingStart > 0 && o.TradingStart < earliest {
			earliest = o.TradingStart
		}
	}
	cs, err := c.CandlesM1(ctx, venue.PriceFeedMainnet, "BTC", earliest, 40)
	if err != nil {
		log.Printf("candles err: %v", err)
	}
	s := venue.BuildSpotSeries(cs)
	fmt.Printf("candles fetched=%d  series keys=%d  (requested from %s)\n",
		len(cs), s.Len(), time.Unix(earliest, 0).UTC().Format(time.RFC3339))
	if len(cs) > 0 {
		f, _ := cs[0].BucketStart.Float()
		l, _ := cs[len(cs)-1].BucketStart.Float()
		fmt.Printf("candle range: %s -> %s\n",
			time.Unix(int64(f), 0).UTC().Format(time.RFC3339),
			time.Unix(int64(l), 0).UTC().Format(time.RFC3339))
	}
	// sample a few BTC observations and test lookup
	n := 0
	for _, o := range obs {
		if o.Asset != "BTC" || o.TradingStart == 0 {
			continue
		}
		mid := o.TradingStart + (o.Expiry-o.TradingStart)/2
		_, ok := s.At(mid)
		fmt.Printf("  win %s dur=%ds mid=%s lookup=%v open=%.2f\n",
			time.Unix(o.TradingStart, 0).UTC().Format("01-02 15:04"),
			o.Expiry-o.TradingStart, time.Unix(mid, 0).UTC().Format("01-02 15:04"), ok, o.Open)
		n++
		if n >= 8 {
			break
		}
	}
}
