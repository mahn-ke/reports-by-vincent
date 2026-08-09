package workers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/riverqueue/river"
)

type FitXArgs struct{}

func (FitXArgs) Kind() string { return "fitx" }

type FitXWorker struct {
	river.WorkerDefaults[FitXArgs]
	Email      string
	Password   string
	APIBaseURL string
}

func (w *FitXWorker) Work(ctx context.Context, job *river.Job[FitXArgs]) error {
	if w.Email == "" || w.Password == "" {
		slog.Warn("FITX_EMAIL or FITX_PASSWORD not set — skipping FitX job")
		return nil
	}

	cookie, err := fitxLogin(ctx, w.Email, w.Password)
	if err != nil {
		return fmt.Errorf("fitx login: %w", err)
	}

	dates, err := fitxCheckins(ctx, cookie, w.Email, w.Password)
	if err != nil {
		return fmt.Errorf("fitx checkins: %w", err)
	}
	slog.Info("FitX fetch complete", "dates", len(dates))

	return postTraining(ctx, w.APIBaseURL, dates)
}

func fitxLogin(ctx context.Context, email, password string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"username": email, "password": password})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://mein.fitx.de/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-nox-client-type", "WEB")
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", email, password)))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("x-public-facility-group", "FITXDE-7B7DAC63E1744DE797245D6E314CD8F6")
	req.Header.Set("x-tenant", "fitx")

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == "SESSION" {
			return c.Value, nil
		}
	}
	return "", fmt.Errorf("SESSION cookie not found (status %d): %s", resp.StatusCode, string(body))
}

type fitxCheckinItem struct {
	CheckinDate string `json:"checkinDate"`
	Date        string `json:"date"`
}

func fitxCheckins(ctx context.Context, sessionVal, email, password string) ([]string, error) {
	now := time.Now()
	from := now.AddDate(-1, 0, 0).Format("2006-01-02")
	to := now.Format("2006-01-02")

	url := fmt.Sprintf("https://mein.fitx.de/nox/v1/studios/checkin/history/report?from=%s&to=%s", from, to)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.AddCookie(&http.Cookie{Name: "SESSION", Value: sessionVal})
	req.Header.Set("x-nox-client-type", "WEB")
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", email, password)))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("x-public-facility-group", "FITXDE-7B7DAC63E1744DE797245D6E314CD8F6")
	req.Header.Set("x-tenant", "fitx")

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read checkins response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checkins status %d: %s", resp.StatusCode, string(body))
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("checkins returned empty body")
	}

	var items []fitxCheckinItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("decode checkins: %w", err)
	}

	seen := map[string]struct{}{}
	var dates []string
	for _, item := range items {
		raw := item.CheckinDate
		if raw == "" {
			raw = item.Date
		}
		if raw == "" {
			continue
		}
		// Parse ISO datetime or date and normalise to YYYY-MM-DD.
		var t time.Time
		var parseErr error
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
			t, parseErr = time.Parse(layout, raw)
			if parseErr == nil {
				break
			}
		}
		if parseErr != nil {
			slog.Warn("could not parse FitX date", "raw", raw)
			continue
		}
		d := t.Format("2006-01-02")
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			dates = append(dates, d)
		}
	}
	return dates, nil
}

func postTraining(ctx context.Context, apiBase string, dates []string) error {
	type trainingInput struct {
		Date string `json:"date"`
	}
	entries := make([]trainingInput, len(dates))
	for i, d := range dates {
		entries[i] = trainingInput{Date: d}
	}
	body, _ := json.Marshal(entries)
	req, _ := http.NewRequestWithContext(ctx, "POST", apiBase+"/internal/training", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API training POST returned %d", resp.StatusCode)
	}
	return nil
}
