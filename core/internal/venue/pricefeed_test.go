package venue

import (
	"fmt"
	"math/rand"
	"testing"
)

// candles builds M1 candle rows whose closes are trivially distinguishable, so
// a test can name exactly which bucket a returned price came from.
func candles(closes ...float64) []FeedCandle {
	cs := make([]FeedCandle, 0, len(closes))
	for i, c := range closes {
		cs = append(cs, FeedCandle{
			Base:        "BTC",
			BucketStart: numStr(fmt.Sprintf("%d", int64(i)*60)),
			Close:       numStr(fmt.Sprintf("%.0f", c*1e18)),
		})
	}
	return cs
}

// TestSpotSeriesRejectsLookAhead is the regression test for the bug that cost
// this project 37% of deployed capital.
//
// The original implementation keyed each candle by the minute its bucket OPENED
// and stored the bucket's CLOSE, so At(t) returned a price observed up to 59
// seconds after t. Every backtest prediction was priced with part of its own
// future, fitK optimised against that leak, and the resulting volatility
// constant shipped to production.
//
// A candle covering [0,60) does not close until t=60. Before that its close is
// unknowable, and the series must say so.
func TestSpotSeriesRejectsLookAhead(t *testing.T) {
	s := BuildSpotSeries(candles(100, 200, 300))

	for _, tt := range []struct {
		at      int64
		want    float64
		wantOK  bool
		because string
	}{
		{at: 0, wantOK: false, because: "no bucket has closed yet"},
		{at: 30, wantOK: false, because: "the [0,60) bucket is still open"},
		{at: 59, wantOK: false, because: "one second before the first close is knowable"},
		{at: 60, want: 100, wantOK: true, because: "the [0,60) bucket has just closed"},
		{at: 119, want: 100, wantOK: true, because: "the [60,120) close is not knowable yet"},
		{at: 120, want: 200, wantOK: true, because: "the [60,120) bucket has closed"},
		{at: 180, want: 300, wantOK: true, because: "the [120,180) bucket has closed"},
	} {
		got, ok := s.At(tt.at)
		if ok != tt.wantOK {
			t.Errorf("At(%d) ok = %v, want %v — %s", tt.at, ok, tt.wantOK, tt.because)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("At(%d) = %v, want %v — %s", tt.at, got, tt.want, tt.because)
		}
	}
}

// TestSpotSeriesCausalInvariant is the general guard: whatever the series is
// built from, At(t) must return the newest observation stamped at or before t.
// Any implementation that peeks forward fails this on random input.
func TestSpotSeriesCausalInvariant(t *testing.T) {
	rng := rand.New(rand.NewSource(7))

	obs := make([]SpotObs, 0, 200)
	at := int64(1_000_000)
	for i := 0; i < 200; i++ {
		at += 1 + rng.Int63n(4) // irregular arrival, like the real feed
		obs = append(obs, SpotObs{At: at, Price: 50_000 + rng.Float64()*1_000})
	}
	s := BuildSpotSeriesFromPoints(obs)

	for q := int64(999_995); q <= at+5; q++ {
		var want float64
		var found bool
		for _, o := range obs { // brute force: the definition of "causal"
			if o.At <= q {
				want, found = o.Price, true
			}
		}
		got, ok := s.At(q)
		if !found {
			if ok {
				t.Fatalf("At(%d) returned %v with no prior observation", q, got)
			}
			continue
		}
		if !ok {
			continue // legitimately refused as stale
		}
		if got != want {
			t.Fatalf("At(%d) = %v, want %v (newest observation at or before t)", q, got, want)
		}
	}
}

// TestSpotSeriesRefusesStale checks the engine's own rule holds in replay: a
// price too old to trust is not a price. Quoting on a stale index is how a
// maker gets picked off.
func TestSpotSeriesRefusesStale(t *testing.T) {
	s := BuildSpotSeriesFromPoints([]SpotObs{{At: 1000, Price: 42}})

	if _, ok := s.At(1029); !ok {
		t.Error("At(1029) refused a 29s-old point; tolerance is 30s")
	}
	if _, ok := s.At(1031); ok {
		t.Error("At(1031) served a 31s-old point; it should be refused as stale")
	}
}

