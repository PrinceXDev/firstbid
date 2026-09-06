package venue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// endlessFeed answers every candle page with a FULL page, so the pager can only
// stop by running out of pages.
func endlessFeed(t *testing.T, pageSize int) *httptest.Server {
	t.Helper()
	var n int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := make([]map[string]any, 0, pageSize)
		for i := 0; i < pageSize; i++ {
			n++
			rows = append(rows, map[string]any{
				"base":        "BTC",
				"bucketStart": fmt.Sprintf("%d", n*60),
				"open":        "1000000000000000000",
				"close":       "1000000000000000000",
			})
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"Candle": rows},
		})
	}))
}

// TestCandlesM1ReportsAnExhaustedPageCap is the guard on the replay's honesty.
//
// Both pagers walk OLDEST-first, so stopping at the page cap drops the NEWEST
// part of the requested history while still returning a full-looking series.
// A backtest built on that replays a shorter window than every number it prints
// claims, which is precisely the kind of quiet mismatch this repo exists to
// refuse. The truncation must therefore surface as an error, not a short slice.
func TestCandlesM1ReportsAnExhaustedPageCap(t *testing.T) {
	srv := endlessFeed(t, 1000)
	defer srv.Close()

	c := &Client{}
	const pages = 3
	got, err := c.CandlesM1(context.Background(), srv.URL, "BTC", 0, pages)
	if !errors.Is(err, ErrPageCapReached) {
		t.Fatalf("err = %v, want ErrPageCapReached", err)
	}
	if len(got) != pages*1000 {
		t.Errorf("got %d candles, want the %d already fetched returned alongside the error",
			len(got), pages*1000)
	}
	// The message has to name the asset and the shortfall, or an operator
	// cannot tell which series was cut.
	if !strings.Contains(err.Error(), "BTC") {
		t.Errorf("error %q does not name the asset", err)
	}
}

// TestCandlesM1SucceedsOnAShortFinalPage: a feed that simply has less history
// than asked for is not a truncation, and must not be reported as one.
func TestCandlesM1SucceedsOnAShortFinalPage(t *testing.T) {
	var call int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		rows := []map[string]any{}
		limit := 1000
		if call == 2 {
			limit = 7 // the feed ran out
		}
		for i := 0; i < limit; i++ {
			rows = append(rows, map[string]any{
				"base":        "BTC",
				"bucketStart": fmt.Sprintf("%d", (call*1000+i)*60),
				"open":        "1000000000000000000",
				"close":       "1000000000000000000",
			})
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Candle": rows}})
	}))
	defer srv.Close()

	got, err := (&Client{}).CandlesM1(context.Background(), srv.URL, "BTC", 0, 50)
	if err != nil {
		t.Fatalf("short final page reported as an error: %v", err)
	}
	if len(got) != 1007 {
		t.Errorf("got %d candles, want 1007", len(got))
	}
}
