package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/riverqueue/river"
)

type GarminArgs struct{}

func (GarminArgs) Kind() string { return "garmin" }

type GarminWorker struct {
	river.WorkerDefaults[GarminArgs]
	GarminURL  string // http://garmin:8080
	APIBaseURL string
}

func (w *GarminWorker) Work(ctx context.Context, job *river.Job[GarminArgs]) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", w.GarminURL+"/fetch", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("garmin fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		slog.Warn("Garmin container unavailable (GARMIN_EMAIL/GARMIN_PASSWORD likely not set)")
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("garmin container returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read garmin response: %w", err)
	}

	// Validate that response is a non-empty JSON array before forwarding.
	var check []json.RawMessage
	if err := json.Unmarshal(body, &check); err != nil {
		return fmt.Errorf("unexpected garmin response format: %w", err)
	}
	if len(check) == 0 {
		slog.Info("Garmin returned no measurements")
		return nil
	}

	req2, _ := http.NewRequestWithContext(ctx, "POST", w.APIBaseURL+"/internal/body", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return fmt.Errorf("post to API: %w", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("API body POST returned %d", resp2.StatusCode)
	}
	slog.Info("Garmin measurements submitted", "count", len(check))
	return nil
}
