package workers

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// syncProgress reports the wger sync's progress to the API, where the calendar page polls it.
// Reporting is best effort: a failed report never fails the sync.
type syncProgress struct {
	apiURL    string
	started   time.Time
	lastPost  time.Time
	lastPhase string
	percent   float64
	post      func(ctx context.Context, url string, payload any) error
}

// Minimum interval between two reports within the same phase.
const progressInterval = 300 * time.Millisecond

func newSyncProgress(apiURL string) *syncProgress {
	return &syncProgress{apiURL: apiURL, started: time.Now(), post: postJSON}
}

// step reports phase progress: done of total mapped onto the percent range from..to. A new phase
// is always reported, further steps within a phase at most every progressInterval.
func (p *syncProgress) step(ctx context.Context, phase string, from, to float64, done, total int) {
	pct := from
	msg := ""
	if total > 0 {
		pct = from + (to-from)*float64(min(done, total))/float64(total)
		msg = fmt.Sprintf("%d of %d", min(done, total), total)
	}
	if phase == p.lastPhase && time.Since(p.lastPost) < progressInterval {
		return
	}
	p.lastPhase, p.lastPost, p.percent = phase, time.Now(), pct
	p.send(ctx, map[string]any{"state": "running", "phase": phase, "percent": pct, "message": msg, "started_at": p.started})
}

// finish reports the end state (done, failed or skipped).
func (p *syncProgress) finish(ctx context.Context, state, message string) {
	pct := p.percent
	if state == "done" {
		pct = 100
	}
	p.send(ctx, map[string]any{"state": state, "phase": p.lastPhase, "percent": pct, "message": message,
		"started_at": p.started, "finished_at": time.Now()})
}

func (p *syncProgress) send(ctx context.Context, status map[string]any) {
	if err := p.post(ctx, p.apiURL+"/internal/wger/sync-status", status); err != nil {
		slog.Warn("reporting wger sync progress failed", "err", err)
	}
}
