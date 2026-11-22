package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/riverqueue/river"
)

type WgerArgs struct{}

func (WgerArgs) Kind() string { return "wger" }

// WgerWorker syncs workout logs, sessions and exercise metadata from wger.
// It authenticates with the OAuth token obtained via the "Connect wger" flow in the app
// (stored by the API) and refreshes it when it is about to expire.
type WgerWorker struct {
	river.WorkerDefaults[WgerArgs]
	WgerURL      string // https://fitness.by.vincent.mahn.ke
	ClientID     string
	ClientSecret string
	APIBaseURL   string
}

var errWgerNotConnected = errors.New("wger not connected")

// wger API types

type wgerWorkoutLog struct {
	ID              int     `json:"id"`
	Exercise        int     `json:"exercise"`
	Session         *int    `json:"session"`
	Date            string  `json:"date"`
	Weight          *string `json:"weight"`
	Repetitions     *string `json:"repetitions"`
	RepetitionsUnit *int    `json:"repetitions_unit"`
	WeightUnit      *int    `json:"weight_unit"`
}

type wgerSession struct {
	ID            int     `json:"id"`
	Day           *int    `json:"day"`
	DatetimeStart *string `json:"datetime_start"`
}

type wgerDay struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type wgerPage[T any] struct {
	Next    *string `json:"next"`
	Results []T     `json:"results"`
}

type wgerTranslation struct {
	Language int    `json:"language"`
	Name     string `json:"name"`
}

type wgerMuscle struct {
	Name   string `json:"name"`
	NameEn string `json:"name_en"`
}

type wgerExerciseInfo struct {
	ID       int `json:"id"`
	Category struct {
		Name string `json:"name"`
	} `json:"category"`
	Muscles      []wgerMuscle      `json:"muscles"`
	Translations []wgerTranslation `json:"translations"`
}

type oauthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

const (
	wgerWeightUnitLb = 2
	wgerRepsUnitReps = 1
	lbToKg           = 0.45359237
)

func (w *WgerWorker) Work(ctx context.Context, job *river.Job[WgerArgs]) error {
	token, err := w.accessToken(ctx)
	if errors.Is(err, errWgerNotConnected) {
		slog.Warn("wger not connected — open /workout/calendar in reports and click \"Connect wger\"; skipping")
		return nil
	}
	if err != nil {
		return fmt.Errorf("wger token: %w", err)
	}

	var logs []wgerWorkoutLog
	if err := wgerGetAll(ctx, w.WgerURL+"/api/v2/workoutlog/?ordering=date&limit=200", token, &logs); err != nil {
		return fmt.Errorf("wger fetch logs: %w", err)
	}
	if len(logs) == 0 {
		slog.Info("wger returned no workout logs")
		return nil
	}

	var sessions []wgerSession
	if err := wgerGetAll(ctx, w.WgerURL+"/api/v2/workoutsession/?limit=200", token, &sessions); err != nil {
		return fmt.Errorf("wger fetch sessions: %w", err)
	}

	// Exercises: names plus category/muscles for push/pull/legs classification.
	seen := map[int]bool{}
	exercises := []map[string]any{}
	for _, l := range logs {
		if seen[l.Exercise] {
			continue
		}
		seen[l.Exercise] = true
		ex := map[string]any{"id": l.Exercise, "name": fmt.Sprintf("Exercise %d", l.Exercise), "category": "", "muscles": []string{}}
		info, err := w.fetchExerciseInfo(ctx, token, l.Exercise)
		if err != nil {
			slog.Warn("wger exercise info failed", "exercise_id", l.Exercise, "err", err)
		} else {
			ex["name"] = exerciseName(info)
			ex["category"] = info.Category.Name
			muscles := make([]string, 0, len(info.Muscles))
			for _, m := range info.Muscles {
				if m.NameEn != "" {
					muscles = append(muscles, m.NameEn)
				} else {
					muscles = append(muscles, m.Name)
				}
			}
			ex["muscles"] = muscles
		}
		exercises = append(exercises, ex)
	}
	if err := postJSON(ctx, w.APIBaseURL+"/internal/workout/exercises", exercises); err != nil {
		return fmt.Errorf("post workout exercises: %w", err)
	}

	// Sessions with their routine day name ("Push", "Pull", "Legs", ...).
	firstLogDate := map[int]string{}
	for _, l := range logs {
		if l.Session != nil {
			if _, ok := firstLogDate[*l.Session]; !ok {
				firstLogDate[*l.Session] = l.Date
			}
		}
	}
	dayNames := map[int]string{}
	sessionRows := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		date := firstLogDate[s.ID]
		if date == "" && s.DatetimeStart != nil && len(*s.DatetimeStart) >= 10 {
			date = (*s.DatetimeStart)[:10]
		}
		if date == "" {
			continue // empty session without date — nothing to show
		}
		name := ""
		if s.Day != nil {
			if n, ok := dayNames[*s.Day]; ok {
				name = n
			} else {
				var d wgerDay
				if err := wgerGet(ctx, fmt.Sprintf("%s/api/v2/day/%d/", w.WgerURL, *s.Day), token, &d); err != nil {
					slog.Warn("wger day lookup failed", "day_id", *s.Day, "err", err)
				} else {
					name = d.Name
				}
				dayNames[*s.Day] = name
			}
		}
		sessionRows = append(sessionRows, map[string]any{"id": s.ID, "date": date, "day_name": name})
	}
	if err := postJSON(ctx, w.APIBaseURL+"/internal/workout/sessions", sessionRows); err != nil {
		return fmt.Errorf("post workout sessions: %w", err)
	}

	entries := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		if l.RepetitionsUnit != nil && *l.RepetitionsUnit != wgerRepsUnitReps {
			continue // seconds, minutes, distance, ... — not comparable as reps
		}
		weight := parseFloat(l.Weight)
		if l.WeightUnit != nil && *l.WeightUnit == wgerWeightUnitLb {
			weight *= lbToKg
		}
		entries = append(entries, map[string]any{
			"wger_id":     l.ID,
			"session_id":  l.Session,
			"exercise_id": l.Exercise,
			"date":        l.Date[:min(10, len(l.Date))],
			"weight":      weight,
			"repetitions": int(parseFloat(l.Repetitions)),
		})
	}
	if err := postJSON(ctx, w.APIBaseURL+"/internal/workout/entries", entries); err != nil {
		return fmt.Errorf("post workout entries: %w", err)
	}

	slog.Info("wger sync complete", "exercises", len(exercises), "sessions", len(sessionRows), "entries", len(entries))
	return nil
}

