package maker

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/firstbid/core/internal/ledger"
	"github.com/firstbid/core/internal/model"
	"github.com/firstbid/core/internal/venue"
)

// Engine runs one quoting goroutine per live window and funnels every write
// through a single executor.
//
// One signer means one nonce sequence, so N goroutines may DECIDE concurrently
// but only one may WRITE. That split is the whole concurrency design.
type Engine struct {
	C       *venue.Client
	Trader  *venue.Trader // nil => dry run: decisions are logged, never sent
	Params  Params
	FeedURL string
	Log     *log.Logger
	Ledger  *ledger.DB // optional; nil disables recording

	spots   atomic.Pointer[map[string]venue.Spot]
	intents chan Intent

	mu      sync.Mutex
	running map[string]context.CancelFunc // marketId -> cancel
	stats   Stats
}

type Stats struct {
	Spawned, Reaped, Quotes, Skips, Sent, Failed, Cancelled, Fills, Crossed, Takes int64
}

// Action distinguishes the two things the executor can do.
type Action uint8

const (
	ActionPlace Action = iota
	ActionCancel
)

// Intent is a decision from a market goroutine, awaiting serialised execution.
type Intent struct {
	Action   Action
	OrderID  *big.Int // for ActionCancel
	Done     chan *venue.PlaceResult
	MarketID string
	Label    string
	Pool     common.Address
	Kind     venue.OrderKind
	Price    *big.Int
	Qty      *big.Int
	HumanPx  float64
	HumanQty float64
	ExpireNs uint64
	OrderTyp venue.OrderType
	// Decision context, recorded so P&L can be explained rather than just counted.
	Mode     string
	Fair     float64
	Spot     float64
	OpenPx   float64
	SecsLeft float64
}

func New(c *venue.Client, tr *venue.Trader, p Params, feedURL string, lg *log.Logger) *Engine {
	return &Engine{
		C: c, Trader: tr, Params: p, FeedURL: feedURL, Log: lg,
		intents: make(chan Intent, 256),
		running: map[string]context.CancelFunc{},
	}
}

func (e *Engine) DryRun() bool { return e.Trader == nil }

// Run starts the price poller, the executor and the discovery supervisor.
func (e *Engine) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); e.pollSpots(ctx) }()
	go func() { defer wg.Done(); e.execute(ctx) }()
	go func() { defer wg.Done(); e.supervise(ctx) }()
	wg.Wait()
	e.Log.Printf("engine stopped | %+v", e.Snapshot())
	return ctx.Err()
}

func (e *Engine) Snapshot() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stats
}

func (e *Engine) bump(f func(*Stats)) {
	e.mu.Lock()
	f(&e.stats)
	e.mu.Unlock()
}

