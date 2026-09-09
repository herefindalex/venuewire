package orderstate

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"bybit/internal/domain"
)

type TransitionError struct {
	From domain.OrderStatus
	To   domain.OrderStatus
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid order state transition %s -> %s", e.From, e.To)
}

func ValidateTransition(from, to domain.OrderStatus) error {
	if from == to {
		return nil
	}
	allowed := map[domain.OrderStatus]map[domain.OrderStatus]bool{
		domain.OrderStatusUnknown: {
			domain.OrderStatusPendingSubmit: true, domain.OrderStatusNew: true, domain.OrderStatusPartiallyFilled: true,
			domain.OrderStatusFilled: true, domain.OrderStatusCancelled: true, domain.OrderStatusRejected: true,
		},
		domain.OrderStatusPendingSubmit: {
			domain.OrderStatusNew: true, domain.OrderStatusPartiallyFilled: true, domain.OrderStatusFilled: true,
			domain.OrderStatusCancelled: true, domain.OrderStatusRejected: true,
		},
		domain.OrderStatusNew: {
			domain.OrderStatusPartiallyFilled: true, domain.OrderStatusFilled: true,
			domain.OrderStatusPendingCancel: true, domain.OrderStatusCancelled: true,
		},
		domain.OrderStatusPartiallyFilled: {
			domain.OrderStatusFilled: true, domain.OrderStatusPendingCancel: true, domain.OrderStatusCancelled: true,
		},
		domain.OrderStatusPendingCancel: {
			domain.OrderStatusCancelled: true, domain.OrderStatusFilled: true,
			domain.OrderStatusRejectedCancel: true,
		},
		domain.OrderStatusCancelled: {domain.OrderStatusFilled: true},
	}
	if allowed[from][to] {
		return nil
	}
	return &TransitionError{From: from, To: to}
}

type Service struct {
	mu       sync.Mutex
	store    StateStore
	snapshot *Snapshot
}

func NewService(ctx context.Context, store StateStore) (*Service, error) {
	if store == nil {
		return nil, errors.New("state store is nil")
	}
	snapshot, err := store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, snapshot: snapshot}, nil
}

func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSnapshot(s.snapshot)
}

func (s *Service) ApplyOrder(ctx context.Context, update domain.Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(ctx, func(snapshot *Snapshot) error { return applyOrderToSnapshot(snapshot, update) })
}

// UpsertREST preserves any state already advanced by an asynchronous stream.
// A late REST acknowledgement or rejection must never regress New/Filled/etc.
func (s *Service) UpsertREST(ctx context.Context, update domain.Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(ctx, func(snapshot *Snapshot) error {
		_, current, found := findOrder(snapshot, update.OrderID, update.OrderLinkID)
		if found && current.Status != domain.OrderStatusUnknown && current.Status != domain.OrderStatusPendingSubmit {
			advancedStatus, advancedRaw, advancedCum, advancedAvg, advancedUpdated := current.Status, current.RawStatus, current.CumFilledQty, current.AvgFillPrice, current.UpdatedAt
			merged := mergeOrder(current, update)
			merged.Status, merged.RawStatus, merged.CumFilledQty, merged.AvgFillPrice, merged.UpdatedAt = advancedStatus, advancedRaw, advancedCum, advancedAvg, advancedUpdated
			return applyOrderToSnapshot(snapshot, merged)
		}
		return applyOrderToSnapshot(snapshot, update)
	})
}

func applyOrderToSnapshot(snapshot *Snapshot, update domain.Order) error {
	key, current, found := findOrder(snapshot, update.OrderID, update.OrderLinkID)
	if !found {
		key = update.OrderLinkID
		if key == "" {
			key = update.OrderID
		}
		if key == "" {
			return errors.New("order update has neither orderId nor orderLinkId")
		}
		current = domain.Order{Status: domain.OrderStatusUnknown, CreatedAt: update.CreatedAt}
	}
	if err := ValidateTransition(current.Status, update.Status); err != nil {
		return err
	}
	merged := mergeOrder(current, update)
	if merged.CreatedAt.IsZero() {
		merged.CreatedAt = time.Now().UTC()
	}
	if merged.UpdatedAt.IsZero() {
		merged.UpdatedAt = time.Now().UTC()
	}
	if key != merged.OrderLinkID && merged.OrderLinkID != "" {
		delete(snapshot.Orders, key)
		key = merged.OrderLinkID
	}
	snapshot.Orders[key] = merged
	return nil
}

