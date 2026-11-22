package workers

import (
	"encoding/json"
	"testing"
)

// Shapes as served by wger 2.7 (/api/v2/schema): log and session ids are UUID strings,
// weight/repetitions are nullable decimal strings, date is a date-time.
func TestDecodeWger27Shapes(t *testing.T) {
	var logs wgerPage[wgerWorkoutLog]
	if err := json.Unmarshal([]byte(`{"next":null,"results":[
		{"id":"0b6e7a52-8f0d-4c55-9a43-3b8f5f2d1a01","exercise":73,"session":"5c1f2f0e-6d0b-4a0c-bb6c-2a9f3c4d5e60",
		 "date":"2026-11-20T18:04:00+01:00","weight":"62.50","repetitions":"10.00","repetitions_unit":1,"weight_unit":1},
		{"id":"0b6e7a52-8f0d-4c55-9a43-3b8f5f2d1a02","exercise":12,"session":null,
		 "date":"2026-11-20T18:10:00+01:00","weight":null,"repetitions":"12","repetitions_unit":null,"weight_unit":null}]}`), &logs); err != nil {
		t.Fatalf("decode workoutlog page: %v", err)
	}
	l := logs.Results[0]
	if l.ID == "" || l.Session == nil || parseFloat(l.Weight) != 62.5 || int(parseFloat(l.Repetitions)) != 10 {
		t.Errorf("unexpected log decode: %+v", l)
	}
	if parseFloat(logs.Results[1].Weight) != 0 || logs.Results[1].Session != nil {
		t.Errorf("nullable fields not handled: %+v", logs.Results[1])
	}

	var sessions wgerPage[wgerSession]
	if err := json.Unmarshal([]byte(`{"next":null,"results":[
		{"id":"5c1f2f0e-6d0b-4a0c-bb6c-2a9f3c4d5e60","day":4,"datetime_start":"2026-11-20T17:58:00+01:00"},
		{"id":"5c1f2f0e-6d0b-4a0c-bb6c-2a9f3c4d5e61","day":null,"datetime_start":null}]}`), &sessions); err != nil {
		t.Fatalf("decode workoutsession page: %v", err)
	}

	var info wgerExerciseInfo
	if err := json.Unmarshal([]byte(`{"id":73,"category":{"id":11,"name":"Chest"},
		"muscles":[{"id":4,"name":"Pectoralis major","name_en":"Chest"}],
		"translations":[{"language":2,"name":"Bench Press"}]}`), &info); err != nil {
		t.Fatalf("decode exerciseinfo: %v", err)
	}
	if exerciseName(info) != "Bench Press" || info.Category.Name != "Chest" {
		t.Errorf("unexpected exerciseinfo decode: %+v", info)
	}
}
