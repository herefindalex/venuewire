package ws

import (
	"testing"
	"time"

	"venuewire/internal/domain"
)

func TestDecodePrivateOrderStatuses(t *testing.T) {
	received := time.UnixMilli(1700000000300).UTC()
	statuses := []struct {
		raw  string
		want domain.OrderStatus
	}{
		{"New", domain.OrderStatusNew}, {"PartiallyFilled", domain.OrderStatusPartiallyFilled},
		{"Filled", domain.OrderStatusFilled}, {"Cancelled", domain.OrderStatusCancelled},
		{"Rejected", domain.OrderStatusRejected},
	}
	for _, tt := range statuses {
		payload := []byte(`{"id":"event-1","topic":"order.linear","creationTime":1700000000200,"data":[{"category":"linear","symbol":"BTCUSDT","orderId":"exchange-1","orderLinkId":"client-1","side":"Buy","orderType":"Limit","price":"100","qty":"1","cumExecQty":"0","avgPrice":"","orderStatus":"` + tt.raw + `","createdTime":"1700000000000","updatedTime":"1700000000100"}]}`)
		events, err := DecodePrivate(payload, received)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].Order == nil || events[0].Order.Status != tt.want || events[0].Order.RawStatus != tt.raw {
			t.Fatalf("%s decoded as %+v", tt.raw, events)
		}
	}
}

func TestDecodeMultipleExecutions(t *testing.T) {
	payload := []byte(`{"id":"event-2","topic":"execution.linear","creationTime":1700000000200,"data":[{"category":"linear","symbol":"BTCUSDT","execId":"exec-1","orderId":"exchange-1","orderLinkId":"client-1","side":"Buy","execPrice":"100","execQty":"0.4","execTime":"1700000000100"},{"category":"linear","symbol":"BTCUSDT","execId":"exec-2","orderId":"exchange-1","orderLinkId":"client-1","side":"Buy","execPrice":"101","execQty":"0.6","execTime":"1700000000150"}]}`)
	events, err := DecodePrivate(payload, time.UnixMilli(1700000000300))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Execution.ExecutionID != "exec-1" || events[1].Execution.ExecutionID != "exec-2" {
		t.Fatalf("not all executions decoded: %+v", events)
	}
}

func TestDecodePosition(t *testing.T) {
	payload := []byte(`{"id":"event-3","topic":"position.linear","creationTime":1700000000200,"data":[{"category":"linear","symbol":"BTCUSDT","side":"Buy","size":"0.01","entryPrice":"100","markPrice":"101","unrealisedPnl":"0.01"}]}`)
	events, err := DecodePrivate(payload, time.UnixMilli(1700000000300))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Position == nil || events[0].Position.Symbol != "BTCUSDT" || events[0].Position.UnrealizedPnL != "0.01" {
		t.Fatalf("unexpected position: %+v", events)
	}
}
