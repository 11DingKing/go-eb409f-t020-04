package workorder

import (
	"testing"
)

func TestInspectionStateTransitions(t *testing.T) {
	m := NewManager()
	wo, err := m.Create(TypeInspection, "C-1", PriorityNormal, []string{"insp-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if wo.State != StateCreated {
		t.Errorf("state = %s, want %s", wo.State, StateCreated)
	}

	// created -> in_progress
	if err := m.Transition(wo.ID, StateInProgress); err != nil {
		t.Fatalf("transition to in_progress: %v", err)
	}

	// in_progress -> anomaly_reported
	if err := m.Transition(wo.ID, StateAnomalyReported); err != nil {
		t.Fatalf("transition to anomaly_reported: %v", err)
	}

	// anomaly_reported -> closed
	if err := m.Transition(wo.ID, StateClosed); err != nil {
		t.Fatalf("transition to closed: %v", err)
	}

	wo, _ = m.Get(wo.ID)
	if wo.State != StateClosed {
		t.Errorf("state = %s, want %s", wo.State, StateClosed)
	}
	if wo.ClosedAt.IsZero() {
		t.Error("closed_at should be set")
	}
}

func TestMaintenanceStateTransitions(t *testing.T) {
	m := NewManager()
	wo, err := m.Create(TypeMaintenance, "C-2", PriorityHigh, []string{"eng-1", "eng-2"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !wo.IsDualPerson() {
		t.Error("maintenance order should be dual-person")
	}

	steps := []State{
		StateAuthorized,
		StateExecuting,
		StatePartsRequested,
		StatePartsConfirmed,
		StateRepairCompleted,
		StatePendingRestoration,
		StateRestorationApproved,
		StateClosed,
	}
	for _, target := range steps {
		if err := m.Transition(wo.ID, target); err != nil {
			t.Fatalf("transition to %s: %v", target, err)
		}
	}
	wo, _ = m.Get(wo.ID)
	if wo.State != StateClosed {
		t.Errorf("final state = %s, want %s", wo.State, StateClosed)
	}
}

func TestInvalidStateTransition(t *testing.T) {
	m := NewManager()
	wo, _ := m.Create(TypeInspection, "C-3", PriorityNormal, []string{"insp-1"})

	// Cannot jump from created directly to closed.
	if err := m.Transition(wo.ID, StateClosed); err == nil {
		t.Error("transition created -> closed should fail")
	}

	// Cannot jump from created to executing (maintenance state).
	if err := m.Transition(wo.ID, StateExecuting); err == nil {
		t.Error("transition created -> executing should fail")
	}

	// Valid transition then invalid reverse.
	_ = m.Transition(wo.ID, StateInProgress)
	if err := m.Transition(wo.ID, StateCreated); err == nil {
		t.Error("transition in_progress -> created should fail")
	}
}

func TestIdempotentTransition(t *testing.T) {
	m := NewManager()
	wo, _ := m.Create(TypeInspection, "C-4", PriorityNormal, []string{"insp-1"})

	// Transitioning to the current state is a no-op.
	if err := m.Transition(wo.ID, StateCreated); err != nil {
		t.Errorf("idempotent transition should be nil, got: %v", err)
	}

	// Closing an already-closed order is idempotent.
	_ = m.Transition(wo.ID, StateInProgress)
	_ = m.Transition(wo.ID, StateClosed)
	if err := m.Transition(wo.ID, StateClosed); err != nil {
		t.Errorf("closing already-closed order should be idempotent, got: %v", err)
	}
}

func TestCreateValidation(t *testing.T) {
	m := NewManager()

	// Missing inspector for inspection.
	if _, err := m.Create(TypeInspection, "C-5", PriorityNormal, nil); err == nil {
		t.Error("should fail without inspector")
	}

	// Single engineer for maintenance.
	if _, err := m.Create(TypeMaintenance, "C-5", PriorityNormal, []string{"eng-1"}); err == nil {
		t.Error("should fail with only one engineer")
	}

	// Invalid priority.
	if _, err := m.Create(TypeInspection, "C-5", Priority(99), []string{"insp-1"}); err == nil {
		t.Error("should fail with invalid priority")
	}

	// Empty cabin ID.
	if _, err := m.Create(TypeInspection, "", PriorityNormal, []string{"insp-1"}); err == nil {
		t.Error("should fail with empty cabin ID")
	}
}
