package orderstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"bybit/internal/domain"
)

const SnapshotVersion = 1

type Snapshot struct {
	Version    int                         `json:"version"`
	UpdatedAt  time.Time                   `json:"updatedAt"`
	Orders     map[string]domain.Order     `json:"orders"`
	Executions map[string]domain.Execution `json:"executions"`
}

func NewSnapshot() *Snapshot {
	return &Snapshot{
		Version:    SnapshotVersion,
		Orders:     make(map[string]domain.Order),
		Executions: make(map[string]domain.Execution),
	}
}

type StateStore interface {
	Load(context.Context) (*Snapshot, error)
	Save(context.Context, *Snapshot) error
}

type TransactionalStore interface {
	StateStore
	Update(context.Context, func(*Snapshot) error) error
}

type FileStore struct {
	Path string
}

func (s FileStore) Update(ctx context.Context, mutate func(*Snapshot) error) error {
	if mutate == nil {
		return errors.New("state mutation is nil")
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open state lock: %w", err)
	}
	defer lock.Close()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf("lock state: %w", err)
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
	return s.Save(ctx, snapshot)
}

func (s FileStore) Load(ctx context.Context) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return NewSnapshot(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state snapshot: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode state snapshot: %w", err)
	}
	if snapshot.Version != SnapshotVersion {
		return nil, fmt.Errorf("unsupported state snapshot version %d", snapshot.Version)
	}
	if snapshot.Orders == nil {
		snapshot.Orders = make(map[string]domain.Order)
	}
	if snapshot.Executions == nil {
		snapshot.Executions = make(map[string]domain.Execution)
	}
	return &snapshot, nil
}

func (s FileStore) Save(ctx context.Context, snapshot *Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if snapshot == nil {
		return errors.New("state snapshot is nil")
	}
	snapshot.Version = SnapshotVersion
	snapshot.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state snapshot: %w", err)
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return fmt.Errorf("create temporary snapshot: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		_ = temp.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary snapshot: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write temporary snapshot: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync temporary snapshot: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary snapshot: %w", err)
	}
	if err := os.Rename(tempPath, s.Path); err != nil {
		return fmt.Errorf("replace state snapshot: %w", err)
	}
	removeTemp = false
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
