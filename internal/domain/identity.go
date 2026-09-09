package domain

import (
	"errors"
	"net/url"
	"strings"
)

type Venue string
type Environment string

const (
	VenueBybit   Venue = "bybit"
	VenueDeribit Venue = "deribit"

	EnvironmentTestnet Environment = "testnet"
)

type AccountKey struct {
	Venue       Venue       `json:"venue"`
	Environment Environment `json:"environment"`
	Alias       string      `json:"alias"`
}

type MarketKey struct {
	Account    AccountKey `json:"account"`
	Category   string     `json:"category"`
	Instrument string     `json:"instrument"`
}

type OrderKey struct {
	Account   AccountKey `json:"account"`
	Namespace string     `json:"namespace"`
	NativeID  string     `json:"nativeId"`
}

type ExecutionKey struct {
	Account   AccountKey `json:"account"`
	Namespace string     `json:"namespace"`
	NativeID  string     `json:"nativeId"`
}

type PositionKey struct {
	Account AccountKey `json:"account"`
	Market  MarketKey  `json:"market"`
}

func (k AccountKey) Validate() error {
	if k.Venue != VenueBybit && k.Venue != VenueDeribit {
		return errors.New("unsupported venue")
	}
	if k.Environment != EnvironmentTestnet {
		return errors.New("environment must be testnet")
	}
	if strings.TrimSpace(k.Alias) == "" {
		return errors.New("account alias is required")
	}
	return nil
}

func (k AccountKey) Canonical() string {
	return joinKey(string(k.Venue), string(k.Environment), k.Alias)
}

func (k MarketKey) Canonical() string {
	return joinKey(k.Account.Canonical(), k.Category, k.Instrument)
}

func (k OrderKey) Canonical() string {
	return joinKey(k.Account.Canonical(), k.Namespace, k.NativeID)
}

func (k ExecutionKey) Canonical() string {
	return joinKey(k.Account.Canonical(), k.Namespace, k.NativeID)
}

func (k PositionKey) Canonical() string {
	return joinKey(k.Account.Canonical(), k.Market.Category, k.Market.Instrument)
}

func joinKey(parts ...string) string {
	escaped := make([]string, len(parts))
	for i, part := range parts {
		escaped[i] = url.QueryEscape(strings.TrimSpace(part))
	}
	return strings.Join(escaped, "|")
}

type Capability string

const (
	CapabilityMetadata      Capability = "metadata"
	CapabilityAccountRead   Capability = "account_read"
	CapabilityOrderRead     Capability = "order_read"
	CapabilityOrderWrite    Capability = "order_write"
	CapabilityPublicStream  Capability = "public_stream"
	CapabilityPrivateStream Capability = "private_stream"
	CapabilityFIXOrderWrite Capability = "fix_order_write"
)

type CapabilitySet map[Capability]bool

func (s CapabilitySet) Supports(capability Capability) bool { return s[capability] }

func BybitCapabilities() CapabilitySet {
	return CapabilitySet{
		CapabilityMetadata: true, CapabilityAccountRead: true, CapabilityOrderRead: true,
		CapabilityOrderWrite: true, CapabilityPublicStream: true, CapabilityPrivateStream: true,
		CapabilityFIXOrderWrite: true,
	}
}

func DeribitR1Capabilities() CapabilitySet {
	return CapabilitySet{
		CapabilityMetadata: true, CapabilityAccountRead: true, CapabilityOrderRead: true,
		CapabilityOrderWrite: true, CapabilityPublicStream: true, CapabilityPrivateStream: true,
		CapabilityFIXOrderWrite: false,
	}
}
