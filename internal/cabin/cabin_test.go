package cabin

import (
	"errors"
	"testing"
)

func TestCabinRegisterAndGet(t *testing.T) {
	m := NewManager()
	c := &Cabin{ID: "C-001", Name: "Ejina-1", Location: "North", TemperatureLimit: 45.0}
	if err := m.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, err := m.Get("C-001")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Ejina-1" {
		t.Errorf("name = %s, want Ejina-1", got.Name)
	}
	if !got.GridConnectionQualified {
		t.Error("new cabin should have grid connection qualified")
	}
	if got.Status != StatusNormal {
		t.Errorf("status = %s, want %s", got.Status, StatusNormal)
	}
	if !got.CoolingFanRunning {
		t.Error("new cabin should have cooling fan running")
	}

	// Duplicate registration.
	if err := m.Register(&Cabin{ID: "C-001"}); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("duplicate register err = %v, want ErrAlreadyExists", err)
	}

	// Unknown cabin.
	if _, err := m.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get unknown err = %v, want ErrNotFound", err)
	}
}

func TestCabinFreezeThaw(t *testing.T) {
	m := NewManager()
	_ = m.Register(&Cabin{ID: "C-002", TemperatureLimit: 50})

	c, _ := m.Get("C-002")
	if !c.GridConnectionQualified {
		t.Fatal("should start qualified")
	}

	if err := m.Freeze("C-002"); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	c, _ = m.Get("C-002")
	if c.GridConnectionQualified {
		t.Error("should be frozen (unqualified)")
	}
	if c.Status != StatusFrozen {
		t.Errorf("status = %s, want %s", c.Status, StatusFrozen)
	}

	if err := m.Thaw("C-002"); err != nil {
		t.Fatalf("thaw: %v", err)
	}
	c, _ = m.Get("C-002")
	if !c.GridConnectionQualified {
		t.Error("should be thawed (qualified)")
	}
	if c.Status != StatusNormal {
		t.Errorf("status = %s, want %s", c.Status, StatusNormal)
	}
}

func TestCabinAnomalyDetection(t *testing.T) {
	m := NewManager()
	_ = m.Register(&Cabin{ID: "C-003", TemperatureLimit: 45.0})

	// Normal conditions — no anomaly.
	_ = m.SetConditions("C-003", 30.0, false, true)
	c, _ := m.Get("C-003")
	if c.HasAnomaly() {
		t.Error("normal conditions should not be anomaly")
	}

	// Temperature over limit.
	_ = m.SetConditions("C-003", 50.0, false, true)
	c, _ = m.Get("C-003")
	if !c.HasAnomaly() {
		t.Error("temperature over limit should be anomaly")
	}

	// Insulation alarm.
	_ = m.SetConditions("C-003", 30.0, true, true)
	c, _ = m.Get("C-003")
	if !c.HasAnomaly() {
		t.Error("insulation alarm should be anomaly")
	}

	// Cooling fan stopped.
	_ = m.SetConditions("C-003", 30.0, false, false)
	c, _ = m.Get("C-003")
	if !c.HasAnomaly() {
		t.Error("cooling fan stopped should be anomaly")
	}
}

func TestCabinAcquireRelease(t *testing.T) {
	m := NewManager()
	_ = m.Register(&Cabin{ID: "C-004", TemperatureLimit: 50})

	// Acquire for inspection.
	if err := m.Acquire("C-004", "WO-1", true); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	c, _ := m.Get("C-004")
	if c.ActiveWorkOrderID != "WO-1" {
		t.Errorf("active = %s, want WO-1", c.ActiveWorkOrderID)
	}
	if c.Status != StatusUnderInspection {
		t.Errorf("status = %s, want %s", c.Status, StatusUnderInspection)
	}

	// Second acquire fails.
	if err := m.Acquire("C-004", "WO-2", false); !errors.Is(err, ErrBusy) {
		t.Errorf("second acquire err = %v, want ErrBusy", err)
	}

	// Release.
	if err := m.Release("C-004"); err != nil {
		t.Fatalf("release: %v", err)
	}
	c, _ = m.Get("C-004")
	if c.ActiveWorkOrderID != "" {
		t.Error("active should be cleared after release")
	}
	if c.Status != StatusNormal {
		t.Errorf("status = %s, want %s", c.Status, StatusNormal)
	}

	// Release after freeze keeps frozen status.
	_ = m.Freeze("C-004")
	_ = m.Acquire("C-004", "WO-3", false)
	_ = m.Release("C-004")
	c, _ = m.Get("C-004")
	if c.Status != StatusFrozen {
		t.Errorf("status after release with freeze = %s, want %s", c.Status, StatusFrozen)
	}
}
