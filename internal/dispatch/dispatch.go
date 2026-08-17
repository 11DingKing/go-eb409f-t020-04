package dispatch

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"microgrid-ops/internal/cabin"
	"microgrid-ops/internal/parts"
	"microgrid-ops/internal/store"
	"microgrid-ops/internal/workorder"
)

// NotificationType categorises system notifications.
type NotificationType string

const (
	NotifEmergencyProcurement NotificationType = "emergency_procurement"
	NotifEscalation           NotificationType = "escalation"
	NotifAnomalyDeadline      NotificationType = "anomaly_deadline_expired"
	NotifAnomalyLateReport    NotificationType = "anomaly_late_report"
	NotifQueueAdvance         NotificationType = "queue_advance"
	NotifPartsFailure         NotificationType = "parts_failure_requeue"
)

// Notification is a system event recorded for operators.
type Notification struct {
	ID          string           `json:"id"`
	Type        NotificationType `json:"type"`
	Message     string           `json:"message"`
	WorkOrderID string           `json:"work_order_id"`
	CabinID     string           `json:"cabin_id"`
	CreatedAt   time.Time        `json:"created_at"`
	Resolved    bool             `json:"resolved"`
}

// Config holds tunable timing parameters.
type Config struct {
	AnomalyReportDeadline time.Duration
	MaintenanceTimeout    time.Duration
}

// Orchestrator coordinates cabins, work orders, parts, and the per-cabin
// priority queue to enforce all business rules.
type Orchestrator struct {
	mu     sync.Mutex
	cabins *cabin.Manager
	orders *workorder.Manager
	parts  *parts.Inventory
	store  *store.Store
	config Config

	// queues maps cabinID -> ordered list of waiting work-order IDs.
	queues map[string][]string

	notifs   []*Notification
	notifSeq int64
}

// NewOrchestrator creates a new orchestrator with the given config and store.
func NewOrchestrator(cfg Config, st *store.Store) *Orchestrator {
	return &Orchestrator{
		cabins: cabin.NewManager(),
		orders: workorder.NewManager(),
		parts:  parts.NewInventory(),
		store:  st,
		config: cfg,
		queues: make(map[string][]string),
		notifs: make([]*Notification, 0),
	}
}

// Cabins returns the cabin manager (for registration and queries).
func (o *Orchestrator) Cabins() *cabin.Manager { return o.cabins }

// Orders returns the work order manager.
func (o *Orchestrator) Orders() *workorder.Manager { return o.orders }

// Parts returns the parts inventory.
func (o *Orchestrator) Parts() *parts.Inventory { return o.parts }

// Notifications returns a copy of all notifications.
func (o *Orchestrator) Notifications() []*Notification {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]*Notification, len(o.notifs))
	copy(out, o.notifs)
	return out
}

func (o *Orchestrator) addNotification(typ NotificationType, msg, woID, cabinID string) *Notification {
	o.notifSeq++
	n := &Notification{
		ID:          fmt.Sprintf("NOTIF-%d", o.notifSeq),
		Type:        typ,
		Message:     msg,
		WorkOrderID: woID,
		CabinID:     cabinID,
		CreatedAt:   time.Now(),
	}
	o.notifs = append(o.notifs, n)
	return n
}

func (o *Orchestrator) persist() {
	if o.store == nil {
		return
	}
	snap := &store.Snapshot{
		Cabins:       o.cabins.SnapshotAll(),
		WorkOrders:   o.orders.SnapshotAll(),
		Parts:        o.parts.SnapshotParts(),
		Requisitions: o.parts.SnapshotRequisitions(),
	}
	_ = o.store.Save(snap)
}

// Restore loads state from the store into all managers.
func (o *Orchestrator) Restore() error {
	if o.store == nil {
		return nil
	}
	snap, err := o.store.Load()
	if err != nil {
		return err
	}
	o.cabins.Restore(snap.Cabins)
	o.orders.Restore(snap.WorkOrders)
	o.parts.Restore(snap.Parts, snap.Requisitions)
	return nil
}

