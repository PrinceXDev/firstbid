package maker

import (
	"context"
	"fmt"
	"log"
	"math"
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
	// Net Up contracts held per market, accumulated from decoded fills. Quote
	// skew and the exposure caps are meaningless without it: a loop that always
	// reports flat will happily build a large one-sided position.
	inv map[string]float64
	// Which asset each live market belongs to, so assetExposure can sum inv
	// across every window on one asset. Kept as a DERIVED read over inv rather
	// than a separately incremented total: a second running total can drift
	// from the thing it is supposed to summarise (see reap, where a window's
	// inv entry dies with the window -- a derived sum forgets it for free;
	// a separately maintained one would not, unless every removal path also
	// remembered to subtract from it).
	marketAsset map[string]string
	// A resting post-only order does not change realised exposure the moment
	// it is placed -- it changes it later, asynchronously, when a
	// counterparty fills it (often discovered only by reconcile(), not this
	// engine's own transaction). Gating a NEW resting order only against
	// CURRENT realised exposure is not enough: several resting orders across
	// different windows can each individually pass that check while none of
	// them has filled yet, and then all fill later and together breach the
	// cap with nothing left to refuse at that point. These track the size of
	// the currently-resting order on each side of each market, so a new
	// resting order can be checked against the WORST CASE for its own
	// direction -- realised exposure plus every other order already resting
	// the same way, as if all of them filled and none of the opposite side
	// did.
	restingUp map[string]float64 // marketId -> size of our resting BuyUp order, if any
	restingDn map[string]float64 // marketId -> size of our resting BuyDn order, if any
	stats     Stats
}

type Stats struct {
	Spawned, Reaped, Quotes, QuotesHeld, Skips, Sent, Failed, Cancelled, Fills, Crossed, Takes int64
	// TakesRefused counts priced takes the per-window risk budget blocked.
	// Reported alongside Takes because what the engine declines to do is a
	// result, not an absence of one.
	TakesRefused int64
	// ExposureRefused counts takes the cross-window asset exposure cap
	// blocked -- distinct from TakesRefused because the two guards catch
	// different failure shapes and collapsing them would hide which one fired.
	ExposureRefused int64
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
	Asset    string
	Label    string
	Pool     common.Address
	Kind     venue.OrderKind
	Price    *big.Int
	Qty      *big.Int
	HumanPx  float64
	HumanQty float64
	ExpireNs uint64
	OrderTyp venue.OrderType
	Dec      int // collateral decimals, for converting decoded fills
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
		intents:     make(chan Intent, 256),
		running:     map[string]context.CancelFunc{},
		inv:         map[string]float64{},
		marketAsset: map[string]string{},
		restingUp:   map[string]float64{},
		restingDn:   map[string]float64{},
	}
}

func (e *Engine) DryRun() bool { return e.Trader == nil }

