package deribit

import "testing"

func TestOrderBookSnapshotDeltaDuplicateAndGap(t *testing.T) {
	book := NewOrderBook("BTC-PERPETUAL")
	snapshot := BookUpdate{Type: "snapshot", InstrumentName: "BTC-PERPETUAL", ChangeID: 10, Bids: [][]any{{"new", 100.0, 2.0}}, Asks: [][]any{{"new", 101.0, 3.0}}}
	if err := book.Apply(snapshot); err != nil {
		t.Fatal(err)
	}
	delta := BookUpdate{Type: "change", InstrumentName: "BTC-PERPETUAL", PrevChangeID: 10, ChangeID: 11, Bids: [][]any{{"change", 100.0, 1.0}}, Asks: [][]any{{"new", 102.0, 4.0}}}
	if err := book.Apply(delta); err != nil {
		t.Fatal(err)
	}
	if err := book.Apply(delta); err != nil {
		t.Fatalf("duplicate rejected: %v", err)
	}
	bids, asks := book.Levels()
	if len(bids) != 1 || bids[0].Amount != 1 || len(asks) != 2 {
		t.Fatalf("bad levels: %+v %+v", bids, asks)
	}
	gap := BookUpdate{Type: "change", InstrumentName: "BTC-PERPETUAL", PrevChangeID: 12, ChangeID: 13}
	if err := book.Apply(gap); err == nil || !book.Stale {
		t.Fatal("gap did not invalidate book")
	}
	if err := book.Apply(delta); err == nil {
		t.Fatal("delta applied while stale")
	}
	if err := book.Apply(snapshot); err != nil || book.Stale {
		t.Fatal("snapshot did not recover")
	}
}

func TestOrderBookRejectsMalformedLevels(t *testing.T) {
	book := NewOrderBook("BTC-PERPETUAL")
	if err := book.Apply(BookUpdate{Type: "snapshot", InstrumentName: "BTC-PERPETUAL", ChangeID: 1, Bids: [][]any{{"new", -1.0, 2.0}}}); err == nil {
		t.Fatal("invalid price accepted")
	}
}
