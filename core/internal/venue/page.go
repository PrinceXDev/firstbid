package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// pageAll walks a Hasura table with keyset pagination on `id`. Offset paging
// degrades badly on these tables (deep offsets are O(n^2)); keyset stays flat.
func pageAll[T any](ctx context.Context, c *Client, query, field string, maxPages int, dst *[]T, since ...int64) error {
	cursor := ""
	for i := 0; i < maxPages; i++ {
		var raw map[string]json.RawMessage
		vars := map[string]any{"a": cursor}
		if len(since) > 0 {
			vars["since"] = since[0]
		}
		if err := c.gqlPost(ctx, query, vars, &raw); err != nil {
			return err
		}
		var batch []T
		if err := json.Unmarshal(raw[field], &batch); err != nil {
			return fmt.Errorf("decode %s: %w", field, err)
		}
		if len(batch) == 0 {
			return nil
		}
		*dst = append(*dst, batch...)

		// The cursor is the last row's `id`; pull it back out generically.
		var ids []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw[field], &ids); err != nil {
			return fmt.Errorf("cursor %s: %w", field, err)
		}
		cursor = ids[len(ids)-1].ID
		if len(batch) < 1000 {
			return nil
		}
	}
	return nil
}

func parseFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func ln(x float64) float64 { return math.Log(x) }
