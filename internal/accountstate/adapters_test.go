package accountstate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"venuewire/internal/deribit"
	"venuewire/internal/rest"
)

type fakeBybitReader struct {
	accounts []rest.WalletAccount
	err      error
}

func (f *fakeBybitReader) WalletBalances(context.Context, string) ([]rest.WalletAccount, rest.ResponseMeta, error) {
	return f.accounts, rest.ResponseMeta{}, f.err
}

type fakeDeribitReader struct {
	summaries []deribit.AccountSummary
	err       error
}

func (f *fakeDeribitReader) AccountSummaries(context.Context) ([]deribit.AccountSummary, error) {
	return f.summaries, f.err
}

func TestBybitProviderNormalizesSnapshotWithoutInventingMissingValues(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	provider := &BybitProvider{
		Client: &fakeBybitReader{accounts: []rest.WalletAccount{{
			AccountType: "UNIFIED",
			TotalEquity: "123.45",
			Coin: []rest.CoinBalance{
				{Coin: " btc ", WalletBalance: "1.5", Free: "0.9", Locked: "0.5", BorrowAmount: "0.1", USDValue: "100"},
				{Coin: "eth", WalletBalance: "1.500000000000000001", Locked: "0.4", BorrowAmount: "0.100000000000000001", USDValue: "20"},
				{Coin: "USDT", WalletBalance: "0.2", Locked: "0.3", BorrowAmount: "0", USDValue: ""},
				{Coin: "XRP", WalletBalance: "4", Locked: "", BorrowAmount: "0", USDValue: "2"},
				{Coin: " ", WalletBalance: "999", Free: "999", USDValue: "999"},
			},
		}}},
		AccountAlias: "demo-bybit",
		Now:          func() time.Time { return now },
	}

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.AccountAlias != "demo-bybit" || snapshot.ExchangeReportedTotalUSD != "123.45" || !snapshot.SnapshotAsOf.Equal(now) {
		t.Fatalf("snapshot metadata = %+v", snapshot)
	}
	if len(snapshot.Assets) != 4 {
		t.Fatalf("len(Assets) = %d, want 4", len(snapshot.Assets))
	}

	assertAvailability(t, snapshot.Assets[0], "BTC", "0.9", "verified")
	assertAvailability(t, snapshot.Assets[1], "ETH", "1", "derived")
	assertAvailability(t, snapshot.Assets[2], "USDT", "0", "derived")
	assertAvailability(t, snapshot.Assets[3], "XRP", "", "unknown")
	if snapshot.Assets[2].Quality != "unpriced" || len(snapshot.UnpricedAssets) != 1 || snapshot.UnpricedAssets[0] != "USDT" {
		t.Fatalf("unpriced state = asset %q, list %v", snapshot.Assets[2].Quality, snapshot.UnpricedAssets)
	}
}

func TestBybitAvailabilityRejectsInvalidOrMissingInputs(t *testing.T) {
	tests := []struct {
		name string
		coin rest.CoinBalance
	}{
		{name: "missing wallet", coin: rest.CoinBalance{Locked: "0", BorrowAmount: "0"}},
		{name: "missing locked", coin: rest.CoinBalance{WalletBalance: "1", BorrowAmount: "0"}},
		{name: "missing borrow", coin: rest.CoinBalance{WalletBalance: "1", Locked: "0"}},
		{name: "invalid wallet", coin: rest.CoinBalance{WalletBalance: "one", Locked: "0", BorrowAmount: "0"}},
		{name: "negative wallet", coin: rest.CoinBalance{WalletBalance: "-1", Locked: "0", BorrowAmount: "0"}},
		{name: "negative locked", coin: rest.CoinBalance{WalletBalance: "1", Locked: "-1", BorrowAmount: "0"}},
		{name: "negative borrow", coin: rest.CoinBalance{WalletBalance: "1", Locked: "0", BorrowAmount: "-1"}},
		{name: "invalid free cannot override", coin: rest.CoinBalance{Free: "NaN", WalletBalance: "", Locked: "", BorrowAmount: ""}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			available, status := bybitAvailable(test.coin)
			if available != "" || status != "unknown" {
				t.Fatalf("bybitAvailable() = (%q, %q), want empty unknown", available, status)
			}
		})
	}
}

