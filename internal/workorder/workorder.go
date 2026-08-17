package workorder

import (
	"fmt"
	"sync"
	"time"
)

// AnomalyInfo tracks the 10-minute anomaly reporting deadline.
type AnomalyInfo struct {
	DetectedAt     time.Time `json:"detected_at"`
	ReportDeadline time.Time `json:"report_deadline"`
	ReportedAt     time.Time `json:"reported_at"`
	Description    string    `json:"description"`
	CabinFrozen    bool      `json:"cabin_frozen"`
}

// IsReported reports whether the anomaly has been formally reported.
func (a *AnomalyInfo) IsReported() bool {
	return !a.ReportedAt.IsZero()
}

// IsExpired reports whether the reporting deadline has passed without a report.
func (a *AnomalyInfo) IsExpired(now time.Time) bool {
	return !a.IsReported() && now.After(a.ReportDeadline)
}

// WorkOrder is the core domain entity for inspection and maintenance work.
type WorkOrder struct {
	ID                 string       `json:"id"`
	CabinID            string       `json:"cabin_id"`
	Type               Type         `json:"type"`
	Priority           Priority     `json:"priority"`
	State              State        `json:"state"`
	InspectorID        string       `json:"inspector_id"`
	EngineerIDs        []string     `json:"engineer_ids"`
	ChiefID            string       `json:"chief_id"`
	AuthorizedChiefID  string       `json:"authorized_chief_id"`
	Anomaly            *AnomalyInfo `json:"anomaly"`
	PartsRequisitionID string       `json:"parts_requisition_id"`
	TimeoutAt          time.Time    `json:"timeout_at"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
	ClosedAt           time.Time    `json:"closed_at"`
}

// IsDualPerson returns true if the maintenance order has two engineers assigned.
func (w *WorkOrder) IsDualPerson() bool {
	return w.Type == TypeMaintenance && len(w.EngineerIDs) >= 2
}

// Snapshot returns a deep copy for safe external use.
func (w *WorkOrder) Snapshot() *WorkOrder {
	cp := *w
	if w.EngineerIDs != nil {
		cp.EngineerIDs = make([]string, len(w.EngineerIDs))
		copy(cp.EngineerIDs, w.EngineerIDs)
	}
	if w.Anomaly != nil {
		acp := *w.Anomaly
		cp.Anomaly = &acp
	}
	return &cp
}

// Manager stores and transitions work orders.
type Manager struct {
	mu     sync.Mutex
	orders map[string]*WorkOrder
	seq    int64
}

// NewManager creates a new work order manager.
func NewManager() *Manager {
	return &Manager{orders: make(map[string]*WorkOrder)}
}

// nextID generates a monotonic work order ID.
func (m *Manager) nextID() string {
	m.seq++
	return fmt.Sprintf("WO-%d", m.seq)
}

// Create creates a new work order with validation.
// For maintenance, assigneeIDs must contain at least two engineer IDs (dual-person rule).
func (m *Manager) Create(typ Type, cabinID string, priority Priority, assigneeIDs []string) (*WorkOrder, error) {
	if cabinID == "" {
		return nil, fmt.Errorf("cabin id is required")
	}
	if priority < PriorityLow || priority > PriorityUrgent {
		return nil, fmt.Errorf("invalid priority: %d", priority)
	}
	switch typ {
	case TypeInspection:
		if len(assigneeIDs) < 1 || assigneeIDs[0] == "" {
			return nil, fmt.Errorf("inspector id is required for inspection orders")
		}
	case TypeMaintenance:
		if len(assigneeIDs) < 2 || assigneeIDs[0] == "" || assigneeIDs[1] == "" {
			return nil, fmt.Errorf("maintenance requires two engineers for dual-person operation")
		}
	default:
		return nil, fmt.Errorf("unknown work order type: %s", typ)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextID()
	now := time.Now()
	wo := &WorkOrder{
		ID:        id,
		CabinID:   cabinID,
		Type:      typ,
		Priority:  priority,
		State:     StateCreated,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if typ == TypeInspection {
		wo.InspectorID = assigneeIDs[0]
	} else {
		wo.EngineerIDs = append(wo.EngineerIDs, assigneeIDs...)
	}
	m.orders[id] = wo
	return wo.Snapshot(), nil
}

// Get retrieves a work order by ID (returns a snapshot copy).
func (m *Manager) Get(id string) (*WorkOrder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wo, ok := m.orders[id]
	if !ok {
		return nil, fmt.Errorf("work order %s not found", id)
	}
	return wo.Snapshot(), nil
}

// Transition moves a work order to a new state.
// Transitioning to the current state is an idempotent no-op.
func (m *Manager) Transition(id string, to State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	wo, ok := m.orders[id]
	if !ok {
		return fmt.Errorf("work order %s not found", id)
	}
	if wo.State == to {
		return nil // idempotent
	}
	if err := ValidateTransition(wo.State, to); err != nil {
		return err
	}
	wo.State = to
	wo.UpdatedAt = time.Now()
	if to.IsTerminal() {
		wo.ClosedAt = wo.UpdatedAt
	}
	return nil
}

// Update mutates a work order under the manager lock.
func (m *Manager) Update(id string, fn func(*WorkOrder)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	wo, ok := m.orders[id]
	if !ok {
		return fmt.Errorf("work order %s not found", id)
	}
	fn(wo)
	wo.UpdatedAt = time.Now()
	return nil
}

// List returns snapshot copies of all work orders.
func (m *Manager) List() []*WorkOrder {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*WorkOrder, 0, len(m.orders))
	for _, wo := range m.orders {
		result = append(result, wo.Snapshot())
	}
	return result
}

// SnapshotAll returns snapshot copies for persistence.
func (m *Manager) SnapshotAll() []*WorkOrder {
	return m.List()
}

// Restore replaces all work orders from a snapshot (used on startup).
func (m *Manager) Restore(orders []*WorkOrder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders = make(map[string]*WorkOrder, len(orders))
	maxSeq := int64(0)
	for _, wo := range orders {
		cp := *wo
		m.orders[wo.ID] = &cp
		// Restore sequence counter from numeric suffix of IDs like "WO-42".
		var n int64
		if _, err := fmt.Sscanf(wo.ID, "WO-%d", &n); err == nil && n > maxSeq {
			maxSeq = n
		}
	}
	m.seq = maxSeq
}
