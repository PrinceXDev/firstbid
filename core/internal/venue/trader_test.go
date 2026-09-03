package venue

import (
	"math/big"
	"testing"
)

// Testnet grid: tick = lot = 1000 at 6 decimals -> 0.001 probability / 0.001 contracts.
var (
	tick6 = big.NewInt(1000)
	lot6  = big.NewInt(1000)
	// Mainnet grid: 1e15 at 18 decimals -> same 0.001 steps at a different scale.
	tick18 = new(big.Int).Exp(big.NewInt(10), big.NewInt(15), nil)
)

func TestSnapPriceLandsOnTickGrid(t *testing.T) {
	cases := []struct {
		in   float64
		want int64
	}{
		{0.500, 500000},
		{0.0512, 51000}, // rounds to the nearest whole tick
		{0.4567, 457000},
		{0.001, 1000},
	}
	for _, c := range cases {
		got := SnapPrice(c.in, tick6, 6)
		if got.Int64() != c.want {
			t.Errorf("SnapPrice(%v) = %v, want %v", c.in, got, c.want)
		}
		if new(big.Int).Mod(got, tick6).Sign() != 0 {
			t.Errorf("SnapPrice(%v) = %v is off the tick grid", c.in, got)
		}
	}
}

// The documented failure: (0.05).toFixed(18) lands three wei off the grid and the
// pool rejects it with InvalidPrice. Snapping must make that impossible.
func TestSnapPriceNeverOffGridAt18Decimals(t *testing.T) {
	for _, p := range []float64{0.05, 0.137, 0.6666666, 0.9999} {
		got := SnapPrice(p, tick18, 18)
		if new(big.Int).Mod(got, tick18).Sign() != 0 {
			t.Fatalf("SnapPrice(%v) = %v is not a whole multiple of the tick", p, got)
		}
	}
}

// Below one lot floors to zero, and the caller must skip rather than send.
func TestSnapQtyBelowOneLotIsZero(t *testing.T) {
	if got := SnapQty(0.0004, lot6, 6); got.Sign() != 0 {
		t.Errorf("SnapQty(0.0004) = %v, want 0 so the caller skips", got)
	}
	if got := SnapQty(5, lot6, 6); got.Int64() != 5_000_000 {
		t.Errorf("SnapQty(5) = %v, want 5000000", got)
	}
}

func TestSnapQtyAlwaysFloorsNeverRoundsUp(t *testing.T) {
	// Rounding up could try to sell inventory that is not held.
	got := SnapQty(1.9999, lot6, 6)
	if got.Int64() != 1_999_000 {
		t.Errorf("SnapQty(1.9999) = %v, want 1999000 (floored)", got)
	}
}

func TestPlaceOrderCalldataPacks(t *testing.T) {
	o := PlaceOrder{
		Kind:     BuyYes,
		Price:    SnapPrice(0.42, tick6, 6),
		Quantity: SnapQty(5, lot6, 6),
		Type:     TypePostOnly,
		ExpireNs: 1_800_000_000_000_000_000,
	}
	data, err := abisBinaryPoolWritePack(o)
	if err != nil {
		t.Fatalf("pack failed: %v", err)
	}
	// 4-byte selector + 9 ABI words
	if len(data) != 4+9*32 {
		t.Fatalf("calldata length = %d, want %d", len(data), 4+9*32)
	}
}