// ---------------------------------------------------------------------------
// Queue helpers
// ---------------------------------------------------------------------------

// enqueue inserts a work order ID into the cabin's priority queue.
// The queue is sorted by priority (descending), then by creation time (ascending).
func (o *Orchestrator) enqueue(cabinID, woID string) {
	q := o.queues[cabinID]
	q = append(q, woID)
	wo, _ := o.orders.Get(woID)
	// Sort by priority desc, then creation time asc.
	sort.SliceStable(q, func(i, j int) bool {
		wi, _ := o.orders.Get(q[i])
		wj, _ := o.orders.Get(q[j])
		if wi.Priority != wj.Priority {
			return wi.Priority > wj.Priority
		}
		return wi.CreatedAt.Before(wj.CreatedAt)
	})
	o.queues[cabinID] = q
	_ = wo
}

// assignFromQueue tries to assign the cabin to the highest-priority waiting order.
func (o *Orchestrator) assignFromQueue(cabinID string) {
	q := o.queues[cabinID]
	for len(q) > 0 {
		woID := q[0]
		q = q[1:]
		o.queues[cabinID] = q

		wo, err := o.orders.Get(woID)
		if err != nil || !wo.State.IsActive() {
			continue // stale entry
		}
		if wo.State != workorder.StateQueued {
			continue
		}

		isInspection := wo.Type == workorder.TypeInspection
		if err := o.cabins.Acquire(cabinID, woID, isInspection); err != nil {
			// Cabin still busy — put it back at front and stop.
			o.queues[cabinID] = append([]string{woID}, o.queues[cabinID]...)
			return
		}

		if isInspection {
			_ = o.orders.Transition(woID, workorder.StateInProgress)
		} else {
			_ = o.orders.Transition(woID, workorder.StateExecuting)
		}
		o.addNotification(NotifQueueAdvance,
			fmt.Sprintf("work order %s advanced from queue for cabin %s", woID, cabinID),
			woID, cabinID)
		return
	}
	o.queues[cabinID] = q
}

// ---------------------------------------------------------------------------
// Inspection operations
// ---------------------------------------------------------------------------

// CreateInspectionOrder creates an inspection order and attempts to acquire the cabin.
// If the cabin is busy the order is queued.
func (o *Orchestrator) CreateInspectionOrder(cabinID, inspectorID string, priority workorder.Priority) (*workorder.WorkOrder, error) {
	wo, err := o.orders.Create(workorder.TypeInspection, cabinID, priority, []string{inspectorID})
	if err != nil {
		return nil, err
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.cabins.Acquire(cabinID, wo.ID, true); err != nil {
		o.enqueue(cabinID, wo.ID)
		_ = o.orders.Transition(wo.ID, workorder.StateQueued)
	} else {
		_ = o.orders.Transition(wo.ID, workorder.StateInProgress)
	}
	o.persist()
	return o.orders.Get(wo.ID)
}

// DetectAnomaly records sensor conditions, freezes grid connection, and starts the
// 10-minute anomaly reporting deadline. The work order must be in the in_progress state.
func (o *Orchestrator) DetectAnomaly(orderID string, temp float64, insulationAlarm, fanRunning bool) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StateInProgress {
		return nil, fmt.Errorf("anomaly can only be detected during in-progress inspection (current: %s)", wo.State)
	}
	if err := o.cabins.SetConditions(wo.CabinID, temp, insulationAlarm, fanRunning); err != nil {
		return nil, err
	}

	c, _ := o.cabins.Get(wo.CabinID)
	if c == nil || !c.HasAnomaly() {
		return nil, fmt.Errorf("no anomaly condition detected for cabin %s", wo.CabinID)
	}

	// Immediately freeze grid-connection qualification.
	if err := o.cabins.Freeze(wo.CabinID); err != nil {
		return nil, err
	}

	now := time.Now()
	deadline := o.config.AnomalyReportDeadline
	if deadline <= 0 {
		deadline = 10 * time.Minute
	}
	_ = o.orders.Update(orderID, func(w *workorder.WorkOrder) {
		w.Anomaly = &workorder.AnomalyInfo{
			DetectedAt:     now,
			ReportDeadline: now.Add(deadline),
			CabinFrozen:    true,
		}
	})
	o.persist()
	return o.orders.Get(orderID)
}