// Run starts the price poller, the executor and the discovery supervisor.
func (e *Engine) Run(ctx context.Context) error {
	e.restoreInventory(ctx)

	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); e.pollSpots(ctx) }()
	go func() { defer wg.Done(); e.execute(ctx) }()
	go func() { defer wg.Done(); e.supervise(ctx) }()
	go func() { defer wg.Done(); e.reconcile(ctx) }()
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
		e.marketAsset[im.MarketID] = im.Asset
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
	// Quote-time exposure state dies with the window; realised accounting lives
	// in the ledger. Keeping it here would leak a map entry per window forever.
	asset := e.marketAsset[marketID]
	delete(e.inv, marketID)
	delete(e.marketAsset, marketID)
	// Any order still resting on this market ages off via its own on-chain
	// expiry (capped at the window's own expiry, minus a margin -- see
	// expireNsFor), so it can no longer fill once the window is over. Its
	// exposure reservation must not outlive it.
	delete(e.restingUp, marketID)
	delete(e.restingDn, marketID)
	e.mu.Unlock()
	e.bump(func(s *Stats) { s.Reaped++ })
	// The dashboard's persisted exposure row is a snapshot from the last fill,
	// not a live query -- without pushing the reduced total here, a window
	// that closes flat (or with its last fill already recorded) would leave a
	// stale, too-high number on screen and, worse, would let the persisted
	// value outlive the position it described.
	if asset != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		e.persistExposure(ctx, asset)
		cancel()
	}
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
		if err := e.Ledger.UpsertWindow(ctx, ledger.WindowRow{
			MarketID: im.MarketID, Label: label, Asset: im.Asset,
			IntervalSec: im.IntervalSecs(), Expiry: int64(m.Expiry),
		}); err != nil {
			e.Log.Printf("[%s] ledger: upsert window: %v", label, err)
		}
	}

	sigma, _ := model.SigmaPerMin(im.Asset, im.IntervalSecs())

	// The venue's tick in human units; two quotes closer than this are the
	// same order once snapped.
	tickSize := rawToHuman(bp.TickSize, im.QuoteDec)
	var resting restingQuote
	// Hoisted: the map is immutable, and rebuilding it per tick would be waste.
	cmap := model.CalibrationMap()

	// Short windows need attention near expiry; long ones do not.
	interval := time.Duration(clampI(im.IntervalSecs()/20, 2, 30)) * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()

	// One budget per window, goroutine-local like everything else in this loop.
	// Takes inside a window are perfectly correlated, so the budget -- not the
	// take rule -- is what bounds the damage from a single wrong belief.
	// Inventory is read per tick via e.inventory rather than accumulated here,
	// so a restart or an out-of-band fill cannot desynchronise it.
	budget := NewTakeBudget(e.Params)

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
			// Both this branch and ctx.Done() are the same lifecycle boundary,
			// and Go may pick either when the ticker and the deadline are both
			// ready. Settlement must be scheduled from whichever one wins, or
			// the window is silently excluded from realised attribution.
			go e.settle(context.WithoutCancel(ctx), im, m, label)
			return
		}

		// The diffusion model states a probability; the calibration map converts
		// it into one the venue's own history supports. Everything downstream --
		// quotes, takes, the ledger's fair -- uses the calibrated value, and the
		// residual travels with it as the error we must clear to act.
		rawFair := model.FairValue(sp.Price, open, sigma, secsLeft)
		fair := cmap.Apply(rawFair)
		// The floor a take must clear: the model's measured calibration error,
		// widened by the fitted map's residual if a validated map is shipped.
		resid := model.EdgeFloor(rawFair)
		// The risk horizon is how long we are exposed before we can requote,
		// not the whole window: one tick plus a round trip.
		unc := model.Uncertainty(sp.Price, open, sigma, secsLeft, interval.Seconds()+3)
		inv := e.inventory(im.MarketID)
		q := Compute(fair, unc, inv, secsLeft, sp.Age.Seconds(), e.Params)

		// Before resting anything, check whether the live book is simply wrong.
		// Crossing a stale quote is worth more than sitting behind it.
		bestBid, bestAsk := e.touch(ctx, m.Pool, im.QuoteDec)
		tk := ShouldTake(fair, unc, resid, bestBid, bestAsk, e.Params)

		// Taking is a WRITE, so it answers to the same pre-signing gates as a
		// quote. Compute already evaluated stale data, imminent lock, certainty
		// bounds and inventory caps; a take that ignores them would route round
		// every safety check the maker path respects.
		if tk.Any() {
			blocked := ""
			switch {
			case q.SkipBid && q.SkipAsk:
				blocked = q.Reason
			case tk.BuyUp && q.SkipBid:
				blocked = "at the long inventory cap"
			case tk.BuyDn && q.SkipAsk:
				blocked = "at the short inventory cap"
			}
			if blocked != "" {
				e.bump(func(s *Stats) { s.Skips++ })
				e.Log.Printf("[%s] t-%3.0fs edge %.3f available but NOT taking (%s)",
					label, secsLeft, tk.Edge, blocked)
				continue
			}
		}

		if tk.Any() {
			kind := venue.BuyYes
			if tk.BuyDn {
				kind = venue.BuyNo
			}
			// Cross with a little slack, IOC so no remainder rests behind us.
			// Slack moves along the YES axis, so its sign depends on the side:
			// lifting an ask means paying up, hitting a bid means going lower.
			limit := tk.Price + 0.005
			if tk.BuyDn {
				limit = tk.Price - 0.005
			}
			cost := collateralCost(kind, limit)

			// The asset exposure cap is NOT checked here. Every marketLoop
			// runs in its own goroutine and decides concurrently, so a check
			// made here can only ever compare against a stale snapshot -- two
			// windows on the same asset could both pass an identical check a
			// moment before either one's fill is recorded, and their
			// sequential fills could together exceed the cap the check meant
			// to enforce. handle() is the one goroutine that ever turns a
			// decision into a write, so it is where that check is atomic; see
			// its exposure guard, which covers this take AND the resting
			// orders below (both go through the same Intent path).
			//
			// The pricing decision says the book is wrong. The budget decides
			// whether we are allowed to act on it again in this window.
			if ok, why := budget.Allow(time.Now(), cost, e.Params.Size); !ok {
				spent, notional := budget.Spent()
				e.Log.Printf("[%s] t-%3.0fs fair=%.3f book=%.3f/%.3f  TAKE REFUSED %s"+
					" (edge=%.3f, spent %d takes / %.2f)",
					label, secsLeft, fair, bestBid, bestAsk, why, tk.Edge, spent, notional)
				e.bump(func(s *Stats) { s.TakesRefused++ })
			} else {
				e.Log.Printf("[%s] t-%3.0fs fair=%.3f book=%.3f/%.3f  TAKE %s edge=%.3f (%s)",
					label, secsLeft, fair, bestBid, bestAsk, kindName(kind), tk.Edge, tk.Why)
				e.bump(func(s *Stats) { s.Takes++ })
				if id := e.emitTyped(ctx, im, m, bp, label, kind, limit, expireNsFor(m, interval),
					venue.TypeMarket, decision{"take", fair, sp.Price, open, secsLeft}); id != nil {
					budget.Record(time.Now(), cost, e.Params.Size)
				}
				continue
			}
		}

		expireNs := expireNsFor(m, interval)
		dc := decision{"make", fair, sp.Price, open, secsLeft}

		// Requoting costs two cancels and two placements in gas. If the new
		// quote lands on the same tick as the one already resting, replacing it
		// buys nothing and burns the budget that keeps the engine alive.
		//
		// The orders carry their own expiry as a dead-man's switch, so leaving
		// them in place is safe: a crashed quoter's orders still age off.
		if !q.SkipBid && !q.SkipAsk && resting.matches(q, tickSize, time.Now()) {
			resting.hold()
			e.bump(func(s *Stats) { s.QuotesHeld++ })
			continue
		}

		// Pull last tick's quotes before posting new ones. Without this the book
		// fills with stale orders at prices we no longer believe, and our own
		// resting orders start self-matching the new ones.
		e.cancelResting(ctx, m, label)
		resting.clear()
		// The cancelled orders are no longer at risk of filling; release their
		// exposure reservation too, or every later quote on this asset would
		// keep being checked against risk that no longer exists.
		e.clearResting(im.MarketID)

		if q.SkipBid && q.SkipAsk {
			e.bump(func(s *Stats) { s.Skips++ })
			e.Log.Printf("[%s] t-%3.0fs fair=%.3f  SKIP (%s)", label, secsLeft, fair, q.Reason)
			continue
		}
		e.bump(func(s *Stats) { s.Quotes++ })
		e.Log.Printf("[%s] t-%3.0fs spot=%.2f open=%.2f fair=%.3f unc=%.3f -> quote %.3f/%.3f (%.3f wide, inv %.1f)",
			label, secsLeft, sp.Price, open, fair, unc, q.BidUp, q.AskUp, q.Wide(), inv)

		// Two opposite-side BUYS are a complete two-sided quote needing ZERO
		// inventory, because the pool mints the pair when they cross
		// (mint-a-pair). BOTH are priced in YES terms: the bid goes in at BidUp,
		// and the Down order goes in at AskUp, where it rests as our ask.
		placed := 0
		if !q.SkipBid {
			if id := e.emit(ctx, im, m, bp, label, venue.BuyYes, q.BidUp, expireNs, dc); id != nil {
				placed++
			}
		}
		if !q.SkipAsk {
			if id := e.emit(ctx, im, m, bp, label, venue.BuyNo, q.AskUp, expireNs, dc); id != nil {
				placed++
			}
		}
		if placed == 2 {
			resting.set(q, tickSize, time.Unix(int64(expireNs/1_000_000_000), 0))
		} else {
			resting.clear()
		}
	}
}

