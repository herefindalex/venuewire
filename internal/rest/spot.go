package rest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

type SpotOrderBook struct {
	Symbol        string     `json:"s"`
	Bids          [][]string `json:"b"`
	Asks          [][]string `json:"a"`
	UpdateID      int64      `json:"u"`
	CrossSequence int64      `json:"seq"`
	TimestampMS   int64      `json:"ts"`
}

type FeeRate struct {
	Symbol       string `json:"symbol"`
	TakerFeeRate string `json:"takerFeeRate"`
	MakerFeeRate string `json:"makerFeeRate"`
}

type SpotBorrowCapacity struct {
	Symbol             string `json:"symbol"`
	Side               string `json:"side"`
	MaxTradeQty        string `json:"maxTradeQty"`
	MaxTradeAmount     string `json:"maxTradeAmount"`
	SpotMaxTradeQty    string `json:"spotMaxTradeQty"`
	SpotMaxTradeAmount string `json:"spotMaxTradeAmount"`
}

func (c *Client) SpotOrderBook(ctx context.Context, symbol string, limit int) (SpotOrderBook, ResponseMeta, error) {
	if symbol == "" {
		return SpotOrderBook{}, ResponseMeta{}, errors.New("spot order book requires symbol")
	}
	if limit <= 0 || limit > 200 {
		return SpotOrderBook{}, ResponseMeta{}, errors.New("spot order book limit must be from 1 to 200")
	}
	query := url.Values{"category": {"spot"}, "symbol": {symbol}, "limit": {strconv.Itoa(limit)}}
	var result SpotOrderBook
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/market/orderbook", query, nil, false, &result)
	return result, meta, err
}

func (c *Client) SpotFeeRates(ctx context.Context, symbol string) ([]FeeRate, ResponseMeta, error) {
	if symbol == "" {
		return nil, ResponseMeta{}, errors.New("spot fee rate requires symbol")
	}
	query := url.Values{"category": {"spot"}, "symbol": {symbol}}
	var result struct {
		List []FeeRate `json:"list"`
	}
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/account/fee-rate", query, nil, true, &result)
	return result.List, meta, err
}

func (c *Client) SpotBorrowCapacity(ctx context.Context, symbol, side string) (SpotBorrowCapacity, ResponseMeta, error) {
	if symbol == "" || (side != "Buy" && side != "Sell") {
		return SpotBorrowCapacity{}, ResponseMeta{}, errors.New("spot borrow capacity requires symbol and Buy or Sell side")
	}
	query := url.Values{"category": {"spot"}, "symbol": {symbol}, "side": {side}}
	var result SpotBorrowCapacity
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/order/spot-borrow-check", query, nil, true, &result)
	return result, meta, err
}
