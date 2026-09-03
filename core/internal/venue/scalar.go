package venue

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// numStr accepts a JSON value that may arrive as a number or a string.
// Hasura's `numeric` columns serialise inconsistently across tables, and
// guessing per-field is how a decoder breaks silently on one endpoint.
type numStr string

func (n *numStr) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" {
		*n = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = numStr(s)
		return nil
	}
	*n = numStr(string(b))
	return nil
}

func (n numStr) String() string { return string(n) }
func (n numStr) Empty() bool    { return n == "" }
func (n numStr) Float() (float64, bool) {
	if n == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(string(n), 64)
	return v, err == nil
}

// NormaliseTo rescales an oracle fixed-point value onto a reference's units.
// The oracle's numericValue scale varies by question (1e2 and 1e4 both observed
// on mainnet), so a hardcoded divisor silently misprices. Anchoring within a 3x
// band is safe: no supported asset moves 3x inside one window.
func NormaliseTo(oracleVal, ref float64) float64 {
	if ref <= 0 || oracleVal <= 0 {
		return oracleVal
	}
	v := oracleVal
	for v > ref*3 {
		v /= 10
	}
	for v < ref/3 {
		v *= 10
	}
	return v
}
