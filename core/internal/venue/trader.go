package venue

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/firstbid/core/internal/venue/abis"
)

// faucetABI is not exported by the SDK; the testnet collateral mints on demand
// via faucet(uint256), capped at 10,000 tUSDC per call.
var faucetABI = abis.MustParse(`[{"name":"faucet","type":"function","stateMutability":"nonpayable",
 "inputs":[{"type":"uint256","name":"amount"}],"outputs":[]}]`)

// Trader owns the single signing key. Every write goes through it, serialised:
// one key means one nonce sequence, and concurrent sends race it.
type Trader struct {
	c       *Client
	key     *ecdsa.PrivateKey
	from    common.Address
	chainID *big.Int
	sem     chan struct{}
}

func NewTrader(ctx context.Context, c *Client, hexKey string) (*Trader, error) {
	key, err := crypto.HexToECDSA(strings.TrimPrefix(hexKey, "0x"))
	if err != nil {
		return nil, fmt.Errorf("bad private key: %w", err)
	}
	id, err := c.eth.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("chain id: %w", err)
	}
	return &Trader{
		c:       c,
		key:     key,
		from:    crypto.PubkeyToAddress(key.PublicKey),
		chainID: id,
		sem:     make(chan struct{}, 1),
	}, nil
}

func (t *Trader) From() common.Address { return t.from }

// send builds, signs and submits one transaction, then waits for its receipt.
// Serialised on t.sem so the locally-tracked nonce can never be raced.
func (t *Trader) send(ctx context.Context, to common.Address, data []byte, value *big.Int) (*types.Receipt, error) {
	t.sem <- struct{}{}
	defer func() { <-t.sem }()

	nonce, err := t.c.eth.PendingNonceAt(ctx, t.from)
	if err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	tip, err := t.c.eth.SuggestGasTipCap(ctx)
	if err != nil {
		tip = big.NewInt(1_000_000_000)
	}
	head, err := t.c.eth.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("head: %w", err)
	}
	feeCap := new(big.Int).Add(tip, new(big.Int).Mul(head.BaseFee, big.NewInt(2)))

	if value == nil {
		value = big.NewInt(0)
	}
	// Estimate first: a revert here is free, whereas a reverted send costs gas
	// and (per the venue docs) does not always surface as an error.
	gas, err := t.c.eth.EstimateGas(ctx, ethereum.CallMsg{
		From: t.from, To: &to, Data: data, Value: value,
	})
	if err != nil {
		return nil, fmt.Errorf("estimate: %w", DecodeRevert(err))
	}
	gas = gas * 13 / 10

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   t.chainID,
		Nonce:     nonce,
		GasTipCap: tip,
		GasFeeCap: feeCap,
		Gas:       gas,
		To:        &to,
		Value:     value,
		Data:      data,
	})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(t.chainID), t.key)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	if err := t.c.eth.SendTransaction(ctx, signed); err != nil {
		return nil, fmt.Errorf("send: %w", DecodeRevert(err))
	}
	rcpt, err := t.waitReceipt(ctx, signed.Hash())
	if err != nil {
		return nil, err
	}
	if rcpt.Status == types.ReceiptStatusFailed {
		return rcpt, fmt.Errorf("tx reverted on-chain: %s", rcpt.TxHash.Hex())
	}
	return rcpt, nil
}