// restingQuote remembers what we last put on the book, so an unchanged quote is
// left alone instead of being cancelled and replaced at the same price.
type restingQuote struct {
	live      bool
	bidTick   int64 // snapped to the venue's grid, not the raw float
	askTick   int64
	expiresAt time.Time
	holds     int
}

// maxHolds forces a full requote cycle periodically even when the quote has not
// moved. That cycle re-reads our resting orders from the pool, so the engine
// cannot drift indefinitely on a remembered state that the chain no longer
// agrees with.
const maxHolds = 8

func (r *restingQuote) set(q Quote, tick float64, expiresAt time.Time) {
	r.live = true
	r.bidTick = snapTick(q.BidUp, tick)
	r.askTick = snapTick(q.AskUp, tick)
	r.expiresAt = expiresAt
	r.holds = 0
}

func (r *restingQuote) clear() { r.live = false; r.holds = 0 }

// matches reports whether a new quote would land on exactly the same ticks as
// the orders already resting.
//
// Comparing raw distance is wrong: the venue rounds each price independently,
// so two prices less than a tick apart can still snap to different ticks
// (0.4504 and 0.4506 are 0.0002 apart and land on 0.450 and 0.451). Only the
// snapped values decide whether the order on the book is the order we want.
//
// It also refuses to hold past the orders' own on-chain expiry: an order that
// has aged off the book is not resting, and believing otherwise leaves the
// market unquoted until something else moves the quote.
func (r *restingQuote) matches(q Quote, tick float64, now time.Time) bool {
	if !r.live || tick <= 0 {
		return false
	}
	if r.holds >= maxHolds {
		return false
	}
	// Stop trusting the memory before the orders actually expire, so we replace
	// them rather than discovering the gap afterwards.
	if !r.expiresAt.IsZero() && now.After(r.expiresAt.Add(-15*time.Second)) {
		return false
	}
	return snapTick(q.BidUp, tick) == r.bidTick && snapTick(q.AskUp, tick) == r.askTick
}

