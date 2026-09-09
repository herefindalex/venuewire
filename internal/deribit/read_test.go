package deribit

import (
	"encoding/json"
	"testing"
)

func TestEffectiveTickSizeUsesHighestStrictlyLowerStep(t *testing.T) {
	instrument := Instrument{
		TickSize: json.Number("0.01"),
		TickSizeSteps: []TickSizeStep{
			{AbovePrice: json.Number("1000"), TickSize: json.Number("1")},
			{AbovePrice: json.Number("100"), TickSize: json.Number("0.1")},
		},
	}
	for _, test := range []struct {
		price string
		want  string
	}{
		{price: "99.99", want: "0.01"},
		{price: "100", want: "0.01"},
		{price: "100.01", want: "0.1"},
		{price: "1000", want: "0.1"},
		{price: "1000.01", want: "1"},
	} {
		t.Run(test.price, func(t *testing.T) {
			got, err := instrument.EffectiveTickSize(test.price)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != test.want {
				t.Fatalf("tick=%s, want %s", got, test.want)
			}
		})
	}
}

func TestEffectiveTickSizeRejectsMalformedMetadata(t *testing.T) {
	instrument := Instrument{
		TickSize:      json.Number("0.5"),
		TickSizeSteps: []TickSizeStep{{AbovePrice: json.Number("100"), TickSize: json.Number("0")}},
	}
	if _, err := instrument.EffectiveTickSize("101"); err == nil {
		t.Fatal("invalid tick step was accepted")
	}
}
