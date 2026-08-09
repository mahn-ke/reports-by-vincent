package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type WorkoutExercise struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type WorkoutLogEntry struct {
	ID          int     `json:"id"`
	ExerciseID  int     `json:"exercise_id"`
	Date        string  `json:"date"`
	Weight      float64 `json:"weight"`
	Repetitions int     `json:"repetitions"`
}

type WorkoutExerciseInput struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type WorkoutLogEntryInput struct {
	ExerciseID  int     `json:"exercise_id"`
	Date        string  `json:"date"`
	Weight      float64 `json:"weight"`
	Repetitions int     `json:"repetitions"`
}

func (h *Handler) ListWorkoutExercises(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, name FROM workout_exercises ORDER BY id ASC`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var exercises []WorkoutExercise
	for rows.Next() {
		var e WorkoutExercise
		if err := rows.Scan(&e.ID, &e.Name); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		exercises = append(exercises, e)
	}
	if exercises == nil {
		exercises = []WorkoutExercise{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(exercises)
}

func (h *Handler) UpsertWorkoutExercises(w http.ResponseWriter, r *http.Request) {
	var entries []WorkoutExerciseInput
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for _, e := range entries {
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO workout_exercises (id, name, updated_at)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, updated_at = EXCLUDED.updated_at`,
			e.ID, e.Name, time.Now()); err != nil {
			http.Error(w, "upsert failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) ListWorkoutLogEntries(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, exercise_id, date, weight, repetitions FROM workout_log_entries ORDER BY date ASC, exercise_id ASC`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []WorkoutLogEntry
	for rows.Next() {
		var e WorkoutLogEntry
		var date time.Time
		if err := rows.Scan(&e.ID, &e.ExerciseID, &date, &e.Weight, &e.Repetitions); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		e.Date = date.Format("2006-01-02")
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []WorkoutLogEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (h *Handler) UpsertWorkoutLogEntries(w http.ResponseWriter, r *http.Request) {
	var entries []WorkoutLogEntryInput
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for _, e := range entries {
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO workout_log_entries (exercise_id, date, weight, repetitions)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (exercise_id, date, weight, repetitions) DO NOTHING`,
			e.ExerciseID, e.Date, e.Weight, e.Repetitions); err != nil {
			http.Error(w, "upsert failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
