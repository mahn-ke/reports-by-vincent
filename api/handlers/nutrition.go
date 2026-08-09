package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type NutritionLog struct {
	ID        int       `json:"id"`
	Date      string    `json:"date"`
	Calories  float64   `json:"calories"`
	Carbs     float64   `json:"carbs"`
	Fat       float64   `json:"fat"`
	Protein   float64   `json:"protein"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NutritionInput struct {
	Date     string  `json:"date"`
	Calories float64 `json:"calories"`
	Carbs    float64 `json:"carbs"`
	Fat      float64 `json:"fat"`
	Protein  float64 `json:"protein"`
}

func (h *Handler) ListNutrition(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, date, calories, carbs, fat, protein, created_at, updated_at
		 FROM nutrition_logs ORDER BY date ASC`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var logs []NutritionLog
	for rows.Next() {
		var l NutritionLog
		var date time.Time
		if err := rows.Scan(&l.ID, &date, &l.Calories, &l.Carbs, &l.Fat, &l.Protein, &l.CreatedAt, &l.UpdatedAt); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		l.Date = date.Format("2006-01-02")
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []NutritionLog{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func (h *Handler) UpsertNutrition(w http.ResponseWriter, r *http.Request) {
	var entries []NutritionInput
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for _, e := range entries {
		// Only update if the incoming calorie count is strictly higher (mirrors existing MFP sheet logic).
		_, err := h.db.Exec(r.Context(), `
			INSERT INTO nutrition_logs (date, calories, carbs, fat, protein)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (date) DO UPDATE SET
				calories   = CASE WHEN EXCLUDED.calories > nutrition_logs.calories THEN EXCLUDED.calories   ELSE nutrition_logs.calories   END,
				carbs      = CASE WHEN EXCLUDED.calories > nutrition_logs.calories THEN EXCLUDED.carbs      ELSE nutrition_logs.carbs      END,
				fat        = CASE WHEN EXCLUDED.calories > nutrition_logs.calories THEN EXCLUDED.fat        ELSE nutrition_logs.fat        END,
				protein    = CASE WHEN EXCLUDED.calories > nutrition_logs.calories THEN EXCLUDED.protein    ELSE nutrition_logs.protein    END,
				updated_at = CASE WHEN EXCLUDED.calories > nutrition_logs.calories THEN NOW()               ELSE nutrition_logs.updated_at END`,
			e.Date, e.Calories, e.Carbs, e.Fat, e.Protein)
		if err != nil {
			http.Error(w, "upsert failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
