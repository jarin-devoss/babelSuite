package catalog

import (
	"context"
	"log/slog"
	"time"
)

const defaultRefreshInterval = 30 * time.Second

// Refresher periodically calls ListPackages on the underlying reader so the
// cache stays warm and requests never block on a cold registry discovery.
type Refresher struct {
	reader   Reader
	interval time.Duration
}

func NewRefresher(reader Reader, interval time.Duration) *Refresher {
	if interval <= 0 {
		interval = defaultRefreshInterval
	}
	return &Refresher{reader: reader, interval: interval}
}

// Start runs the refresh loop until ctx is cancelled.
func (r *Refresher) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.refresh(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refresh(ctx)
		}
	}
}

func (r *Refresher) refresh(ctx context.Context) {
	refreshCtx, cancel := context.WithTimeout(ctx, r.interval)
	defer cancel()
	if _, err := r.reader.ListPackages(refreshCtx); err != nil {
		slog.WarnContext(ctx, "catalog background refresh failed", "error", err)
	}
}
