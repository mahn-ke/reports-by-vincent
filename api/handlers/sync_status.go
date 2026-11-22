package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type WgerSyncStatus struct {
	State      string     `json:"state"`
	Phase      string     `json:"phase"`
	Percent    float64    `json:"percent"`
	Message    string     `json:"message"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

var syncStates = map[string]bool{"queued": true, "running": true, "done": true, "failed": true, "skipped": true}

// GetWgerSyncStatus returns the progress of the current/last wger sync ({"state":"none"} before the first one).
func (h *Handler) GetWgerSyncStatus(w http.ResponseWriter, r *http.Request) {
	var s WgerSyncStatus
	err := h.db.QueryRow(r.Context(),
		`SELECT state, phase, percent, message, started_at, finished_at, updated_at FROM wger_sync_status WHERE id = 1`).
		Scan(&s.State, &s.Phase, &s.Percent, &s.Message, &s.StartedAt, &s.FinishedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, map[string]string{"state": "none"})
		return
	}
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, s)
}

// PutWgerSyncStatus stores the sync progress; updated_at is always set by the database.
func (h *Handler) PutWgerSyncStatus(w http.ResponseWriter, r *http.Request) {
	var s WgerSyncStatus
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil || !syncStates[s.State] {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	if s.Percent < 0 {
		s.Percent = 0
	} else if s.Percent > 100 {
		s.Percent = 100
	}
	if len(s.Message) > 500 {
		s.Message = s.Message[:500]
	}
	if _, err := h.db.Exec(r.Context(),
		`INSERT INTO wger_sync_status (id, state, phase, percent, message, started_at, finished_at, updated_at)
		 VALUES (1, $1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (id) DO UPDATE SET state = EXCLUDED.state, phase = EXCLUDED.phase, percent = EXCLUDED.percent,
		   message = EXCLUDED.message, started_at = EXCLUDED.started_at, finished_at = EXCLUDED.finished_at, updated_at = NOW()`,
		s.State, s.Phase, s.Percent, s.Message, s.StartedAt, s.FinishedAt); err != nil {
		http.Error(w, "upsert failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
