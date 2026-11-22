package workers

import (
	"context"
	"testing"
	"time"
)

func TestSyncProgress(t *testing.T) {
	var got []map[string]any
	p := &syncProgress{started: time.Now(), post: func(_ context.Context, _ string, payload any) error {
		got = append(got, payload.(map[string]any))
		return nil
	}}
	ctx := context.Background()

	p.step(ctx, "Fetching workout logs", 2, 30, 200, 800)
	p.step(ctx, "Fetching workout logs", 2, 30, 400, 800) // throttled: same phase, too soon
	p.step(ctx, "Fetching exercise details", 38, 78, 5, 10)
	p.finish(ctx, "failed", "boom")

	if len(got) != 3 {
		t.Fatalf("want 3 reports (one throttled), got %d: %v", len(got), got)
	}
	if pct := got[0]["percent"].(float64); pct != 9 {
		t.Errorf("200 of 800 in 2..30 = %v, want 9", pct)
	}
	if got[0]["message"] != "200 of 800" || got[0]["state"] != "running" {
		t.Errorf("unexpected first report %v", got[0])
	}
	if pct := got[1]["percent"].(float64); pct != 58 {
		t.Errorf("5 of 10 in 38..78 = %v, want 58", pct)
	}
	// A failure keeps the bar where it stopped; done fills it.
	if got[2]["state"] != "failed" || got[2]["percent"].(float64) != 58 || got[2]["finished_at"] == nil {
		t.Errorf("unexpected failure report %v", got[2])
	}
	p.finish(ctx, "done", "ok")
	if got[3]["percent"].(float64) != 100 {
		t.Errorf("done should report 100%%, got %v", got[3]["percent"])
	}
}
