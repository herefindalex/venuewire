package deribitfix

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"venuewire/internal/deribit"
	bybitfix "venuewire/internal/fix"
)

const (
	QtyTypeUnits     = "0"
	QtyTypeContracts = "1"
)

func SecurityListRequestFields(requestID, currency string) ([]bybitfix.Field, error) {
	if err := validateIdentifier("SecurityReqID", requestID); err != nil {
		return nil, err
	}
	fields := []bybitfix.Field{
		{Tag: 320, Value: requestID},
		{Tag: 559, Value: "0"},
		{Tag: 263, Value: "0"},
		{Tag: 167, Value: "FUT"},
	}
	if currency != "" {
		currency = strings.ToUpper(currency)
		if currency != "BTC" && currency != "ETH" {
			return nil, errors.New("SecurityList currency must be BTC or ETH")
		}
		fields = append(fields, bybitfix.Field{Tag: 15, Value: currency})
	}
	return fields, nil
}

type Security struct {
	Symbol             string
	SecurityType       string
	SettlementCurrency string
	CommissionCurrency string
	PriceQuoteCurrency string
	ContractMultiplier string
	MinTradeVolume     string
	RawFields          []bybitfix.Field `json:"-"`
}

type UnsupportedRepeatingGroupError struct{ Tag int }

func (e *UnsupportedRepeatingGroupError) Error() string {
	return fmt.Sprintf("unsupported Deribit FIX repeating group tag %d", e.Tag)
}

// ParseSecurityList preserves every group field in wire order. Nested groups
// that this connector cannot safely interpret are rejected explicitly.
func ParseSecurityList(message bybitfix.Message) ([]Security, error) {
	if typ, _ := message.Get(35); typ != "y" {
		return nil, errors.New("Deribit FIX SecurityList must have MsgType y")
	}
	countRaw, ok := message.Get(146)
	if !ok {
		return nil, errors.New("Deribit FIX SecurityList missing NoRelatedSym(146)")
	}
	count, err := strconv.Atoi(countRaw)
	if err != nil || count < 0 {
		return nil, errors.New("invalid Deribit FIX NoRelatedSym(146)")
	}
	var securities []Security
	var current *Security
	for _, field := range message.Fields {
		if field.Tag == 35 || field.Tag == 146 || isSessionHeaderTag(field.Tag) {
			continue
		}
		if field.Tag == 55 {
			securities = append(securities, Security{Symbol: field.Value})
			current = &securities[len(securities)-1]
		}
		if current == nil {
			continue
		}
		current.RawFields = append(current.RawFields, field)
		switch field.Tag {
		case 55:
			current.Symbol = field.Value
		case 167:
			current.SecurityType = field.Value
		case 120:
			current.SettlementCurrency = field.Value
		case 479:
			current.CommissionCurrency = field.Value
		case 1524:
			current.PriceQuoteCurrency = field.Value
		case 231:
			current.ContractMultiplier = field.Value
		case 562:
			current.MinTradeVolume = field.Value
		case 454, 1205:
			nested, parseErr := strconv.Atoi(field.Value)
			if parseErr != nil || nested != 0 {
				return nil, &UnsupportedRepeatingGroupError{Tag: field.Tag}
			}
		}
	}
	if len(securities) != count {
		return nil, fmt.Errorf("Deribit FIX SecurityList group count mismatch: declared=%d parsed=%d", count, len(securities))
	}
	return securities, nil
}

func isSessionHeaderTag(tag int) bool {
	switch tag {
	case 34, 49, 52, 56:
		return true
	default:
		return false
	}
}

type QuantitySpec struct {
	Symbol             string
	SettlementCurrency string
	JSONAmountUnit     string
	FIXQtyType         string
	ContractMultiplier string
	MinTradeAmount     string
	MinTradeContracts  string
}

