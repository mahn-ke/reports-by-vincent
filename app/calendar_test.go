package main

import (
	"encoding/json"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
)

func intp(i int) *int { return &i }

func TestDayTypeFromName(t *testing.T) {
	cases := map[string]string{
		"Push":        dayPush,
		"Push A":      dayPush,
		"pull day":    dayPull,
		"Legs":        dayLegs,
		"Leg day":     dayLegs,
		"Upper body":  "",
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

func TestBuildCalendar(t *testing.T) {
	exercises := []WorkoutExercise{
		{ID: 1, Name: "Bench", Category: "Chest", Muscles: []string{"Chest"}},
		{ID: 2, Name: "Row", Category: "Back", Muscles: []string{"Lats"}},
		{ID: 3, Name: "Squat", Category: "Legs", Muscles: []string{"Quads"}},
		{ID: 4, Name: "Pushup", Category: "Chest", Muscles: []string{"Chest"}},
		{ID: 5, Name: "Plank", Category: "Abs"},
	}
	sessions := []WorkoutSession{{ID: 10, Date: "2026-11-09", DayName: "Pull"}}
	set := func(date string, ex int, w float64, reps int) WorkoutLogEntry {
		return WorkoutLogEntry{Date: date, ExerciseID: ex, Weight: w, Repetitions: reps}
	}
	entries := []WorkoutLogEntry{
		// push #1: 2 identical sets must both count -> 1200
		set("2026-11-02", 1, 60, 10), set("2026-11-02", 1, 60, 10),
		set("2026-11-03", 2, 50, 10), // pull #1: 500
		set("2026-11-04", 3, 100, 5), // legs #1: 500
		// push #2: 1250 > 1200 -> up
		set("2026-11-05", 1, 62.5, 10), set("2026-11-05", 1, 62.5, 10),
		// pull #2: same volume -> same
		set("2026-11-06", 2, 50, 10),
		// legs #2: less -> down
		set("2026-11-07", 3, 100, 4),
		// abs only: unclassified
		set("2026-11-08", 5, 0, 30),
		// session day name "Pull" wins over the chest muscles of the exercise -> pull, 600 > 500 -> up
		{Date: "2026-11-09", ExerciseID: 1, Weight: 60, Repetitions: 10, SessionID: intp(10)},
		// zero reps are ignored entirely
		set("2026-11-10", 1, 60, 0),
		// bodyweight push days compare reps: first bodyweight-only push vs push #2 (1250 kg) -> down
		set("2026-11-11", 4, 0, 20),
	}
	days := buildCalendar(entries, exercises, sessions)

	want := []struct{ date, typ, status, prev string }{
		{"2026-11-02", dayPush, statusFirst, ""},
		{"2026-11-03", dayPull, statusFirst, ""},
		{"2026-11-04", dayLegs, statusFirst, ""},
		{"2026-11-05", dayPush, statusUp, "2026-11-02"},
		{"2026-11-06", dayPull, statusSame, "2026-11-03"},
		{"2026-11-07", dayLegs, statusDown, "2026-11-04"},
		{"2026-11-08", "", statusFirst, ""},
		{"2026-11-09", dayPull, statusUp, "2026-11-06"},
		{"2026-11-11", dayPush, statusDown, "2026-11-05"},
	}
	if len(days) != len(want) {
		t.Fatalf("got %d days, want %d: %+v", len(days), len(want), days)
	}
	for i, w := range want {
		d := days[i]
		if d.Date != w.date || d.Type != w.typ || d.Status != w.status || d.PrevDate != w.prev {
			t.Errorf("day %d = {%s %s %s prev=%s}, want {%s %s %s prev=%s}",
				i, d.Date, d.Type, d.Status, d.PrevDate, w.date, w.typ, w.status, w.prev)
		}
	}
	if days[0].Volume != 1200 || days[0].Exercises[0].Sets != 2 {
		t.Errorf("duplicate sets not counted: %+v", days[0])
	}
	if days[7].Source != "routine" {
		t.Errorf("expected routine source for session-linked day, got %q", days[7].Source)
	}
	if pv := days[3].Exercises[0].PrevVolume; pv == nil || *pv != 1200 {
		t.Errorf("expected per-exercise prevVolume 1200, got %v", pv)
	}
}

func TestCompareDaysBodyweight(t *testing.T) {
	if got := compareDays(0, 25, 0, 20); got != statusUp {
		t.Errorf("bodyweight reps up: got %q", got)
	}
	if got := compareDays(0, 20, 0, 20); got != statusSame {
		t.Errorf("bodyweight reps same: got %q", got)
	}
	if got := compareDays(100.04, 0, 100.0, 0); got != statusSame {
		t.Errorf("rounding to 0.1 should be same: got %q", got)
	}
}

func TestCalendarTemplateRenders(t *testing.T) {
	days := buildCalendar([]WorkoutLogEntry{{Date: "2026-11-02", ExerciseID: 1, Weight: 60, Repetitions: 10}},
		[]WorkoutExercise{{ID: 1, Name: "</script><b>x", Category: "Chest"}}, nil)
	daysJ, _ := json.Marshal(days)
	rec := httptest.NewRecorder()
	renderTemplate(rec, "calendar", calendarPageData{Days: template.JS(daysJ), OAuthEnabled: true})
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Connect wger") || !strings.Contains(body, `const DAYS = [{"date":"2026-11-02"`) {
		t.Fatalf("unexpected render (%d): %.400s", rec.Code, body)
	}
	if strings.Contains(body, "</script><b>") {
		t.Fatal("exercise name not escaped inside script")
	}
}
