package deribit

import (
	"errors"
	"sort"
)

type BookChange struct {
	Type   string
	Price  float64
	Amount float64
}

type BookUpdate struct {
	Type           string  `json:"type"`
	InstrumentName string  `json:"instrument_name"`
	ChangeID       int64   `json:"change_id"`
	PrevChangeID   int64   `json:"prev_change_id"`
	Timestamp      int64   `json:"timestamp"`
	Bids           [][]any `json:"bids"`
	Asks           [][]any `json:"asks"`
}

type BookLevel struct{ Price, Amount float64 }

type OrderBook struct {
	Instrument string
	ChangeID   int64
	Stale      bool
	bids       map[float64]float64
	asks       map[float64]float64
}

func NewOrderBook(instrument string) *OrderBook {
	return &OrderBook{Instrument: instrument, Stale: true, bids: map[float64]float64{}, asks: map[float64]float64{}}
}

func (b *OrderBook) Apply(update BookUpdate) error {
	if update.InstrumentName != b.Instrument {
		return errors.New("order-book instrument mismatch")
	}
	if update.Type == "snapshot" {
		b.bids, b.asks = map[float64]float64{}, map[float64]float64{}
		if err := applyLevels(b.bids, update.Bids); err != nil {
			b.Stale = true
			return err
		}
		if err := applyLevels(b.asks, update.Asks); err != nil {
			b.Stale = true
			return err
		}
		b.ChangeID, b.Stale = update.ChangeID, false
		return nil
	}
	if update.Type != "change" {
		b.Stale = true
		return errors.New("unknown order-book update type")
	}
	if b.Stale {
		return errors.New("order book is stale; snapshot required")
	}
	if update.ChangeID == b.ChangeID {
		return nil
	} // duplicate
	if update.PrevChangeID != b.ChangeID || update.ChangeID <= b.ChangeID {
		b.Stale = true
		return errors.New("order-book sequence gap")
	}
	if err := applyLevels(b.bids, update.Bids); err != nil {
		b.Stale = true
		return err
	}
	if err := applyLevels(b.asks, update.Asks); err != nil {
		b.Stale = true
		return err
	}
	b.ChangeID = update.ChangeID
	return nil
}

func applyLevels(side map[float64]float64, levels [][]any) error {
	for _, raw := range levels {
		if len(raw) != 3 {
			return errors.New("malformed order-book level")
		}
		action, ok := raw[0].(string)
		if !ok {
			return errors.New("malformed order-book action")
		}
		price, ok := raw[1].(float64)
		if !ok || price <= 0 {
			return errors.New("malformed order-book price")
		}
		amount, ok := raw[2].(float64)
		if !ok || amount < 0 {
			return errors.New("malformed order-book amount")
		}
		switch action {
		case "new", "change":
			if amount == 0 {
				delete(side, price)
			} else {
				side[price] = amount
			}
		case "delete":
			delete(side, price)
		default:
			return errors.New("unknown order-book action")
		}
	}
	return nil
}

func (b *OrderBook) Levels() (bids, asks []BookLevel) {
	for price, amount := range b.bids {
		bids = append(bids, BookLevel{price, amount})
	}
	for price, amount := range b.asks {
		asks = append(asks, BookLevel{price, amount})
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i].Price > bids[j].Price })
	sort.Slice(asks, func(i, j int) bool { return asks[i].Price < asks[j].Price })
	return
}
