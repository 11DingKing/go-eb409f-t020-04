package scheduler

import (
	"context"
	"sync"
	"time"

	"microgrid-ops/internal/dispatch"
)

// Scheduler runs background checks for anomaly deadlines and maintenance timeouts.
type Scheduler struct {
	orch     *dispatch.Orchestrator
	interval time.Duration
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
}

// New creates a scheduler that polls the orchestrator at the given interval.
func New(orch *dispatch.Orchestrator, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = 1 * time.Second
	}
	return &Scheduler{orch: orch, interval: interval}
}

// Start launches the background monitoring goroutine.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.run(ctx)
	return nil
}

// Stop signals the background goroutine to stop and waits for it.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			s.orch.CheckAnomalyDeadlines(now)
			s.orch.CheckMaintenanceTimeouts(now)
		}
	}
}

// Tick runs one monitoring cycle synchronously (useful for tests).
func (s *Scheduler) Tick(now time.Time) {
	s.orch.CheckAnomalyDeadlines(now)
	s.orch.CheckMaintenanceTimeouts(now)
}