// pollSpots keeps one shared index-price snapshot so N market goroutines cost
// one request per cycle rather than N.
func (e *Engine) pollSpots(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		s, err := e.C.Spots(ctx, e.FeedURL)
		if err == nil {
			e.spots.Store(&s)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (e *Engine) spot(asset string) (venue.Spot, bool) {
	p := e.spots.Load()
	if p == nil {
		return venue.Spot{}, false
	}
	s, ok := (*p)[asset]
	return s, ok
}

// supervise discovers live windows and owns the lifetime of every market loop.
func (e *Engine) supervise(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		e.discover(ctx)
		select {
		case <-ctx.Done():
			e.stopAll()
			return
		case <-t.C:
		}
	}
}

func (e *Engine) discover(ctx context.Context) {
	live, err := e.C.DiscoverLive(ctx)
	if err != nil {
		e.Log.Printf("discover: %v", err)
		return
	}
	ids := make([]string, 0, len(live))
	for _, m := range live {
		ids = append(ids, m.RowID)
	}
	opens, err := e.C.OpeningPrices(ctx, ids)
	if err != nil {
		e.Log.Printf("opening prices: %v", err)
		return
	}

	for _, im := range live {
		e.mu.Lock()
		_, already := e.running[im.MarketID]
		e.mu.Unlock()
		if already {
			continue
		}
		open, ok := opens[im.RowID]
		if !ok {
			continue // no line to beat yet; pick it up on the next sweep
		}
		// Never quote a series we have no validated model for. Extrapolating
		// sqrt(t) from 60m out to 240m is a guess, and a maker that guesses pays.
		if !model.Calibrated(im.Asset, im.IntervalSecs()) {
			continue
		}
		m, err := e.C.ReadMarket(ctx, im.ID())
		if err != nil {
			continue
		}
		st, err := e.C.ReadState(ctx, m.MarketAddr)
		if err != nil || st.Status != venue.StatusTrading {
			continue
		}

		// The deadline guarantees teardown even if we never see a settlement
		// event: a goroutine cannot outlive the window it trades.
		mctx, cancel := context.WithDeadline(ctx, time.Unix(int64(m.Expiry), 0))
		e.mu.Lock()
		e.running[im.MarketID] = cancel
		e.mu.Unlock()
		e.bump(func(s *Stats) { s.Spawned++ })

		go e.marketLoop(mctx, im, m, open)
	}
}

func (e *Engine) stopAll() {
	e.mu.Lock()
	for _, c := range e.running {
		c()
	}
	e.running = map[string]context.CancelFunc{}
	e.mu.Unlock()
}

func (e *Engine) reap(marketID string) {
	e.mu.Lock()
	if c, ok := e.running[marketID]; ok {
		c()
		delete(e.running, marketID)
	}
	e.mu.Unlock()
	e.bump(func(s *Stats) { s.Reaped++ })
}

// marketLoop quotes one window for its whole life, then exits.
func (e *Engine) marketLoop(ctx context.Context, im venue.IndexerMarket, m *venue.Market, openRaw float64) {
	defer e.reap(im.MarketID)
	label := fmt.Sprintf("%s/%dm", im.Asset, im.IntervalSecs()/60)

	bp, err := e.C.BookParams(ctx, m.Pool)
	if err != nil {
		e.Log.Printf("[%s] book params: %v", label, err)
		return
	}
	// Each pool pulls collateral itself, so each pool needs its own approval.
	// Pools are recycled across windows, so this is usually already in place.
	if !e.DryRun() {
		granted, err := e.Trader.EnsureApproval(ctx, venue.TestUSDC, m.Pool)
		if err != nil {
			e.Log.Printf("[%s] approval failed, not quoting: %v", label, err)
			return
		}
		if granted {
			e.Log.Printf("[%s] approved pool %s", label, m.Pool.Hex())
		}
	}

	if e.Ledger != nil {
		_ = e.Ledger.UpsertWindow(ctx, ledger.WindowRow{
			MarketID: im.MarketID, Label: label, Asset: im.Asset,
			IntervalSec: im.IntervalSecs(), Expiry: int64(m.Expiry),
		})
	}

	sigma, _ := model.SigmaPerMin(im.Asset, im.IntervalSecs())

	// Short windows need attention near expiry; long ones do not.
	interval := time.Duration(clampI(im.IntervalSecs()/20, 2, 30)) * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()

	var inventory float64 // contracts of Up held; negative means net Down

	for {
		select {
		case <-ctx.Done():
			e.Log.Printf("[%s] window closed, loop exiting", label)
			// Detach from the cancelled context: settlement happens after expiry.
			go e.settle(context.WithoutCancel(ctx), im, m, label)
			return
		case <-t.C:
		}

		sp, ok := e.spot(im.Asset)
		if !ok {
			continue
		}
		open := venue.NormaliseTo(openRaw, sp.Price)
		secsLeft := float64(int64(m.Expiry) - time.Now().Unix())
		if secsLeft <= 0 {
			return
		}

		fair := model.FairValue(sp.Price, open, sigma, secsLeft)
		// The risk horizon is how long we are exposed before we can requote,
		// not the whole window: one tick plus a round trip.
		unc := model.Uncertainty(sp.Price, open, sigma, secsLeft, interval.Seconds()+3)
		q := Compute(fair, unc, inventory, secsLeft, sp.Age.Seconds(), e.Params)

		// Before resting anything, check whether the live book is simply wrong.
		// Crossing a stale quote is worth more than sitting behind it.
		bestBid, bestAsk := e.touch(ctx, m.Pool, im.QuoteDec)
		if tk := ShouldTake(fair, unc, bestBid, bestAsk, e.Params); tk.Any() && secsLeft > e.Params.MinSecondsLeft {
			kind := venue.BuyYes
			if tk.BuyDn {
				kind = venue.BuyNo
			}
			e.Log.Printf("[%s] t-%3.0fs fair=%.3f book=%.3f/%.3f  TAKE %s edge=%.3f (%s)",
				label, secsLeft, fair, bestBid, bestAsk, kindName(kind), tk.Edge, tk.Why)
			e.bump(func(s *Stats) { s.Takes++ })
			// Cross with a little slack, IOC so no remainder rests behind us.
			// Slack moves along the YES axis, so its sign depends on the side:
			// lifting an ask means paying up, hitting a bid means going lower.
			limit := tk.Price + 0.005
			if tk.BuyDn {
				limit = tk.Price - 0.005
			}
			e.emitTyped(ctx, im, m, bp, label, kind, limit, expireNsFor(m, interval), venue.TypeMarket,
				decision{"take", fair, sp.Price, open, secsLeft})
			continue
		}

		expireNs := expireNsFor(m, interval)
		dc := decision{"make", fair, sp.Price, open, secsLeft}

		// Pull last tick's quotes before posting new ones. Without this the book
		// fills with stale orders at prices we no longer believe, and our own
		// resting orders start self-matching the new ones.
		e.cancelResting(ctx, m, label)

		if q.SkipBid && q.SkipAsk {
			e.bump(func(s *Stats) { s.Skips++ })
			e.Log.Printf("[%s] t-%3.0fs fair=%.3f  SKIP (%s)", label, secsLeft, fair, q.Reason)
			continue
		}
		e.bump(func(s *Stats) { s.Quotes++ })
		e.Log.Printf("[%s] t-%3.0fs spot=%.2f open=%.2f fair=%.3f unc=%.3f -> quote %.3f/%.3f (%.3f wide, inv %.1f)",
			label, secsLeft, sp.Price, open, fair, unc, q.BidUp, q.AskUp, q.Wide(), inventory)

		// Two opposite-side BUYS are a complete two-sided quote needing ZERO
		// inventory, because the pool mints the pair when they cross
		// (mint-a-pair). BOTH are priced in YES terms: the bid goes in at BidUp,
		// and the Down order goes in at AskUp, where it rests as our ask.
		if !q.SkipBid {
			e.emit(ctx, im, m, bp, label, venue.BuyYes, q.BidUp, expireNs, dc)
		}
		if !q.SkipAsk {
			e.emit(ctx, im, m, bp, label, venue.BuyNo, q.AskUp, expireNs, dc)
		}
	}
}

func (e *Engine) emit(ctx context.Context, im venue.IndexerMarket, m *venue.Market,
	bp *venue.BookParams, label string, kind venue.OrderKind, px float64, expireNs uint64,
	dc decision) *big.Int {
	return e.emitTyped(ctx, im, m, bp, label, kind, px, expireNs, venue.TypePostOnly, dc)
}

func (e *Engine) emitTyped(ctx context.Context, im venue.IndexerMarket, m *venue.Market,
	bp *venue.BookParams, label string, kind venue.OrderKind, px float64, expireNs uint64,
	ot venue.OrderType, dc decision) *big.Int {

	price := venue.SnapPrice(px, bp.TickSize, im.QuoteDec)
	qty := venue.SnapQty(e.Params.Size, bp.LotSize, im.QuoteDec)
	if qty.Sign() == 0 || price.Sign() == 0 {
		return nil // below one lot floors to zero and would never reach the book
	}
	done := make(chan *venue.PlaceResult, 1)
	select {
	case e.intents <- Intent{
		Action: ActionPlace, Done: done,
		MarketID: im.MarketID, Label: label, Pool: m.Pool,
		Kind: kind, Price: price, Qty: qty, HumanPx: px, HumanQty: e.Params.Size,
		ExpireNs: expireNs, OrderTyp: ot,
		Mode: dc.mode, Fair: dc.fair, Spot: dc.spot, OpenPx: dc.open, SecsLeft: dc.secsLeft,
	}:
	case <-ctx.Done():
		return nil
	default:
		e.Log.Printf("[%s] intent queue full, dropping quote", label)
		return nil
	}
	select {
	case r := <-done:
		if r == nil {
			return nil
		}
		return r.OrderID
	case <-ctx.Done():
		return nil
	}
}

// cancelResting pulls every order of ours still on this pool's book, as the
// pool itself reports it. Ids from receipts proved unreliable; the pool is truth.
func (e *Engine) cancelResting(ctx context.Context, m *venue.Market, label string) {
	if e.DryRun() {
		return
	}
	ids, err := e.C.OwnOpenOrders(ctx, m.Pool, e.Trader.From())
	if err != nil {
		e.Log.Printf("[%s] own open orders: %v", label, err)
		return
	}
	for _, id := range ids {
		done := make(chan *venue.PlaceResult, 1)
		select {
		case e.intents <- Intent{Action: ActionCancel, OrderID: id, Pool: m.Pool, Label: label, Done: done}:
		case <-ctx.Done():
			return
		}
		select {
		case <-done:
		case <-ctx.Done():
			return
		}
	}
}

// execute is the ONLY writer. Serialising here is what keeps the nonce sane.
func (e *Engine) execute(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case in := <-e.intents:
			e.handle(ctx, in)
		}
	}
}