// TestSpotSeriesDeduplicatesTimestamps guards the ordering assumption that the
// binary search depends on.
func TestSpotSeriesDeduplicatesTimestamps(t *testing.T) {
	s := BuildSpotSeriesFromPoints([]SpotObs{
		{At: 300, Price: 3}, {At: 100, Price: 1},
		{At: 200, Price: 2}, {At: 200, Price: 9},
	})
	if s.Len() != 3 {
		t.Errorf("Len() = %d, want 3 after collapsing the duplicate timestamp", s.Len())
	}
	if got, ok := s.At(210); !ok || got != 9 {
		t.Errorf("At(210) = %v (ok=%v), want 9 (last write for a timestamp wins)", got, ok)
	}
	lo, hi := s.Span()
	if lo != 100 || hi != 300 {
		t.Errorf("Span() = (%d,%d), want (100,300)", lo, hi)
	}
}

// TestSpotSeriesEmpty makes the zero value safe: an asset whose feed failed to
// load must refuse every lookup rather than panic mid-replay.
func TestSpotSeriesEmpty(t *testing.T) {
	var s SpotSeries
	if _, ok := s.At(123); ok {
		t.Error("empty series served a price")
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
	if lo, hi := s.Span(); lo != 0 || hi != 0 {
		t.Errorf("Span() = (%d,%d), want (0,0)", lo, hi)
	}
}

// ohlc builds M1 candles carrying both ends, so a test can tell which end a
// returned price came from.
func ohlc(pairs ...[2]float64) []FeedCandle {
	cs := make([]FeedCandle, 0, len(pairs))
	for i, p := range pairs {
		cs = append(cs, FeedCandle{
			Base:        "BTC",
			BucketStart: numStr(fmt.Sprintf("%d", int64(i)*60)),
			Open:        numStr(fmt.Sprintf("%.0f", p[0]*1e18)),
			Close:       numStr(fmt.Sprintf("%.0f", p[1]*1e18)),
		})
	}
	return cs
}

// TestSpotSeriesUsesBothCandleEnds covers the reconciliation of two independent
// fixes for the look-ahead bug.
//
// One fix kept only each bucket's OPEN, which is knowable at bucketStart. The
// other kept only the CLOSE, stamped at bucketStart+60. Both are causal, and
// each discards half the information a candle carries. The series now emits
// both, so a decision mid-bucket prices off that bucket's open rather than the
// previous bucket's close -- a full minute fresher, and still strictly causal.
func TestSpotSeriesUsesBothCandleEnds(t *testing.T) {
	// bucket [0,60): open 100, close 110.  [60,120): open 110, close 120.
	s := BuildSpotSeries(ohlc([2]float64{100, 110}, [2]float64{110, 120}))

	// Three, not four: one bucket's close and the next bucket's open describe
	// the same instant, so they collapse to a single observation.
	if s.Len() != 3 {
		t.Errorf("Len() = %d, want 3 (four ends, with the shared boundary collapsed)",
			s.Len())
	}
	for _, tt := range []struct {
		at      int64
		want    float64
		because string
	}{
		{at: 0, want: 100, because: "the first bucket's open is knowable at bucketStart"},
		{at: 30, want: 100, because: "mid-bucket, the open is the freshest causal price"},
		{at: 59, want: 100, because: "the close is still one second away"},
		{at: 60, want: 110, because: "the bucket has closed; open of the next is identical here"},
		{at: 119, want: 110, because: "second bucket's close not yet knowable"},
		{at: 120, want: 120, because: "second bucket has closed"},
	} {
		got, ok := s.At(tt.at)
		if !ok {
			t.Errorf("At(%d) refused — %s", tt.at, tt.because)
			continue
		}
		if got != tt.want {
			t.Errorf("At(%d) = %v, want %v — %s", tt.at, got, tt.want, tt.because)
		}
	}
	// The invariant that matters: no price is served before it existed.
	if _, ok := s.At(-1); ok {
		t.Error("At(-1) served a price before the series began")
	}
}
