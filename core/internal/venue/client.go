// Package venue is a pure-Go client for DreamDEX Event Contracts on Somnia.
// It talks to the chain directly via the ABIs the official SDK exports, and to
// the public indexer for discovery only. No Node, no JS runtime.
package venue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/firstbid/core/internal/venue/abis"
)

// Shannon testnet. Core addresses are CREATE3-deployed and identical on mainnet.
const (
	ShannonRPC     = "https://api.infra.testnet.somnia.network"
	ShannonWS      = "wss://api.infra.testnet.somnia.network/ws"
	ShannonGQL     = "https://dev.smk.somnia.host/v1/graphql"
	MainnetGQL     = "https://prd.smk.somnia.host/v1/graphql"
	ChainIDShannon = 50312
)

var (
	BinaryModule     = common.HexToAddress("0x3ecC694Cef705358864a646142ac17A90E29e388")
	BinarySettlement = common.HexToAddress("0xbF4a49e0Dfd092e5FBE8E5761064C49533e6Ed23")
	OutcomeToken     = common.HexToAddress("0xB52c5934113Af5c0Bb20eb3C72290C8215f755b9")
	TestUSDC         = common.HexToAddress("0x70a86D8842FB63C4Ad2b7cdddF530eBf1BB25d8E")
)

// Status is the on-chain market lifecycle. Only Trading accepts orders.
type Status uint8

const (
	StatusListed   Status = 0
	StatusTrading  Status = 1
	StatusLocked   Status = 2
	StatusSettling Status = 3 // in the enum, effectively never observed
	StatusResolved Status = 4
	StatusVoided   Status = 5
)

func (s Status) String() string {
	switch s {
	case StatusListed:
		return "Listed"
	case StatusTrading:
		return "Trading"
	case StatusLocked:
		return "Locked"
	case StatusSettling:
		return "Settling"
	case StatusResolved:
		return "Resolved"
	case StatusVoided:
		return "Voided"
	}
	return fmt.Sprintf("Unknown(%d)", uint8(s))
}

// OrderKind mirrors the venue's four sides. Prices are ALWAYS in YES terms.
type OrderKind uint8

const (
	BuyYes  OrderKind = 0
	SellYes OrderKind = 1
	BuyNo   OrderKind = 2
	SellNo  OrderKind = 3
)

// OrderType mirrors ORDER_TYPE in the SDK.
type OrderType uint8

const (
	TypeLimit      OrderType = 0
	TypeFillOrKill OrderType = 1
	TypeMarket     OrderType = 2 // IOC
	TypePostOnly   OrderType = 3
)

type Client struct {
	eth *ethclient.Client
	gql string
}

func Dial(ctx context.Context, rpc, gql string) (*Client, error) {
	c, err := ethclient.DialContext(ctx, rpc)
	if err != nil {
		return nil, fmt.Errorf("dial rpc: %w", err)
	}
	return &Client{eth: c, gql: gql}, nil
}

func (c *Client) Eth() *ethclient.Client { return c.eth }
func (c *Client) Close()                 { c.eth.Close() }

// call performs an eth_call and unpacks the result against the given ABI method.
func (c *Client) call(ctx context.Context, a abi.ABI, to common.Address, method string, args ...any) ([]any, error) {
	data, err := a.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	out, err := c.eth.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, DecodeRevert(err))
	}
	vals, err := a.Unpack(method, out)
	if err != nil {
		return nil, fmt.Errorf("unpack %s: %w", method, err)
	}
	return vals, nil
}

// callRaw performs an eth_call and returns the undecoded return data, for
// callers that want UnpackIntoInterface against a named struct.
func (c *Client) callRaw(ctx context.Context, a abi.ABI, to common.Address, method string, args ...any) ([]byte, error) {
	data, err := a.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	out, err := c.eth.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, DecodeRevert(err))
	}
	return out, nil
}

// Market is the module registry's record for one window.
type Market struct {
	MarketID         common.Hash
	OracleQuestionID *big.Int
	OutcomeSlotCount uint8
	VoidPolicy       uint8
	Collateral       common.Address
	OriginOperatorID uint32
	OriginVenueID    common.Hash
	OracleAdapter    common.Address
	Creator          common.Address
	MarketAddr       common.Address
	Pool             common.Address
	YesID            *big.Int
	NoID             *big.Int
	TradingStart     uint64
	Expiry           uint64
}