func kindName(k venue.OrderKind) string {
	switch k {
	case venue.BuyYes:
		return "BUY_UP"
	case venue.SellYes:
		return "SELL_UP"
	case venue.BuyNo:
		return "BUY_DN"
	case venue.SellNo:
		return "SELL_DN"
	}
	return "?"
}

func clampI(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// handle performs one write. It is called only from execute, so writes are
// strictly serialised and the locally-tracked nonce can never be raced.
func (e *Engine) handle(ctx context.Context, in Intent) {
	reply := func(r *venue.PlaceResult) {
		if in.Done != nil {
			in.Done <- r
		}
	}

	if in.Action == ActionCancel {
		if e.DryRun() {
			e.Log.Printf("  DRY  %s CANCEL  #%s", in.Label, in.OrderID)
			reply(nil)
			return
		}
		if _, err := e.Trader.Cancel(ctx, in.Pool, in.OrderID); err != nil {
			// An order that already filled or expired cannot be cancelled; that
			// is a normal race on a quoting loop, not a fault.
			e.Log.Printf("  CXL  %s #%s: %v", in.Label, in.OrderID, err)
		} else {
			e.bump(func(s *Stats) { s.Cancelled++ })
		}
		reply(nil)
		return
	}

	if e.DryRun() {
		e.Log.Printf("  DRY  %s %-7s %.3f x %.1f", in.Label, kindName(in.Kind), in.HumanPx, in.HumanQty)
		reply(nil)
		return
	}
	res, err := e.Trader.PlaceTracked(ctx, venue.PlaceOrder{
		Pool: in.Pool, Kind: in.Kind, Price: in.Price, Quantity: in.Qty,
		Type: in.OrderTyp, ExpireNs: in.ExpireNs,
	})
	if err != nil {
		// PostOnlyWouldCross is not a fault: the touch moved through our price
		// between the read and the send. On a quoting loop that is routine.
		if strings.Contains(err.Error(), "PostOnlyWouldCross") {
			e.bump(func(s *Stats) { s.Crossed++ })
			e.Log.Printf("  xx   %s %-7s %.3f would cross, requoting", in.Label, kindName(in.Kind), in.HumanPx)
			reply(nil)
			return
		}
		e.bump(func(s *Stats) { s.Failed++ })
		e.Log.Printf("  SEND %s %-7s %.3f FAILED: %v", in.Label, kindName(in.Kind), in.HumanPx, err)
		reply(nil)
		return
	}
	e.bump(func(s *Stats) { s.Sent++ })
	if n := len(res.Fills); n > 0 {
		e.bump(func(s *Stats) { s.Fills += int64(n) })
	}
	e.record(ctx, in, res)
	e.Log.Printf("  SENT %s %-7s %.3f x %.1f  id=%v rested=%v fills=%d tx=%s",
		in.Label, kindName(in.Kind), in.HumanPx, in.HumanQty,
		res.OrderID, res.Rested, len(res.Fills), res.TxHash.Hex())
	reply(res)
}

// touch reads the best bid and ask from ONE block, in Up terms. Two unpinned
// reads can straddle a block and produce a crossed book that never existed.
func (e *Engine) touch(ctx context.Context, pool common.Address, dec int) (bid, ask float64) {
	b, err := e.C.ReadBook(ctx, pool, 1)
	if err != nil {
		return 0, 0
	}
	return rawToProb(b.BestBid(), dec), rawToProb(b.BestAsk(), dec)
}

func rawToProb(v *big.Int, dec int) float64 {
	if v == nil {
		return 0
	}
	d := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), d).Float64()
	return f
}

