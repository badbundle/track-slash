package githubintegration

import (
	"context"
	"log"
	"time"

	"github.com/bradleymackey/track-slash/internal/store"
)

type WorkerOptions struct {
	BatchSize    int
	PollInterval time.Duration
	Lease        time.Duration
}

type Worker struct {
	store        *store.Store
	service      *Service
	batchSize    int
	pollInterval time.Duration
	lease        time.Duration
}

func NewWorker(s *store.Store, service *Service, opts WorkerOptions) *Worker {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 20
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Minute
	}
	if opts.Lease <= 0 {
		opts.Lease = 5 * time.Minute
	}
	return &Worker{store: s, service: service, batchSize: opts.BatchSize, pollInterval: opts.PollInterval, lease: opts.Lease}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	w.process(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.process(ctx)
		}
	}
}

func (w *Worker) process(ctx context.Context) {
	links, err := w.store.ClaimGitHubIssueLinks(ctx, w.batchSize, w.lease)
	if err != nil {
		log.Printf("github refresh claim failed: %v", err)
		return
	}
	for _, link := range links {
		if ctx.Err() != nil {
			return
		}
		if _, err := w.service.RefreshLink(ctx, link.ID); err != nil {
			// RefreshLink records a safe, user-visible failure and retry time.
			log.Printf("github link refresh failed: link=%s", link.ID)
		}
	}
}