// ReportAnomaly submits the formal anomaly report. Must be within the 10-minute deadline.
func (o *Orchestrator) ReportAnomaly(orderID, description string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StateInProgress {
		return nil, fmt.Errorf("can only report anomaly for in-progress inspection (current: %s)", wo.State)
	}
	if wo.Anomaly == nil {
		return nil, fmt.Errorf("no anomaly detected for order %s; call detect-anomaly first", orderID)
	}

	now := time.Now()
	late := now.After(wo.Anomaly.ReportDeadline)
	if late {
		o.addNotification(NotifAnomalyLateReport,
			fmt.Sprintf("anomaly report for order %s submitted after deadline", orderID),
			orderID, wo.CabinID)
	}

	_ = o.orders.Update(orderID, func(w *workorder.WorkOrder) {
		if w.Anomaly != nil {
			w.Anomaly.ReportedAt = now
			w.Anomaly.Description = description
		}
	})
	_ = o.orders.Transition(orderID, workorder.StateAnomalyReported)
	o.persist()
	return o.orders.Get(orderID)
}

// ---------------------------------------------------------------------------
// Maintenance operations
// ---------------------------------------------------------------------------

// CreateMaintenanceOrder creates a maintenance order in the created state.
// Two engineer IDs are required (dual-person rule). The order must be authorized
// before the engineers can enter the cabin.
func (o *Orchestrator) CreateMaintenanceOrder(cabinID string, engineerIDs []string, priority workorder.Priority) (*workorder.WorkOrder, error) {
	wo, err := o.orders.Create(workorder.TypeMaintenance, cabinID, priority, engineerIDs)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.persist()
	return o.orders.Get(wo.ID)
}

// AuthorizeMaintenance records the dispatch chief's job authorization and sets
// the maintenance response timeout.
func (o *Orchestrator) AuthorizeMaintenance(orderID, chiefID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.Type != workorder.TypeMaintenance {
		return nil, fmt.Errorf("order %s is not a maintenance order", orderID)
	}
	if !wo.IsDualPerson() {
		return nil, fmt.Errorf("order %s does not have two engineers assigned", orderID)
	}

	timeout := o.config.MaintenanceTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	_ = o.orders.Update(orderID, func(w *workorder.WorkOrder) {
		w.AuthorizedChiefID = chiefID
		w.TimeoutAt = time.Now().Add(timeout)
	})
	if err := o.orders.Transition(orderID, workorder.StateAuthorized); err != nil {
		return nil, err
	}
	o.persist()
	return o.orders.Get(orderID)
}

// EnterCabin attempts to acquire the cabin for the maintenance engineers.
// If the cabin is busy the order is queued and will auto-advance when free.
func (o *Orchestrator) EnterCabin(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StateAuthorized && wo.State != workorder.StateEscalated {
		return nil, fmt.Errorf("order %s must be authorized or resumed before entering (current: %s)", orderID, wo.State)
	}
	if !wo.IsDualPerson() {
		return nil, fmt.Errorf("dual-person badge check failed for order %s", orderID)
	}

	if err := o.cabins.Acquire(wo.CabinID, orderID, false); err != nil {
		o.enqueue(wo.CabinID, orderID)
		_ = o.orders.Transition(orderID, workorder.StateQueued)
		o.persist()
		return o.orders.Get(orderID)
	}

	_ = o.orders.Transition(orderID, workorder.StateExecuting)
	o.persist()
	return o.orders.Get(orderID)
}

