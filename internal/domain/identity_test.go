package domain

import "testing"

func TestCompoundKeysIsolateVenuesAndAccounts(t *testing.T) {
	bybit := AccountKey{Venue: VenueBybit, Environment: EnvironmentTestnet, Alias: "test"}
	deribit := AccountKey{Venue: VenueDeribit, Environment: EnvironmentTestnet, Alias: "test"}
	other := AccountKey{Venue: VenueBybit, Environment: EnvironmentTestnet, Alias: "other"}

	keys := []string{
		(OrderKey{Account: bybit, Namespace: "native", NativeID: "same"}).Canonical(),
		(OrderKey{Account: deribit, Namespace: "native", NativeID: "same"}).Canonical(),
		(OrderKey{Account: other, Namespace: "native", NativeID: "same"}).Canonical(),
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			t.Fatalf("compound key collision: %q", key)
		}
		seen[key] = true
	}
}

func TestCompoundKeyEscapesDelimiters(t *testing.T) {
	a := AccountKey{Venue: VenueBybit, Environment: EnvironmentTestnet, Alias: "a|b"}
	b := AccountKey{Venue: VenueBybit, Environment: EnvironmentTestnet, Alias: "a"}
	left := (OrderKey{Account: a, Namespace: "c", NativeID: "d"}).Canonical()
	right := (OrderKey{Account: b, Namespace: "b|c", NativeID: "d"}).Canonical()
	if left == right {
		t.Fatalf("escaped compound keys collided: %q", left)
	}
}

func TestAccountKeyValidation(t *testing.T) {
	valid := AccountKey{Venue: VenueDeribit, Environment: EnvironmentTestnet, Alias: "deribit-test"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid account rejected: %v", err)
	}
	for _, invalid := range []AccountKey{
		{Venue: "unknown", Environment: EnvironmentTestnet, Alias: "a"},
		{Venue: VenueBybit, Environment: "mainnet", Alias: "a"},
		{Venue: VenueBybit, Environment: EnvironmentTestnet},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid account accepted: %+v", invalid)
		}
	}
}

func TestCapabilitySetsKeepFIXDialectExplicit(t *testing.T) {
	if !BybitCapabilities().Supports(CapabilityFIXOrderWrite) {
		t.Fatal("Bybit FIX capability unexpectedly absent")
	}
	if DeribitR1Capabilities().Supports(CapabilityFIXOrderWrite) {
		t.Fatal("Deribit R1 must not claim FIX order capability")
	}
}
