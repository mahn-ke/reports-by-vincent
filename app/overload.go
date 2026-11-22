package main

import (
	"math"
	"sort"
	"strings"
)

// Progressive overload as in the wger app (ViMaSter/wger-flutter,
// lib/features/routines/progressive_overload.dart), so a day the app's suggestions were followed
// on shows as overloaded here:
//
//   - Each exercise is compared with its own previous session, not with the previous day of a type.
//   - Volume per set is weight × repetitions, or the repetitions for sets without weight.
//   - The baseline is the first N sets of the previous session, N being the sets planned for the
//     exercise today (all sets when unknown), so extra sets done last time don't raise the bar.
//     Today counts every set, extra sets included.
//   - Overload is strictly more volume than the baseline (with a tolerance for float noise).
//
// Days are then scored by muscle group: every exercise adds its volume and baseline to each of its
// primary muscles (its category when wger lists no muscles). A group is overloaded when its volume
// beats its baseline. A day is green when every group was overloaded, yellow when at least half
// were, red when fewer than half were. A day where every group repeated its baseline exactly is
// yellow too (identical, nothing lost).

// Tolerance for floating point noise, as in the app.
const overloadEpsilon = 1e-9

const (
	statusUp      = "up"      // every muscle group overloaded (green); per exercise/group: beat baseline
	statusPartial = "partial" // at least half of the groups overloaded (yellow)
	statusSame    = "same"    // every group exactly at its baseline (yellow); per exercise/group: equal
	statusDown    = "down"    // fewer than half of the groups overloaded (red); per exercise/group: below
	statusFirst   = "first"   // nothing to compare with yet (neutral)
)

type CalendarExercise struct {
	Name         string   `json:"name"`
	Groups       []string `json:"groups"`
	Sets         int      `json:"sets"`
	Reps         int      `json:"reps"`
	Volume       float64  `json:"volume"`
	Unit         string   `json:"unit"` // kg (weight × reps) or reps (every set without weight)
	Baseline     *float64 `json:"baseline,omitempty"`
	BaselineSets int      `json:"baselineSets,omitempty"`
	PlannedSets  *int     `json:"plannedSets,omitempty"`
	PrevDate     string   `json:"prevDate,omitempty"`
	Status       string   `json:"status"`
}

type CalendarGroup struct {
	Name     string  `json:"name"`
	Volume   float64 `json:"volume"`
	Baseline float64 `json:"baseline"`
	Status   string  `json:"status"`
}

type CalendarDay struct {
	Date        string             `json:"date"`
	Type        string             `json:"type"`   // push | pull | legs | full | upper | ""
	Source      string             `json:"source"` // plan | muscles | session | ""
	PlanDay     string             `json:"planDay,omitempty"`
	Iteration   int                `json:"iteration,omitempty"`
	Volume      float64            `json:"volume"`
	Status      string             `json:"status"`
	GroupsUp    int                `json:"groupsUp"`
	GroupsTotal int                `json:"groupsTotal"`
	Groups      []CalendarGroup    `json:"groups"`
	Exercises   []CalendarExercise `json:"exercises"`
}

// setVolume is the volume of one set as in the app: weight × repetitions, the repetitions without weight.
func setVolume(weight float64, reps int) float64 {
	if weight == 0 {
		return float64(reps)
	}
	return weight * float64(reps)
}

func compareVolumes(cur, base float64) string {
	switch d := cur - base; {
	case d > overloadEpsilon:
		return statusUp
	case d < -overloadEpsilon:
		return statusDown
	default:
		return statusSame
	}
}

// muscleGroups are the groups an exercise counts towards: its primary muscles, else its category.
func muscleGroups(ex WorkoutExercise) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range ex.Muscles {
		if m = strings.TrimSpace(m); m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		if ex.Category != "" {
			return []string{ex.Category}
		}
		return []string{"Other"}
	}
	return out
}

// plannedSets is the number of sets planned for exerciseID on plan day p in iteration it: per slot
// entry the replace-config with the highest iteration up to it (the first one before that), summed
// over the slot entries of that exercise. Nil when the plan has no set count for it.
func plannedSets(p *WorkoutPlanDay, exerciseID, it int) *int {
	if p == nil {
		return nil
	}
	total, found := 0, false
	for _, e := range p.Entries {
		if e.ExerciseID != exerciseID || len(e.Sets) == 0 {
			continue
		}
		// The latest config at or before iteration it; the earliest one if all start later.
		bi := -1
		for i, s := range e.Sets {
			if s.Iteration <= it && (bi < 0 || s.Iteration > e.Sets[bi].Iteration) {
				bi = i
			}
		}
		if bi < 0 {
			bi = 0
			for i, s := range e.Sets {
				if s.Iteration < e.Sets[bi].Iteration {
					bi = i
				}
			}
		}
		if best := e.Sets[bi]; best.Value > 0 {
			total += best.Value
			found = true
		}
	}
	if !found {
		return nil
	}
	return &total
}