// RequestParts submits a spare-parts requisition signed by the engineer.
// If stock is insufficient the order is suspended and emergency procurement is triggered.
func (o *Orchestrator) RequestParts(orderID, sku string, qty int) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StateExecuting {
		return nil, fmt.Errorf("parts can only be requested during execution (current: %s)", wo.State)
	}
	engineerID := ""
	if len(wo.EngineerIDs) > 0 {
		engineerID = wo.EngineerIDs[0]
	}
	req, err := o.parts.SubmitRequisition(orderID, sku, qty, engineerID)
	if err != nil {
		return nil, err
	}
	_ = o.orders.Update(orderID, func(w *workorder.WorkOrder) {
		w.PartsRequisitionID = req.ID
	})

	// Always move to parts_requested first.
	_ = o.orders.Transition(orderID, workorder.StatePartsRequested)
	// If out of stock, further transition to parts_suspended.
	if req.Status == parts.RequisitionOutOfStock {
		_ = o.orders.Transition(orderID, workorder.StatePartsSuspended)
		o.addNotification(NotifEmergencyProcurement,
			fmt.Sprintf("part %s out of stock for order %s; emergency procurement initiated", sku, orderID),
			orderID, wo.CabinID)
	}
	o.persist()
	return o.orders.Get(orderID)
}

// ConfirmParts applies the warehouse manager's signature (dual-sign).
func (o *Orchestrator) ConfirmParts(orderID, managerID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StatePartsRequested && wo.State != workorder.StatePartsSuspended {
		return nil, fmt.Errorf("order %s is not awaiting parts confirmation (current: %s)", orderID, wo.State)
	}
	if wo.PartsRequisitionID == "" {
		return nil, fmt.Errorf("order %s has no parts requisition", orderID)
	}
	if _, err := o.parts.ConfirmRequisition(wo.PartsRequisitionID, managerID); err != nil {
		return nil, err
	}
	_ = o.orders.Transition(orderID, workorder.StatePartsConfirmed)
	o.persist()
	return o.orders.Get(orderID)
}

// EmergencyProcurement adds stock and re-checks any suspended requisition for the order.
func (o *Orchestrator) EmergencyProcurement(orderID, sku string, qty int) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if err := o.parts.EmergencyProcurement(sku, qty); err != nil {
		return nil, err
	}
	if wo.PartsRequisitionID != "" {
		if _, err := o.parts.RecheckRequisition(wo.PartsRequisitionID); err != nil {
			return nil, err
		}
	}
	// If the order was suspended, move it back to parts_requested so the manager can sign.
	if wo.State == workorder.StatePartsSuspended {
		_ = o.orders.Transition(orderID, workorder.StatePartsRequested)
	}
	o.persist()
	return o.orders.Get(orderID)
}

// CompleteRepair marks the repair as finished.
func (o *Orchestrator) CompleteRepair(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StatePartsConfirmed {
		return nil, fmt.Errorf("can only complete repair after parts confirmed (current: %s)", wo.State)
	}
	if err := o.orders.Transition(orderID, workorder.StateRepairCompleted); err != nil {
		return nil, err
	}
	o.persist()
	return o.orders.Get(orderID)
}

// RequestRestoration moves the order to pending restoration approval.
func (o *Orchestrator) RequestRestoration(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StateRepairCompleted {
		return nil, fmt.Errorf("can only request restoration after repair completed (current: %s)", wo.State)
	}
	if err := o.orders.Transition(orderID, workorder.StatePendingRestoration); err != nil {
		return nil, err
	}
	o.persist()
	return o.orders.Get(orderID)
}