func (r *restingQuote) hold() { r.holds++ }

// snapTick maps a probability onto the venue's integer tick grid.
func snapTick(p, tick float64) int64 {
	if tick <= 0 {
		return 0
	}
	return int64(math.Round(p / tick))
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
		MarketID: im.MarketID, Asset: im.Asset, Label: label, Pool: m.Pool,
		Kind: kind, Price: price, Qty: qty, HumanPx: px, HumanQty: e.Params.Size,
		ExpireNs: expireNs, OrderTyp: ot,
		Mode: dc.mode, Fair: dc.fair, Spot: dc.spot, OpenPx: dc.open, SecsLeft: dc.secsLeft,
		Dec: im.QuoteDec,
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

	// The cross-window asset exposure cap is checked HERE rather than in
	// marketLoop. Several marketLoop goroutines can decide concurrently, but
	// handle() is the one goroutine every write funnels through and is
	// strictly serialised by the intents channel, so a check made here sees
	// the true, up-to-date exposure left by every fill this engine has
	// already recorded -- including the one immediately before it in the
	// queue. That is what closes the TOCTOU gap a check made earlier could
	// not: two takes on the same asset can no longer both pass against the
	// same stale number, because the second one is only evaluated after the
	// first has either landed (and been recorded) or been rejected.
	delta := in.HumanQty
	if in.Kind == venue.BuyNo || in.Kind == venue.SellNo {
		delta = -in.HumanQty
	}
	if ok, prospective := ExposureAllows(e.exposureBaseline(in.Asset, in.Mode, delta), delta, e.Params.MaxAssetExposure); !ok {
		e.bump(func(s *Stats) { s.ExposureRefused++ })
		e.Log.Printf("[%s] %-7s %.3f x %.1f  REFUSED asset exposure cap"+
			" (%s would reach %.1f of %.1f including resting orders, fair=%.3f)",
			in.Label, kindName(in.Kind), in.HumanPx, in.HumanQty, in.Asset, prospective, e.Params.MaxAssetExposure, in.Fair)
		reply(nil)
		return
	}

	if e.DryRun() {
		e.Log.Printf("  DRY  %s %-7s %.3f x %.1f", in.Label, kindName(in.Kind), in.HumanPx, in.HumanQty)
		reply(nil)
		return
	}
	// Somnia's fast finality is the whole reason a quote priced to seconds-left
	// uncertainty is tradeable at all -- a chain slow to confirm could not
	// safely act this close to expiry. This is the number that claim rests on,
	// measured rather than asserted: signing, RPC submission and mining, start
	// to finish, on every order actually sent.
	sendStart := time.Now()
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
	if in.Mode == "make" {
		// Reserve exactly what is still unfilled and actually resting, so the
		// next order's worst-case check sees this one. A quantity already
		// filled in this same transaction is realised, not resting -- it is
		// accounted for below by e.record(), and double-reserving it here
		// would count it against the cap twice.
		var filled float64
		for _, f := range res.Fills {
			filled += rawToHuman(f.Quantity, in.Dec)
		}
		if remaining := in.HumanQty - filled; res.Rested && remaining > 0 {
			e.setResting(in.MarketID, in.Kind, remaining)
		} else {
			e.setResting(in.MarketID, in.Kind, 0)
		}
	}
	if e.Ledger != nil {
		millis := float64(time.Since(sendStart).Microseconds()) / 1000
		if err := e.Ledger.RecordLatency(ctx, in.MarketID, "submit-to-receipt", millis); err != nil {
			e.Log.Printf("ledger: record latency %s: %v", in.MarketID, err)
		}
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
// collateralCost converts a YES-axis wire price into the collateral actually
// paid per contract for a given side.
//
// Limits, book touches and fair values all live on the YES axis, but a BUY_DN
// at YES price p is a NO contract costing 1-p. The risk budget is denominated
// in money, so it must see the second number: charging the YES price for a Down
// take overstates it whenever YES is expensive -- a take at 0.90 would consume
// 0.90 of budget for something that cost 0.10 -- and the window would then
// refuse later takes it could afford. record() applies the same conversion for
// the ledger; this keeps the two accounts describing the same trade.
func collateralCost(kind venue.OrderKind, yesPrice float64) float64 {
	if kind == venue.BuyNo || kind == venue.SellNo {
		return 1 - yesPrice
	}
	return yesPrice
}

type decision struct {
	mode     string
	fair     float64
	spot     float64
	open     float64
	secsLeft float64
}

func (e *Engine) record(ctx context.Context, in Intent, res *venue.PlaceResult) {
	if res == nil {
		return
	}
	kind := kindName(in.Kind)

	// Orders are priced in YES terms on the wire, but the ledger needs what we
	// actually PAID. A BUY_DN at YES price p costs 1-p for a Down contract.
	toCost := func(yesPrice float64) float64 {
		return collateralCost(in.Kind, yesPrice)
	}

	if e.Ledger != nil {
		_ = e.Ledger.RecordOrder(ctx, ledger.OrderRow{
			TxHash: res.TxHash.Hex(), MarketID: in.MarketID, Label: in.Label,
			Kind: kind, Mode: in.Mode, Price: toCost(in.HumanPx), Quantity: in.HumanQty,
			Fair: in.Fair, Spot: in.Spot, OpenPx: in.OpenPx, SecsLeft: in.SecsLeft,
			Rested: res.Rested, Fills: len(res.Fills),
		})
	}

	// Each OrderFilled event carries the quantity and price that actually
	// executed. Recording the requested size and the submitted limit instead
	// would multiply partial fills by the full order size and discard any price
	// improvement or slippage — the ledger would describe what we asked for
	// rather than what happened.
	for i, f := range res.Fills {
		qty := rawToHuman(f.Quantity, in.Dec)
		px := rawToHuman(f.Price, in.Dec)
		if qty <= 0 {
			continue
		}
		if px <= 0 {
			px = in.HumanPx // no price in the log; the limit is the best we know
		}
		signed := qty
		if in.Kind == venue.BuyNo || in.Kind == venue.SellNo {
			signed = -qty
		}
		e.addInventory(in.MarketID, signed)
		e.persistExposure(ctx, in.Asset)

		if e.Ledger != nil {
			if err := e.Ledger.RecordFill(ctx, ledger.FillRow{
				Key:    fmt.Sprintf("rcpt:%s:%d", res.TxHash.Hex(), i),
				TxHash: res.TxHash.Hex(), MarketID: in.MarketID, Kind: kind,
				Price: toCost(px), Quantity: qty, Fair: in.Fair,
				// A receipt-decoded fill always carries the decision-time model
				// value from Intent.Fair, never a price fallback.
				HasModelFair: true,
			}); err != nil {
				e.Log.Printf("ledger: record fill %s: %v", res.TxHash.Hex(), err)
			}
		}
	}
}

// inventory is our net Up exposure on one market, in contracts.
func (e *Engine) inventory(marketID string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.inv[marketID]
}

// setMarketAsset records which asset a market belongs to, so assetExposure
// can find it. Idempotent and safe to call more than once for the same
// market (discover, restoreInventory and reconcile can all learn about the
// same market at different times).
func (e *Engine) setMarketAsset(marketID, asset string) {
	e.mu.Lock()
	e.marketAsset[marketID] = asset
	e.mu.Unlock()
}

func (e *Engine) addInventory(marketID string, delta float64) {
	e.mu.Lock()
	e.inv[marketID] += delta
	e.mu.Unlock()
}

// assetExposure is our net Up exposure on one ASSET, summed live across every
// market currently tracked on it. Deliberately recomputed from inv rather
// than kept as a separate running total -- see the marketAsset field comment
// for why that would drift.
func (e *Engine) assetExposure(asset string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	var sum float64
	for marketID, qty := range e.inv {
		if e.marketAsset[marketID] == asset {
			sum += qty
		}
	}
	return sum
}

// assetRestingLong sums the size of every currently-resting BuyUp order on
// one asset -- the most exposure that could still materialise on the long
// side if every one of them filled.
func (e *Engine) assetRestingLong(asset string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	var sum float64
	for marketID, sz := range e.restingUp {
		if e.marketAsset[marketID] == asset {
			sum += sz
		}
	}
	return sum
}

// assetRestingShort is assetRestingLong's mirror for resting BuyDn orders.
func (e *Engine) assetRestingShort(asset string) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	var sum float64
	for marketID, sz := range e.restingDn {
		if e.marketAsset[marketID] == asset {
			sum += sz
		}
	}
	return sum
}

// exposureBaseline is what a prospective order of `delta` contracts on
// `asset` should be checked against before ExposureAllows adds delta to it.
//
// A take is IOC and fills synchronously inside the same call that places it,
// so checking it against current realised exposure IS checking it against
// the exposure it is about to create.
//
// A resting maker order is different: placing it does not mean it filled,
// only that it now sits on the book, and it may fill much later in a
// COUNTERPARTY's transaction this engine only learns about through
// reconcile(). Checking it against realised exposure alone would let several
// resting orders across different windows each pass this same check while
// none of them has filled yet -- and then let all of them fill later and
// together breach the cap with no guarded moment left to refuse anything. So
// a resting order is checked against the WORST CASE for its own direction:
// realised exposure plus every other order already resting the same way, as
// if every one of them filled and none of the opposite side did.
func (e *Engine) exposureBaseline(asset, mode string, delta float64) float64 {
	baseline := e.assetExposure(asset)
	if mode != "make" {
		return baseline
	}
	if delta > 0 {
		return baseline + e.assetRestingLong(asset)
	}
	return baseline - e.assetRestingShort(asset)
}

// setResting records that a market's resting order on one side is now this
// size (0 clears it). Called once handle() knows a maker order actually rests
// on the book, and whenever a fill or a cancel consumes it.
func (e *Engine) setResting(marketID string, kind venue.OrderKind, size float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if kind == venue.BuyNo || kind == venue.SellNo {
		if size <= 0 {
			delete(e.restingDn, marketID)
		} else {
			e.restingDn[marketID] = size
		}
		return
	}
	if size <= 0 {
		delete(e.restingUp, marketID)
	} else {
		e.restingUp[marketID] = size
	}
}

// clearResting releases both sides' reservations for one market: it is about
// to be cancelled and replaced, or the window is over and any order left on
// the book will age off by itself.
func (e *Engine) clearResting(marketID string) {
	e.mu.Lock()
	delete(e.restingUp, marketID)
	delete(e.restingDn, marketID)
	e.mu.Unlock()
}

// persistExposure recomputes one asset's current exposure and writes it to
// the ledger, so the dashboard -- a separate process -- can show the same
// number this engine is actually enforcing. Call it after anything changes
// that asset's inventory.
func (e *Engine) persistExposure(ctx context.Context, asset string) {
	if e.Ledger == nil {
		return
	}
	net := e.assetExposure(asset)
	if err := e.Ledger.UpsertExposure(ctx, asset, net, e.Params.MaxAssetExposure); err != nil {
		e.Log.Printf("ledger: record exposure %s: %v", asset, err)
	}
}

// restoreInventory rebuilds in-memory position state from the ledger before
// trading begins, so a restart mid-window is not silently treated as flat.
// Without this, the asset exposure cap would pass trades it should refuse
// until enough new fills happened to repopulate a map that started empty.
func (e *Engine) restoreInventory(ctx context.Context) {
	if e.Ledger == nil {
		return
	}
	positions, err := e.Ledger.OpenPositions(ctx)
	if err != nil {
		e.Log.Printf("restore inventory: %v", err)
		return
	}
	if len(positions) == 0 {
		return
	}
	e.mu.Lock()
	for _, p := range positions {
		e.inv[p.MarketID] = p.Signed
		e.marketAsset[p.MarketID] = p.Asset
	}
	e.mu.Unlock()
	e.Log.Printf("restored %d open position(s) from the ledger", len(positions))
	for _, p := range positions {
		e.persistExposure(ctx, p.Asset)
	}
}

// rawToHuman converts a pool-scaled integer to collateral units.
func rawToHuman(v *big.Int, dec int) float64 {
	if v == nil || dec < 0 {
		return 0
	}
	den := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), den).Float64()
	return f
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
		if err := e.Ledger.Settle(ctx, im.MarketID, winner, st.IsVoided, 0, 0); err != nil {
			e.Log.Printf("[%s] ledger: settle: %v", label, err)
		}
		e.Log.Printf("[%s] SETTLED winner=%d voided=%v", label, winner, st.IsVoided)
		return
	}
}

