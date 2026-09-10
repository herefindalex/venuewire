package deribit

import (
	"context"
	"encoding/json"
	"errors"
)

type SpotOrderBook struct {
	InstrumentName string          `json:"instrument_name"`
	TimestampMS    int64           `json:"timestamp"`
	ChangeID       int64           `json:"change_id"`
	Bids           [][]json.Number `json:"bids"`
	Asks           [][]json.Number `json:"asks"`
}

func (c *Client) SpotOrderBook(ctx context.Context, instrument string, depth int) (SpotOrderBook, error) {
	if instrument == "" {
		return SpotOrderBook{}, errors.New("spot order book requires instrument")
	}
	if depth <= 0 || depth > 1000 {
		return SpotOrderBook{}, errors.New("spot order book depth must be from 1 to 1000")
	}
	var result SpotOrderBook
	err := c.Public(ctx, "public/get_order_book", map[string]any{"instrument_name": instrument, "depth": depth}, &result)
	return result, err
}