// ApproveRestoration performs mandatory black-start and off-grid rechecks,
// then thaws the cabin's grid connection and approves restoration.
func (o *Orchestrator) ApproveRestoration(orderID, chiefID string, blackStartOK, offGridOK bool) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StatePendingRestoration {
		return nil, fmt.Errorf("can only approve restoration from pending_restoration (current: %s)", wo.State)
	}

	// Mandatory rechecks before grid reconnection.
	if err := o.cabins.RecheckBlackStart(wo.CabinID, blackStartOK); err != nil {
		return nil, err
	}
	if err := o.cabins.RecheckOffGrid(wo.CabinID, offGridOK); err != nil {
		return nil, err
	}
	if !blackStartOK || !offGridOK {
		// Approval interrupted: fall back to pending recheck.
		_ = o.orders.Update(orderID, func(w *workorder.WorkOrder) {
			w.ChiefID = chiefID
		})
		_ = o.orders.Transition(orderID, workorder.StatePendingRecheck)
		o.persist()
		return o.orders.Get(orderID)
	}

	// All checks pass: thaw grid connection and approve.
	if err := o.cabins.Thaw(wo.CabinID); err != nil {
		return nil, err
	}
	_ = o.orders.Update(orderID, func(w *workorder.WorkOrder) {
		w.ChiefID = chiefID
	})
	if err := o.orders.Transition(orderID, workorder.StateRestorationApproved); err != nil {
		return nil, err
	}
	o.persist()
	return o.orders.Get(orderID)
}

// InterruptApproval simulates an approval interruption, falling back to pending recheck.
func (o *Orchestrator) InterruptApproval(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StatePendingRestoration {
		return nil, fmt.Errorf("can only interrupt from pending_restoration (current: %s)", wo.State)
	}
	if err := o.orders.Transition(orderID, workorder.StatePendingRecheck); err != nil {
		return nil, err
	}
	o.persist()
	return o.orders.Get(orderID)
}

// RecheckComplete moves the order from pending_recheck back to pending_restoration
// after the mandatory rechecks have been re-performed.
func (o *Orchestrator) RecheckComplete(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StatePendingRecheck {
		return nil, fmt.Errorf("can only recheck from pending_recheck (current: %s)", wo.State)
	}
	if err := o.orders.Transition(orderID, workorder.StatePendingRestoration); err != nil {
		return nil, err
	}
	o.persist()
	return o.orders.Get(orderID)
}

// ---------------------------------------------------------------------------
// Failure recovery
// ---------------------------------------------------------------------------

// FailPartsRequisition rolls back inventory and requeues the work order,
// releasing the cabin for other orders.
func (o *Orchestrator) FailPartsRequisition(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StatePartsRequested && wo.State != workorder.StatePartsSuspended {
		return nil, fmt.Errorf("can only fail parts from parts_requested or parts_suspended (current: %s)", wo.State)
	}

	// Roll back reserved stock.
	if wo.PartsRequisitionID != "" {
		_ = o.parts.RollbackRequisition(wo.PartsRequisitionID)
	}

	// Release the cabin so other orders can proceed.
	_ = o.cabins.Release(wo.CabinID)

	// Requeue the order.
	_ = o.orders.Transition(orderID, workorder.StateQueued)
	o.enqueue(wo.CabinID, orderID)

	o.addNotification(NotifPartsFailure,
		fmt.Sprintf("parts requisition failed for order %s; inventory rolled back, order requeued", orderID),
		orderID, wo.CabinID)

	// Try to immediately reassign the cabin (this order or a higher-priority one).
	o.assignFromQueue(wo.CabinID)

	o.persist()
	return o.orders.Get(orderID)
}

// EscalateOrder moves a maintenance order to the escalated state (timeout).
func (o *Orchestrator) EscalateOrder(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StateAuthorized && wo.State != workorder.StateExecuting {
		return nil, fmt.Errorf("can only escalate authorized or executing orders (current: %s)", wo.State)
	}
	if err := o.orders.Transition(orderID, workorder.StateEscalated); err != nil {
		return nil, err
	}
	o.addNotification(NotifEscalation,
		fmt.Sprintf("maintenance order %s escalated to dispatch chief (timeout)", orderID),
		orderID, wo.CabinID)
	o.persist()
	return o.orders.Get(orderID)
}

