package series

import (
	"context"
	"log"
	"time"
)

// Scheduler wakes infrequently; per-watch next_check_at controls actual work.
type Scheduler struct {
	Manager   *Manager
	PollEvery time.Duration
}

func (s *Scheduler) Start(ctx context.Context) {
	if s.Manager == nil {
		return
	}
	interval := s.PollEvery
	if interval <= 0 {
		interval = time.Minute
	}
	// Run once at startup so overdue watches are not delayed by a full tick.
	if err := s.Manager.CheckDue(ctx); err != nil {
		log.Printf("series scheduler initial check: %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Manager.CheckDue(ctx); err != nil {
				log.Printf("series scheduler check: %v", err)
			}
		}
	}
}
