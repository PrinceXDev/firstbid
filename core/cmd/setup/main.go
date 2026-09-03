// Command setup mints testnet collateral for the signer.
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	_ = godotenv.Load("../executor/.env", ".env")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	c, err := venue.Dial(ctx, venue.ShannonRPC, venue.ShannonGQL)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	tr, err := venue.NewTrader(ctx, c, os.Getenv("FIRSTBID_PRIVATE_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("signer %s\n", tr.From().Hex())

	before, _ := c.ERC20Balance(ctx, venue.TestUSDC, tr.From())
	fmt.Printf("tUSDC before: %s\n", human(before, 6))

	// The faucet credits msg.sender and is capped at 10,000 tUSDC per call.
	amount := new(big.Int).Mul(big.NewInt(10_000), big.NewInt(1_000_000))
	fmt.Println("calling faucet(10000e6)...")
	rcpt, err := tr.Faucet(ctx, amount)
	if err != nil {
		log.Fatalf("faucet failed: %v", err)
	}
	fmt.Printf("  tx %s  gasUsed=%d  block=%d\n", rcpt.TxHash.Hex(), rcpt.GasUsed, rcpt.BlockNumber)

	after, _ := c.ERC20Balance(ctx, venue.TestUSDC, tr.From())
	fmt.Printf("tUSDC after : %s\n", human(after, 6))
}

func human(v *big.Int, dec int) string {
	if v == nil {
		return "?"
	}
	d := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), d).Float64()
	return fmt.Sprintf("%.2f", f)
}
