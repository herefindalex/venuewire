package deribitfix

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/herefindalex/venuewire/internal/deribit"
	bybitfix "github.com/herefindalex/venuewire/internal/fix"
)

func testQuantitySpec() QuantitySpec {
	return QuantitySpec{
		Symbol:             "BTC-PERPETUAL",
		SettlementCurrency: "BTC",
		JSONAmountUnit:     "USD_notional",
		FIXQtyType:         QtyTypeUnits,
		ContractMultiplier: "10",
		MinTradeAmount:     "10",
		MinTradeContracts:  "1",
	}
}

func TestSecurityListRequestIsSnapshotOnlyAndCurrencyBounded(t *testing.T) {
	fields, err := SecurityListRequestFields("security-1", "btc")
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, fields, 320, "security-1")
	assertField(t, fields, 559, "0")
	assertField(t, fields, 263, "0")
	assertField(t, fields, 167, "FUT")
	assertField(t, fields, 15, "BTC")
	if _, err := SecurityListRequestFields("security-2", "USDC"); err == nil {
		t.Fatal("unsupported settlement currency was accepted")
	}
}

func TestParseSecurityListPreservesGroupsAndValidatesMetadataConversion(t *testing.T) {
	message := mustMessage(t, []bybitfix.Field{
		{Tag: 35, Value: "y"},
		{Tag: 320, Value: "request-1"},
		{Tag: 146, Value: "2"},
		{Tag: 55, Value: "BTC-PERPETUAL"},
		{Tag: 167, Value: "FUT"},
		{Tag: 120, Value: "BTC"},
		{Tag: 479, Value: "BTC"},
		{Tag: 1524, Value: "USD"},
		{Tag: 231, Value: "10"},
		{Tag: 562, Value: "1"},
		{Tag: 9999, Value: "preserved"},
		{Tag: 55, Value: "ETH-PERPETUAL"},
		{Tag: 167, Value: "FUT"},
		{Tag: 120, Value: "ETH"},
		{Tag: 231, Value: "1"},
		{Tag: 562, Value: "1"},
	})
	securities, err := ParseSecurityList(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(securities) != 2 || securities[0].Symbol != "BTC-PERPETUAL" || securities[1].Symbol != "ETH-PERPETUAL" {
		t.Fatalf("unexpected groups: %+v", securities)
	}
	if got := securities[0].RawFields[len(securities[0].RawFields)-1]; got.Tag != 9999 || got.Value != "preserved" {
		t.Fatalf("unknown field was not preserved: %+v", got)
	}
	instrument := deribit.Instrument{
		InstrumentName:     "BTC-PERPETUAL",
		Kind:               "future",
		SettlementCurrency: "BTC",
		ContractSize:       json.Number("10"),
		MinTradeAmount:     json.Number("10"),
	}
	spec, err := QuantitySpecFromMetadata(instrument, securities[0])
	if err != nil {
		t.Fatal(err)
	}
	if contracts, err := spec.ContractsForUnits("10"); err != nil || contracts != "1" {
		t.Fatalf("contracts=%q err=%v", contracts, err)
	}
}

func TestQuantityMetadataDistinguishesUSDSettlFromNativeCommissionCurrency(t *testing.T) {
	instrument := deribit.Instrument{
		InstrumentName:     "BTC-PERPETUAL",
		Kind:               "future",
		CounterCurrency:    "USD",
		SettlementCurrency: "BTC",
		ContractSize:       json.Number("10"),
		MinTradeAmount:     json.Number("10"),
	}
	security := Security{
		Symbol:             "BTC-PERPETUAL",
		SecurityType:       "FUT",
		SettlementCurrency: "USD",
		CommissionCurrency: "BTC",
		PriceQuoteCurrency: "USD",
		ContractMultiplier: "10",
		MinTradeVolume:     "1",
	}
	if _, err := QuantitySpecFromMetadata(instrument, security); err != nil {
		t.Fatalf("valid inverse-future currency semantics rejected: %v", err)
	}
	security.CommissionCurrency = "USDC"
	if _, err := QuantitySpecFromMetadata(instrument, security); err == nil {
		t.Fatal("non-native commission currency was accepted")
	}
}

func TestSecurityListRejectsUnknownNestedGroupAndMismatchedMultiplier(t *testing.T) {
	message := mustMessage(t, []bybitfix.Field{
		{Tag: 35, Value: "y"},
		{Tag: 146, Value: "1"},
		{Tag: 55, Value: "BTC-PERPETUAL"},
		{Tag: 454, Value: "1"},
	})
	_, err := ParseSecurityList(message)
	var unsupported *UnsupportedRepeatingGroupError
	if !errors.As(err, &unsupported) || unsupported.Tag != 454 {
		t.Fatalf("error=%T %v", err, err)
	}

	instrument := deribit.Instrument{
		InstrumentName:     "BTC-PERPETUAL",
		Kind:               "future",
		SettlementCurrency: "BTC",
		ContractSize:       json.Number("10"),
		MinTradeAmount:     json.Number("10"),
	}
	_, err = QuantitySpecFromMetadata(instrument, Security{
		Symbol:             "BTC-PERPETUAL",
		SecurityType:       "FUT",
		SettlementCurrency: "BTC",
		ContractMultiplier: "1",
		MinTradeVolume:     "1",
	})
	if err == nil {
		t.Fatal("mismatched JSON/FIX multiplier was accepted")
	}
}

func TestNewReplaceAndCancelFieldsUseValidatedUnits(t *testing.T) {
	quantity := testQuantitySpec()
	newFields, err := NewOrderFields(NewOrderRequest{
		ClientClOrdID: "intent-1",
		Label:         "intent-1",
		Symbol:        "BTC-PERPETUAL",
		Side:          "buy",
		Amount:        "10",
		Price:         "80000",
		TimeInForce:   "gtc",
		PostOnly:      true,
		ReduceOnly:    true,
		Quantity:      quantity,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, newFields, 11, "intent-1")
	assertField(t, newFields, 38, "10")
	assertField(t, newFields, 854, QtyTypeUnits)
	assertField(t, newFields, 18, "6AE")

	replaceFields, err := ReplaceOrderFields(ReplaceOrderRequest{
		ExistingOrderRef: ExistingOrderRef{ServerClOrdID: "server-1", Symbol: "BTC-PERPETUAL"},
		Side:             "buy",
		Amount:           "20",
		Price:            "79999.5",
		Quantity:         quantity,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, replaceFields, 41, "server-1")
	assertField(t, replaceFields, 38, "20")
	assertField(t, replaceFields, 854, QtyTypeUnits)

	cancelFields, err := CancelOrderFields(ExistingOrderRef{ServerClOrdID: "server-1", Symbol: "BTC-PERPETUAL"})
	if err != nil {
		t.Fatal(err)
	}
	assertField(t, cancelFields, 41, "server-1")
	assertField(t, cancelFields, 55, "BTC-PERPETUAL")

	if _, err := quantity.ContractsForUnits("15"); err == nil {
		t.Fatal("fractional FIX contract conversion was accepted")
	}
	if _, err := NewOrderFields(NewOrderRequest{ClientClOrdID: "bad id", Symbol: quantity.Symbol, Quantity: quantity}); err == nil {
		t.Fatal("unsafe client identifier was accepted")
	}
}

func TestExecutionReportCorrelatesOrigClOrdIDNotReplacedTag11(t *testing.T) {
	message := mustMessage(t, []bybitfix.Field{
		{Tag: 35, Value: "8"},
		{Tag: 37, Value: "native-1"},
		{Tag: 11, Value: "deribit-server-id"},
		{Tag: 41, Value: "intent-1"},
		{Tag: 100010, Value: "intent-1"},
		{Tag: 39, Value: "1"},
		{Tag: 54, Value: "1"},
		{Tag: 55, Value: "BTC-PERPETUAL"},
		{Tag: 38, Value: "1"},
		{Tag: 14, Value: "0.5"},
		{Tag: 151, Value: "0.5"},
		{Tag: 17, Value: "fix-exec-evidence"},
		{Tag: 32, Value: "0.5"},
		{Tag: 31, Value: "80000"},
		{Tag: 12, Value: "0.00000006"},
		{Tag: 479, Value: "BTC"},
	})
	evidence, err := ParseExecutionReport(message, "intent-1", "intent-1", "10", testQuantitySpec())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ServerClOrdID != "deribit-server-id" || evidence.ClientClOrdID != "intent-1" || evidence.JSONAmountUnits != "10" {
		t.Fatalf("unexpected mapping: %+v", evidence)
	}
	if evidence.FIXExecutionID == "" || evidence.CommissionCurrency != "BTC" {
		t.Fatalf("fill evidence missing: %+v", evidence)
	}

	_, err = ParseExecutionReport(message, "different-intent", "different-label", "10", testQuantitySpec())
	if err == nil {
		t.Fatal("uncorrelated report was accepted")
	}
	_, err = ParseExecutionReport(message, "intent-1", "intent-1", "20", testQuantitySpec())
	if err == nil {
		t.Fatal("mismatched FIX/JSON amount was accepted")
	}
}

func TestCancelledExecutionReportMayOmitOptionalNativeOrderID(t *testing.T) {
	message := mustMessage(t, []bybitfix.Field{
		{Tag: 35, Value: "8"},
		{Tag: 11, Value: "server-1"},
		{Tag: 41, Value: "intent-1"},
		{Tag: 100010, Value: "intent-1"},
		{Tag: 39, Value: "4"},
		{Tag: 14, Value: "0"},
		{Tag: 151, Value: "0"},
		{Tag: 58, Value: "sensitive free text"},
	})
	evidence, err := ParseExecutionReport(message, "intent-1", "intent-1", "10", testQuantitySpec())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Status != "4" || evidence.NativeOrderID != "" || evidence.FIXOrderContracts != "" {
		t.Fatalf("unexpected cancellation evidence: %+v", evidence)
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sensitive free text") || strings.Contains(string(encoded), "RawFields") {
		t.Fatalf("raw FIX fields leaked into JSON: %s", encoded)
	}
}

func TestOrderCancelRejectDoesNotExposeText(t *testing.T) {
	message := mustMessage(t, []bybitfix.Field{
		{Tag: 35, Value: "9"},
		{Tag: 37, Value: "native-1"},
		{Tag: 11, Value: "intent-1"},
		{Tag: 41, Value: "server-1"},
		{Tag: 39, Value: "0"},
		{Tag: 102, Value: "1"},
		{Tag: 58, Value: "secret echo"},
	})
	evidence, err := ParseOrderCancelReject(message)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ReasonCode != "1" || evidence.NativeOrderID != "native-1" {
		t.Fatalf("unexpected evidence: %+v", evidence)
	}
}

func mustMessage(t *testing.T, fields []bybitfix.Field) bybitfix.Message {
	t.Helper()
	raw, err := bybitfix.Encode("FIX.4.4", fields)
	if err != nil {
		t.Fatal(err)
	}
	message, err := bybitfix.ParseStrict(raw)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func assertField(t *testing.T, fields []bybitfix.Field, tag int, want string) {
	t.Helper()
	for _, field := range fields {
		if field.Tag == tag {
			if field.Value != want {
				t.Fatalf("tag %d=%q, want %q", tag, field.Value, want)
			}
			return
		}
	}
	t.Fatalf("tag %d missing", tag)
}
