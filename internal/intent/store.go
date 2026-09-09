package intent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const SnapshotVersion = 1

type Status string

const (
	StatusPlanned        Status = "Planned"
	StatusExecuting      Status = "Executing"
	StatusSubmitted      Status = "Submitted"
	StatusRejected       Status = "Rejected"
	StatusOutcomeUnknown Status = "OutcomeUnknown"
	StatusExpired        Status = "Expired"
	StatusNeedsReview    Status = "NeedsReview"
)

type Plan struct {
	ID                 string    `json:"id"`
	Venue              string    `json:"venue"`
	Environment        string    `json:"environment"`
	AccountAlias       string    `json:"accountAlias"`
	Instrument         string    `json:"instrument"`
	SettlementCurrency string    `json:"settlementCurrency"`
	Side               string    `json:"side"`
	OrderType          string    `json:"orderType"`
	Transport          string    `json:"transport"`
	Amount             string    `json:"amount"`
	AmountUnit         string    `json:"amountUnit"`
	Price              string    `json:"price,omitempty"`
	TimeInForce        string    `json:"timeInForce,omitempty"`
	PostOnly           bool      `json:"postOnly,omitempty"`
	ReduceOnly         bool      `json:"reduceOnly,omitempty"`
	MarkPrice          string    `json:"markPrice"`
	OpenOrderCount     int       `json:"openOrderCount"`
	AggregateOpenUSD   string    `json:"aggregateOpenUsd"`
	CreatedAt          time.Time `json:"createdAt"`
	ExpiresAt          time.Time `json:"expiresAt"`
	Status             Status    `json:"status"`
	Attempt            int       `json:"attempt"`
	NativeOrderID      string    `json:"nativeOrderId,omitempty"`
	LastError          string    `json:"lastError,omitempty"`
}

type Snapshot struct {
	Version   int                    `json:"version"`
	UpdatedAt time.Time              `json:"updatedAt"`
	Plans     map[string]Plan        `json:"plans"`
	Cursors   map[string]TradeCursor `json:"cursors,omitempty"`
}

type TradeCursor struct {
	Timestamp int64  `json:"timestamp"`
	TradeID   string `json:"tradeId"`
}
type Store struct{ Path string }

func (s Store) Get(ctx context.Context, id string) (Plan, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return Plan{}, err
	}
	plan, ok := snapshot.Plans[id]
	if !ok {
		return Plan{}, errors.New("plan not found")
	}
	return plan, nil
}

func (s Store) List(ctx context.Context) ([]Plan, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Plan, 0, len(snapshot.Plans))
	for _, plan := range snapshot.Plans {
		result = append(result, plan)
	}
	return result, nil
}

func (s Store) Load(ctx context.Context) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return &Snapshot{Version: SnapshotVersion, Plans: map[string]Plan{}, Cursors: map[string]TradeCursor{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode intent snapshot: %w", err)
	}
	if snapshot.Version != SnapshotVersion {
		return nil, fmt.Errorf("unsupported intent snapshot version %d", snapshot.Version)
	}
	if snapshot.Plans == nil {
		snapshot.Plans = map[string]Plan{}
	}
	if snapshot.Cursors == nil {
		snapshot.Cursors = map[string]TradeCursor{}
	}
	return &snapshot, nil
}

func (s Store) Cursor(ctx context.Context, key string) (TradeCursor, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return TradeCursor{}, err
	}
	return snapshot.Cursors[key], nil
}
func (s Store) SetCursor(ctx context.Context, key string, cursor TradeCursor) error {
	return s.update(ctx, func(snapshot *Snapshot) error {
		current := snapshot.Cursors[key]
		if cursor.Timestamp < current.Timestamp || (cursor.Timestamp == current.Timestamp && cursor.TradeID < current.TradeID) {
			return errors.New("cursor cannot move backwards")
		}
		snapshot.Cursors[key] = cursor
		return nil
	})
}

func (s Store) SavePlan(ctx context.Context, plan Plan) error {
	return s.update(ctx, func(snapshot *Snapshot) error {
		if _, exists := snapshot.Plans[plan.ID]; exists {
			return errors.New("intent ID already exists")
		}
		snapshot.Plans[plan.ID] = plan
		return nil
	})
}

func (s Store) Claim(ctx context.Context, id string, now time.Time) (Plan, error) {
	var claimed Plan
	expired := false
	err := s.update(ctx, func(snapshot *Snapshot) error {
		plan, ok := snapshot.Plans[id]
		if !ok {
			return errors.New("plan not found")
		}
		if plan.Status != StatusPlanned {
			return fmt.Errorf("plan is not executable: %s", plan.Status)
		}
		if !now.Before(plan.ExpiresAt) {
			plan.Status = StatusExpired
			snapshot.Plans[id] = plan
			expired = true
			return nil
		}
		plan.Status = StatusExecuting
		plan.Attempt++
		snapshot.Plans[id] = plan
		claimed = plan
		return nil
	})
	if err == nil && expired {
		return Plan{}, errors.New("plan expired")
	}
	return claimed, err
}

func (s Store) Finish(ctx context.Context, id string, status Status, orderID, lastError string) error {
	return s.update(ctx, func(snapshot *Snapshot) error {
		plan, ok := snapshot.Plans[id]
		if !ok {
			return errors.New("plan not found")
		}
		plan.Status = status
		plan.NativeOrderID = orderID
		plan.LastError = lastError
		snapshot.Plans[id] = plan
		return nil
	})
}

func (s Store) update(ctx context.Context, mutate func(*Snapshot) error) error {
	if s.Path == "" {
		return errors.New("intent state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	snapshot, err := s.Load(ctx)
	if err != nil {
		return err
	}
	if err := mutate(snapshot); err != nil {
		return err
	}
	snapshot.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temp, err := os.CreateTemp(filepath.Dir(s.Path), ".intents-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0o600); err == nil {
		_, err = temp.Write(raw)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, s.Path)
}
