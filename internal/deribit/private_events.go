package deribit

import (
	"bytes"
	"encoding/json"
	"errors"
)

type UserChanges struct {
	Orders    []Order         `json:"orders"`
	Trades    []Trade         `json:"trades"`
	Positions []Position      `json:"positions"`
	Raw       json.RawMessage `json:"-"`
}

func DecodeUserChanges(raw json.RawMessage) (UserChanges, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return UserChanges{}, errors.New("Deribit user changes payload is empty")
	}
	var changes UserChanges
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&changes); err != nil {
		return UserChanges{}, errors.New("malformed Deribit user changes payload")
	}
	for _, order := range changes.Orders {
		if order.OrderID == "" || order.InstrumentName == "" || order.OrderState == "" {
			return UserChanges{}, errors.New("Deribit user changes order lacks identity or state")
		}
	}
	for _, trade := range changes.Trades {
		if trade.TradeID == "" || trade.OrderID == "" || trade.InstrumentName == "" {
			return UserChanges{}, errors.New("Deribit user changes trade lacks canonical identity")
		}
	}
	for _, position := range changes.Positions {
		if position.InstrumentName == "" {
			return UserChanges{}, errors.New("Deribit user changes position lacks instrument identity")
		}
	}
	changes.Raw = append(json.RawMessage(nil), raw...)
	return changes, nil
}
