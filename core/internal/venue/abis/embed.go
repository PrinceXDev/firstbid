// Package abis embeds the contract ABIs extracted from @somnia-chain/markets-sdk.
// Extraction is a one-time dev-time step; nothing here depends on Node at runtime.
package abis

import (
	_ "embed"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

//go:embed binaryModuleRead.json
var BinaryModuleReadJSON string

//go:embed binaryModuleWrite.json
var BinaryModuleWriteJSON string

//go:embed binaryPoolRead.json
var BinaryPoolReadJSON string

//go:embed binaryPoolWrite.json
var BinaryPoolWriteJSON string

//go:embed binaryMarketRead.json
var BinaryMarketReadJSON string

//go:embed erc6909.json
var ERC6909JSON string

//go:embed erc20Read.json
var ERC20ReadJSON string

//go:embed contractErrors.json
var ContractErrorsJSON string

//go:embed orderBookEvents.json
var OrderBookEventsJSON string

// MustParse panics at init on a malformed ABI — these are compile-time assets,
// so a failure here is a build defect, never a runtime condition.
func MustParse(j string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(j))
	if err != nil {
		panic("abis: " + err.Error())
	}
	return a
}

var (
	BinaryModuleRead  = MustParse(BinaryModuleReadJSON)
	BinaryModuleWrite = MustParse(BinaryModuleWriteJSON)
	BinaryPoolRead    = MustParse(BinaryPoolReadJSON)
	BinaryPoolWrite   = MustParse(BinaryPoolWriteJSON)
	BinaryMarketRead  = MustParse(BinaryMarketReadJSON)
	ERC6909           = MustParse(ERC6909JSON)
	ERC20Read         = MustParse(ERC20ReadJSON)
	OrderBookEvents   = MustParse(OrderBookEventsJSON)
)
