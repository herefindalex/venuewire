package reconcile

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"bybit/internal/domain"
	"bybit/internal/orderstate"
)

func TestReconciliationRequiredOrderScenarios(t *testing.T) {
	tests := []struct {
		name         string
		local        domain.OrderStatus
		open, recent []domain.Order
		want         domain.OrderStatus
	}{
		{"local new exchange cancelled", domain.OrderStatusNew, []domain.Order{remoteOrder(domain.OrderStatusCancelled)}, nil, domain.OrderStatusCancelled},
		{"local new exchange filled", domain.OrderStatusNew, []domain.Order{remoteOrder(domain.OrderStatusFilled)}, nil, domain.OrderStatusFilled},
		{"local partial exchange filled", domain.OrderStatusPartiallyFilled, []domain.Order{remoteOrder(domain.OrderStatusFilled)}, nil, domain.OrderStatusFilled},
		{"absent open present recent", domain.OrderStatusNew, nil, []domain.Order{remoteOrder(domain.OrderStatusCancelled)}, domain.OrderStatusCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, _ := serviceWithLocal(t, tt.local)
			report, err := (Reconciler{State: service, Exchange: fixtureReader{open: tt.open, recent: tt.recent}}).Reconcile(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := service.Snapshot().Orders["client-1"].Status; got != tt.want {
				t.Fatalf("status = %s, want %s", got, tt.want)
			}
			if len(report.Discrepancies) == 0 {
				t.Fatal("repair was not reported")
			}
		})
	}
}

func TestMissingAndDuplicateExecutions(t *testing.T) {
	service, _ := serviceWithLocal(t, domain.OrderStatusNew)
	execution := domain.Execution{ExecutionID: "exec-1", OrderID: "exchange-1", OrderLinkID: "client-1", Qty: "0.4", Price: "100", ExchangeTime: time.Now(), ReceivedAt: time.Now()}
	reconciler := Reconciler{State: service, Exchange: fixtureReader{executions: []domain.Execution{execution, execution}}}
	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if snapshot.Orders["client-1"].CumFilledQty != "0.4" || snapshot.Orders["client-1"].Status != domain.OrderStatusPartiallyFilled || len(snapshot.Executions) != 1 {
		t.Fatalf("execution reconciliation not idempotent: %+v", snapshot)
	}
	if _, err := reconciler.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Orders["client-1"].CumFilledQty; got != "0.4" {
		t.Fatalf("second run double counted: %s", got)
	}
}

func TestUnknownExchangeOrderImported(t *testing.T) {
	service, _ := serviceWithLocal(t, "")
	report, err := (Reconciler{State: service, Exchange: fixtureReader{open: []domain.Order{remoteOrder(domain.OrderStatusNew)}}}).Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := service.Snapshot().Orders["client-1"].Status; got != domain.OrderStatusNew {
		t.Fatalf("status=%s", got)
	}
	if len(report.Discrepancies) != 1 || report.Discrepancies[0].Kind != "unknown_exchange_order" {
		t.Fatalf("report=%+v", report)
	}
}

func TestRepeatedReconciliationProducesSameStateAndNoSecondRepair(t *testing.T) {
	service, _ := serviceWithLocal(t, domain.OrderStatusNew)
	reconciler := Reconciler{State: service, Exchange: fixtureReader{recent: []domain.Order{remoteOrder(domain.OrderStatusCancelled)}}}
	first, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stateAfterFirst := service.Snapshot()
	second, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stateAfterSecond := service.Snapshot()
	stateAfterFirst.UpdatedAt, stateAfterSecond.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(stateAfterFirst, stateAfterSecond) {
		t.Fatalf("state changed: first=%+v second=%+v", stateAfterFirst, stateAfterSecond)
	}
	if len(first.Discrepancies) != 1 || len(second.Discrepancies) != 0 {
		t.Fatalf("reports: first=%+v second=%+v", first, second)
	}
}

func TestRestartLoadsSnapshotThenRepairs(t *testing.T) {
	service, path := serviceWithLocal(t, domain.OrderStatusPartiallyFilled)
	_ = service
	reloaded, err := orderstate.NewService(context.Background(), orderstate.FileStore{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Reconciler{State: reloaded, Exchange: fixtureReader{recent: []domain.Order{remoteOrder(domain.OrderStatusFilled)}}}).Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot().Orders["client-1"].Status; got != domain.OrderStatusFilled {
		t.Fatalf("status=%s", got)
	}
}

func TestAmbiguousAbsentOrderRemainsVisible(t *testing.T) {
	service, _ := serviceWithLocal(t, domain.OrderStatusNew)
	report, err := (Reconciler{State: service, Exchange: fixtureReader{}}).Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if service.Snapshot().Orders["client-1"].Status != domain.OrderStatusNew {
		t.Fatal("ambiguous order was guessed")
	}
	if len(report.Discrepancies) != 1 || report.Discrepancies[0].Action != "left_visible_ambiguous" {
		t.Fatalf("report=%+v", report)
	}
}

func TestFetchFailureIsExplicitAndDoesNotMutate(t *testing.T) {
	service, _ := serviceWithLocal(t, domain.OrderStatusNew)
	_, err := (Reconciler{State: service, Exchange: fixtureReader{err: errors.New("fixture failure")}}).Reconcile(context.Background())
	if err == nil {
		t.Fatal("expected fetch error")
	}
	if service.Snapshot().Orders["client-1"].Status != domain.OrderStatusNew {
		t.Fatal("failed run mutated state")
	}
}

type fixtureReader struct {
	open, recent []domain.Order
	executions   []domain.Execution
	err          error
}

func (f fixtureReader) OpenOrders(context.Context) ([]domain.Order, error)   { return f.open, f.err }
func (f fixtureReader) RecentOrders(context.Context) ([]domain.Order, error) { return f.recent, f.err }
func (f fixtureReader) Executions(context.Context) ([]domain.Execution, error) {
	return f.executions, f.err
}

func serviceWithLocal(t *testing.T, status domain.OrderStatus) (*orderstate.Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "orders.json")
	service, err := orderstate.NewService(context.Background(), orderstate.FileStore{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if status != "" {
		if err := service.ApplyOrder(context.Background(), localOrder(status)); err != nil {
			t.Fatal(err)
		}
	}
	return service, path
}
func localOrder(status domain.OrderStatus) domain.Order {
	return domain.Order{Exchange: "bybit", Category: "linear", Symbol: "BTCUSDT", OrderID: "exchange-1", OrderLinkID: "client-1", Qty: "1.0", CumFilledQty: "0.0", Status: status, RawStatus: string(status), UpdatedAt: time.Now().Add(-time.Minute)}
}
func remoteOrder(status domain.OrderStatus) domain.Order {
	order := localOrder(status)
	order.UpdatedAt = time.Now()
	if status == domain.OrderStatusFilled {
		order.CumFilledQty = "1.0"
	}
	return order
}