// QuantitySpecFromMetadata proves the JSON-RPC and FIX multipliers describe
// the same inverse future. Live FIX writes must not bypass this check.
func QuantitySpecFromMetadata(instrument deribit.Instrument, security Security) (QuantitySpec, error) {
	if instrument.InstrumentName == "" || instrument.InstrumentName != security.Symbol {
		return QuantitySpec{}, errors.New("JSON-RPC and FIX instrument symbols do not match")
	}
	if instrument.Kind != "future" || security.SecurityType != "FUT" {
		return QuantitySpec{}, errors.New("only Deribit futures have a validated FIX quantity conversion")
	}
	settlement := strings.ToUpper(instrument.SettlementCurrency)
	if settlement != "BTC" && settlement != "ETH" {
		return QuantitySpec{}, errors.New("only native BTC/ETH-settled futures are supported")
	}
	if security.SettlementCurrency != "" &&
		!strings.EqualFold(security.SettlementCurrency, settlement) &&
		!strings.EqualFold(security.SettlementCurrency, instrument.CounterCurrency) &&
		!strings.EqualFold(security.SettlementCurrency, instrument.QuoteCurrency) {
		return QuantitySpec{}, fmt.Errorf("FIX SettlCurrency %q matches neither JSON settlement nor quote currency", security.SettlementCurrency)
	}
	if security.CommissionCurrency != "" && !strings.EqualFold(security.CommissionCurrency, settlement) {
		return QuantitySpec{}, fmt.Errorf("FIX CommCurrency %q does not match native JSON settlement currency %q", security.CommissionCurrency, settlement)
	}
	jsonMultiplier, err := positiveDecimal("JSON contract_size", instrument.ContractSize.String())
	if err != nil {
		return QuantitySpec{}, err
	}
	fixMultiplier, err := positiveDecimal("FIX ContractMultiplier(231)", security.ContractMultiplier)
	if err != nil {
		return QuantitySpec{}, err
	}
	if jsonMultiplier.Cmp(fixMultiplier) != 0 {
		return QuantitySpec{}, errors.New("JSON contract_size and FIX ContractMultiplier differ")
	}
	minimumUnits, err := positiveDecimal("JSON min_trade_amount", instrument.MinTradeAmount.String())
	if err != nil {
		return QuantitySpec{}, err
	}
	minimumContracts, err := positiveDecimal("FIX MinTradeVol(562)", security.MinTradeVolume)
	if err != nil {
		return QuantitySpec{}, err
	}
	convertedMinimum := new(big.Rat).Mul(minimumContracts, fixMultiplier)
	if convertedMinimum.Cmp(minimumUnits) != 0 {
		return QuantitySpec{}, errors.New("JSON minimum amount does not match FIX minimum contracts times multiplier")
	}
	return QuantitySpec{
		Symbol:             instrument.InstrumentName,
		SettlementCurrency: settlement,
		JSONAmountUnit:     "USD_notional",
		FIXQtyType:         QtyTypeUnits,
		ContractMultiplier: security.ContractMultiplier,
		MinTradeAmount:     instrument.MinTradeAmount.String(),
		MinTradeContracts:  security.MinTradeVolume,
	}, nil
}

func (q QuantitySpec) ContractsForUnits(amount string) (string, error) {
	if q.FIXQtyType != QtyTypeUnits || q.JSONAmountUnit != "USD_notional" {
		return "", errors.New("FIX quantity mode is not validated USD units")
	}
	units, err := positiveDecimal("order amount", amount)
	if err != nil {
		return "", err
	}
	minimum, err := positiveDecimal("minimum order amount", q.MinTradeAmount)
	if err != nil {
		return "", err
	}
	if units.Cmp(minimum) < 0 {
		return "", fmt.Errorf("order amount is below minimum %s USD", q.MinTradeAmount)
	}
	multiplier, err := positiveDecimal("contract multiplier", q.ContractMultiplier)
	if err != nil {
		return "", err
	}
	contracts := new(big.Rat).Quo(units, multiplier)
	if contracts.Denom().Cmp(big.NewInt(1)) != 0 {
		return "", errors.New("order amount does not convert to a whole number of FIX contracts")
	}
	return contracts.Num().String(), nil
}

type NewOrderRequest struct {
	ClientClOrdID string
	Label         string
	Symbol        string
	Side          string
	Amount        string
	Price         string
	TimeInForce   string
	PostOnly      bool
	ReduceOnly    bool
	Quantity      QuantitySpec
}

func NewOrderFields(request NewOrderRequest) ([]bybitfix.Field, error) {
	if err := validateIdentifier("ClOrdID", request.ClientClOrdID); err != nil {
		return nil, err
	}
	if request.Label == "" {
		request.Label = request.ClientClOrdID
	}
	if err := validateIdentifier("DeribitLabel", request.Label); err != nil {
		return nil, err
	}
	if request.Symbol == "" || request.Symbol != request.Quantity.Symbol {
		return nil, errors.New("order symbol does not match validated quantity metadata")
	}
	side, err := sideCode(request.Side)
	if err != nil {
		return nil, err
	}
	if _, err := request.Quantity.ContractsForUnits(request.Amount); err != nil {
		return nil, err
	}
	if _, err := positiveDecimal("price", request.Price); err != nil {
		return nil, err
	}
	tif, err := timeInForceCode(request.TimeInForce)
	if err != nil {
		return nil, err
	}
	fields := []bybitfix.Field{
		{Tag: 11, Value: request.ClientClOrdID},
		{Tag: 54, Value: side},
		{Tag: 38, Value: request.Amount},
		{Tag: 44, Value: request.Price},
		{Tag: 55, Value: request.Symbol},
		{Tag: 40, Value: "2"},
		{Tag: 59, Value: tif},
		{Tag: 854, Value: QtyTypeUnits},
		{Tag: 100010, Value: request.Label},
	}
	var execInst string
	if request.PostOnly {
		execInst += "6A"
	}
	if request.ReduceOnly {
		execInst += "E"
	}
	if execInst != "" {
		fields = append(fields, bybitfix.Field{Tag: 18, Value: execInst})
	}
	return fields, nil
}

