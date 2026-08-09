package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/riverqueue/river"
)

type WgerArgs struct{}

func (WgerArgs) Kind() string { return "wger" }

type WgerWorker struct {
	river.WorkerDefaults[WgerArgs]
	WgerURL    string // https://fitness.by.vincent.mahn.ke
	WgerToken  string
	APIBaseURL string
}

// wger API types

type wgerWorkoutLog struct {
	ID          int     `json:"id"`
	Exercise    int     `json:"exercise"`
	Date        string  `json:"date"`
	Weight      string  `json:"weight"`
	Repetitions string  `json:"repetitions"`
	RepsUnit    int     `json:"reps_unit"`
	WeightUnit  int     `json:"weight_unit"`
}

type wgerLogPage struct {
	Count    int              `json:"count"`
	Next     *string          `json:"next"`
	Previous *string          `json:"previous"`
	Results  []wgerWorkoutLog `json:"results"`
}

type wgerTranslation struct {
	Language int    `json:"language"`
	Name     string `json:"name"`
}

type wgerExerciseInfo struct {
	ID           int               `json:"id"`
	Translations []wgerTranslation `json:"translations"`
}

func (w *WgerWorker) Work(ctx context.Context, job *river.Job[WgerArgs]) error {
	if w.WgerToken == "" {
		slog.Warn("WGER_TOKEN not set — skipping wger job")
		return nil
	}

	logs, err := w.fetchAllLogs(ctx)
	if err != nil {
		return fmt.Errorf("wger fetch logs: %w", err)
	}
	if len(logs) == 0 {
		slog.Info("wger returned no workout logs")
		return nil
	}

	// Collect unique exercise IDs
	exerciseIDs := map[int]struct{}{}
	for _, l := range logs {
		exerciseIDs[l.Exercise] = struct{}{}
	}

	// Fetch names for all exercises
	exercises := make([]map[string]any, 0, len(exerciseIDs))
	for id := range exerciseIDs {
		name, err := w.fetchExerciseName(ctx, id)
		if err != nil {
			slog.Warn("wger exercise info failed", "exercise_id", id, "err", err)
			name = fmt.Sprintf("Exercise %d", id)
		}
		exercises = append(exercises, map[string]any{"id": id, "name": name})
	}

	if err := w.postJSON(ctx, w.APIBaseURL+"/internal/workout/exercises", exercises); err != nil {
		return fmt.Errorf("post workout exercises: %w", err)
	}

	// Build entry list
	entries := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		weight := 0.0
		fmt.Sscanf(l.Weight, "%f", &weight)
		reps := 0
		fmt.Sscanf(l.Repetitions, "%d", &reps)
		entries = append(entries, map[string]any{
			"exercise_id":  l.Exercise,
			"date":         l.Date,
			"weight":       weight,
			"repetitions":  reps,
		})
	}

	if err := w.postJSON(ctx, w.APIBaseURL+"/internal/workout/entries", entries); err != nil {
		return fmt.Errorf("post workout entries: %w", err)
	}

	slog.Info("wger sync complete", "exercises", len(exercises), "entries", len(entries))
	return nil
}

func (w *WgerWorker) fetchAllLogs(ctx context.Context) ([]wgerWorkoutLog, error) {
	oneYearAgo := time.Now().AddDate(-1, 0, 0).Format("2006-01-02")
	url := fmt.Sprintf("%s/api/v2/workoutlog/?ordering=date&date__gte=%s&limit=100", w.WgerURL, oneYearAgo)

	var all []wgerWorkoutLog
	for url != "" {
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req.Header.Set("Authorization", "Token "+w.WgerToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("wger workoutlog returned %d: %s", resp.StatusCode, string(body))
		}

		var page wgerLogPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode workoutlog page: %w", err)
		}
		all = append(all, page.Results...)
		if page.Next != nil {
			url = *page.Next
		} else {
			url = ""
		}
	}
	return all, nil
}

func (w *WgerWorker) fetchExerciseName(ctx context.Context, id int) (string, error) {
	url := fmt.Sprintf("%s/api/v2/exerciseinfo/%d/", w.WgerURL, id)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Token "+w.WgerToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("exerciseinfo %d returned %d", id, resp.StatusCode)
	}

	var info wgerExerciseInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return "", err
	}

	// prefer English (language id 2), fall back to first available
	for _, t := range info.Translations {
		if t.Language == 2 && t.Name != "" {
			return t.Name, nil
		}
	}
	if len(info.Translations) > 0 && info.Translations[0].Name != "" {
		return info.Translations[0].Name, nil
	}
	return fmt.Sprintf("Exercise %d", id), nil
}

func (w *WgerWorker) postJSON(ctx context.Context, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s returned %d", url, resp.StatusCode)
	}
	return nil
}
