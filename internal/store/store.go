package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"microgrid-ops/internal/cabin"
	"microgrid-ops/internal/parts"
	"microgrid-ops/internal/workorder"
)

// Snapshot is the full persisted state of the system.
type Snapshot struct {
	Cabins       []*cabin.Cabin         `json:"cabins"`
	WorkOrders   []*workorder.WorkOrder `json:"work_orders"`
	Parts        []*parts.Part          `json:"parts"`
	Requisitions []*parts.Requisition   `json:"requisitions"`
	Timestamp    time.Time              `json:"timestamp"`
}

// Store is a file-backed JSON persistence layer.
type Store struct {
	mu   sync.Mutex
	path string
}

// New creates a store that reads and writes JSON to the given path.
func New(path string) *Store {
	return &Store{path: path}
}

// Save serialises the snapshot to disk atomically.
func (s *Store) Save(snap *Snapshot) error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if snap == nil {
		return fmt.Errorf("nil snapshot")
	}
	snap.Timestamp = time.Now()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create store dir: %w", err)
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename tmp file: %w", err)
	}
	return nil
}

// Load reads the snapshot from disk. Returns an empty snapshot if the file does not exist.
func (s *Store) Load() (*Snapshot, error) {
	if s == nil || s.path == "" {
		return &Snapshot{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Snapshot{}, nil
		}
		return nil, fmt.Errorf("read store file: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return &snap, nil
}
