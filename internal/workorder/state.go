package workorder

import "fmt"

// Type identifies a work order as inspection or maintenance.
type Type string

const (
	TypeInspection  Type = "inspection"
	TypeMaintenance Type = "maintenance"
)

// Priority controls queue ordering when multiple orders contend for one cabin.
type Priority int

const (
	PriorityLow    Priority = 1
	PriorityNormal Priority = 2
	PriorityHigh   Priority = 3
	PriorityUrgent Priority = 4
)

func (p Priority) String() string {
	switch p {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityUrgent:
		return "urgent"
	default:
		return "unknown"
	}
}

// State is the lifecycle state of a work order.
type State string

const (
	StateCreated             State = "created"
	StateQueued              State = "queued"
	StateInProgress          State = "in_progress"
	StateAnomalyReported     State = "anomaly_reported"
	StateAuthorized          State = "authorized"
	StateExecuting           State = "executing"
	StatePartsRequested      State = "parts_requested"
	StatePartsSuspended      State = "parts_suspended"
	StatePartsConfirmed      State = "parts_confirmed"
	StateRepairCompleted     State = "repair_completed"
	StatePendingRestoration  State = "pending_restoration"
	StatePendingRecheck      State = "pending_recheck"
	StateRestorationApproved State = "restoration_approved"
	StateEscalated           State = "escalated"
	StateClosed              State = "closed"
	StateCancelled           State = "cancelled"
)

// allowedTransitions defines the legal state machine edges.
var allowedTransitions = map[State][]State{
	StateCreated:             {StateQueued, StateInProgress, StateAuthorized, StateCancelled},
	StateQueued:              {StateInProgress, StateExecuting, StateCancelled},
	StateInProgress:          {StateAnomalyReported, StateClosed, StateCancelled},
	StateAnomalyReported:     {StateClosed, StateCancelled},
	StateAuthorized:          {StateExecuting, StateEscalated, StateQueued, StateCancelled},
	StateExecuting:           {StatePartsRequested, StateEscalated, StateQueued, StateCancelled},
	StateEscalated:           {StateExecuting, StateQueued, StateAuthorized, StateCancelled},
	StatePartsRequested:      {StatePartsConfirmed, StatePartsSuspended, StateQueued, StateCancelled},
	StatePartsSuspended:      {StatePartsConfirmed, StatePartsRequested, StateQueued, StateCancelled},
	StatePartsConfirmed:      {StateRepairCompleted, StateCancelled},
	StateRepairCompleted:     {StatePendingRestoration, StateCancelled},
	StatePendingRestoration:  {StateRestorationApproved, StatePendingRecheck, StateCancelled},
	StatePendingRecheck:      {StatePendingRestoration, StateCancelled},
	StateRestorationApproved: {StateClosed, StateCancelled},
	StateClosed:              {},
	StateCancelled:           {},
}

// CanTransition reports whether a direct state transition is legal.
func CanTransition(from, to State) bool {
	targets, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// ValidateTransition returns an error if the transition is illegal.
func ValidateTransition(from, to State) error {
	if from == to {
		return nil // idempotent
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("invalid state transition: %s -> %s", from, to)
	}
	return nil
}

// IsTerminal reports whether the state is final (closed or cancelled).
func (s State) IsTerminal() bool {
	return s == StateClosed || s == StateCancelled
}

// IsActive reports whether the state is non-terminal.
func (s State) IsActive() bool {
	return !s.IsTerminal()
}
