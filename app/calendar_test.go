package main

import (
	"encoding/json"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

func TestDayTypeFromName(t *testing.T) {
	cases := map[string]string{
		"Push":        dayPush,
		"Push A":      dayPush,
		"pull day":    dayPull,
		"Legs":        dayLegs,
		"Leg day":     dayLegs,
		"Upper body":  dayUpper,
		"":            "",
		"Pushups 100": "",
	}
	for name, want := range cases {
		if got := dayTypeFromName(name); got != want {
			t.Errorf("dayTypeFromName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestExerciseType(t *testing.T) {
	cases := []struct {
		ex   WorkoutExercise
		want string
	}{
		{WorkoutExercise{Muscles: []string{"Chest", "Triceps"}}, dayPush},
		{WorkoutExercise{Muscles: []string{"Lats", "Biceps"}}, dayPull},
		{WorkoutExercise{Muscles: []string{"Hamstrings", "Glutes"}}, dayLegs},
		{WorkoutExercise{Muscles: []string{"Biceps femoris"}}, dayLegs}, // not the arm biceps
		{WorkoutExercise{Category: "Arms", Muscles: []string{"Triceps"}}, dayPush},
		{WorkoutExercise{Category: "Back"}, dayPull}, // no muscles: category fallback
		{WorkoutExercise{Category: "Arms"}, ""},      // ambiguous
		{WorkoutExercise{Category: "Abs", Muscles: []string{"Abs"}}, ""},
		{WorkoutExercise{Muscles: []string{"Chest", "Lats"}}, ""}, // tie, no category
	}
	for i, c := range cases {
		if got := exerciseType(c.ex); got != c.want {
			t.Errorf("case %d: exerciseType(%+v) = %q, want %q", i, c.ex, got, c.want)
		}
	}
}

func TestCalendarTemplateRenders(t *testing.T) {
	days := buildCalendar([]WorkoutLogEntry{{Date: "2026-11-02", ExerciseID: 1, Weight: 60, Repetitions: 10}},
		[]WorkoutExercise{{ID: 1, Name: "</script><b>x", Category: "Chest"}}, nil, nil)
	daysJ, _ := json.Marshal(days)
	rec := httptest.NewRecorder()
	renderTemplate(rec, "calendar", calendarPageData{Days: template.JS(daysJ), SyncStatus: template.JS(`{"state":"none"}`), OAuthEnabled: true})
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Connect wger") || !strings.Contains(body, `const DAYS = [{"date":"2026-11-02"`) {
		t.Fatalf("unexpected render (%d): %.400s", rec.Code, body)
	}
	if strings.Contains(body, "</script><b>") {
		t.Fatal("exercise name not escaped inside script")
	}
}

func TestMuscleDayTypeFullBody(t *testing.T) {
	cases := []struct {
		sets map[string]int
		want string
	}{
		{map[string]int{dayPull: 21}, dayPull},
		{map[string]int{dayPush: 14, dayPull: 4}, dayPush}, // 78% push
		{map[string]int{dayPush: 8, dayPull: 4, dayLegs: 8}, dayFull},
		{map[string]int{dayLegs: 10, dayPush: 6}, dayLegs}, // 62.5% legs is still a majority
		{map[string]int{dayLegs: 5, dayPush: 5}, dayFull},  // tie between two groups
		{map[string]int{dayPush: 9, dayPull: 8, dayLegs: 1}, dayUpper},
		{map[string]int{}, ""},
	}
	for i, c := range cases {
		if got := muscleDayType(c.sets); got != c.want {
			t.Errorf("case %d %v: got %q, want %q", i, c.sets, got, c.want)
		}
	}
}

func TestPullNameOverrides(t *testing.T) {
	for _, name := range []string{"Pec deck rear delt fly", "Rear Delt Raises", "Reverse Pec Deck", "Pullover", "Face Pull"} {
		if got := exerciseType(WorkoutExercise{Name: name, Category: "Shoulders", Muscles: []string{"Chest"}}); got != dayPull {
			t.Errorf("%s: got %q, want pull", name, got)
		}
	}
	for name, want := range map[string]string{"Full Body": dayFull, "Lower #1": dayLegs, "Upper #2": dayUpper, "New day 6": ""} {
		if got := dayTypeFromName(name); got != want {
			t.Errorf("%s -> %q, want %q", name, got, want)
		}
	}
}
