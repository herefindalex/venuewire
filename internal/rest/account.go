package rest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

type AccountInfo struct {
	UnifiedMarginStatus int    `json:"unifiedMarginStatus"`
	MarginMode          string `json:"marginMode"`
	IsMasterTrader      bool   `json:"isMasterTrader"`
	SpotHedgingStatus   string `json:"spotHedgingStatus"`
	UpdatedTime         string `json:"updatedTime"`
	DCPStatus           string `json:"dcpStatus"`
	TimeWindow          int    `json:"timeWindow"`
	SMPGroup            int    `json:"smpGroup"`
	IsUTAUpgrade        bool   `json:"isUTAUpgrade"`
}

type WalletAccount struct {
	AccountType            string        `json:"accountType"`
	AccountIMRate          string        `json:"accountIMRate"`
	AccountMMRate          string        `json:"accountMMRate"`
	AccountLTV             string        `json:"accountLTV"`
	TotalEquity            string        `json:"totalEquity"`
	TotalWalletBalance     string        `json:"totalWalletBalance"`
	TotalMarginBalance     string        `json:"totalMarginBalance"`
	TotalAvailableBalance  string        `json:"totalAvailableBalance"`
	TotalPerpUPL           string        `json:"totalPerpUPL"`
	TotalInitialMargin     string        `json:"totalInitialMargin"`
	TotalMaintenanceMargin string        `json:"totalMaintenanceMargin"`
	Coin                   []CoinBalance `json:"coin"`
}

type CoinBalance struct {
	Coin                string `json:"coin"`
	Equity              string `json:"equity"`
	USDValue            string `json:"usdValue"`
	WalletBalance       string `json:"walletBalance"`
	Free                string `json:"free"`
	Locked              string `json:"locked"`
	SpotHedgingQty      string `json:"spotHedgingQty"`
	BorrowAmount        string `json:"borrowAmount"`
	AvailableToWithdraw string `json:"availableToWithdraw"`
	AccruedInterest     string `json:"accruedInterest"`
	TotalOrderIM        string `json:"totalOrderIM"`
	TotalPositionIM     string `json:"totalPositionIM"`
	TotalPositionMM     string `json:"totalPositionMM"`
	UnrealisedPnL       string `json:"unrealisedPnl"`
	CumRealisedPnL      string `json:"cumRealisedPnl"`
	Bonus               string `json:"bonus"`
	CollateralSwitch    bool   `json:"collateralSwitch"`
	MarginCollateral    bool   `json:"marginCollateral"`
}

func (c *Client) AccountInfo(ctx context.Context) (AccountInfo, ResponseMeta, error) {
	var result AccountInfo
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/account/info", nil, nil, true, &result)
	return result, meta, err
}

func (c *Client) WalletBalances(ctx context.Context, coinFilter string) ([]WalletAccount, ResponseMeta, error) {
	coins, err := normalizeCoinFilter(coinFilter)
	if err != nil {
		return nil, ResponseMeta{}, err
	}
	query := url.Values{"accountType": {"UNIFIED"}}
	setIfNotEmpty(query, "coin", coins)
	var result struct {
		List []WalletAccount `json:"list"`
	}
	meta, _, err := c.do(ctx, http.MethodGet, "/v5/account/wallet-balance", query, nil, true, &result)
	return result.List, meta, err
}

func normalizeCoinFilter(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parts := strings.Split(value, ",")
	for index, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part == "" {
			return "", fmt.Errorf("coin filter contains an empty symbol")
		}
		for _, character := range part {
			if !unicode.IsUpper(character) && !unicode.IsDigit(character) {
				return "", fmt.Errorf("coin symbol %q must contain only letters and digits", part)
			}
		}
		parts[index] = part
	}
	return strings.Join(parts, ","), nil
}
