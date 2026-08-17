package dispatch

import (
	"sync"
	"testing"
	"time"

	"microgrid-ops/internal/cabin"
	"microgrid-ops/internal/parts"
	"microgrid-ops/internal/workorder"
)

func newTestOrchestrator() *Orchestrator {
	return NewOrchestrator(Config{
		AnomalyReportDeadline: 10 * time.Minute,
		MaintenanceTimeout:    5 * time.Minute,
	}, nil)
}

func registerCabin(orch *Orchestrator, id string) {
	_ = orch.Cabins().Register(&cabin.Cabin{ID: id, Name: id, TemperatureLimit: 45.0})
}

// Test 1: anomaly detection freezes grid connection, report within deadline, close.
func TestInspectionAnomalyFreeze(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-1")

	wo, err := orch.CreateInspectionOrder("C-1", "insp-1", workorder.PriorityNormal)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if wo.State != workorder.StateInProgress {
		t.Fatalf("state = %s, want in_progress", wo.State)
	}

	// Detect anomaly: temperature over limit.
	wo, err = orch.DetectAnomaly(wo.ID, 50.0, false, true)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	c, _ := orch.Cabins().Get("C-1")
	if c.GridConnectionQualified {
		t.Error("cabin should be frozen (grid connection revoked)")
	}
	if wo.Anomaly == nil {
		t.Fatal("anomaly info should be set")
	}
	if wo.Anomaly.ReportDeadline.IsZero() {
		t.Error("report deadline should be set")
	}

	// Report anomaly within deadline.
	wo, err = orch.ReportAnomaly(wo.ID, "temperature exceeded 45C limit")
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if wo.State != workorder.StateAnomalyReported {
		t.Errorf("state = %s, want anomaly_reported", wo.State)
	}

	// Close order — cabin stays frozen.
	wo, _ = orch.CloseOrder(wo.ID)
	if wo.State != workorder.StateClosed {
		t.Errorf("state = %s, want closed", wo.State)
	}
	c, _ = orch.Cabins().Get("C-1")
	if c.GridConnectionQualified {
		t.Error("cabin should remain frozen after inspection close")
	}
	if c.Status != cabin.StatusFrozen {
		t.Errorf("cabin status = %s, want frozen", c.Status)
	}
}

// Test 2: same cabin cannot hold inspection and maintenance simultaneously; later order queues and auto-advances.
func TestCabinMutexQueueing(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-2")

	insp, _ := orch.CreateInspectionOrder("C-2", "insp-1", workorder.PriorityNormal)
	maint, _ := orch.CreateMaintenanceOrder("C-2", []string{"eng-1", "eng-2"}, workorder.PriorityNormal)

	// Authorize and try to enter cabin — should queue because inspection holds it.
	orch.AuthorizeMaintenance(maint.ID, "chief-1")
	maint, _ = orch.EnterCabin(maint.ID)
	if maint.State != workorder.StateQueued {
		t.Fatalf("maintenance state = %s, want queued (cabin busy)", maint.State)
	}
	if orch.QueueLength("C-2") != 1 {
		t.Errorf("queue length = %d, want 1", orch.QueueLength("C-2"))
	}

	// Close inspection — maintenance should auto-advance to executing.
	orch.CloseOrder(insp.ID)
	maint, _ = orch.Orders().Get(maint.ID)
	if maint.State != workorder.StateExecuting {
		t.Errorf("maintenance state = %s, want executing (auto-advanced)", maint.State)
	}
	c, _ := orch.Cabins().Get("C-2")
	if c.ActiveWorkOrderID != maint.ID {
		t.Errorf("cabin active = %s, want %s", c.ActiveWorkOrderID, maint.ID)
	}
}

// Test 3: when multiple orders queue, highest priority advances first.
func TestPriorityQueueAdvance(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-3")

	// First order acquires the cabin.
	woA, _ := orch.CreateInspectionOrder("C-3", "insp-A", workorder.PriorityLow)
	// Two more queue behind it with different priorities.
	woB, _ := orch.CreateInspectionOrder("C-3", "insp-B", workorder.PriorityUrgent)
	woC, _ := orch.CreateInspectionOrder("C-3", "insp-C", workorder.PriorityHigh)

	if woB.State != workorder.StateQueued || woC.State != workorder.StateQueued {
		t.Fatalf("B=%s C=%s, both should be queued", woB.State, woC.State)
	}

	// Close A — B (urgent) should advance before C (high).
	orch.CloseOrder(woA.ID)

	woB, _ = orch.Orders().Get(woB.ID)
	woC, _ = orch.Orders().Get(woC.ID)
	if woB.State != workorder.StateInProgress {
		t.Errorf("B (urgent) state = %s, want in_progress", woB.State)
	}
	if woC.State != workorder.StateQueued {
		t.Errorf("C (high) state = %s, want queued", woC.State)
	}

	// Close B — C should advance.
	orch.CloseOrder(woB.ID)
	woC, _ = orch.Orders().Get(woC.ID)
	if woC.State != workorder.StateInProgress {
		t.Errorf("C state after B close = %s, want in_progress", woC.State)
	}
}