type ExistingOrderRef struct {
	ServerClOrdID string
	Label         string
	Symbol        string
}

func CancelOrderFields(request ExistingOrderRef) ([]bybitfix.Field, error) {
	if request.Symbol == "" {
		return nil, errors.New("cancel symbol is required")
	}
	identifier, err := orderReferenceFields(request)
	if err != nil {
		return nil, err
	}
	return append(identifier, bybitfix.Field{Tag: 55, Value: request.Symbol}), nil
}

type ReplaceOrderRequest struct {
	ExistingOrderRef
	Side       string
	Amount     string
	Price      string
	PostOnly   bool
	ReduceOnly bool
	Quantity   QuantitySpec
}

func ReplaceOrderFields(request ReplaceOrderRequest) ([]bybitfix.Field, error) {
	if request.Symbol == "" || request.Symbol != request.Quantity.Symbol {
		return nil, errors.New("replace symbol does not match validated quantity metadata")
	}
	identifier, err := orderReferenceFields(request.ExistingOrderRef)
	if err != nil {
		return nil, err
	}
	side, err := sideCode(request.Side)
	if err != nil {
		return nil, err
	}
	if _, err := request.Quantity.ContractsForUnits(request.Amount); err != nil {
		return nil, err
	}
	if _, err := positiveDecimal("price", request.Price); err != nil {
		return nil, err
	}
	fields := append(identifier,
		bybitfix.Field{Tag: 55, Value: request.Symbol},
		bybitfix.Field{Tag: 54, Value: side},
		bybitfix.Field{Tag: 38, Value: request.Amount},
		bybitfix.Field{Tag: 40, Value: "2"},
		bybitfix.Field{Tag: 44, Value: request.Price},
		bybitfix.Field{Tag: 854, Value: QtyTypeUnits},
	)
	var execInst string
	if request.PostOnly {
		execInst += "6A"
	}
	if request.ReduceOnly {
		execInst += "E"
	}
	if execInst != "" {
		fields = append(fields, bybitfix.Field{Tag: 18, Value: execInst})
	}
	return fields, nil
}

func orderReferenceFields(request ExistingOrderRef) ([]bybitfix.Field, error) {
	if request.ServerClOrdID != "" && request.Label != "" {
		return nil, errors.New("exactly one server ClOrdID or Deribit label is required")
	}
	if request.ServerClOrdID != "" {
		if err := validateIdentifier("OrigClOrdID", request.ServerClOrdID); err != nil {
			return nil, err
		}
		return []bybitfix.Field{{Tag: 41, Value: request.ServerClOrdID}}, nil
	}
	if err := validateIdentifier("DeribitLabel", request.Label); err != nil {
		return nil, err
	}
	return []bybitfix.Field{{Tag: 100010, Value: request.Label}}, nil
}

type OrderEvidence struct {
	NativeOrderID      string
	ClientClOrdID      string
	ServerClOrdID      string
	Label              string
	Symbol             string
	Status             string
	Side               string
	Price              string
	FIXOrderContracts  string
	JSONAmountUnits    string
	CumContracts       string
	LeavesContracts    string
	FIXExecutionID     string
	LastQtyContracts   string
	LastPrice          string
	Commission         string
	CommissionCurrency string
	RejectReason       string
	RawFields          []bybitfix.Field `json:"-"`
}

