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

// Log and session ids are UUIDs since wger 2.7 (PowerSync); day and exercise ids stay integers.
type wgerWorkoutLog struct {
	ID              string  `json:"id"`
	Exercise        int     `json:"exercise"`
	Session         *string `json:"session"`
	Date            string  `json:"date"`
	Weight          *string `json:"weight"`
	Repetitions     *string `json:"repetitions"`
	RepetitionsUnit *int    `json:"repetitions_unit"`
	WeightUnit      *int    `json:"weight_unit"`
}

type wgerSession struct {
	ID            string  `json:"id"`
	Day           *int    `json:"day"`
	DatetimeStart *string `json:"datetime_start"`
}

type wgerDay struct {
	ID      int    `json:"id"`
	Routine int    `json:"routine"`
	Name    string `json:"name"`
	IsRest  bool   `json:"is_rest"`
}

type wgerSlot struct {
	ID  int `json:"id"`
	Day int `json:"day"`
}

type wgerSlotEntry struct {
	ID       int `json:"id"`
	Slot     int `json:"slot"`
	Exercise int `json:"exercise"`
}

// wgerSetsConfig is the planned number of sets of a slot entry from an iteration on ("r" = replace).
type wgerSetsConfig struct {
	SlotEntry int    `json:"slot_entry"`
	Iteration int    `json:"iteration"`
	Value     *int   `json:"value"`
	Operation string `json:"operation"`
}

type wgerPage[T any] struct {
	Count   int     `json:"count"`
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

// Progress ranges (percent of the whole sync) per phase, in the order the sync runs them.
const (
	progLogsFrom, progLogsTo         = 2, 30
	progSessionsFrom, progSessionsTo = 30, 38
	progExerciseFrom, progExerciseTo = 38, 78
	progPlanFrom, progPlanTo         = 80, 88
)

func (w *WgerWorker) Work(ctx context.Context, job *river.Job[WgerArgs]) error {
	p := newSyncProgress(w.APIBaseURL)
	p.step(ctx, "Connecting to wger", 0, 0, 0, 0)
	err := w.sync(ctx, p)
	switch {
	case errors.Is(err, errWgerNotConnected):
		slog.Warn("wger not connected — open /workout/calendar in reports and click \"Connect wger\"; skipping")
		p.finish(ctx, "skipped", "wger is not connected")
		return nil
	case err != nil:
		msg := err.Error()
		if job.Attempt < job.MaxAttempts {
			msg += fmt.Sprintf(" (attempt %d of %d, retrying automatically)", job.Attempt, job.MaxAttempts)
		}
		p.finish(ctx, "failed", msg)
		return err
	}
	return nil
}

func (w *WgerWorker) sync(ctx context.Context, p *syncProgress) error {
	token, err := w.accessToken(ctx)
	if errors.Is(err, errWgerNotConnected) {
		return err
	}
	if err != nil {
		return fmt.Errorf("wger token: %w", err)
	}

	var logs []wgerWorkoutLog
	if err := wgerGetAll(ctx, w.WgerURL+"/api/v2/workoutlog/?ordering=date&limit=200", token, &logs, func(done, total int) {
		p.step(ctx, "Fetching workout logs", progLogsFrom, progLogsTo, done, total)
	}); err != nil {
		return fmt.Errorf("wger fetch logs: %w", err)
	}
	if len(logs) == 0 {
		slog.Info("wger returned no workout logs")
		p.finish(ctx, "done", "wger has no workout logs")
		return nil
	}

	var sessions []wgerSession
	if err := wgerGetAll(ctx, w.WgerURL+"/api/v2/workoutsession/?limit=200", token, &sessions, func(done, total int) {
		p.step(ctx, "Fetching workout sessions", progSessionsFrom, progSessionsTo, done, total)
	}); err != nil {
		return fmt.Errorf("wger fetch sessions: %w", err)
	}

	// Exercises: names plus category/muscles for push/pull/legs classification.
	seen := map[int]bool{}
	exerciseIDs := []int{}
	for _, l := range logs {
		if !seen[l.Exercise] {
			seen[l.Exercise] = true
			exerciseIDs = append(exerciseIDs, l.Exercise)
		}
	}
	exercises := []map[string]any{}
	for i, id := range exerciseIDs {
		p.step(ctx, "Fetching exercise details", progExerciseFrom, progExerciseTo, i, len(exerciseIDs))
		ex := map[string]any{"id": id, "name": fmt.Sprintf("Exercise %d", id), "category": "", "muscles": []string{}}
		info, err := w.fetchExerciseInfo(ctx, token, id)
		if err != nil {
			slog.Warn("wger exercise info failed", "exercise_id", id, "err", err)
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
	p.step(ctx, "Saving exercises", progExerciseTo, progExerciseTo, 0, 0)
	if err := postJSON(ctx, w.APIBaseURL+"/internal/workout/exercises", exercises); err != nil {
		return fmt.Errorf("post workout exercises: %w", err)
	}

	// Sessions with their routine day name ("Push", "Pull", "Legs", ...).
	firstLogDate := map[string]string{}
	for _, l := range logs {
		if l.Session != nil {
			if _, ok := firstLogDate[*l.Session]; !ok {
				firstLogDate[*l.Session] = l.Date
			}
		}
	}
	// Routine days and the exercises they plan: the calendar matches logged days against them.
	var days []wgerDay
	var slots []wgerSlot
	var slotEntries []wgerSlotEntry
	var setsConfigs []wgerSetsConfig
	planFetches := []func() error{
		func() error { return wgerGetAll(ctx, w.WgerURL+"/api/v2/day/?limit=200", token, &days, nil) },
		func() error { return wgerGetAll(ctx, w.WgerURL+"/api/v2/slot/?limit=500", token, &slots, nil) },
		func() error {
			return wgerGetAll(ctx, w.WgerURL+"/api/v2/slot-entry/?limit=1000", token, &slotEntries, nil)
		},
		func() error {
			return wgerGetAll(ctx, w.WgerURL+"/api/v2/sets-config/?limit=1000", token, &setsConfigs, nil)
		},
	}
	for i, f := range planFetches {
		p.step(ctx, "Fetching routine plan", progPlanFrom, progPlanTo, i, len(planFetches))
		if err := f(); err != nil {
			return fmt.Errorf("wger fetch routine plan: %w", err)
		}
	}
	dayNames := map[int]string{}
	for _, d := range days {
		dayNames[d.ID] = d.Name
	}
	p.step(ctx, "Saving routine plan", 90, 90, 0, 0)
	if err := postJSON(ctx, w.APIBaseURL+"/internal/workout/plan", planDays(days, slots, slotEntries, setsConfigs)); err != nil {
		return fmt.Errorf("post workout plan: %w", err)
	}

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
			name = dayNames[*s.Day]
		}
		sessionRows = append(sessionRows, map[string]any{"id": s.ID, "date": date, "day_name": name})
	}
	p.step(ctx, "Saving sessions", 92, 92, 0, 0)
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
		var loggedAt *time.Time
		if t, err := time.Parse(time.RFC3339, l.Date); err == nil {
			loggedAt = &t
		}
		entries = append(entries, map[string]any{
			"wger_id":     l.ID,
			"session_id":  l.Session,
			"logged_at":   loggedAt,
			"exercise_id": l.Exercise,
			"date":        l.Date[:min(10, len(l.Date))],
			"weight":      weight,
			"repetitions": int(parseFloat(l.Repetitions)),
		})
	}
	p.step(ctx, "Saving sets", 95, 95, 0, 0)
	if err := postJSON(ctx, w.APIBaseURL+"/internal/workout/entries", entries); err != nil {
		return fmt.Errorf("post workout entries: %w", err)
	}

	slog.Info("wger sync complete", "exercises", len(exercises), "sessions", len(sessionRows), "entries", len(entries), "plan_days", len(days))
	p.finish(ctx, "done", fmt.Sprintf("Synced %d sets from %d sessions", len(entries), len(sessionRows)))
	return nil
}