// expireNsFor caps an order's expiry at the market's own, which the pool
// enforces with OrderExpiryBeyondMarket, and keeps it just past our requote
// interval so a crashed quoter's orders age off the book by themselves.
func expireNsFor(m *venue.Market, interval time.Duration) uint64 {
	expireAt := time.Now().Add(3 * interval)
	if lock := time.Unix(int64(m.Expiry), 0).Add(-2 * time.Second); expireAt.After(lock) {
		expireAt = lock
	}
	return uint64(expireAt.Unix()) * 1_000_000_000
}

// decision is the model state that justified one order, carried so the ledger
// can attribute profit to a belief rather than just tally cash.
type decision struct {
	mode     string
	fair     float64
	spot     float64
	open     float64
	secsLeft float64
}

func (e *Engine) record(ctx context.Context, in Intent, res *venue.PlaceResult) {
	if e.Ledger == nil || res == nil {
		return
	}
	kind := kindName(in.Kind)

	// Orders are priced in YES terms on the wire, but the ledger needs what we
	// actually PAID. A BUY_DN at YES price p costs 1-p for a Down contract.
	// Storing the wire price here would inflate reported edge enormously.
	cost := in.HumanPx
	if in.Kind == venue.BuyNo || in.Kind == venue.SellNo {
		cost = 1 - in.HumanPx
	}
	_ = e.Ledger.RecordOrder(ctx, ledger.OrderRow{
		TxHash: res.TxHash.Hex(), MarketID: in.MarketID, Label: in.Label,
		Kind: kind, Mode: in.Mode, Price: cost, Quantity: in.HumanQty,
		Fair: in.Fair, Spot: in.Spot, OpenPx: in.OpenPx, SecsLeft: in.SecsLeft,
		Rested: res.Rested, Fills: len(res.Fills),
	})
	for range res.Fills {
		// A fill's economics are the price we committed to and the size we asked
		// for; the receipt's raw units are pool-scaled, so the human figures the
		// decision was made on are the honest record.
		_ = e.Ledger.RecordFill(ctx, ledger.FillRow{
			TxHash: res.TxHash.Hex(), MarketID: in.MarketID, Kind: kind,
			Price: cost, Quantity: in.HumanQty, Fair: in.Fair,
		})
	}
}

