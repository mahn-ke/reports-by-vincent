package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// log builds the sets of one exercise on one day, in order.
type logBuilder struct{ entries []WorkoutLogEntry }

func (b *logBuilder) add(date string, ex int, weight float64, reps ...int) *logBuilder {
	for _, r := range reps {
		b.entries = append(b.entries, WorkoutLogEntry{Date: date, ExerciseID: ex, Weight: weight, Repetitions: r})
	}
	return b
}

func planWithSets(name string, dayID int, sets map[int][][2]int) WorkoutPlanDay {
	p := WorkoutPlanDay{DayID: dayID, Name: name}
	for ex, cfgs := range sets {
		p.ExerciseIDs = append(p.ExerciseIDs, ex)
		e := PlanEntry{SlotEntryID: ex * 10, ExerciseID: ex}
		for _, c := range cfgs {
			e.Sets = append(e.Sets, struct {
				Iteration int `json:"iteration"`
				Value     int `json:"value"`
			}{c[0], c[1]})
		}
		p.Entries = append(p.Entries, e)
	}
	return p
}

func exerciseOn(t *testing.T, days []CalendarDay, date, name string) CalendarExercise {
	t.Helper()
	for _, d := range days {
		if d.Date != date {
			continue
		}
		for _, e := range d.Exercises {
			if e.Name == name {
				return e
			}
		}
	}
	t.Fatalf("%s not found on %s", name, date)
	return CalendarExercise{}
}

// Cases from the app's own tests (test/features/routines/progressive_overload_test.dart): doing
// what the app suggests must count as overload here.
func TestMatchesAppSuggestions(t *testing.T) {
	exercises := []WorkoutExercise{
		{ID: 94, Name: "Biceps Curls", Muscles: []string{"Biceps"}},
		{ID: 1627, Name: "Pec deck rear delt fly", Category: "Shoulders"},
		{ID: 167, Name: "Crunches", Muscles: []string{"Abs"}},
	}
	b := &logBuilder{}
	// biceps: an extra set beyond the 4 planned is not part of the baseline:
	// 17.5×6 + 3 × 22.5×6 = 510 (not 615); the app suggests 22.5 × 6,6,6,5 = 517.5
	b.add("2026-11-10", 94, 17.5, 6).add("2026-11-10", 94, 22.5, 6, 6, 6).add("2026-11-10", 94, 17.5, 6)
	b.add("2026-11-17", 94, 22.5, 6, 6, 6, 5)
	// pec deck: 4×8 @ 33 = 1056; the app suggests 40 × 7,7,7,6 = 1080
	b.add("2026-11-10", 1627, 33, 8, 8, 8, 8)
	b.add("2026-11-17", 1627, 40, 7, 7, 7, 6)
	// bodyweight: volume are the repetitions, 15,12 = 27; one more rep = 28
	b.add("2026-11-10", 167, 0, 15, 12)
	b.add("2026-11-17", 167, 0, 15, 13)

	plan := planWithSets("Pull", 4, map[int][][2]int{94: {{1, 4}}, 1627: {{1, 4}}, 167: {{1, 2}}})
	days := buildCalendar(b.entries, exercises, nil, []WorkoutPlanDay{plan})

	biceps := exerciseOn(t, days, "2026-11-17", "Biceps Curls")
	if biceps.Baseline == nil || *biceps.Baseline != 510 || biceps.BaselineSets != 4 || biceps.Volume != 517.5 || biceps.Status != statusUp {
		t.Errorf("biceps = %+v (baseline %v), want 517.5 vs 510 over 4 planned sets: up", biceps, deref(biceps.Baseline))
	}
	if pec := exerciseOn(t, days, "2026-11-17", "Pec deck rear delt fly"); *pec.Baseline != 1056 || pec.Volume != 1080 || pec.Status != statusUp {
		t.Errorf("pec deck = %+v", pec)
	}
	if abs := exerciseOn(t, days, "2026-11-17", "Crunches"); *abs.Baseline != 27 || abs.Volume != 28 || abs.Status != statusUp {
		t.Errorf("crunches = %+v", abs)
	}
	if last := days[len(days)-1]; last.Status != statusUp || last.GroupsUp != 3 || last.GroupsTotal != 3 {
		t.Errorf("day = %s with %d/%d groups, want up 3/3", last.Status, last.GroupsUp, last.GroupsTotal)
	}

	// Without a planned set count the whole last session is the baseline (615), as in the app.
	days = buildCalendar(b.entries, exercises, nil, nil)
	if biceps := exerciseOn(t, days, "2026-11-17", "Biceps Curls"); *biceps.Baseline != 615 || biceps.Status != statusDown {
		t.Errorf("without plan: biceps baseline %v status %s, want 615 down", deref(biceps.Baseline), biceps.Status)
	}
}

