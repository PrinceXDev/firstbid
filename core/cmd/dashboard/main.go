// Command dashboard serves Firstbid's evidence: the calibration the model was
// validated on, the live book against live fair value, and realised P&L.
//
// It is a single Go binary with the UI embedded, so the whole stack stays
// Node-free and the judge runs one command.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/firstbid/core/internal/ledger"
	"github.com/firstbid/core/internal/maker"
	"github.com/firstbid/core/internal/model"
	"github.com/firstbid/core/internal/venue"
)

// The UI is a Next.js static export, built by `npm run embed` in ../../web and
// baked in here. Node is a build-time tool only: nothing JavaScript-side runs
// at serve time, and the judge needs no toolchain to see the interface.
//
//go:embed all:dist
var ui embed.FS

// legacyHTML is the zero-dependency fallback page, kept so the binary still
// serves something useful if the export has not been built.
//
//go:embed index.html
var legacyHTML []byte

type server struct {
	c         *venue.Client
	feedURL   string
	ledger    *ledger.DB
	calibFile string
}

func main() {
	var (
		addr   = flag.String("addr", ":8080", "listen address")
		net_   = flag.String("net", "testnet", "testnet | mainnet")
		calib  = flag.String("calibration", "../docs/calibration.json", "backtest artifact")
		dbPath = flag.String("db", "firstbid.db", "ledger database")
	)
	flag.Parse()

	rpc, gql, feed := venue.ShannonRPC, venue.ShannonGQL, venue.PriceFeedTestnet
	if *net_ == "mainnet" {
		rpc, gql, feed = "https://api.infra.mainnet.somnia.network", venue.MainnetGQL, venue.PriceFeedMainnet
	}

	ctx := context.Background()
	c, err := venue.Dial(ctx, rpc, gql)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	db, err := ledger.Open(*dbPath)
	if err != nil {
		log.Printf("ledger unavailable (%v); P&L panel will be empty", err)
	}

	s := &server{c: c, feedURL: feed, ledger: db, calibFile: *calib}

	http.Handle("/", uiHandler())
	http.Handle("/api/calibration", devCORS(http.HandlerFunc(s.calibration)))
	http.Handle("/api/live", devCORS(http.HandlerFunc(s.live)))
	http.Handle("/api/pnl", devCORS(http.HandlerFunc(s.pnl)))

	log.Printf("firstbid dashboard on http://localhost%s  (net=%s)", *addr, *net_)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

// devCORS lets `next dev` on another port read the API.
//
// In production the Go binary serves the UI and the API from one origin, so no
// CORS is involved at all. This exists only for the documented development
// setup, and it is deliberately restricted to loopback origins: a wildcard here
// would turn a local read API into one any web page could query.
func devCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); isLoopbackOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) calibration(w http.ResponseWriter, r *http.Request) {
	blob, err := os.ReadFile(s.calibFile)
	if err != nil {
		http.Error(w, `{"error":"run cmd/backtest with FB_EXPORT first"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("content-type", "application/json")
	w.Write(blob)
}

// LiveRow is one live window: what the market says, what our model says.
type LiveRow struct {
	Label       string  `json:"label"`
	Asset       string  `json:"asset"`
	SecondsLeft float64 `json:"secondsLeft"`
	Spot        float64 `json:"spot"`
	Open        float64 `json:"open"`
	Fair        float64 `json:"fair"`
	Uncertainty float64 `json:"uncertainty"`
	Bid         float64 `json:"bid"`
	Ask         float64 `json:"ask"`
	Spread      float64 `json:"spread"`
	OurSpread   float64 `json:"ourSpread"`
	Verdict     string  `json:"verdict"`
	Calibrated  bool    `json:"calibrated"`
}

func (s *server) live(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	spots, err := s.c.Spots(ctx, s.feedURL)
	if err != nil {
		http.Error(w, `{"error":"price feed unavailable"}`, http.StatusBadGateway)
		return
	}
	live, err := s.c.DiscoverLive(ctx)
	if err != nil {
		http.Error(w, `{"error":"indexer unavailable"}`, http.StatusBadGateway)
		return
	}
	ids := make([]string, 0, len(live))
	for _, m := range live {
		ids = append(ids, m.RowID)
	}
	opens, _ := s.c.OpeningPrices(ctx, ids)

	now := time.Now().Unix()
	out := []LiveRow{}
	for _, im := range live {
		sp, ok := spots[im.Asset]
		if !ok {
			continue
		}
		openRaw, ok := opens[im.RowID]
		if !ok {
			continue
		}
		m, err := s.c.ReadMarket(ctx, im.ID())
		if err != nil {
			continue
		}
		st, err := s.c.ReadState(ctx, m.MarketAddr)
		if err != nil || st.Status != venue.StatusTrading {
			continue
		}
		secs := float64(int64(m.Expiry) - now)
		if secs <= 0 {
			continue
		}
		row := LiveRow{
			Label: im.Asset + "/" + strconv.FormatInt(im.IntervalSecs()/60, 10) + "m",
			Asset: im.Asset, SecondsLeft: secs, Spot: sp.Price,
			Calibrated: model.Calibrated(im.Asset, im.IntervalSecs()),
		}
		row.Open = venue.NormaliseTo(openRaw, sp.Price)

		if b, err := s.c.ReadBook(ctx, m.Pool, 1); err == nil {
			row.Bid = probOf(b.BestBid(), im.QuoteDec)
			row.Ask = probOf(b.BestAsk(), im.QuoteDec)
			if row.Bid > 0 && row.Ask > 0 {
				row.Spread = row.Ask - row.Bid
			}
		}

		if !row.Calibrated {
			row.Verdict = "no validated model — we do not quote this cadence"
			out = append(out, row)
			continue
		}
		sigma, _ := model.SigmaPerMin(im.Asset, im.IntervalSecs())
		row.Fair = model.FairValue(sp.Price, row.Open, sigma, secs)
		row.Uncertainty = model.Uncertainty(sp.Price, row.Open, sigma, secs, 15)

		// Ask the engine's own logic what it would do, rather than restating it
		// here. A dashboard that reimplements the strategy eventually lies about it.
		p := maker.DefaultParams()
		if tk := maker.ShouldTake(row.Fair, row.Uncertainty, row.Bid, row.Ask, p); tk.Any() {
			row.Verdict = "TAKE — " + tk.Why
			row.OurSpread = 0
		} else {
			q := maker.Compute(row.Fair, row.Uncertainty, 0, secs, sp.Age.Seconds(), p)
			if q.SkipBid && q.SkipAsk {
				row.Verdict = "skip — " + q.Reason
			} else {
				row.OurSpread = q.Wide()
				if row.Spread > 0 && row.OurSpread < row.Spread {
					row.Verdict = "quote inside their spread"
				} else {
					row.Verdict = "quote wider — uncertainty is real here"
				}
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SecondsLeft < out[j].SecondsLeft })
	writeJSON(w, map[string]any{"rows": out, "spotAgeSec": spots["BTC"].Age.Seconds()})
}

func (s *server) pnl(w http.ResponseWriter, r *http.Request) {
	if s.ledger == nil {
		writeJSON(w, map[string]any{"windows": []any{}, "totals": ledger.Totals{}})
		return
	}
	as, err := s.ledger.Attribute(r.Context())
	if err != nil {
		http.Error(w, `{"error":"ledger read failed"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"windows": as, "totals": ledger.Sum(as)})
}

// probOf converts a raw pool price to a 0-1 probability at the collateral's scale.
func probOf(v *big.Int, dec int) float64 {
	if v == nil {
		return 0
	}
	den := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), den).Float64()
	return f
}

// uiHandler serves the embedded static export, falling back to the legacy page
// when the export is absent.
func uiHandler() http.Handler {
	sub, err := fs.Sub(ui, "dist")
	if err != nil {
		log.Printf("embedded UI unavailable (%v); serving fallback page", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/html; charset=utf-8")
			_, _ = w.Write(legacyHTML)
		})
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		log.Printf("no built UI in dist (%v); serving fallback page", err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/html; charset=utf-8")
			_, _ = w.Write(legacyHTML)
		})
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hashed build assets are immutable; the HTML must never be cached or a
		// judge reloading after a redeploy sees a stale interface.
		if strings.HasPrefix(r.URL.Path, "/_next/static/") {
			w.Header().Set("cache-control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("cache-control", "no-store")
		}
		files.ServeHTTP(w, r)
	})
}