// ResumeFromEscalation returns an escalated order to executing (chief intervention).
func (o *Orchestrator) ResumeFromEscalation(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State != workorder.StateEscalated {
		return nil, fmt.Errorf("can only resume from escalated (current: %s)", wo.State)
	}
	// If the order was escalated while authorized (before entering cabin),
	// go back to authorized; otherwise back to executing.
	target := workorder.StateExecuting
	c, _ := o.cabins.Get(wo.CabinID)
	if c != nil && c.ActiveWorkOrderID != orderID {
		target = workorder.StateAuthorized
		_ = o.orders.Update(orderID, func(w *workorder.WorkOrder) {
			w.TimeoutAt = time.Now().Add(o.config.MaintenanceTimeout)
		})
	}
	_ = o.orders.Transition(orderID, target)
	o.persist()
	return o.orders.Get(orderID)
}

// ---------------------------------------------------------------------------
// Close and queue processing
// ---------------------------------------------------------------------------

// CloseOrder closes a work order, releases the cabin, and auto-advances the queue.
func (o *Orchestrator) CloseOrder(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State.IsTerminal() {
		return wo, nil // idempotent
	}
	if err := o.orders.Transition(orderID, workorder.StateClosed); err != nil {
		return nil, err
	}

	// Release the cabin if this order held it.
	c, _ := o.cabins.Get(wo.CabinID)
	if c != nil && c.ActiveWorkOrderID == orderID {
		_ = o.cabins.Release(wo.CabinID)
	}

	// Auto-advance the next queued order.
	o.assignFromQueue(wo.CabinID)

	o.persist()
	return o.orders.Get(orderID)
}

// CancelOrder cancels a work order, releases the cabin, and advances the queue.
func (o *Orchestrator) CancelOrder(orderID string) (*workorder.WorkOrder, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	wo, err := o.orders.Get(orderID)
	if err != nil {
		return nil, err
	}
	if wo.State.IsTerminal() {
		return wo, nil
	}
	if err := o.orders.Transition(orderID, workorder.StateCancelled); err != nil {
		return nil, err
	}

	c, _ := o.cabins.Get(wo.CabinID)
	if c != nil && c.ActiveWorkOrderID == orderID {
		_ = o.cabins.Release(wo.CabinID)
	}
	o.assignFromQueue(wo.CabinID)
	o.persist()
	return o.orders.Get(orderID)
}

// QueueLength returns the number of waiting orders for a cabin.
func (o *Orchestrator) QueueLength(cabinID string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.queues[cabinID])
}

// CheckAnomalyDeadlines inspects all in-progress inspection orders for expired
// anomaly reporting deadlines and records a notification for each.
func (o *Orchestrator) CheckAnomalyDeadlines(now time.Time) []string {
	o.mu.Lock()
	defer o.mu.Unlock()

	var expired []string
	for _, wo := range o.orders.List() {
		if wo.State != workorder.StateInProgress || wo.Anomaly == nil {
			continue
		}
		if wo.Anomaly.IsExpired(now) {
			expired = append(expired, wo.ID)
			o.addNotification(NotifAnomalyDeadline,
				fmt.Sprintf("anomaly report deadline expired for order %s", wo.ID),
				wo.ID, wo.CabinID)
		}
	}
	if len(expired) > 0 {
		o.persist()
	}
	return expired
}

// CheckMaintenanceTimeouts inspects authorized/queued maintenance orders for
// expired response timeouts and escalates them.
func (o *Orchestrator) CheckMaintenanceTimeouts(now time.Time) []string {
	o.mu.Lock()
	defer o.mu.Unlock()

	var timedOut []string
	for _, wo := range o.orders.List() {
		if wo.Type != workorder.TypeMaintenance {
			continue
		}
		if wo.State != workorder.StateAuthorized {
			continue
		}
		if !wo.TimeoutAt.IsZero() && now.After(wo.TimeoutAt) {
			timedOut = append(timedOut, wo.ID)
			_ = o.orders.Transition(wo.ID, workorder.StateEscalated)
			o.addNotification(NotifEscalation,
				fmt.Sprintf("maintenance order %s escalated to dispatch chief (timeout)", wo.ID),
				wo.ID, wo.CabinID)
		}
	}
	if len(timedOut) > 0 {
		o.persist()
	}
	return timedOut
}
