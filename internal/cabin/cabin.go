package cabin

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Status represents the operational status of a battery cabin.
type Status string

const (
	StatusNormal           Status = "normal"
	StatusFrozen           Status = "frozen"
	StatusUnderInspection  Status = "under_inspection"
	StatusUnderMaintenance Status = "under_maintenance"
)

// Domain errors.
var (
	ErrNotFound      = errors.New("cabin not found")
	ErrAlreadyExists = errors.New("cabin already registered")
	ErrBusy          = errors.New("cabin is already acquired by another work order")
	ErrNotAcquired   = errors.New("cabin is not acquired by any work order")
	ErrStillFrozen   = errors.New("cabin grid connection is frozen; restoration not approved")
)

// Cabin represents an energy storage battery cabin in the microgrid.
type Cabin struct {
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	Location                string    `json:"location"`
	Status                  Status    `json:"status"`
	GridConnectionQualified bool      `json:"grid_connection_qualified"`
	BlackStartOK            bool      `json:"black_start_ok"`
	OffGridOperationOK      bool      `json:"off_grid_operation_ok"`
	Temperature             float64   `json:"temperature"`
	TemperatureLimit        float64   `json:"temperature_limit"`
	InsulationAlarm         bool      `json:"insulation_alarm"`
	CoolingFanRunning       bool      `json:"cooling_fan_running"`
	ActiveWorkOrderID       string    `json:"active_work_order_id"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// HasAnomaly reports whether the cabin has any anomaly condition:
// temperature over limit, insulation alarm, or cooling fan stopped.
func (c *Cabin) HasAnomaly() bool {
	if c.TemperatureLimit > 0 && c.Temperature > c.TemperatureLimit {
		return true
	}
	if c.InsulationAlarm {
		return true
	}
	if !c.CoolingFanRunning {
		return true
	}
	return false
}

// Snapshot returns a deep copy of the cabin.
func (c *Cabin) Snapshot() *Cabin {
	cp := *c
	return &cp
}

// Manager manages all battery cabins in the microgrid.
type Manager struct {
	mu     sync.Mutex
	cabins map[string]*Cabin
}

// NewManager creates a new cabin manager.
func NewManager() *Manager {
	return &Manager{cabins: make(map[string]*Cabin)}
}

// Register registers a new battery cabin with sensible defaults.
func (m *Manager) Register(c *Cabin) error {
	if c.ID == "" {
		return fmt.Errorf("cabin id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.cabins[c.ID]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, c.ID)
	}
	c.Status = StatusNormal
	c.GridConnectionQualified = true
	c.BlackStartOK = true
	c.OffGridOperationOK = true
	c.CoolingFanRunning = true
	c.UpdatedAt = time.Now()
	m.cabins[c.ID] = c
	return nil
}

// Get retrieves a cabin by ID (returns a snapshot copy).
func (m *Manager) Get(id string) (*Cabin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cabins[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return c.Snapshot(), nil
}

// List returns snapshot copies of all registered cabins.
func (m *Manager) List() []*Cabin {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*Cabin, 0, len(m.cabins))
	for _, c := range m.cabins {
		result = append(result, c.Snapshot())
	}
	return result
}

// Freeze immediately revokes grid-connection qualification.
func (m *Manager) Freeze(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cabins[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c.GridConnectionQualified = false
	if c.Status == StatusNormal {
		c.Status = StatusFrozen
	}
	c.UpdatedAt = time.Now()
	return nil
}

// Thaw restores grid-connection qualification.
func (m *Manager) Thaw(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cabins[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c.GridConnectionQualified = true
	if c.Status == StatusFrozen {
		c.Status = StatusNormal
	}
	c.UpdatedAt = time.Now()
	return nil
}

// Acquire attempts to acquire a cabin for a work order.
// Returns ErrBusy if already held by a different order.
func (m *Manager) Acquire(id, workOrderID string, isInspection bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cabins[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if c.ActiveWorkOrderID != "" {
		return fmt.Errorf("%w: cabin %s held by %s", ErrBusy, id, c.ActiveWorkOrderID)
	}
	c.ActiveWorkOrderID = workOrderID
	if isInspection {
		c.Status = StatusUnderInspection
	} else {
		c.Status = StatusUnderMaintenance
	}
	c.UpdatedAt = time.Now()
	return nil
}

// Release clears the active work order and resets status.
// If the cabin is still frozen (grid connection not qualified) it stays frozen.
func (m *Manager) Release(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cabins[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c.ActiveWorkOrderID = ""
	if !c.GridConnectionQualified {
		c.Status = StatusFrozen
	} else {
		c.Status = StatusNormal
	}
	c.UpdatedAt = time.Now()
	return nil
}

// SetConditions sets the physical sensor readings of the cabin.
func (m *Manager) SetConditions(id string, temp float64, insulationAlarm, fanRunning bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cabins[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c.Temperature = temp
	c.InsulationAlarm = insulationAlarm
	c.CoolingFanRunning = fanRunning
	c.UpdatedAt = time.Now()
	return nil
}

// SetTemperatureLimit configures the temperature alarm threshold.
func (m *Manager) SetTemperatureLimit(id string, limit float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cabins[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c.TemperatureLimit = limit
	c.UpdatedAt = time.Now()
	return nil
}

// RecheckBlackStart records a black-start indicator recheck result.
func (m *Manager) RecheckBlackStart(id string, ok bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok2 := m.cabins[id]
	if !ok2 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c.BlackStartOK = ok
	c.UpdatedAt = time.Now()
	return nil
}

// RecheckOffGrid records an off-grid independent operation indicator recheck result.
func (m *Manager) RecheckOffGrid(id string, ok bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok2 := m.cabins[id]
	if !ok2 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	c.OffGridOperationOK = ok
	c.UpdatedAt = time.Now()
	return nil
}

// SnapshotAll returns snapshot copies for persistence.
func (m *Manager) SnapshotAll() []*Cabin {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*Cabin, 0, len(m.cabins))
	for _, c := range m.cabins {
		result = append(result, c.Snapshot())
	}
	return result
}

// Restore replaces all cabins from a snapshot (used on startup).
func (m *Manager) Restore(cabins []*Cabin) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cabins = make(map[string]*Cabin, len(cabins))
	for _, c := range cabins {
		cp := *c
		m.cabins[c.ID] = &cp
	}
}
