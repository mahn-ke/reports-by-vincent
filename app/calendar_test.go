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

func TestBuildCalendar(t *testing.T) {
	exercises := []WorkoutExercise{
		{ID: 1, Name: "Bench", Category: "Chest", Muscles: []string{"Chest"}},
		{ID: 2, Name: "Row", Category: "Back", Muscles: []string{"Lats"}},
		{ID: 3, Name: "Squat", Category: "Legs", Muscles: []string{"Quads"}},
		{ID: 4, Name: "Pushup", Category: "Chest", Muscles: []string{"Chest"}},
		{ID: 5, Name: "Plank", Category: "Abs"},
	}
	sessions := []WorkoutSession{{ID: "3f2b0c9e-0000-4000-8000-000000000010", Date: "2026-11-09", DayName: "Pull"}}
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
		// session attached to routine day "Pull", but the muscles say push (wger's schedule, not
		// what was trained): muscles win -> push, 600 < 1250 -> down
		{Date: "2026-11-09", ExerciseID: 1, Weight: 60, Repetitions: 10, SessionID: strp("3f2b0c9e-0000-4000-8000-000000000010")},
		// zero reps are ignored entirely
		set("2026-11-10", 1, 60, 0),
		// bodyweight push days compare reps: first bodyweight-only push vs push #2 (1250 kg) -> down
		set("2026-11-11", 4, 0, 20),
	}
	days := buildCalendar(entries, exercises, sessions, nil)

	want := []struct{ date, typ, status, prev string }{
		{"2026-11-02", dayPush, statusFirst, ""},
		{"2026-11-03", dayPull, statusFirst, ""},
		{"2026-11-04", dayLegs, statusFirst, ""},
		{"2026-11-05", dayPush, statusUp, "2026-11-02"},
		{"2026-11-06", dayPull, statusSame, "2026-11-03"},
		{"2026-11-07", dayLegs, statusDown, "2026-11-04"},
		{"2026-11-08", "", statusFirst, ""},
		{"2026-11-09", dayPush, statusDown, "2026-11-05"},
		{"2026-11-11", dayPush, statusDown, "2026-11-09"},
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
	if days[7].Source != sourceMuscles {
		t.Errorf("expected muscles to beat the session's day name, got %q", days[7].Source)
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
		[]WorkoutExercise{{ID: 1, Name: "</script><b>x", Category: "Chest"}}, nil, nil)
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

// Real case from 2026-11-22: a pull workout (exactly the routine's Pull day) logged into a session
// that wger had attached to the routine day "Legs".
func TestPlanMatchBeatsSessionDay(t *testing.T) {
	exercises := []WorkoutExercise{
		{ID: 94, Name: "Biceps Curls With SZ-bar", Category: "Arms", Muscles: []string{"Biceps"}},
		{ID: 158, Name: "Close-grip Lat Pull Down", Category: "Back", Muscles: []string{"Lats"}},
		{ID: 272, Name: "Hammer Curls", Category: "Arms", Muscles: []string{"Biceps"}},
		{ID: 1186, Name: "One Arm Bent Row", Category: "Back", Muscles: []string{"Lats"}},
		{ID: 1627, Name: "Pec deck rear delt fly", Category: "Shoulders"},
		{ID: 369, Name: "Leg Extension", Category: "Legs", Muscles: []string{"Quads"}},
		{ID: 507, Name: "Romanian Deadlift", Category: "Legs", Muscles: []string{"Hamstrings", "Glutes"}},
		{ID: 989, Name: "Bulgarian split squats right", Category: "Legs", Muscles: []string{"Glutes", "Quads"}},
		{ID: 590, Name: "Sitting Calf Raises", Category: "Calves", Muscles: []string{"Soleus"}},
		{ID: 173, Name: "Crunches With Cable", Category: "Abs", Muscles: []string{"Abs"}},
	}
	plans := []WorkoutPlanDay{
		{DayID: 2, Name: "Legs", ExerciseIDs: []int{989, 173, 369, 507, 590}},
		{DayID: 4, Name: "Pull", ExerciseIDs: []int{94, 158, 272, 1186, 1627}},
		{DayID: 5, Name: "Full Body", IsRest: true},
	}
	legsSession := "01a20066-0000-4000-8000-000000000000"
	sessions := []WorkoutSession{{ID: legsSession, Date: "2026-11-22", DayName: "Legs"}}
	var entries []WorkoutLogEntry
	add := func(date string, ex int, w float64, reps, sets int, session *string) {
		for i := 0; i < sets; i++ {
			entries = append(entries, WorkoutLogEntry{Date: date, ExerciseID: ex, Weight: w, Repetitions: reps, SessionID: session})
		}
	}
	add("2026-11-16", 989, 20, 10, 4, nil)
	add("2026-11-16", 369, 50, 12, 4, nil)
	add("2026-11-16", 507, 60, 10, 4, nil)
	add("2026-11-16", 590, 40, 15, 4, nil)
	add("2026-11-17", 94, 25, 6, 4, nil)
	add("2026-11-17", 158, 65, 6, 6, nil)
	add("2026-11-22", 94, 25, 6, 4, strp(legsSession))
	add("2026-11-22", 158, 67.5, 6, 6, strp(legsSession))
	add("2026-11-22", 272, 10, 8, 2, strp(legsSession))
	add("2026-11-22", 1186, 25, 6, 5, strp(legsSession))
	add("2026-11-22", 1627, 40, 7, 4, strp(legsSession))

	days := buildCalendar(entries, exercises, sessions, plans)
	last := days[len(days)-1]
	if last.Type != dayPull || last.Source != sourcePlan || last.PlanDay != "Pull" {
		t.Fatalf("2026-11-22 = %s via %s (%q), want pull via plan", last.Type, last.Source, last.PlanDay)
	}
	if last.PrevDate != "2026-11-17" {
		t.Errorf("compared with %s, want the previous pull day 2026-11-17", last.PrevDate)
	}
	// Without the plan, muscles alone (rear-delt fly counted as pull) still say pull.
	days = buildCalendar(entries, exercises, sessions, nil)
	if last := days[len(days)-1]; last.Type != dayPull || last.Source != sourceMuscles {
		t.Errorf("without plan: %s via %s, want pull via muscles", last.Type, last.Source)
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
