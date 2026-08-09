package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type TrainingLog struct {
	ID        int       `json:"id"`
	Date      string    `json:"date"`
	CreatedAt time.Time `json:"created_at"`
}

type TrainingInput struct {
	Date string `json:"date"`
}

func (h *Handler) ListTraining(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, date, created_at FROM training_logs ORDER BY date ASC`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var logs []TrainingLog
	for rows.Next() {
		var l TrainingLog
		var date time.Time
		if err := rows.Scan(&l.ID, &date, &l.CreatedAt); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		l.Date = date.Format("2006-01-02")
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []TrainingLog{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func (h *Handler) UpsertTraining(w http.ResponseWriter, r *http.Request) {
	var entries []TrainingInput
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for _, e := range entries {
		if _, err := h.db.Exec(r.Context(),
			`INSERT INTO training_logs (date) VALUES ($1) ON CONFLICT (date) DO NOTHING`,
			e.Date); err != nil {
			http.Error(w, "upsert failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