func TestBybitProviderRejectsUnavailableAccountSnapshots(t *testing.T) {
	provider := &BybitProvider{}
	if _, err := provider.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot() with nil client error = nil")
	}
	provider.Client = &fakeBybitReader{err: errors.New("request failed")}
	if _, err := provider.Snapshot(context.Background()); !errors.Is(err, provider.Client.(*fakeBybitReader).err) {
		t.Fatalf("Snapshot() error = %v, want provider error", err)
	}
	provider.Client = &fakeBybitReader{accounts: []rest.WalletAccount{{AccountType: "CONTRACT"}}}
	if _, err := provider.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot() with no UNIFIED account error = nil")
	}
}

func TestDeribitProviderNormalizesAvailabilityAndPreservesUnknown(t *testing.T) {
	now := time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC)
	provider := &DeribitProvider{
		Client: &fakeDeribitReader{summaries: []deribit.AccountSummary{
			{Currency: " btc ", Balance: json.Number("1.2"), Equity: json.Number("1.1"), AvailableFunds: json.Number("0.8")},
			{Currency: "ETH", Balance: json.Number("0"), Equity: json.Number("0"), AvailableFunds: json.Number("0")},
			{Currency: "USDC", Balance: json.Number("2"), Equity: json.Number("2"), AvailableFunds: json.Number("")},
			{Currency: "USDT", Balance: json.Number("2"), Equity: json.Number("2"), AvailableFunds: json.Number("-1")},
			{Currency: "SOL", Balance: json.Number(""), Equity: json.Number("2"), AvailableFunds: json.Number("1")},
			{Currency: " ", Balance: json.Number("999"), AvailableFunds: json.Number("999")},
		}},
		AccountAlias: "demo-deribit",
		Now:          func() time.Time { return now },
	}

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.AccountAlias != "demo-deribit" || !snapshot.SnapshotAsOf.Equal(now) {
		t.Fatalf("snapshot metadata = %+v", snapshot)
	}
	if len(snapshot.Assets) != 5 || len(snapshot.UnpricedAssets) != 5 {
		t.Fatalf("asset counts = %d/%d, want 5/5", len(snapshot.Assets), len(snapshot.UnpricedAssets))
	}
	assertAvailability(t, snapshot.Assets[0], "BTC", "0.8", "derived")
	assertAvailability(t, snapshot.Assets[1], "ETH", "0", "derived")
	assertAvailability(t, snapshot.Assets[2], "USDC", "", "unknown")
	assertAvailability(t, snapshot.Assets[3], "USDT", "", "unknown")
	assertAvailability(t, snapshot.Assets[4], "SOL", "", "unknown")
	if snapshot.Assets[0].Liability != "" {
		t.Fatalf("Deribit liability = %q, want unknown", snapshot.Assets[0].Liability)
	}
}

func TestDeribitProviderPropagatesReaderFailure(t *testing.T) {
	want := errors.New("RPC unavailable")
	provider := &DeribitProvider{Client: &fakeDeribitReader{err: want}}
	if _, err := provider.Snapshot(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Snapshot() error = %v, want %v", err, want)
	}
}

func assertAvailability(t *testing.T, asset Asset, name, amount, status string) {
	t.Helper()
	if asset.Asset != name || asset.AvailableToTrade != amount || asset.AvailableStatus != status {
		t.Fatalf("availability = (%q, %q, %q), want (%q, %q, %q)", asset.Asset, asset.AvailableToTrade, asset.AvailableStatus, name, amount, status)
	}
}
