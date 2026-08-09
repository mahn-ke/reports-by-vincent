package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type BodyMeasurement struct {
	ID                 int       `json:"id"`
	MeasuredAt         time.Time `json:"measured_at"`
	Weight             float64   `json:"weight"`
	BMI                float64   `json:"bmi"`
	BodyFat            float64   `json:"body_fat"`
	SkeletalMuscleMass float64   `json:"skeletal_muscle_mass"`
	BoneMass           float64   `json:"bone_mass"`
	BodyWater          float64   `json:"body_water"`
	CreatedAt          time.Time `json:"created_at"`
}

type BodyInput struct {
	MeasuredAt         string  `json:"measured_at"`
	Weight             float64 `json:"weight"`
	BMI                float64 `json:"bmi"`
	BodyFat            float64 `json:"body_fat"`
	SkeletalMuscleMass float64 `json:"skeletal_muscle_mass"`
	BoneMass           float64 `json:"bone_mass"`
	BodyWater          float64 `json:"body_water"`
}

func (h *Handler) ListBody(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, measured_at, weight, bmi, body_fat, skeletal_muscle_mass, bone_mass, body_water, created_at
		 FROM body_measurements ORDER BY measured_at ASC`)
	if err != nil {
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var measurements []BodyMeasurement
	for rows.Next() {
		var m BodyMeasurement
		if err := rows.Scan(&m.ID, &m.MeasuredAt, &m.Weight, &m.BMI, &m.BodyFat,
			&m.SkeletalMuscleMass, &m.BoneMass, &m.BodyWater, &m.CreatedAt); err != nil {
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		measurements = append(measurements, m)
	}
	if measurements == nil {
		measurements = []BodyMeasurement{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(measurements)
}

func (h *Handler) UpsertBody(w http.ResponseWriter, r *http.Request) {
	var entries []BodyInput
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for _, e := range entries {
		_, err := h.db.Exec(r.Context(), `
			INSERT INTO body_measurements
				(measured_at, weight, bmi, body_fat, skeletal_muscle_mass, bone_mass, body_water)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (measured_at) DO UPDATE SET
				weight               = EXCLUDED.weight,
				bmi                  = EXCLUDED.bmi,
				body_fat             = EXCLUDED.body_fat,
				skeletal_muscle_mass = EXCLUDED.skeletal_muscle_mass,
				bone_mass            = EXCLUDED.bone_mass,
				body_water           = EXCLUDED.body_water`,
			e.MeasuredAt, e.Weight, e.BMI, e.BodyFat, e.SkeletalMuscleMass, e.BoneMass, e.BodyWater)
		if err != nil {
			http.Error(w, "upsert failed", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