func deref(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func TestStrictlyMoreAndEqual(t *testing.T) {
	ex := []WorkoutExercise{{ID: 1, Name: "Bench", Muscles: []string{"Chest"}}}
	b := (&logBuilder{}).add("2026-11-01", 1, 60, 10, 10).add("2026-11-08", 1, 60, 10, 10).add("2026-11-15", 1, 60, 10, 9)
	days := buildCalendar(b.entries, ex, nil, nil)
	want := []string{statusFirst, statusSame, statusDown}
	for i, w := range want {
		if days[i].Status != w {
			t.Errorf("%s: %s, want %s", days[i].Date, days[i].Status, w)
		}
	}
}

// Every exercise counts towards each of its primary muscles; the day is green when every group
// was overloaded, yellow when at least half were, red when fewer than half.
func TestMuscleGroupDayStatus(t *testing.T) {
	ex := []WorkoutExercise{
		{ID: 1, Name: "Row", Muscles: []string{"Lats"}},
		{ID: 2, Name: "Curl", Muscles: []string{"Biceps"}},
		{ID: 3, Name: "Rear delt", Category: "Shoulders"}, // no muscles in wger: its category
		{ID: 4, Name: "Pull-up", Muscles: []string{"Lats", "Biceps"}},
	}
	base := func() *logBuilder {
		return (&logBuilder{}).add("2026-11-01", 1, 50, 10).add("2026-11-01", 2, 20, 10).add("2026-11-01", 3, 10, 10)
	}
	cases := []struct {
		name      string
		row, curl int
		delt      int
		want      string
		up, total int
	}{
		{"all three groups up", 11, 11, 11, statusUp, 3, 3},
		{"two of three up", 11, 11, 9, statusPartial, 2, 3},
		{"one of three up", 11, 9, 9, statusDown, 1, 3},
		{"all identical", 10, 10, 10, statusSame, 0, 3},
		{"one up, rest identical", 11, 10, 10, statusDown, 1, 3},
	}
	for _, c := range cases {
		b := base().add("2026-11-08", 1, 50, c.row).add("2026-11-08", 2, 20, c.curl).add("2026-11-08", 3, 10, c.delt)
		days := buildCalendar(b.entries, ex, nil, nil)
		d := days[1]
		if d.Status != c.want || d.GroupsUp != c.up || d.GroupsTotal != c.total {
			t.Errorf("%s: %s %d/%d, want %s %d/%d", c.name, d.Status, d.GroupsUp, d.GroupsTotal, c.want, c.up, c.total)
		}
	}

	// A compound exercise adds to every one of its muscles: Lats and Biceps both get the pull-up.
	b := (&logBuilder{}).add("2026-11-01", 4, 0, 8).add("2026-11-08", 4, 0, 9)
	d := buildCalendar(b.entries, ex, nil, nil)[1]
	if d.GroupsTotal != 2 || d.Status != statusUp || d.Groups[0].Name != "Lats" || d.Groups[1].Name != "Biceps" {
		t.Errorf("pull-up groups = %+v, status %s", d.Groups, d.Status)
	}
}

func TestPlannedSetsByIteration(t *testing.T) {
	p := planWithSets("Pull", 4, map[int][][2]int{94: {{1, 4}, {3, 5}}, 158: {{2, 6}}})
	for _, c := range []struct{ ex, it, want int }{
		{94, 1, 4}, {94, 2, 4}, {94, 3, 5}, {94, 9, 5},
		{158, 1, 6}, // only a later config: the earliest one applies
		{158, 2, 6},
	} {
		if got := plannedSets(&p, c.ex, c.it); got == nil || *got != c.want {
			t.Errorf("exercise %d iteration %d: %v, want %d", c.ex, c.it, got, c.want)
		}
	}
	if got := plannedSets(&p, 999, 1); got != nil {
		t.Errorf("unplanned exercise: %v, want nil", *got)
	}

	// The iteration is the number of times the plan day was trained: 4 sets planned on the first
	// two visits, 5 from the third on.
	ex := []WorkoutExercise{{ID: 94, Name: "Curl", Muscles: []string{"Biceps"}}, {ID: 158, Name: "Pulldown", Muscles: []string{"Lats"}}}
	b := &logBuilder{}
	for _, date := range []string{"2026-11-01", "2026-11-08", "2026-11-15"} {
		b.add(date, 94, 20, 10, 10, 10, 10, 10).add(date, 158, 50, 10)
	}
	days := buildCalendar(b.entries, ex, nil, []WorkoutPlanDay{p})
	if c := exerciseOn(t, days, "2026-11-08", "Curl"); c.PlannedSets == nil || *c.PlannedSets != 4 || c.BaselineSets != 4 {
		t.Errorf("visit 2: %+v", c)
	}
	if c := exerciseOn(t, days, "2026-11-15", "Curl"); c.PlannedSets == nil || *c.PlannedSets != 5 || c.BaselineSets != 5 || days[2].Iteration != 3 {
		t.Errorf("visit 3: %+v (iteration %d)", c, days[2].Iteration)
	}
}

// Real case from 2026-11-22: a pull workout logged into a session wger attached to "Legs" is
// labelled Pull; each exercise is compared with its own previous session.
func TestRealPullDayLabelAndBaselines(t *testing.T) {
	ex := []WorkoutExercise{
		{ID: 94, Name: "Biceps Curls With SZ-bar", Category: "Arms", Muscles: []string{"Biceps"}},
		{ID: 158, Name: "Close-grip Lat Pull Down", Category: "Back", Muscles: []string{"Lats"}},
		{ID: 989, Name: "Bulgarian split squats right", Category: "Legs", Muscles: []string{"Glutes", "Quads"}},
	}
	plans := []WorkoutPlanDay{
		planWithSets("Legs", 2, map[int][][2]int{989: {{1, 5}}}),
		planWithSets("Pull", 4, map[int][][2]int{94: {{1, 4}}, 158: {{1, 6}}}),
	}
	legs := "01a20066-0000-4000-8000-000000000000"
	b := (&logBuilder{}).add("2026-11-16", 989, 20, 10, 10, 10, 10, 10).
		add("2026-11-17", 94, 25, 6, 6, 6, 6).add("2026-11-17", 158, 65, 6, 6, 6, 6, 6, 6)
	b.add("2026-11-22", 94, 25, 6, 6, 6, 7).add("2026-11-22", 158, 67.5, 6, 6, 6, 6, 6, 6)
	for i := range b.entries {
		if b.entries[i].Date == "2026-11-22" {
			b.entries[i].SessionID = strp(legs)
		}
	}
	days := buildCalendar(b.entries, ex, []WorkoutSession{{ID: legs, Date: "2026-11-22", DayName: "Legs"}}, plans)
	last := days[len(days)-1]
	if last.Type != dayPull || last.PlanDay != "Pull" || last.Status != statusUp || last.GroupsTotal != 2 {
		t.Errorf("2026-11-22 = %s (%s) %s %d/%d, want pull, up 2/2", last.Type, last.PlanDay, last.Status, last.GroupsUp, last.GroupsTotal)
	}
	if c := exerciseOn(t, days, "2026-11-22", "Biceps Curls With SZ-bar"); c.PrevDate != "2026-11-17" {
		t.Errorf("curls compared with %s, want 2026-11-17", c.PrevDate)
	}
}

func TestFirstDayHasEmptyListsNotNull(t *testing.T) {
	days := buildCalendar([]WorkoutLogEntry{{Date: "2026-11-01", ExerciseID: 1, Weight: 10, Repetitions: 5}},
		[]WorkoutExercise{{ID: 1, Name: "Row", Muscles: []string{"Lats"}}}, nil, nil)
	j, _ := json.Marshal(days[0])
	if strings.Contains(string(j), "null") {
		t.Errorf("first day JSON contains null: %s", j)
	}
}