func (s *Service) ApplyExecution(ctx context.Context, execution domain.Execution) error {
	if execution.ExecutionID == "" {
		return errors.New("execution update has no executionId")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(ctx, func(snapshot *Snapshot) error {
		if _, duplicate := snapshot.Executions[execution.ExecutionID]; duplicate {
			return nil
		}
		key, order, found := findOrder(snapshot, execution.OrderID, execution.OrderLinkID)
		if !found {
			return fmt.Errorf("execution %q references unknown order", execution.ExecutionID)
		}
		cumulative, err := addDecimal(order.CumFilledQty, execution.Qty)
		if err != nil {
			return fmt.Errorf("execution %q quantity: %w", execution.ExecutionID, err)
		}
		order.CumFilledQty, order.AvgFillPrice, order.UpdatedAt = cumulative, execution.Price, execution.ReceivedAt
		if order.UpdatedAt.IsZero() {
			order.UpdatedAt = time.Now().UTC()
		}
		next := domain.OrderStatusPartiallyFilled
		if compareDecimal(cumulative, order.Qty) >= 0 && order.Qty != "" {
			next = domain.OrderStatusFilled
		} else if order.Status == domain.OrderStatusPendingCancel {
			next = domain.OrderStatusPendingCancel
		}
		if next != order.Status {
			if err := ValidateTransition(order.Status, next); err != nil {
				return err
			}
			order.Status = next
		}
		snapshot.Executions[execution.ExecutionID], snapshot.Orders[key] = execution, order
		return nil
	})
}

// RecordExecution persists an execution already reflected by an authoritative
// cumulative quantity (for example, a REST order snapshot) without adding its
// quantity a second time.
func (s *Service) RecordExecution(ctx context.Context, execution domain.Execution) error {
	if execution.ExecutionID == "" {
		return errors.New("execution update has no executionId")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutate(ctx, func(snapshot *Snapshot) error {
		if _, duplicate := snapshot.Executions[execution.ExecutionID]; duplicate {
			return nil
		}
		if _, _, found := findOrder(snapshot, execution.OrderID, execution.OrderLinkID); !found {
			return fmt.Errorf("execution %q references unknown order", execution.ExecutionID)
		}
		snapshot.Executions[execution.ExecutionID] = execution
		return nil
	})
}

func findOrder(snapshot *Snapshot, orderID, orderLinkID string) (string, domain.Order, bool) {
	if orderLinkID != "" {
		if order, ok := snapshot.Orders[orderLinkID]; ok {
			return orderLinkID, order, true
		}
	}
	for key, order := range snapshot.Orders {
		if orderID != "" && order.OrderID == orderID {
			return key, order, true
		}
	}
	return "", domain.Order{}, false
}

func (s *Service) mutate(ctx context.Context, operation func(*Snapshot) error) error {
	if transactional, ok := s.store.(TransactionalStore); ok {
		err := transactional.Update(ctx, func(latest *Snapshot) error {
			return operation(latest)
		})
		if err != nil {
			return err
		}
		latest, err := s.store.Load(ctx)
		if err != nil {
			return err
		}
		s.snapshot = latest
		return nil
	}
	if err := operation(s.snapshot); err != nil {
		return err
	}
	return s.store.Save(ctx, s.snapshot)
}

func mergeOrder(current, update domain.Order) domain.Order {
	if update.Exchange != "" {
		current.Exchange = update.Exchange
	}
	if update.Category != "" {
		current.Category = update.Category
	}
	if update.Symbol != "" {
		current.Symbol = update.Symbol
	}
	if update.OrderID != "" {
		current.OrderID = update.OrderID
	}
	if update.OrderLinkID != "" {
		current.OrderLinkID = update.OrderLinkID
	}
	if update.Side != "" {
		current.Side = update.Side
	}
	if update.Type != "" {
		current.Type = update.Type
	}
	if update.Price != "" {
		current.Price = update.Price
	}
	if update.Qty != "" {
		current.Qty = update.Qty
	}
	if update.CumFilledQty != "" {
		current.CumFilledQty = update.CumFilledQty
	}
	if update.AvgFillPrice != "" {
		current.AvgFillPrice = update.AvgFillPrice
	}
	if update.Status != "" {
		current.Status = update.Status
	}
	if update.RawStatus != "" {
		current.RawStatus = update.RawStatus
	}
	if !update.CreatedAt.IsZero() && current.CreatedAt.IsZero() {
		current.CreatedAt = update.CreatedAt
	}
	if !update.UpdatedAt.IsZero() {
		current.UpdatedAt = update.UpdatedAt
	}
	return current
}

func addDecimal(left, right string) (string, error) {
	if left == "" {
		left = "0"
	}
	l, ok := new(big.Rat).SetString(left)
	if !ok {
		return "", fmt.Errorf("invalid decimal %q", left)
	}
	r, ok := new(big.Rat).SetString(right)
	if !ok {
		return "", fmt.Errorf("invalid decimal %q", right)
	}
	places := decimalPlaces(left)
	if candidate := decimalPlaces(right); candidate > places {
		places = candidate
	}
	return new(big.Rat).Add(l, r).FloatString(places), nil
}

func compareDecimal(left, right string) int {
	l, lok := new(big.Rat).SetString(left)
	r, rok := new(big.Rat).SetString(right)
	if !lok || !rok {
		return -1
	}
	return l.Cmp(r)
}

func decimalPlaces(value string) int {
	for i := range value {
		if value[i] == '.' {
			return len(value) - i - 1
		}
	}
	return 0
}

func cloneSnapshot(source *Snapshot) Snapshot {
	copy := Snapshot{Version: source.Version, UpdatedAt: source.UpdatedAt, Orders: make(map[string]domain.Order, len(source.Orders)), Executions: make(map[string]domain.Execution, len(source.Executions))}
	for key, value := range source.Orders {
		copy.Orders[key] = value
	}
	for key, value := range source.Executions {
		copy.Executions[key] = value
	}
	return copy
}
