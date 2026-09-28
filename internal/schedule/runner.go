// Runner polls the schedule store and fires due jobs through a handler.
package schedule

import (
	"context"
	"log"
	"time"
)

// Handler runs one due job (typically an agent turn plus delivery).
// Runner already marked the job fired; handler errors are logged only.
type Handler func(ctx context.Context, job Job) error

// Runner periodically checks for due jobs.
type Runner struct {
	Store    *Store
	Interval time.Duration
	Now      func() time.Time
	Handle   Handler
}

// DefaultInterval is the poll cadence for due jobs.
const DefaultInterval = 30 * time.Second

// Start blocks until ctx is done, firing due jobs every tick.
func (r *Runner) Start(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	r.fireDue(ctx, "boot")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.fireDue(ctx, "tick")
		}
	}
}

func (r *Runner) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

// FireOnce checks once and fires all due jobs. Useful for tests and CLI.
func (r *Runner) FireOnce(ctx context.Context) int {
	return r.fireDue(ctx, "once")
}

func (r *Runner) fireDue(ctx context.Context, reason string) int {
	if r == nil || r.Store == nil || r.Handle == nil {
		return 0
	}
	now := r.now()
	due, err := r.Store.Due(now)
	if err != nil {
		log.Printf("[schedule] due check (%s): %v", reason, err)
		return 0
	}
	fired := 0
	for _, job := range due {
		updated, _, err := r.Store.MarkFired(job.ID, now)
		if err != nil {
			log.Printf("[schedule] mark fired %s: %v", job.ID, err)
			continue
		}
		fired++
		go func(j Job) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("[schedule] handler panic %s: %v", j.ID, rec)
				}
			}()
			runCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
			defer cancel()
			if err := r.Handle(runCtx, j); err != nil {
				log.Printf("[schedule] job %s failed: %v", j.ID, err)
			} else {
				log.Printf("[schedule] job %s fired (%s)", j.ID, updated.Describe())
			}
		}(updated)
	}
	return fired
}