// Test 4: full maintenance lifecycle with restoration approval and cabin thaw.
func TestMaintenanceFullFlow(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-4")
	_ = orch.Parts().RegisterPart(&parts.Part{SKU: "FAN-X", Name: "Fan", Stock: 10, MinStock: 1})

	wo, _ := orch.CreateMaintenanceOrder("C-4", []string{"eng-1", "eng-2"}, workorder.PriorityNormal)
	wo, _ = orch.AuthorizeMaintenance(wo.ID, "chief-1")
	if wo.State != workorder.StateAuthorized {
		t.Fatalf("state = %s, want authorized", wo.State)
	}

	wo, _ = orch.EnterCabin(wo.ID)
	if wo.State != workorder.StateExecuting {
		t.Fatalf("state = %s, want executing", wo.State)
	}

	wo, _ = orch.RequestParts(wo.ID, "FAN-X", 2)
	if wo.State != workorder.StatePartsRequested {
		t.Fatalf("state = %s, want parts_requested", wo.State)
	}

	wo, _ = orch.ConfirmParts(wo.ID, "mgr-1")
	if wo.State != workorder.StatePartsConfirmed {
		t.Fatalf("state = %s, want parts_confirmed", wo.State)
	}

	wo, _ = orch.CompleteRepair(wo.ID)
	if wo.State != workorder.StateRepairCompleted {
		t.Fatalf("state = %s, want repair_completed", wo.State)
	}

	wo, _ = orch.RequestRestoration(wo.ID)
	if wo.State != workorder.StatePendingRestoration {
		t.Fatalf("state = %s, want pending_restoration", wo.State)
	}

	// Approve with successful rechecks — cabin should be thawed.
	wo, _ = orch.ApproveRestoration(wo.ID, "chief-1", true, true)
	if wo.State != workorder.StateRestorationApproved {
		t.Fatalf("state = %s, want restoration_approved", wo.State)
	}
	c, _ := orch.Cabins().Get("C-4")
	if !c.GridConnectionQualified {
		t.Error("cabin should be thawed after restoration approval")
	}
	if !c.BlackStartOK || !c.OffGridOperationOK {
		t.Error("black start and off-grid should be OK")
	}

	wo, _ = orch.CloseOrder(wo.ID)
	if wo.State != workorder.StateClosed {
		t.Fatalf("state = %s, want closed", wo.State)
	}
}

// Test 5: approval interruption falls back to pending recheck, then re-submits.
func TestApprovalInterruptionRecheck(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-5")
	_ = orch.Parts().RegisterPart(&parts.Part{SKU: "P1", Name: "Part", Stock: 5})

	wo, _ := orch.CreateMaintenanceOrder("C-5", []string{"e1", "e2"}, workorder.PriorityNormal)
	orch.AuthorizeMaintenance(wo.ID, "chief-1")
	orch.EnterCabin(wo.ID)
	orch.RequestParts(wo.ID, "P1", 1)
	orch.ConfirmParts(wo.ID, "mgr-1")
	orch.CompleteRepair(wo.ID)
	orch.RequestRestoration(wo.ID)

	// Interrupt approval.
	wo, _ = orch.InterruptApproval(wo.ID)
	if wo.State != workorder.StatePendingRecheck {
		t.Fatalf("state = %s, want pending_recheck", wo.State)
	}

	// Recheck complete returns to pending restoration.
	wo, _ = orch.RecheckComplete(wo.ID)
	if wo.State != workorder.StatePendingRestoration {
		t.Fatalf("state = %s, want pending_restoration", wo.State)
	}

	// Approve successfully.
	wo, _ = orch.ApproveRestoration(wo.ID, "chief-1", true, true)
	if wo.State != workorder.StateRestorationApproved {
		t.Fatalf("state = %s, want restoration_approved", wo.State)
	}
}

// Test 6: parts failure rolls back inventory and requeues the order.
func TestPartsFailureRollbackRequeue(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-6")
	_ = orch.Parts().RegisterPart(&parts.Part{SKU: "P2", Name: "Part", Stock: 5, MinStock: 1})

	wo, _ := orch.CreateMaintenanceOrder("C-6", []string{"e1", "e2"}, workorder.PriorityNormal)
	orch.AuthorizeMaintenance(wo.ID, "chief-1")
	orch.EnterCabin(wo.ID)
	orch.RequestParts(wo.ID, "P2", 3)

	// Verify stock reserved.
	p, _ := orch.Parts().GetPart("P2")
	if p.Reserved != 3 {
		t.Fatalf("reserved = %d, want 3", p.Reserved)
	}

	// Fail parts requisition.
	wo, _ = orch.FailPartsRequisition(wo.ID)

	// Stock should be rolled back.
	p, _ = orch.Parts().GetPart("P2")
	if p.Reserved != 0 {
		t.Errorf("reserved after fail = %d, want 0", p.Reserved)
	}
	if p.Stock != 5 {
		t.Errorf("stock after fail = %d, want 5", p.Stock)
	}

	// Requisition should be rolled back.
	req, _ := orch.Parts().GetRequisition(wo.PartsRequisitionID)
	if req.Status != parts.RequisitionRolledBack {
		t.Errorf("requisition status = %s, want rolled_back", req.Status)
	}

	// Order should have been requeued and auto-advanced back to executing.
	wo, _ = orch.Orders().Get(wo.ID)
	if wo.State != workorder.StateExecuting {
		t.Errorf("order state after requeue = %s, want executing", wo.State)
	}

	// Verify notification was created.
	notifs := orch.Notifications()
	found := false
	for _, n := range notifs {
		if n.Type == NotifPartsFailure {
			found = true
		}
	}
	if !found {
		t.Error("expected parts failure notification")
	}
}