// settle waits for the oracle to resolve a window and records the outcome, so
// realised P&L becomes arithmetic rather than an estimate.
//
// Resolution is keeper-free: Somnia reactivity delivers the oracle answer into
// the module's callback, so we only have to watch, never to poke.
func (e *Engine) settle(ctx context.Context, im venue.IndexerMarket, m *venue.Market, label string) {
	if e.Ledger == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			e.Log.Printf("[%s] settlement not observed before timeout", label)
			return
		case <-t.C:
		}
		st, err := e.C.ReadState(ctx, m.MarketAddr)
		if err != nil {
			continue
		}
		if !st.IsResolved && !st.IsVoided {
			continue
		}
		winner := -1
		if st.IsResolved {
			// Never guess the winner: a wrong outcome silently inverts the P&L
			// of every fill in this window. If the read fails, retry next tick.
			w, err := e.C.WinningOutcome(ctx, m.MarketAddr)
			if err != nil {
				e.Log.Printf("[%s] resolved but winner unreadable, retrying: %v", label, err)
				continue
			}
			winner = w
		}
		_ = e.Ledger.Settle(ctx, im.MarketID, winner, st.IsVoided, 0, 0)
		e.Log.Printf("[%s] SETTLED winner=%d voided=%v", label, winner, st.IsVoided)
		return
	}
}