// buildCalendar aggregates sets into training days, labels each day's type and scores it with the
// app's overload rules, per muscle group. Days are returned in date order.
func buildCalendar(entries []WorkoutLogEntry, exercises []WorkoutExercise, sessions []WorkoutSession, plans []WorkoutPlanDay) []CalendarDay {
	exByID := make(map[int]WorkoutExercise, len(exercises))
	for _, e := range exercises {
		exByID[e.ID] = e
	}
	sessionName := make(map[string]string, len(sessions))
	for _, s := range sessions {
		sessionName[s.ID] = s.DayName
	}

	type set struct {
		weight float64
		reps   int
	}
	type dayAgg struct {
		sessionType string
		typeSets    map[string]int
		order       []int         // exercise ids in order of their first set
		sets        map[int][]set // per exercise, in logged order
	}
	days := map[string]*dayAgg{}
	for _, e := range entries { // ordered by date and time logged (API)
		if e.Repetitions <= 0 {
			continue
		}
		d := days[e.Date]
		if d == nil {
			d = &dayAgg{typeSets: map[string]int{}, sets: map[int][]set{}}
			days[e.Date] = d
		}
		if d.sessionType == "" && e.SessionID != nil {
			d.sessionType = dayTypeFromName(sessionName[*e.SessionID])
		}
		if t := exerciseType(exByID[e.ExerciseID]); t != "" {
			d.typeSets[t]++
		}
		if _, ok := d.sets[e.ExerciseID]; !ok {
			d.order = append(d.order, e.ExerciseID)
		}
		d.sets[e.ExerciseID] = append(d.sets[e.ExerciseID], set{e.Weight, e.Repetitions})
	}

	dates := make([]string, 0, len(days))
	for date := range days {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	lastDate := map[int]string{} // exercise -> date of its previous session
	planVisits := map[int]int{}  // plan day id -> times trained so far (its iteration)
	out := make([]CalendarDay, 0, len(dates))
	for _, date := range dates {
		d := days[date]
		// Empty slices, not nil: the page reads .length of both.
		cd := CalendarDay{Date: date, Status: statusFirst, Groups: []CalendarGroup{}, Exercises: []CalendarExercise{}}

		logged := make(map[int]bool, len(d.sets))
		for id := range d.sets {
			logged[id] = true
		}
		var plan *WorkoutPlanDay
		if t, p := planMatch(logged, plans); t != "" {
			cd.Type, cd.Source, cd.PlanDay, plan = t, sourcePlan, p.Name, p
			planVisits[p.DayID]++
			cd.Iteration = planVisits[p.DayID]
		} else if t := muscleDayType(d.typeSets); t != "" {
			cd.Type, cd.Source = t, sourceMuscles
		} else if d.sessionType != "" {
			cd.Type, cd.Source = d.sessionType, sourceSession
		}

		groupIdx := map[string]int{}
		for _, id := range d.order {
			ex := exByID[id]
			name := ex.Name
			if name == "" {
				name = "Exercise"
			}
			cur := d.sets[id]
			ce := CalendarExercise{Name: name, Groups: muscleGroups(ex), Sets: len(cur), Unit: "reps", Status: statusFirst}
			for _, s := range cur {
				ce.Reps += s.reps
				ce.Volume += setVolume(s.weight, s.reps)
				if s.weight != 0 {
					ce.Unit = "kg"
				}
			}
			cd.Volume += ce.Volume

			if prevDate, ok := lastDate[id]; ok {
				prev := days[prevDate].sets[id]
				n := len(prev)
				if ps := plannedSets(plan, id, cd.Iteration); ps != nil {
					ce.PlannedSets = ps
					n = min(n, *ps)
				}
				base := 0.0
				for _, s := range prev[:n] {
					base += setVolume(s.weight, s.reps)
				}
				ce.Baseline, ce.BaselineSets, ce.PrevDate = &base, n, prevDate
				ce.Status = compareVolumes(ce.Volume, base)

				for _, g := range ce.Groups {
					i, ok := groupIdx[g]
					if !ok {
						i = len(cd.Groups)
						groupIdx[g] = i
						cd.Groups = append(cd.Groups, CalendarGroup{Name: g})
					}
					cd.Groups[i].Volume += ce.Volume
					cd.Groups[i].Baseline += base
				}
			}
			lastDate[id] = date
			cd.Exercises = append(cd.Exercises, roundExercise(ce))
		}

		up, down := 0, 0
		for i := range cd.Groups {
			g := &cd.Groups[i]
			g.Status = compareVolumes(g.Volume, g.Baseline)
			switch g.Status {
			case statusUp:
				up++
			case statusDown:
				down++
			}
			g.Volume, g.Baseline = round2(g.Volume), round2(g.Baseline)
		}
		cd.GroupsUp, cd.GroupsTotal = up, len(cd.Groups)
		switch total := len(cd.Groups); {
		case total == 0:
			cd.Status = statusFirst
		case up == total:
			cd.Status = statusUp
		case up == 0 && down == 0:
			cd.Status = statusSame
		case up*2 >= total:
			cd.Status = statusPartial
		default:
			cd.Status = statusDown
		}
		cd.Volume = round2(cd.Volume)
		out = append(out, cd)
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func roundExercise(ce CalendarExercise) CalendarExercise {
	ce.Volume = round2(ce.Volume)
	if ce.Baseline != nil {
		b := round2(*ce.Baseline)
		ce.Baseline = &b
	}
	return ce
}
