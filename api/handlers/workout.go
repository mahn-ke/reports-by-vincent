package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type WorkoutExercise struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Muscles  []string `json:"muscles"`
}

type WorkoutLogEntry struct {
	ID          int        `json:"id"`
	WgerID      *string    `json:"wger_id,omitempty"`
	SessionID   *string    `json:"session_id,omitempty"`
	LoggedAt    *time.Time `json:"logged_at,omitempty"`
	ExerciseID  int        `json:"exercise_id"`
	Date        string     `json:"date"`
	Weight      float64    `json:"weight"`
	Repetitions int        `json:"repetitions"`
}

type WorkoutLogEntryInput struct {
	WgerID      string     `json:"wger_id"`
	SessionID   *string    `json:"session_id"`
	LoggedAt    *time.Time `json:"logged_at"`
	ExerciseID  int        `json:"exercise_id"`
	Date        string     `json:"date"`
	Weight      float64    `json:"weight"`
	Repetitions int        `json:"repetitions"`
}

type WorkoutSession struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	DayName string `json:"day_name"`
}

type WgerOAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) ListWorkoutExercises(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, name, category, muscles FROM workout_exercises ORDER BY id ASC`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	exercises := []WorkoutExercise{}
	for rows.Next() {
		var e WorkoutExercise
		if err := rows.Scan(&e.ID, &e.Name, &e.Category, &e.Muscles); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		exercises = append(exercises, e)
	}
	writeJSON(w, exercises)
}

func (h *Handler) UpsertWorkoutExercises(w http.ResponseWriter, r *http.Request) {
	var entries []WorkoutExercise
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for _, e := range entries {
		if e.Muscles == nil {
			e.Muscles = []string{}
		}
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO workout_exercises (id, name, category, muscles, updated_at)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, category = EXCLUDED.category,
			   muscles = EXCLUDED.muscles, updated_at = EXCLUDED.updated_at`,
			e.ID, e.Name, e.Category, e.Muscles, time.Now()); err != nil {
			http.Error(w, "upsert failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) ListWorkoutLogEntries(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, wger_id, session_id, logged_at, exercise_id, date, weight, repetitions
		 FROM workout_log_entries ORDER BY date ASC, logged_at ASC NULLS LAST, id ASC`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := []WorkoutLogEntry{}
	for rows.Next() {
		var e WorkoutLogEntry
		var date time.Time
		if err := rows.Scan(&e.ID, &e.WgerID, &e.SessionID, &e.LoggedAt, &e.ExerciseID, &date, &e.Weight, &e.Repetitions); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		e.Date = date.Format("2006-01-02")
		entries = append(entries, e)
	}
	writeJSON(w, entries)
}

// UpsertWorkoutLogEntries replaces the synced set list: entries are upserted by wger id and
// rows whose wger id is no longer present (deleted in wger) are removed.
func (h *Handler) UpsertWorkoutLogEntries(w http.ResponseWriter, r *http.Request) {
	var entries []WorkoutLogEntryInput
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "begin failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.WgerID)
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO workout_log_entries (wger_id, session_id, logged_at, exercise_id, date, weight, repetitions)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (wger_id) DO UPDATE SET session_id = EXCLUDED.session_id, logged_at = EXCLUDED.logged_at,
			   exercise_id = EXCLUDED.exercise_id, date = EXCLUDED.date,
			   weight = EXCLUDED.weight, repetitions = EXCLUDED.repetitions`,
			e.WgerID, e.SessionID, e.LoggedAt, e.ExerciseID, e.Date, e.Weight, e.Repetitions); err != nil {
			http.Error(w, "upsert failed", http.StatusInternalServerError)
			return
		}
	}
	if _, err := tx.Exec(r.Context(),
		`DELETE FROM workout_log_entries WHERE NOT (wger_id = ANY($1))`, ids); err != nil {
		http.Error(w, "prune failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "commit failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) ListWorkoutSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, date, day_name FROM workout_sessions ORDER BY date ASC, id ASC`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	sessions := []WorkoutSession{}
	for rows.Next() {
		var s WorkoutSession
		var date time.Time
		if err := rows.Scan(&s.ID, &date, &s.DayName); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		s.Date = date.Format("2006-01-02")
		sessions = append(sessions, s)
	}
	writeJSON(w, sessions)
}

func (h *Handler) UpsertWorkoutSessions(w http.ResponseWriter, r *http.Request) {
	var sessions []WorkoutSession
	if err := json.NewDecoder(r.Body).Decode(&sessions); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for _, s := range sessions {
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO workout_sessions (id, date, day_name) VALUES ($1, $2, $3)
			 ON CONFLICT (id) DO UPDATE SET date = EXCLUDED.date, day_name = EXCLUDED.day_name`,
			s.ID, s.Date, s.DayName); err != nil {
			http.Error(w, "upsert failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// GetWgerOAuth returns the stored wger OAuth token, or 404 if reports is not connected to wger.
// Internal-only: never register this under the external /api routes.
func (h *Handler) GetWgerOAuth(w http.ResponseWriter, r *http.Request) {
	var t WgerOAuthToken
	err := h.db.QueryRow(r.Context(),
		`SELECT access_token, refresh_token, expires_at FROM wger_oauth WHERE id = 1`).
		Scan(&t.AccessToken, &t.RefreshToken, &t.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "not connected", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, t)
}

func (h *Handler) PutWgerOAuth(w http.ResponseWriter, r *http.Request) {
	var t WgerOAuthToken
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil || t.AccessToken == "" || t.RefreshToken == "" {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if _, err := h.db.Exec(r.Context(),
		`INSERT INTO wger_oauth (id, access_token, refresh_token, expires_at, updated_at)
		 VALUES (1, $1, $2, $3, NOW())
		 ON CONFLICT (id) DO UPDATE SET access_token = EXCLUDED.access_token,
		   refresh_token = EXCLUDED.refresh_token, expires_at = EXCLUDED.expires_at, updated_at = NOW()`,
		t.AccessToken, t.RefreshToken, t.ExpiresAt); err != nil {
		http.Error(w, "upsert failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type WorkoutPlanDay struct {
	DayID       int             `json:"day_id"`
	RoutineID   int             `json:"routine_id"`
	Name        string          `json:"name"`
	IsRest      bool            `json:"is_rest"`
	ExerciseIDs []int           `json:"exercise_ids"`
	Entries     json.RawMessage `json:"entries"` // [{slot_entry_id, exercise_id, sets: [{iteration, value}]}]
}

func (h *Handler) ListWorkoutPlanDays(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT day_id, routine_id, name, is_rest, exercise_ids, entries FROM workout_plan_days ORDER BY routine_id, day_id`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	days := []WorkoutPlanDay{}
	for rows.Next() {
		var d WorkoutPlanDay
		if err := rows.Scan(&d.DayID, &d.RoutineID, &d.Name, &d.IsRest, &d.ExerciseIDs, &d.Entries); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		days = append(days, d)
	}
	writeJSON(w, days)
}

// ReplaceWorkoutPlanDays stores the full set of wger routine days, removing days deleted in wger.
func (h *Handler) ReplaceWorkoutPlanDays(w http.ResponseWriter, r *http.Request) {
	var days []WorkoutPlanDay
	if err := json.NewDecoder(r.Body).Decode(&days); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		http.Error(w, "begin failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `DELETE FROM workout_plan_days`); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	for _, d := range days {
		if d.ExerciseIDs == nil {
			d.ExerciseIDs = []int{}
		}
		if len(d.Entries) == 0 || string(d.Entries) == "null" {
			d.Entries = json.RawMessage("[]")
		}
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO workout_plan_days (day_id, routine_id, name, is_rest, exercise_ids, entries) VALUES ($1, $2, $3, $4, $5, $6)`,
			d.DayID, d.RoutineID, d.Name, d.IsRest, d.ExerciseIDs, string(d.Entries)); err != nil {
			http.Error(w, "insert failed", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "commit failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
