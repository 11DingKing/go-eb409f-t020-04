package parts

import (
	"fmt"
	"sync"
	"time"
)

// RequisitionStatus tracks a spare-parts requisition lifecycle.
type RequisitionStatus string

const (
	RequisitionPending    RequisitionStatus = "pending"
	RequisitionConfirmed  RequisitionStatus = "confirmed"
	RequisitionOutOfStock RequisitionStatus = "out_of_stock"
	RequisitionRolledBack RequisitionStatus = "rolled_back"
)

// Part is a spare part in the warehouse.
type Part struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Stock    int    `json:"stock"`
	Reserved int    `json:"reserved"`
	MinStock int    `json:"min_stock"`
}

// Available returns the unreserved stock count.
func (p *Part) Available() int {
	return p.Stock - p.Reserved
}

// IsLowStock reports whether available stock is at or below the minimum threshold.
func (p *Part) IsLowStock() bool {
	return p.Available() <= p.MinStock
}

// Requisition is a spare-parts withdrawal request requiring dual signature.
type Requisition struct {
	ID             string            `json:"id"`
	WorkOrderID    string            `json:"work_order_id"`
	PartSKU        string            `json:"part_sku"`
	Quantity       int               `json:"quantity"`
	EngineerID     string            `json:"engineer_id"`
	ManagerID      string            `json:"manager_id"`
	EngineerSigned bool              `json:"engineer_signed"`
	ManagerSigned  bool              `json:"manager_signed"`
	Status         RequisitionStatus `json:"status"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// IsDualSigned reports whether both engineer and manager have signed.
func (r *Requisition) IsDualSigned() bool {
	return r.EngineerSigned && r.ManagerSigned
}

// Snapshot returns a copy for safe external use.
func (r *Requisition) Snapshot() *Requisition {
	cp := *r
	return &cp
}

// Inventory manages spare parts and requisitions.
type Inventory struct {
	mu           sync.Mutex
	parts        map[string]*Part
	requisitions map[string]*Requisition
	seq          int64
}

// NewInventory creates a new inventory.
func NewInventory() *Inventory {
	return &Inventory{
		parts:        make(map[string]*Part),
		requisitions: make(map[string]*Requisition),
	}
}

// RegisterPart adds or replaces a part definition.
func (inv *Inventory) RegisterPart(p *Part) error {
	if p.SKU == "" {
		return fmt.Errorf("part sku is required")
	}
	if p.Stock < 0 {
		return fmt.Errorf("stock cannot be negative")
	}
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if existing, ok := inv.parts[p.SKU]; ok {
		existing.Name = p.Name
		existing.Stock = p.Stock
		existing.MinStock = p.MinStock
		return nil
	}
	cp := *p
	inv.parts[p.SKU] = &cp
	return nil
}

// GetPart retrieves a part by SKU.
func (inv *Inventory) GetPart(sku string) (*Part, error) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	p, ok := inv.parts[sku]
	if !ok {
		return nil, fmt.Errorf("part %s not found", sku)
	}
	cp := *p
	return &cp, nil
}

// ListParts returns all parts.
func (inv *Inventory) ListParts() []*Part {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	result := make([]*Part, 0, len(inv.parts))
	for _, p := range inv.parts {
		cp := *p
		result = append(result, &cp)
	}
	return result
}

// nextReqID generates a monotonic requisition ID.
func (inv *Inventory) nextReqID() string {
	inv.seq++
	return fmt.Sprintf("REQ-%d", inv.seq)
}

// SubmitRequisition creates a requisition signed by the engineer.
// If sufficient stock is available it is reserved; otherwise the status is out_of_stock.
func (inv *Inventory) SubmitRequisition(woID, sku string, qty int, engineerID string) (*Requisition, error) {
	if woID == "" {
		return nil, fmt.Errorf("work order id is required")
	}
	if qty <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if engineerID == "" {
		return nil, fmt.Errorf("engineer id is required")
	}
	inv.mu.Lock()
	defer inv.mu.Unlock()
	p, ok := inv.parts[sku]
	if !ok {
		return nil, fmt.Errorf("part %s not found", sku)
	}
	inv.seq++
	reqID := fmt.Sprintf("REQ-%d", inv.seq)
	now := time.Now()
	req := &Requisition{
		ID:             reqID,
		WorkOrderID:    woID,
		PartSKU:        sku,
		Quantity:       qty,
		EngineerID:     engineerID,
		EngineerSigned: true,
		Status:         RequisitionPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if p.Available() >= qty {
		p.Reserved += qty
	} else {
		req.Status = RequisitionOutOfStock
	}
	inv.requisitions[reqID] = req
	return req.Snapshot(), nil
}

// ConfirmRequisition applies the warehouse manager signature, completing dual-sign.
// Reserved stock is deducted from inventory. Out-of-stock requisitions must be
// re-supplied via RecheckRequisition before they can be confirmed.
func (inv *Inventory) ConfirmRequisition(reqID, managerID string) (*Requisition, error) {
	if managerID == "" {
		return nil, fmt.Errorf("manager id is required")
	}
	inv.mu.Lock()
	defer inv.mu.Unlock()
	req, ok := inv.requisitions[reqID]
	if !ok {
		return nil, fmt.Errorf("requisition %s not found", reqID)
	}
	if req.Status == RequisitionConfirmed {
		return req.Snapshot(), nil // idempotent
	}
	if req.Status == RequisitionRolledBack {
		return nil, fmt.Errorf("cannot confirm rolled-back requisition %s", reqID)
	}
	if req.Status == RequisitionOutOfStock {
		return nil, fmt.Errorf("cannot confirm out-of-stock requisition %s; emergency procurement required", reqID)
	}
	if req.Status != RequisitionPending {
		return nil, fmt.Errorf("requisition %s is not pending (status: %s)", reqID, req.Status)
	}
	p, ok := inv.parts[req.PartSKU]
	if !ok {
		return nil, fmt.Errorf("part %s not found", req.PartSKU)
	}
	if p.Reserved < req.Quantity {
		return nil, fmt.Errorf("reserved stock inconsistent for requisition %s", reqID)
	}
	req.ManagerID = managerID
	req.ManagerSigned = true
	p.Reserved -= req.Quantity
	p.Stock -= req.Quantity
	req.Status = RequisitionConfirmed
	req.UpdatedAt = time.Now()
	return req.Snapshot(), nil
}

// RollbackRequisition releases any reserved stock and marks the requisition as rolled back.
func (inv *Inventory) RollbackRequisition(reqID string) error {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	req, ok := inv.requisitions[reqID]
	if !ok {
		return fmt.Errorf("requisition %s not found", reqID)
	}
	if req.Status == RequisitionRolledBack {
		return nil // idempotent
	}
	if req.Status == RequisitionConfirmed {
		return fmt.Errorf("cannot rollback confirmed requisition %s", reqID)
	}
	if req.Status == RequisitionPending {
		p, ok := inv.parts[req.PartSKU]
		if ok {
			p.Reserved -= req.Quantity
			if p.Reserved < 0 {
				p.Reserved = 0
			}
		}
	}
	req.Status = RequisitionRolledBack
	req.UpdatedAt = time.Now()
	return nil
}

// RecheckRequisition re-evaluates an out-of-stock requisition after emergency procurement.
// If stock is now available it is reserved and the status returns to pending.
func (inv *Inventory) RecheckRequisition(reqID string) (*Requisition, error) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	req, ok := inv.requisitions[reqID]
	if !ok {
		return nil, fmt.Errorf("requisition %s not found", reqID)
	}
	if req.Status != RequisitionOutOfStock {
		return req.Snapshot(), nil
	}
	p, ok := inv.parts[req.PartSKU]
	if !ok {
		return nil, fmt.Errorf("part %s not found", req.PartSKU)
	}
	if p.Available() >= req.Quantity {
		p.Reserved += req.Quantity
		req.Status = RequisitionPending
		req.UpdatedAt = time.Now()
	}
	return req.Snapshot(), nil
}

// EmergencyProcurement adds stock for a part, typically after an out-of-stock event.
func (inv *Inventory) EmergencyProcurement(sku string, qty int) error {
	if qty <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	inv.mu.Lock()
	defer inv.mu.Unlock()
	p, ok := inv.parts[sku]
	if !ok {
		return fmt.Errorf("part %s not found", sku)
	}
	p.Stock += qty
	return nil
}

// GetRequisition retrieves a requisition by ID.
func (inv *Inventory) GetRequisition(id string) (*Requisition, error) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	req, ok := inv.requisitions[id]
	if !ok {
		return nil, fmt.Errorf("requisition %s not found", id)
	}
	return req.Snapshot(), nil
}

// ListRequisitions returns all requisitions.
func (inv *Inventory) ListRequisitions() []*Requisition {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	result := make([]*Requisition, 0, len(inv.requisitions))
	for _, r := range inv.requisitions {
		result = append(result, r.Snapshot())
	}
	return result
}

// SnapshotParts returns copies for persistence.
func (inv *Inventory) SnapshotParts() []*Part {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	result := make([]*Part, 0, len(inv.parts))
	for _, p := range inv.parts {
		cp := *p
		result = append(result, &cp)
	}
	return result
}

// SnapshotRequisitions returns copies for persistence.
func (inv *Inventory) SnapshotRequisitions() []*Requisition {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	result := make([]*Requisition, 0, len(inv.requisitions))
	for _, r := range inv.requisitions {
		result = append(result, r.Snapshot())
	}
	return result
}

// Restore replaces all parts and requisitions from snapshots.
func (inv *Inventory) Restore(partsList []*Part, reqs []*Requisition) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.parts = make(map[string]*Part, len(partsList))
	for _, p := range partsList {
		cp := *p
		inv.parts[p.SKU] = &cp
	}
	inv.requisitions = make(map[string]*Requisition, len(reqs))
	maxSeq := int64(0)
	for _, r := range reqs {
		cp := *r
		inv.requisitions[r.ID] = &cp
		var n int64
		if _, err := fmt.Sscanf(r.ID, "REQ-%d", &n); err == nil && n > maxSeq {
			maxSeq = n
		}
	}
	inv.seq = maxSeq
}
