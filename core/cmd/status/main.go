// Command status reports whether the signer is ready to trade.
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/joho/godotenv"

	"github.com/firstbid/core/internal/venue"
)

func main() {
	_ = godotenv.Load("../executor/.env", ".env")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := venue.Dial(ctx, venue.ShannonRPC, venue.ShannonGQL)
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	keyHex := os.Getenv("FIRSTBID_PRIVATE_KEY")
	if keyHex == "" {
		log.Fatal("FIRSTBID_PRIVATE_KEY not set")
	}
	key, err := crypto.HexToECDSA(trim0x(keyHex))
	if err != nil {
		log.Fatal(err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)

	bal, err := c.Eth().BalanceAt(ctx, addr, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("signer      : %s\n", addr.Hex())
	fmt.Printf("STT (gas)   : %s\n", ether(bal))

	usdc, err := c.ERC20Balance(ctx, venue.TestUSDC, addr)
	if err != nil {
		fmt.Printf("tUSDC       : error %v\n", err)
	} else {
		fmt.Printf("tUSDC       : %s (6dp)\n", units(usdc, 6))
	}

	ready := bal.Sign() > 0
	fmt.Printf("\ngas ready   : %v\n", ready)
	if !ready {
		fmt.Println("-> fund the address above with STT before running -live")
	}
}

func trim0x(s string) string {
	if len(s) > 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}

func ether(v *big.Int) string { return units(v, 18) }

func units(v *big.Int, dec int) string {
	d := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), d).Float64()
	return fmt.Sprintf("%.6f", f)
}

var _ = common.Address{}