// reconcile ingests fills that never appeared in one of our own receipts.
//
// A post-only order rests, and a counterparty fills it in THEIR transaction.
// We get no receipt and no log — yet that is the maker path working as
// designed, so a ledger fed only by submission receipts systematically omits
// the fills the strategy exists to produce.
//
// Each sweep re-reads an overlapping window and relies on the ledger's unique
// fill key for idempotency, so restarts and repeated scans cannot double-count.
func (e *Engine) reconcile(ctx context.Context) {
	if e.Ledger == nil || e.DryRun() {
		return
	}
	me := strings.ToLower(e.Trader.From().Hex())
	t := time.NewTicker(45 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		// Overlap generously: cheap, and the dedupe key makes it safe.
		since := time.Now().Add(-30 * time.Minute).Unix()
		fills, err := e.C.UserFills(ctx, me, since)
		if err != nil {
			e.Log.Printf("reconcile: %v", err)
			continue
		}

		var added int
		for _, f := range fills {
			key := "idx:" + f.ID
			if seen, err := e.Ledger.HasFill(ctx, key); err != nil || seen {
				continue
			}
			// Only account for markets this engine actually traded; the wallet
			// may carry activity from other tools.
			if known, err := e.Ledger.KnownMarket(ctx, f.MarketID); err != nil || !known {
				continue
			}

			dec := f.Market.QuoteDecimals
			qty := parseScaled(f.Quantity, dec)
			yesPx := parseScaled(f.FillPrice, dec)
			if qty <= 0 {
				continue
			}

			// Work out which side WE were on. The taker's direction is known;
			// the maker is the opposite of it.
			weAreTaker := strings.EqualFold(f.Taker, me)
			buyingUp := f.TakerIsBid
			if !weAreTaker {
				buyingUp = !f.TakerIsBid
			}

			kind, cost, signed := "BUY_UP", yesPx, qty
			if !buyingUp {
				kind, cost, signed = "BUY_DN", 1-yesPx, -qty
			}

			if err := e.Ledger.RecordFill(ctx, ledger.FillRow{
				Key: key, TxHash: f.TxHash, MarketID: f.MarketID, Kind: kind,
				// No model snapshot exists for a fill we did not submit, so fair
				// value is recorded as the execution price. That makes its edge
				// contribution exactly zero rather than a guess — the honest
				// choice when we cannot know what we believed at the time. It is
				// a price, not a prediction, so it must never be scored as one --
				// see HasModelFair and internal/ledger.RecentHealth.
				Price: cost, Quantity: qty, Fair: yesPx,
				HasModelFair: false,
			}); err != nil {
				continue
			}
			// This fill's market is one we spawned (KnownMarket, above), so
			// marketAsset should already hold its asset from discover() or a
			// restart's restoreInventory -- set defensively so the exposure cap
			// and dashboard stay correct even if this fill is somehow the first
			// thing this process ever learns about the market.
			e.setMarketAsset(f.MarketID, f.Market.Asset)
			e.addInventory(f.MarketID, signed)
			// This is exactly the moment a resting order's risk stops being
			// potential and becomes realised: the reservation from handle()
			// must be released now, or the same exposure would be counted in
			// BOTH assetExposure (via addInventory, above) AND
			// assetRestingLong/Short for as long as the reservation lingered.
			restingKind := venue.BuyYes
			if !buyingUp {
				restingKind = venue.BuyNo
			}
			e.setResting(f.MarketID, restingKind, 0)
			e.persistExposure(ctx, f.Market.Asset)
			added++
		}
		if added > 0 {
			e.bump(func(s *Stats) { s.Fills += int64(added) })
			e.Log.Printf("reconcile: ingested %d fill(s) that arrived without a receipt", added)
		}
	}
}

// parseScaled converts a decimal string in pool units to collateral units.
func parseScaled(v string, dec int) float64 {
	f, ok := new(big.Float).SetString(v)
	if !ok {
		return 0
	}
	den := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	out, _ := new(big.Float).Quo(f, den).Float64()
	return out
}
