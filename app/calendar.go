package main

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

// --- Push / pull / legs classification and progressive-overload scoring ---

const (
	dayPush = "push"
	dayPull = "pull"
	dayLegs = "legs"
)

const (
	statusUp    = "up"    // more volume than the last day of the same type (green)
	statusSame  = "same"  // identical volume (yellow)
	statusDown  = "down"  // less volume (red)
	statusFirst = "first" // no earlier day of that type, or unclassified (neutral)
)

type CalendarExercise struct {
	Name       string   `json:"name"`
	Sets       int      `json:"sets"`
	Reps       int      `json:"reps"`
	Volume     float64  `json:"volume"`
	PrevVolume *float64 `json:"prevVolume,omitempty"`
}

type CalendarDay struct {
	Date       string             `json:"date"`
	Type       string             `json:"type"`   // push | pull | legs | ""
	Source     string             `json:"source"` // routine | muscles | ""
	Volume     float64            `json:"volume"`
	Reps       int                `json:"reps"`
	PrevDate   string             `json:"prevDate,omitempty"`
	PrevVolume float64            `json:"prevVolume"`
	PrevReps   int                `json:"prevReps"`
	Status     string             `json:"status"`
	Exercises  []CalendarExercise `json:"exercises"`
}

var dayNamePattern = regexp.MustCompile(`(?i)\b(push|pull|legs?)\b`)

// dayTypeFromName maps a wger routine day name ("Push A", "Leg day", ...) to a day type.
func dayTypeFromName(name string) string {
	m := dayNamePattern.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	switch strings.ToLower(m[1]) {
	case "push":
		return dayPush
	case "pull":
		return dayPull
	default:
		return dayLegs
	}
}

// muscleType maps a wger muscle (English or Latin name) to push/pull/legs. Leg muscles are
// checked first so that "Biceps femoris" (hamstring) is not mistaken for the arm biceps.
func muscleType(muscle string) string {
	m := strings.ToLower(muscle)
	switch {
	case containsAny(m, "hamstring", "femoris", "quad", "glute", "calf", "calves", "gastrocnemius", "soleus", "adductor", "abductor"):
		return dayLegs
	case containsAny(m, "chest", "pectoral", "shoulder", "deltoid", "tricep", "serratus"):
		return dayPush
	case containsAny(m, "bicep", "brachialis", "lats", "latissimus", "trapez", "lower back", "erector", "rhomboid"):
		return dayPull
	}
	return ""
}

func categoryType(category string) string {
	switch strings.ToLower(category) {
	case "chest", "shoulders":
		return dayPush
	case "back":
		return dayPull
	case "legs", "calves":
		return dayLegs
	}
	return "" // Arms (ambiguous without muscles), Abs, Cardio
}

// exerciseType classifies an exercise by its primary muscles (majority), falling back to its category.
func exerciseType(ex WorkoutExercise) string {
	counts := map[string]int{}
	for _, m := range ex.Muscles {
		if t := muscleType(m); t != "" {
			counts[t]++
		}
	}
	if t := majority(counts); t != "" {
		return t
	}
	return categoryType(ex.Category)
}

// majority returns the key with the strictly highest count, or "" on a tie / empty map.
func majority(counts map[string]int) string {
	best, bestN, tie := "", 0, false
	for _, t := range []string{dayPush, dayPull, dayLegs} {
		switch n := counts[t]; {
		case n > bestN:
			best, bestN, tie = t, n, false
		case n == bestN && n > 0:
			tie = true
		}
	}
	if tie {
		return ""
	}
	return best
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// compareDays returns up/same/down for cur vs prev. Volume (kg × reps) is compared; when both
// days are pure bodyweight (zero volume) the total reps are compared instead.
func compareDays(curVol float64, curReps int, prevVol float64, prevReps int) string {
	a, b := round1(curVol), round1(prevVol)
	if a == 0 && b == 0 {
		a, b = float64(curReps), float64(prevReps)
	}
	switch {
	case a > b:
		return statusUp
	case a < b:
		return statusDown
	default:
		return statusSame
	}
}

// buildCalendar aggregates sets into training days, classifies each day as push/pull/legs and
// scores it against the previous day of the same type. Days are returned in date order.
func buildCalendar(entries []WorkoutLogEntry, exercises []WorkoutExercise, sessions []WorkoutSession) []CalendarDay {
	exByID := make(map[int]WorkoutExercise, len(exercises))
	for _, e := range exercises {
		exByID[e.ID] = e
	}
	sessionName := make(map[int]string, len(sessions))
	for _, s := range sessions {
		sessionName[s.ID] = s.DayName
	}

	type exAgg struct {
		order  int
		sets   int
		reps   int
		volume float64
	}
	type dayAgg struct {
		volume    float64
		reps      int
		routine   string         // first push/pull/legs found in a linked session's day name
		typeSets  map[string]int // sets per muscle-derived type
		exercises map[int]*exAgg
	}
	days := map[string]*dayAgg{}

	for _, e := range entries {
		if e.Repetitions <= 0 {
			continue
		}
		d := days[e.Date]
		if d == nil {
			d = &dayAgg{typeSets: map[string]int{}, exercises: map[int]*exAgg{}}
			days[e.Date] = d
		}
		vol := e.Weight * float64(e.Repetitions)
		d.volume += vol
		d.reps += e.Repetitions
		if d.routine == "" && e.SessionID != nil {
			d.routine = dayTypeFromName(sessionName[*e.SessionID])
		}
		if t := exerciseType(exByID[e.ExerciseID]); t != "" {
			d.typeSets[t]++
		}
		x := d.exercises[e.ExerciseID]
		if x == nil {
			x = &exAgg{order: len(d.exercises)}
			d.exercises[e.ExerciseID] = x
		}
		x.sets++
		x.reps += e.Repetitions
		x.volume += vol
	}

	dates := make([]string, 0, len(days))
	for date := range days {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	out := make([]CalendarDay, 0, len(dates))
	lastOfType := map[string]int{} // type -> index in out
	for _, date := range dates {
		d := days[date]
		cd := CalendarDay{Date: date, Volume: round1(d.volume), Reps: d.reps, Status: statusFirst}
		switch {
		case d.routine != "":
			cd.Type, cd.Source = d.routine, "routine"
		default:
			if t := majority(d.typeSets); t != "" {
				cd.Type, cd.Source = t, "muscles"
			}
		}

		ids := make([]int, 0, len(d.exercises))
		for id := range d.exercises {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return d.exercises[ids[i]].order < d.exercises[ids[j]].order })

		var prev *CalendarDay
		if i, ok := lastOfType[cd.Type]; ok && cd.Type != "" {
			prev = &out[i]
			cd.PrevDate, cd.PrevVolume, cd.PrevReps = prev.Date, prev.Volume, prev.Reps
			cd.Status = compareDays(cd.Volume, cd.Reps, prev.Volume, prev.Reps)
		}
		prevVolByName := map[string]float64{}
		if prev != nil {
			for _, x := range prev.Exercises {
				prevVolByName[x.Name] = x.Volume
			}
		}
		for _, id := range ids {
			x := d.exercises[id]
			name := exByID[id].Name
			if name == "" {
				name = "Exercise"
			}
			ce := CalendarExercise{Name: name, Sets: x.sets, Reps: x.reps, Volume: round1(x.volume)}
			if v, ok := prevVolByName[name]; ok {
				ce.PrevVolume = &v
			}
			cd.Exercises = append(cd.Exercises, ce)
		}

		out = append(out, cd)
		if cd.Type != "" {
			lastOfType[cd.Type] = len(out) - 1
		}
	}
	return out
}