func ParseExecutionReport(message bybitfix.Message, expectedClientClOrdID, expectedLabel, expectedAmount string, quantity QuantitySpec) (OrderEvidence, error) {
	if typ, _ := message.Get(35); typ != "8" {
		return OrderEvidence{}, errors.New("Deribit FIX order evidence must be ExecutionReport(8)")
	}
	evidence := OrderEvidence{RawFields: append([]bybitfix.Field(nil), message.Fields...)}
	evidence.NativeOrderID, _ = message.Get(37)
	evidence.ServerClOrdID, _ = message.Get(11)
	evidence.ClientClOrdID, _ = message.Get(41)
	evidence.Label, _ = message.Get(100010)
	evidence.Symbol, _ = message.Get(55)
	evidence.Status, _ = message.Get(39)
	evidence.Side, _ = message.Get(54)
	evidence.Price, _ = message.Get(44)
	evidence.FIXOrderContracts, _ = message.Get(38)
	evidence.CumContracts, _ = message.Get(14)
	evidence.LeavesContracts, _ = message.Get(151)
	evidence.FIXExecutionID, _ = message.Get(17)
	evidence.LastQtyContracts, _ = message.Get(32)
	evidence.LastPrice, _ = message.Get(31)
	evidence.Commission, _ = message.Get(12)
	evidence.CommissionCurrency, _ = message.Get(479)
	evidence.RejectReason, _ = message.Get(103)
	if evidence.ClientClOrdID != expectedClientClOrdID && evidence.Label != expectedLabel {
		return OrderEvidence{}, errors.New("ExecutionReport cannot be correlated by OrigClOrdID or DeribitLabel")
	}
	if evidence.Status == "" {
		return OrderEvidence{}, errors.New("ExecutionReport missing OrdStatus(39)")
	}
	if evidence.FIXOrderContracts != "" {
		multiplier, err := positiveDecimal("contract multiplier", quantity.ContractMultiplier)
		if err != nil {
			return OrderEvidence{}, err
		}
		contracts, err := nonNegativeDecimal("ExecutionReport OrderQty", evidence.FIXOrderContracts)
		if err != nil {
			return OrderEvidence{}, err
		}
		units := new(big.Rat).Mul(contracts, multiplier)
		expected, err := positiveDecimal("expected JSON amount", expectedAmount)
		if err != nil {
			return OrderEvidence{}, err
		}
		if units.Cmp(expected) != 0 {
			return OrderEvidence{}, errors.New("ExecutionReport contract quantity does not match planned JSON amount")
		}
		evidence.JSONAmountUnits = expectedAmount
	}
	return evidence, nil
}

type CancelRejectEvidence struct {
	NativeOrderID string
	ClientClOrdID string
	ServerClOrdID string
	Status        string
	ReasonCode    string
	RawFields     []bybitfix.Field `json:"-"`
}

func ParseOrderCancelReject(message bybitfix.Message) (CancelRejectEvidence, error) {
	if typ, _ := message.Get(35); typ != "9" {
		return CancelRejectEvidence{}, errors.New("Deribit FIX cancel evidence must be OrderCancelReject(9)")
	}
	evidence := CancelRejectEvidence{RawFields: append([]bybitfix.Field(nil), message.Fields...)}
	evidence.NativeOrderID, _ = message.Get(37)
	evidence.ClientClOrdID, _ = message.Get(11)
	evidence.ServerClOrdID, _ = message.Get(41)
	evidence.Status, _ = message.Get(39)
	evidence.ReasonCode, _ = message.Get(102)
	if evidence.ReasonCode == "" {
		return CancelRejectEvidence{}, errors.New("OrderCancelReject missing CxlRejReason(102)")
	}
	return evidence, nil
}

func sideCode(side string) (string, error) {
	switch strings.ToLower(side) {
	case "buy":
		return "1", nil
	case "sell":
		return "2", nil
	default:
		return "", errors.New("side must be buy or sell")
	}
}

func timeInForceCode(value string) (string, error) {
	switch strings.ToLower(value) {
	case "", "good_til_cancelled", "gtc":
		return "1", nil
	case "good_til_day", "gtd":
		return "0", nil
	case "immediate_or_cancel", "ioc":
		return "3", nil
	case "fill_or_kill", "fok":
		return "4", nil
	default:
		return "", errors.New("unsupported Deribit FIX time in force")
	}
}

func validateIdentifier(name, value string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 64 {
		return fmt.Errorf("%s must be valid UTF-8 with 1 to 64 characters", name)
	}
	for _, character := range value {
		if character > 127 || !(character == '-' || character == '_' || character == '.' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z') {
			return fmt.Errorf("%s must use the connector's safe ASCII identifier subset", name)
		}
	}
	return nil
}

func positiveDecimal(name, value string) (*big.Rat, error) {
	result, ok := new(big.Rat).SetString(value)
	if !ok || result.Sign() <= 0 {
		return nil, fmt.Errorf("%s must be a positive decimal", name)
	}
	return result, nil
}

func nonNegativeDecimal(name, value string) (*big.Rat, error) {
	result, ok := new(big.Rat).SetString(value)
	if !ok || result.Sign() < 0 {
		return nil, fmt.Errorf("%s must be a non-negative decimal", name)
	}
	return result, nil
}