// Test 7: two concurrent inspection orders for same cabin — one wins, one queues.
func TestConcurrentInspectionOrders(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-7")

	var wg sync.WaitGroup
	var wo1, wo2 *workorder.WorkOrder
	wg.Add(2)
	go func() {
		defer wg.Done()
		wo1, _ = orch.CreateInspectionOrder("C-7", "i1", workorder.PriorityNormal)
	}()
	go func() {
		defer wg.Done()
		wo2, _ = orch.CreateInspectionOrder("C-7", "i2", workorder.PriorityHigh)
	}()
	wg.Wait()

	w1, _ := orch.Orders().Get(wo1.ID)
	w2, _ := orch.Orders().Get(wo2.ID)

	var inProg, queued string
	if w1.State == workorder.StateInProgress && w2.State == workorder.StateQueued {
		inProg, queued = w1.ID, w2.ID
	} else if w2.State == workorder.StateInProgress && w1.State == workorder.StateQueued {
		inProg, queued = w2.ID, w1.ID
	} else {
		t.Fatalf("expected one in_progress and one queued, got %s and %s", w1.State, w2.State)
	}

	// Close the in-progress order; queued one should auto-advance.
	orch.CloseOrder(inProg)
	wq, _ := orch.Orders().Get(queued)
	if wq.State != workorder.StateInProgress {
		t.Errorf("queued order state = %s, want in_progress after auto-advance", wq.State)
	}
}

// Test 8: maintenance timeout escalates to dispatch chief, then can resume.
func TestMaintenanceTimeoutEscalation(t *testing.T) {
	orch := NewOrchestrator(Config{
		AnomalyReportDeadline: 10 * time.Minute,
		MaintenanceTimeout:    50 * time.Millisecond,
	}, nil)
	registerCabin(orch, "C-8")

	wo, _ := orch.CreateMaintenanceOrder("C-8", []string{"e1", "e2"}, workorder.PriorityNormal)
	orch.AuthorizeMaintenance(wo.ID, "chief-1")

	// Wait for timeout.
	time.Sleep(100 * time.Millisecond)

	timedOut := orch.CheckMaintenanceTimeouts(time.Now())
	if len(timedOut) != 1 {
		t.Fatalf("expected 1 timeout, got %d", len(timedOut))
	}

	wo, _ = orch.Orders().Get(wo.ID)
	if wo.State != workorder.StateEscalated {
		t.Errorf("state = %s, want escalated", wo.State)
	}

	// Resume from escalation — should go back to authorized (cabin not held).
	wo, _ = orch.ResumeFromEscalation(wo.ID)
	if wo.State != workorder.StateAuthorized {
		t.Errorf("state after resume = %s, want authorized", wo.State)
	}

	// Verify escalation notification.
	notifs := orch.Notifications()
	found := false
	for _, n := range notifs {
		if n.Type == NotifEscalation {
			found = true
		}
	}
	if !found {
		t.Error("expected escalation notification")
	}
}

// Test 9: out-of-stock triggers emergency procurement, then parts can be confirmed.
func TestOutOfStockEmergencyProcurement(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-9")
	_ = orch.Parts().RegisterPart(&parts.Part{SKU: "P3", Name: "Rare Part", Stock: 1, MinStock: 1})

	wo, _ := orch.CreateMaintenanceOrder("C-9", []string{"e1", "e2"}, workorder.PriorityNormal)
	orch.AuthorizeMaintenance(wo.ID, "chief-1")
	orch.EnterCabin(wo.ID)

	// Request 5 but only 1 in stock — should suspend.
	wo, _ = orch.RequestParts(wo.ID, "P3", 5)
	if wo.State != workorder.StatePartsSuspended {
		t.Fatalf("state = %s, want parts_suspended", wo.State)
	}

	// Verify emergency procurement notification.
	notifs := orch.Notifications()
	found := false
	for _, n := range notifs {
		if n.Type == NotifEmergencyProcurement {
			found = true
		}
	}
	if !found {
		t.Error("expected emergency procurement notification")
	}

	// Emergency procurement adds stock.
	wo, _ = orch.EmergencyProcurement(wo.ID, "P3", 10)
	if wo.State != workorder.StatePartsRequested {
		t.Fatalf("state = %s, want parts_requested after emergency procurement", wo.State)
	}

	// Now manager can confirm.
	wo, _ = orch.ConfirmParts(wo.ID, "mgr-1")
	if wo.State != workorder.StatePartsConfirmed {
		t.Errorf("state = %s, want parts_confirmed", wo.State)
	}
}