// ReadMarket is chain truth for a market: one call returns pool, token ids and window.
func (c *Client) ReadMarket(ctx context.Context, id common.Hash) (*Market, error) {
	v, err := c.call(ctx, abis.BinaryModuleRead, BinaryModule, "markets", id)
	if err != nil {
		return nil, err
	}
	if len(v) < 14 {
		return nil, fmt.Errorf("markets: expected 14 outputs, got %d", len(v))
	}
	return &Market{
		MarketID:         id,
		OracleQuestionID: v[0].(*big.Int),
		OutcomeSlotCount: v[1].(uint8),
		VoidPolicy:       v[2].(uint8),
		Collateral:       v[3].(common.Address),
		OriginOperatorID: v[4].(uint32),
		OriginVenueID:    common.Hash(v[5].([32]byte)),
		OracleAdapter:    v[6].(common.Address),
		Creator:          v[7].(common.Address),
		MarketAddr:       v[8].(common.Address),
		Pool:             v[9].(common.Address),
		YesID:            v[10].(*big.Int),
		NoID:             v[11].(*big.Int),
		TradingStart:     v[12].(uint64),
		Expiry:           v[13].(uint64),
	}, nil
}

// BookParams is the venue's price/size grid. Read it; never hardcode it —
// it scales with the collateral's decimals (6 on testnet, 18 on mainnet).
type BookParams struct {
	TickSize    *big.Int
	MinQuantity *big.Int
	LotSize     *big.Int
}

func (c *Client) BookParams(ctx context.Context, pool common.Address) (*BookParams, error) {
	v, err := c.call(ctx, abis.BinaryPoolRead, pool, "getOrderBookParameters")
	if err != nil {
		return nil, err
	}
	// A single tuple output decodes to an anonymous struct; ConvertType maps it
	// onto ours by field order — the same thing abigen-generated code does.
	return abi.ConvertType(v[0], new(BookParams)).(*BookParams), nil
}

// Level is one price level of resting depth.
type Level struct {
	Price    *big.Int
	Quantity *big.Int
}

// BookLevels reads resting depth straight from the pool. isBid selects the side.
func (c *Client) BookLevels(ctx context.Context, pool common.Address, isBid bool, max uint64) ([]Level, error) {
	v, err := c.call(ctx, abis.BinaryPoolRead, pool, "getBookLevels", isBid, max)
	if err != nil {
		return nil, err
	}
	if len(v) == 0 {
		return nil, nil
	}
	return *abi.ConvertType(v[0], new([]Level)).(*[]Level), nil
}

// callAt performs an eth_call pinned to a specific block, so several reads can
// be taken from one consistent view of the chain.
func (c *Client) callAt(ctx context.Context, a abi.ABI, to common.Address, block *big.Int, method string, args ...any) ([]any, error) {
	data, err := a.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	out, err := c.eth.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, block)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, DecodeRevert(err))
	}
	return a.Unpack(method, out)
}

// Book is both sides of one pool's resting depth, read at a single block.
type Book struct {
	Bids  []Level
	Asks  []Level
	Block *big.Int
}

// BestBid returns the top bid price, or 0 when the side is empty.
func (b Book) BestBid() *big.Int {
	if len(b.Bids) == 0 {
		return nil
	}
	return b.Bids[0].Price
}

// BestAsk returns the top ask price, or 0 when the side is empty.
func (b Book) BestAsk() *big.Int {
	if len(b.Asks) == 0 {
		return nil
	}
	return b.Asks[0].Price
}

// ReadBook reads both sides at ONE block height.
//
// Reading the sides in two unpinned calls can straddle a block boundary and
// produce a crossed book (bid above ask) that never actually existed. A maker
// that believes a phantom crossed book will quote into it.
func (c *Client) ReadBook(ctx context.Context, pool common.Address, depth uint64) (*Book, error) {
	bn, err := c.eth.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("block number: %w", err)
	}
	at := new(big.Int).SetUint64(bn)

	bidsV, err := c.callAt(ctx, abis.BinaryPoolRead, pool, at, "getBookLevels", true, depth)
	if err != nil {
		return nil, err
	}
	asksV, err := c.callAt(ctx, abis.BinaryPoolRead, pool, at, "getBookLevels", false, depth)
	if err != nil {
		return nil, err
	}
	b := &Book{Block: at}
	if len(bidsV) > 0 {
		b.Bids = *abi.ConvertType(bidsV[0], new([]Level)).(*[]Level)
	}
	if len(asksV) > 0 {
		b.Asks = *abi.ConvertType(asksV[0], new([]Level)).(*[]Level)
	}
	return b, nil
}

// OutcomeBalance reads an ERC-6909 position by token id.
func (c *Client) OutcomeBalance(ctx context.Context, owner common.Address, id *big.Int) (*big.Int, error) {
	v, err := c.call(ctx, abis.ERC6909, OutcomeToken, "balanceOf", owner, id)
	if err != nil {
		return nil, err
	}
	return v[0].(*big.Int), nil
}

// dataError is what go-ethereum's RPC layer implements when a node returns
// revert data alongside the error.
type dataError interface{ ErrorData() any }