func (t *Trader) waitReceipt(ctx context.Context, h common.Hash) (*types.Receipt, error) {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		r, err := t.c.eth.TransactionReceipt(ctx, h)
		if err == nil {
			return r, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("receipt timeout for %s", h.Hex())
}

// ---- venue actions -------------------------------------------------------

// Faucet mints testnet collateral to the caller. Capped at 10,000 tUSDC.
func (t *Trader) Faucet(ctx context.Context, amount *big.Int) (*types.Receipt, error) {
	data, err := faucetABI.Pack("faucet", amount)
	if err != nil {
		return nil, err
	}
	return t.send(ctx, TestUSDC, data, nil)
}

// Approve lets a pool pull collateral when an order is placed.
func (t *Trader) Approve(ctx context.Context, token, spender common.Address, amount *big.Int) (*types.Receipt, error) {
	data, err := abis.MustParse(`[{"name":"approve","type":"function","stateMutability":"nonpayable",
	 "inputs":[{"type":"address","name":"spender"},{"type":"uint256","name":"amount"}],
	 "outputs":[{"type":"bool"}]}]`).Pack("approve", spender, amount)
	if err != nil {
		return nil, err
	}
	return t.send(ctx, token, data, nil)
}

// PlaceOrder submits one order to a binary pool. price and quantity must already
// be snapped to the venue grid — see SnapPrice / SnapQty.
type PlaceOrder struct {
	Pool      common.Address
	Kind      OrderKind
	Price     *big.Int // always in YES terms
	Quantity  *big.Int
	Type      OrderType
	ExpireNs  uint64
	SelfMatch uint8
}

func (t *Trader) Place(ctx context.Context, o PlaceOrder) (*types.Receipt, error) {
	if o.ExpireNs == 0 {
		// Expiry is mandatory; 0 reverts with OrderAlreadyExpired. Treat it as a
		// dead-man's switch so a crashed quoter's orders age off the book.
		o.ExpireNs = uint64(time.Now().Add(5*time.Minute).Unix()) * 1_000_000_000
	}
	data, err := abis.BinaryPoolWrite.Pack("placeBinaryOrder",
		uint8(o.Kind),
		o.Price,
		o.Quantity,
		o.ExpireNs,
		uint8(o.Type),
		o.SelfMatch,
		common.Address{}, // no builder
		big.NewInt(0),    // no builder fee
		uint64(0),        // userData
	)
	if err != nil {
		return nil, fmt.Errorf("pack placeBinaryOrder: %w", err)
	}
	return t.send(ctx, o.Pool, data, nil)
}

func (t *Trader) Cancel(ctx context.Context, pool common.Address, orderID *big.Int) (*types.Receipt, error) {
	data, err := abis.BinaryPoolWrite.Pack("cancelOrder", orderID)
	if err != nil {
		return nil, err
	}
	return t.send(ctx, pool, data, nil)
}

// ---- grid snapping -------------------------------------------------------
// Anything below one lot floors to ZERO and the order silently never appears.

// SnapPrice rounds a probability in (0,1) to the venue's tick grid.
func SnapPrice(p float64, tick *big.Int, decimals int) *big.Int {
	one := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	raw := new(big.Float).Mul(big.NewFloat(p), new(big.Float).SetInt(one))
	i, _ := raw.Int(nil)
	if tick == nil || tick.Sign() == 0 {
		return i
	}
	half := new(big.Int).Div(tick, big.NewInt(2))
	i.Add(i, half)
	i.Div(i, tick)
	return i.Mul(i, tick)
}

// SnapQty floors a contract count to the lot grid. A zero result means the size
// was below one lot and the caller MUST skip the order.
func SnapQty(q float64, lot *big.Int, decimals int) *big.Int {
	one := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	raw := new(big.Float).Mul(big.NewFloat(q), new(big.Float).SetInt(one))
	i, _ := raw.Int(nil)
	if lot == nil || lot.Sign() == 0 {
		return i
	}
	i.Div(i, lot)
	return i.Mul(i, lot)
}

var _ = abi.ABI{}

// abisBinaryPoolWritePack exposes calldata construction for tests without
// requiring a signer or a network.
func abisBinaryPoolWritePack(o PlaceOrder) ([]byte, error) {
	return abis.BinaryPoolWrite.Pack("placeBinaryOrder",
		uint8(o.Kind), o.Price, o.Quantity, o.ExpireNs,
		uint8(o.Type), o.SelfMatch, common.Address{}, big.NewInt(0), uint64(0))
}

// EnsureApproval approves a pool to pull collateral if it cannot already.
// Pools are recycled across windows, so this is checked per pool, once.
func (t *Trader) EnsureApproval(ctx context.Context, token, pool common.Address) (bool, error) {
	v, err := t.c.call(ctx, abis.ERC20Read, token, "allowance", t.from, pool)
	if err != nil {
		return false, err
	}
	cur := v[0].(*big.Int)
	// Anything above this and the pool can pull whatever an order needs.
	threshold := new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)
	if cur.Cmp(threshold) >= 0 {
		return false, nil
	}
	maxUint := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if _, err := t.Approve(ctx, token, pool, maxUint); err != nil {
		return false, err
	}
	return true, nil
}

// ---- receipt decoding ----------------------------------------------------

// Fill is one match against our order, decoded from the receipt.
type Fill struct {
	TakerOrderID *big.Int
	MakerOrderID *big.Int
	Quantity     *big.Int
	Price        *big.Int
}

// PlaceResult is what actually happened, as opposed to what we asked for.
// An order can rest, fill partly, fill fully, or be cancelled pre-fill, and the
// only way to know is the receipt.
type PlaceResult struct {
	TxHash    common.Hash
	GasUsed   uint64
	OrderID   *big.Int
	Rested    bool
	Fills     []Fill
	Cancelled bool
}

var (
	evOrderPlaced    = abis.OrderBookEvents.Events["OrderPlaced"]
	evOrderRested    = abis.OrderBookEvents.Events["OrderRested"]
	evOrderFilled    = abis.OrderBookEvents.Events["OrderFilled"]
	evOrderCancelled = abis.OrderBookEvents.Events["OrderCancelled"]
)

func decodePlacement(rcpt *types.Receipt) *PlaceResult {
	res := &PlaceResult{TxHash: rcpt.TxHash, GasUsed: rcpt.GasUsed}
	for _, lg := range rcpt.Logs {
		if len(lg.Topics) == 0 {
			continue
		}
		switch lg.Topics[0] {
		case evOrderPlaced.ID:
			if len(lg.Topics) > 1 {
				res.OrderID = new(big.Int).SetBytes(lg.Topics[1].Bytes())
			}
		case evOrderRested.ID:
			res.Rested = true
			if res.OrderID == nil && len(lg.Topics) > 1 {
				res.OrderID = new(big.Int).SetBytes(lg.Topics[1].Bytes())
			}
		case evOrderCancelled.ID:
			res.Cancelled = true
		case evOrderFilled.ID:
			f := Fill{}
			if len(lg.Topics) > 2 {
				f.TakerOrderID = new(big.Int).SetBytes(lg.Topics[1].Bytes())
				f.MakerOrderID = new(big.Int).SetBytes(lg.Topics[2].Bytes())
			}
			vals, err := abis.OrderBookEvents.Unpack("OrderFilled", lg.Data)
			if err == nil && len(vals) >= 4 {
				f.Quantity, _ = vals[0].(*big.Int)
				f.Price, _ = vals[3].(*big.Int)
			}
			res.Fills = append(res.Fills, f)
		}
	}
	return res
}

// PlaceTracked places an order and reports what the chain actually did with it.
func (t *Trader) PlaceTracked(ctx context.Context, o PlaceOrder) (*PlaceResult, error) {
	rcpt, err := t.Place(ctx, o)
	if err != nil {
		return nil, err
	}
	return decodePlacement(rcpt), nil
}
