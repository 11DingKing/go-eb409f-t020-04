package dispatch

import (
	"fmt"
	"sync"
	"testing"

	"microgrid-ops/internal/workorder"
)

// TestNotificationsReadableWhileOrdersEscalate exercises the supported
// concurrent usage: operators poll the notification feed while dispatch work
// keeps producing notifications. Every entry handed to a reader must be usable,
// and no notification may be lost.
func TestNotificationsReadableWhileOrdersEscalate(t *testing.T) {
	orch := newTestOrchestrator()
	registerCabin(orch, "C-40")

	const (
		writers        = 4
		perWriter      = 60
		wantTotalNotif = writers * perWriter
	)

	var producers sync.WaitGroup
	for w := 0; w < writers; w++ {
		producers.Add(1)
		go func(w int) {
			defer producers.Done()
			for i := 0; i < perWriter; i++ {
				wo, err := orch.CreateMaintenanceOrder("C-40", []string{"eng-1", "eng-2"}, workorder.PriorityNormal)
				if err != nil {
					t.Errorf("create maintenance order: %v", err)
					return
				}
				if _, err := orch.AuthorizeMaintenance(wo.ID, fmt.Sprintf("chief-%d", w)); err != nil {
					t.Errorf("authorize %s: %v", wo.ID, err)
					return
				}
				if _, err := orch.EscalateOrder(wo.ID); err != nil {
					t.Errorf("escalate %s: %v", wo.ID, err)
					return
				}
			}
		}(w)
	}

	var readers sync.WaitGroup
	stop := make(chan struct{})
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				for i, n := range orch.Notifications() {
					if n == nil {
						t.Errorf("notification feed returned an empty entry at index %d", i)
						return
					}
					_ = n.Message
				}
			}
		}
	}()

	producers.Wait()
	close(stop)
	readers.Wait()

	if got := len(orch.Notifications()); got != wantTotalNotif {
		t.Fatalf("notifications = %d, want %d", got, wantTotalNotif)
	}
}