// DecodeRevert turns a raw revert blob into the contract's own error name,
// using the 500 error signatures the SDK exports. Without this every failure
// reads "execution reverted", which says nothing about what to fix.
func DecodeRevert(err error) error {
	if err == nil {
		return nil
	}
	var blob []byte
	var de dataError
	if errors.As(err, &de) {
		if s, ok := de.ErrorData().(string); ok {
			blob = common.FromHex(s)
		}
	}
	if len(blob) >= 4 {
		sel := blob[:4]
		for name, e := range errorsABI.Errors {
			if len(e.ID.Bytes()) >= 4 && bytes.Equal(e.ID.Bytes()[:4], sel) {
				if args, uerr := e.Inputs.Unpack(blob[4:]); uerr == nil && len(args) > 0 {
					return fmt.Errorf("%s%v", name, args)
				}
				return fmt.Errorf("%s()", name)
			}
		}
		return fmt.Errorf("unknown revert selector 0x%x (%w)", sel, err)
	}
	msg := err.Error()
	for name := range errorsABI.Errors {
		if strings.Contains(msg, name) {
			return fmt.Errorf("%s (%w)", name, err)
		}
	}
	return err
}

var errorsABI = abis.MustParse(abis.ContractErrorsJSON)

// MarketState is chain-truth lifecycle for one market, read from the market clone.
type MarketState struct {
	Status     Status
	IsResolved bool
	IsVoided   bool
}

// ReadState gates every write. The indexer's status column trails this by seconds;
// an order sent to a Locked market reverts or fails silently.
func (c *Client) ReadState(ctx context.Context, marketAddr common.Address) (*MarketState, error) {
	sv, err := c.call(ctx, abis.BinaryMarketRead, marketAddr, "status")
	if err != nil {
		return nil, err
	}
	rv, err := c.call(ctx, abis.BinaryMarketRead, marketAddr, "isResolved")
	if err != nil {
		return nil, err
	}
	vv, err := c.call(ctx, abis.BinaryMarketRead, marketAddr, "isVoided")
	if err != nil {
		return nil, err
	}
	return &MarketState{
		Status:     Status(sv[0].(uint8)),
		IsResolved: rv[0].(bool),
		IsVoided:   vv[0].(bool),
	}, nil
}

// ERC20Balance reads a token balance for an account.
func (c *Client) ERC20Balance(ctx context.Context, token, account common.Address) (*big.Int, error) {
	v, err := c.call(ctx, abis.ERC20Read, token, "balanceOf", account)
	if err != nil {
		return nil, err
	}
	return v[0].(*big.Int), nil
}

// callFrom performs an eth_call with an explicit sender, for pool views like
// getOwnOpenOrders() that answer from msg.sender.
func (c *Client) callFrom(ctx context.Context, a abi.ABI, from, to common.Address, method string, args ...any) ([]any, error) {
	data, err := a.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	out, err := c.eth.CallContract(ctx, ethereum.CallMsg{From: from, To: &to, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, DecodeRevert(err))
	}
	return a.Unpack(method, out)
}

// OwnOpenOrders asks the pool which of our orders are still resting.
//
// This is chain truth and replaces tracking ids out of receipts: an order can
// fill, expire or be swept between ticks, and only the pool knows what is
// actually still on the book under our address.
func (c *Client) OwnOpenOrders(ctx context.Context, pool, owner common.Address) ([]*big.Int, error) {
	v, err := c.callFrom(ctx, abis.BinaryPoolRead, owner, pool, "getOwnOpenOrders")
	if err != nil {
		return nil, err
	}
	if len(v) == 0 {
		return nil, nil
	}
	raw, ok := v[0].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("getOwnOpenOrders: unexpected shape %T", v[0])
	}
	return raw, nil
}

// WinningOutcome reads which side won, as an outcome index (0 = Up, 1 = Down).
func (c *Client) WinningOutcome(ctx context.Context, marketAddr common.Address) (int, error) {
	v, err := c.call(ctx, abis.BinaryMarketRead, marketAddr, "payoutNumerators")
	if err != nil {
		return -1, err
	}
	nums, ok := v[0].([]*big.Int)
	if !ok || len(nums) == 0 {
		return -1, fmt.Errorf("payoutNumerators: unexpected shape %T", v[0])
	}
	best, idx := big.NewInt(-1), -1
	for i, n := range nums {
		if n.Cmp(best) > 0 {
			best, idx = n, i
		}
	}
	return idx, nil
}

// HashOf converts a hex market id string to a Hash.
func HashOf(s string) common.Hash { return common.HexToHash(s) }

// GQL exposes an indexer query for tools that need a shape the client does not model.
func (c *Client) GQL(ctx context.Context, query string, vars map[string]any, out any) error {
	return c.gqlPost(ctx, query, vars, out)
}
