package parts

import (
	"testing"
)

func TestRequisitionDualSign(t *testing.T) {
	inv := NewInventory()
	_ = inv.RegisterPart(&Part{SKU: "FAN-100", Name: "Cooling Fan", Stock: 10, MinStock: 2})

	// Engineer submits requisition — signs.
	req, err := inv.SubmitRequisition("WO-1", "FAN-100", 3, "eng-1")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !req.EngineerSigned {
		t.Error("engineer should have signed")
	}
	if req.ManagerSigned {
		t.Error("manager should not have signed yet")
	}
	if req.Status != RequisitionPending {
		t.Errorf("status = %s, want %s", req.Status, RequisitionPending)
	}

	// Stock reserved.
	p, _ := inv.GetPart("FAN-100")
	if p.Reserved != 3 {
		t.Errorf("reserved = %d, want 3", p.Reserved)
	}
	if p.Available() != 7 {
		t.Errorf("available = %d, want 7", p.Available())
	}

	// Manager confirms — dual sign complete.
	req, err = inv.ConfirmRequisition(req.ID, "mgr-1")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !req.IsDualSigned() {
		t.Error("should be dual-signed after confirmation")
	}
	if req.Status != RequisitionConfirmed {
		t.Errorf("status = %s, want %s", req.Status, RequisitionConfirmed)
	}

	// Stock deducted.
	p, _ = inv.GetPart("FAN-100")
	if p.Stock != 7 {
		t.Errorf("stock = %d, want 7", p.Stock)
	}
	if p.Reserved != 0 {
		t.Errorf("reserved = %d, want 0 after confirmation", p.Reserved)
	}
}

func TestRequisitionOutOfStock(t *testing.T) {
	inv := NewInventory()
	_ = inv.RegisterPart(&Part{SKU: "CELL-200", Name: "Battery Cell", Stock: 2, MinStock: 1})

	// Request 5 but only 2 available.
	req, err := inv.SubmitRequisition("WO-2", "CELL-200", 5, "eng-1")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if req.Status != RequisitionOutOfStock {
		t.Errorf("status = %s, want %s", req.Status, RequisitionOutOfStock)
	}

	// Cannot confirm an out-of-stock requisition.
	if _, err := inv.ConfirmRequisition(req.ID, "mgr-1"); err == nil {
		t.Error("should not be able to confirm out-of-stock requisition")
	}

	// Emergency procurement adds stock.
	if err := inv.EmergencyProcurement("CELL-200", 10); err != nil {
		t.Fatalf("emergency procurement: %v", err)
	}

	// Recheck now finds stock available.
	req, err = inv.RecheckRequisition(req.ID)
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if req.Status != RequisitionPending {
		t.Errorf("status after recheck = %s, want %s", req.Status, RequisitionPending)
	}

	// Now can confirm.
	req, err = inv.ConfirmRequisition(req.ID, "mgr-1")
	if err != nil {
		t.Fatalf("confirm after recheck: %v", err)
	}
	if req.Status != RequisitionConfirmed {
		t.Errorf("status = %s, want %s", req.Status, RequisitionConfirmed)
	}
}

func TestRequisitionRollback(t *testing.T) {
	inv := NewInventory()
	_ = inv.RegisterPart(&Part{SKU: "INV-300", Name: "Inverter", Stock: 5, MinStock: 1})

	req, _ := inv.SubmitRequisition("WO-3", "INV-300", 2, "eng-1")

	// Stock reserved.
	p, _ := inv.GetPart("INV-300")
	if p.Reserved != 2 {
		t.Errorf("reserved = %d, want 2", p.Reserved)
	}

	// Rollback releases reservation.
	if err := inv.RollbackRequisition(req.ID); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	p, _ = inv.GetPart("INV-300")
	if p.Reserved != 0 {
		t.Errorf("reserved after rollback = %d, want 0", p.Reserved)
	}
	if p.Stock != 5 {
		t.Errorf("stock after rollback = %d, want 5 (unchanged)", p.Stock)
	}

	req, _ = inv.GetRequisition(req.ID)
	if req.Status != RequisitionRolledBack {
		t.Errorf("status = %s, want %s", req.Status, RequisitionRolledBack)
	}

	// Rolling back again is idempotent.
	if err := inv.RollbackRequisition(req.ID); err != nil {
		t.Errorf("idempotent rollback should be nil, got: %v", err)
	}

	// Cannot rollback a confirmed requisition.
	req2, _ := inv.SubmitRequisition("WO-4", "INV-300", 1, "eng-1")
	_, _ = inv.ConfirmRequisition(req2.ID, "mgr-1")
	if err := inv.RollbackRequisition(req2.ID); err == nil {
		t.Error("should not be able to rollback confirmed requisition")
	}
}

func TestLowStockDetection(t *testing.T) {
	inv := NewInventory()
	_ = inv.RegisterPart(&Part{SKU: "PCS-400", Name: "PCS Module", Stock: 3, MinStock: 3})

	p, _ := inv.GetPart("PCS-400")
	if !p.IsLowStock() {
		t.Error("stock at min should be low stock")
	}

	_ = inv.EmergencyProcurement("PCS-400", 5)
	p, _ = inv.GetPart("PCS-400")
	if p.IsLowStock() {
		t.Error("stock above min should not be low stock")
	}
}
