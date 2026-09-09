package deribit

import "testing"

func TestDecodeUserChangesPreservesMultipleArraysAndDecimalText(t *testing.T) {
	raw := []byte(`{
		"orders":[
			{"order_id":"o1","instrument_name":"BTC-PERPETUAL","order_state":"open","amount":10},
			{"order_id":"o2","instrument_name":"ETH-PERPETUAL","order_state":"filled","amount":1}
		],
		"trades":[
			{"trade_id":"t1","order_id":"o2","instrument_name":"ETH-PERPETUAL","amount":1,"price":2500.05,"fee":-0.00000001},
			{"trade_id":"t2","order_id":"o1","instrument_name":"BTC-PERPETUAL","amount":10,"price":80000.5,"fee":0.00000006}
		],
		"positions":[
			{"instrument_name":"BTC-PERPETUAL","size":10},
			{"instrument_name":"ETH-PERPETUAL","size":1}
		]
	}`)
	changes, err := DecodeUserChanges(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Orders) != 2 || len(changes.Trades) != 2 || len(changes.Positions) != 2 {
		t.Fatalf("changes=%+v", changes)
	}
	if changes.Trades[0].Fee.String() != "-0.00000001" || changes.Trades[1].Price.String() != "80000.5" {
		t.Fatalf("decimal text changed: %+v", changes.Trades)
	}
	if len(changes.Raw) == 0 {
		t.Fatal("raw event was not preserved")
	}
}

func TestDecodeUserChangesRejectsMalformedCanonicalIdentity(t *testing.T) {
	for _, raw := range [][]byte{
		{},
		[]byte(`not-json`),
		[]byte(`{"orders":[{"instrument_name":"BTC-PERPETUAL","order_state":"open"}]}`),
		[]byte(`{"trades":[{"trade_id":"t1","instrument_name":"BTC-PERPETUAL"}]}`),
		[]byte(`{"positions":[{}]}`),
	} {
		if _, err := DecodeUserChanges(raw); err == nil {
			t.Fatalf("payload %q was accepted", raw)
		}
	}
}