// accessToken returns a valid wger access token, refreshing (and persisting the rotated
// refresh token) when the stored one expires within a minute.
func (w *WgerWorker) accessToken(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", w.APIBaseURL+"/internal/wger/oauth", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", errWgerNotConnected
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /internal/wger/oauth returned %d", resp.StatusCode)
	}
	var tok oauthToken
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if time.Until(tok.ExpiresAt) > time.Minute {
		return tok.AccessToken, nil
	}

	if w.ClientID == "" || w.ClientSecret == "" {
		return "", errors.New("wger token expired and WGER_OAUTH_CLIENT_ID/SECRET not set")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
	}
	treq, _ := http.NewRequestWithContext(ctx, "POST", w.WgerURL+"/identity/o/api/token", strings.NewReader(form.Encode()))
	treq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	treq.SetBasicAuth(url.QueryEscape(w.ClientID), url.QueryEscape(w.ClientSecret))
	tresp, err := http.DefaultClient.Do(treq)
	if err != nil {
		return "", err
	}
	defer tresp.Body.Close()
	body, _ := io.ReadAll(tresp.Body)
	if tresp.StatusCode != http.StatusOK {
		// Do not log the body verbatim beyond the error code; it may echo token material.
		return "", fmt.Errorf("wger token refresh returned %d — reconnect wger in reports", tresp.StatusCode)
	}
	var fresh struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &fresh); err != nil {
		return "", fmt.Errorf("decode refresh response: %w", err)
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = tok.RefreshToken
	}
	updated := oauthToken{
		AccessToken:  fresh.AccessToken,
		RefreshToken: fresh.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(fresh.ExpiresIn) * time.Second),
	}
	if err := postJSON(ctx, w.APIBaseURL+"/internal/wger/oauth", updated); err != nil {
		return "", fmt.Errorf("persist refreshed wger token: %w", err)
	}
	slog.Info("wger token refreshed")
	return updated.AccessToken, nil
}

func (w *WgerWorker) fetchExerciseInfo(ctx context.Context, token string, id int) (wgerExerciseInfo, error) {
	var info wgerExerciseInfo
	err := wgerGet(ctx, fmt.Sprintf("%s/api/v2/exerciseinfo/%d/", w.WgerURL, id), token, &info)
	return info, err
}

func exerciseName(info wgerExerciseInfo) string {
	// prefer English (language id 2), fall back to first available
	for _, t := range info.Translations {
		if t.Language == 2 && t.Name != "" {
			return t.Name
		}
	}
	if len(info.Translations) > 0 && info.Translations[0].Name != "" {
		return info.Translations[0].Name
	}
	return fmt.Sprintf("Exercise %d", info.ID)
}

func wgerGet(ctx context.Context, u, token string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s returned %d", u, resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

func wgerGetAll[T any](ctx context.Context, u, token string, out *[]T) error {
	for u != "" {
		var page wgerPage[T]
		if err := wgerGet(ctx, u, token, &page); err != nil {
			return err
		}
		*out = append(*out, page.Results...)
		u = ""
		if page.Next != nil {
			u = *page.Next
		}
	}
	return nil
}

func parseFloat(s *string) float64 {
	if s == nil {
		return 0
	}
	f, _ := strconv.ParseFloat(strings.TrimSpace(*s), 64)
	return f
}

func postJSON(ctx context.Context, u string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s returned %d", u, resp.StatusCode)
	}
	return nil
}
