package accountstate

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/herefindalex/venuewire/internal/deribit"
	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/rest"
)

type BybitReader interface {
	WalletBalances(context.Context, string) ([]rest.WalletAccount, rest.ResponseMeta, error)
}

type BybitProvider struct {
	Client       BybitReader
	AccountAlias string
	Now          func() time.Time
}

func (p *BybitProvider) Venue() domain.Venue { return domain.VenueBybit }

func (p *BybitProvider) Snapshot(ctx context.Context) (Snapshot, error) {
	if p.Client == nil {
		return Snapshot{}, errors.New("Bybit account client is unavailable")
	}
	accounts, _, err := p.Client.WalletBalances(ctx, "")
	if err != nil {
		return Snapshot{}, err
	}
	if len(accounts) != 1 || accounts[0].AccountType != "UNIFIED" {
		return Snapshot{}, errors.New("Bybit UNIFIED account snapshot is unavailable")
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	account := accounts[0]
	result := Snapshot{
		Venue: domain.VenueBybit, Environment: "testnet", AccountAlias: p.AccountAlias, AccountType: account.AccountType,
		SnapshotAsOf: now, ExchangeReportedTotalUSD: account.TotalEquity, ExchangeReportedAsOf: now,
		ValuationBasis: "exchange-reported USD value", Completeness: "exchange-reported", LiabilityStatus: "none",
		DerivativePositionEvidence: "wallet snapshot does not prove derivative-position absence",
	}
	for _, coin := range account.Coin {
		asset := strings.ToUpper(strings.TrimSpace(coin.Coin))
		if asset == "" {
			continue
		}
		available, status := bybitAvailable(coin)
		quality := "snapshot"
		if coin.USDValue == "" {
			quality = "unpriced"
			result.UnpricedAssets = append(result.UnpricedAssets, asset)
		}
		result.Assets = append(result.Assets, Asset{Asset: asset, Balance: coin.WalletBalance, Equity: coin.Equity, Locked: coin.Locked, Liability: coin.BorrowAmount, AvailableToTrade: available, AvailableToTradeAsOf: now, AvailableStatus: status, ValuationQuantity: coin.WalletBalance, QuantityBasis: "wallet balance", ExchangeReportedUSDValue: coin.USDValue, USDValue: coin.USDValue, PriceSource: "Bybit wallet snapshot", PriceAsOf: now, Quality: quality})
		liability, liabilityOK := nonNegativeRat(coin.BorrowAmount)
		if !liabilityOK {
			result.LiabilityStatus = "unknown"
		} else if liability.Sign() > 0 && result.LiabilityStatus != "unknown" {
			result.LiabilityStatus = "present"
		}
	}
	return result, nil
}

func bybitAvailable(coin rest.CoinBalance) (string, string) {
	if validNonNegative(coin.Free) {
		return coin.Free, "verified"
	}
	wallet, walletOK := nonNegativeRat(coin.WalletBalance)
	locked, lockedOK := nonNegativeRat(coin.Locked)
	borrow, borrowOK := nonNegativeRat(coin.BorrowAmount)
	if !walletOK || !lockedOK || !borrowOK {
		return "", "unknown"
	}
	available := new(big.Rat).Sub(wallet, locked)
	available.Sub(available, borrow)
	if available.Sign() < 0 {
		available.SetInt64(0)
	}
	return decimal(available), "derived"
}

type DeribitReader interface {
	AccountSummaries(context.Context) ([]deribit.AccountSummary, error)
}

type DeribitProvider struct {
	Client       DeribitReader
	AccountAlias string
	Now          func() time.Time
}

func (p *DeribitProvider) Venue() domain.Venue { return domain.VenueDeribit }

func (p *DeribitProvider) Snapshot(ctx context.Context) (Snapshot, error) {
	if p.Client == nil {
		return Snapshot{}, errors.New("Deribit account client is unavailable")
	}
	summaries, err := p.Client.AccountSummaries(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	result := Snapshot{
		Venue: domain.VenueDeribit, Environment: "testnet", AccountAlias: p.AccountAlias, AccountType: "account summaries",
		SnapshotAsOf: now, ValuationBasis: "unpriced native balances", Completeness: "unpriced", LiabilityStatus: "unknown",
		DerivativePositionEvidence: "account summaries do not prove derivative-position absence",
	}
	for _, summary := range summaries {
		asset := strings.ToUpper(strings.TrimSpace(summary.Currency))
		if asset == "" {
			continue
		}
		balance, balanceOK := nonNegativeRat(summary.Balance.String())
		availableFunds, availableOK := nonNegativeRat(summary.AvailableFunds.String())
		available, status := "", "unknown"
		if balanceOK && availableOK {
			available, status = decimal(minimumRat(balance, availableFunds)), "derived"
		}
		result.Assets = append(result.Assets, Asset{Asset: asset, Balance: summary.Balance.String(), Equity: summary.Equity.String(), Locked: summary.InitialMargin.String(), AvailableToTrade: available, AvailableToTradeAsOf: now, AvailableStatus: status, ValuationQuantity: summary.Equity.String(), QuantityBasis: "account equity", Quality: "unpriced"})
		result.UnpricedAssets = append(result.UnpricedAssets, asset)
	}
	return result, nil
}

func validNonNegative(value string) bool {
	r, ok := rat(value)
	return ok && r.Sign() >= 0
}

func rat(value string) (*big.Rat, bool) {
	result, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	return result, ok
}

func nonNegativeRat(value string) (*big.Rat, bool) {
	result, ok := rat(value)
	return result, ok && result.Sign() >= 0
}

func minimumRat(left, right *big.Rat) *big.Rat {
	if left.Cmp(right) <= 0 {
		return new(big.Rat).Set(left)
	}
	return new(big.Rat).Set(right)
}

func decimal(value *big.Rat) string {
	text := strings.TrimRight(strings.TrimRight(value.FloatString(18), "0"), ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}