type planSets struct {
	Iteration int `json:"iteration"`
	Value     int `json:"value"`
}

type planEntry struct {
	SlotEntryID int        `json:"slot_entry_id"`
	ExerciseID  int        `json:"exercise_id"`
	Sets        []planSets `json:"sets"`
}

// planDays joins routine days with their slot entries and planned sets into
// [{day_id, routine_id, name, is_rest, exercise_ids, entries}]. Only replace ("r") set configs are
// kept: wger's other operations add to or subtract from the previous iteration.
func planDays(days []wgerDay, slots []wgerSlot, entries []wgerSlotEntry, setsConfigs []wgerSetsConfig) []map[string]any {
	slotDay := make(map[int]int, len(slots))
	for _, s := range slots {
		slotDay[s.ID] = s.Day
	}
	sets := map[int][]planSets{}
	for _, c := range setsConfigs {
		if c.Value != nil && (c.Operation == "" || c.Operation == "r") {
			sets[c.SlotEntry] = append(sets[c.SlotEntry], planSets{Iteration: c.Iteration, Value: *c.Value})
		}
	}
	exercises := map[int][]int{}
	dayEntries := map[int][]planEntry{}
	seen := map[[2]int]bool{}
	for _, e := range entries {
		day, ok := slotDay[e.Slot]
		if !ok {
			continue
		}
		ps := sets[e.ID]
		if ps == nil {
			ps = []planSets{}
		}
		dayEntries[day] = append(dayEntries[day], planEntry{SlotEntryID: e.ID, ExerciseID: e.Exercise, Sets: ps})
		if !seen[[2]int{day, e.Exercise}] {
			seen[[2]int{day, e.Exercise}] = true
			exercises[day] = append(exercises[day], e.Exercise)
		}
	}
	out := make([]map[string]any, 0, len(days))
	for _, d := range days {
		ids := exercises[d.ID]
		if ids == nil {
			ids = []int{}
		}
		pe := dayEntries[d.ID]
		if pe == nil {
			pe = []planEntry{}
		}
		out = append(out, map[string]any{"day_id": d.ID, "routine_id": d.Routine, "name": d.Name, "is_rest": d.IsRest,
			"exercise_ids": ids, "entries": pe})
	}
	return out
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

// wgerGetAll follows wger's pagination; onPage (optional) gets the items fetched so far and the total.
func wgerGetAll[T any](ctx context.Context, u, token string, out *[]T, onPage func(done, total int)) error {
	for u != "" {
		var page wgerPage[T]
		if err := wgerGet(ctx, u, token, &page); err != nil {
			return err
		}
		*out = append(*out, page.Results...)
		if onPage != nil {
			onPage(len(*out), page.Count)
		}
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
