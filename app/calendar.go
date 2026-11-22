package main

import (
	"regexp"
	"strings"
)

// --- Day type: push / pull / legs / full body / upper (a label; overload is scored per exercise, see overload.go) ---

const (
	dayPush  = "push"
	dayPull  = "pull"
	dayLegs  = "legs"
	dayFull  = "full"  // full body
	dayUpper = "upper" // upper body (push + pull) in upper/lower splits; "lower" days count as legs
)

// Classification sources, strongest first.
const (
	sourcePlan    = "plan"    // logged exercises match a wger routine day's planned exercises
	sourceMuscles = "muscles" // majority of sets by the exercises' muscles
	sourceSession = "session" // the day the wger session was attached to (follows wger's schedule)
)

const (
	// Minimum Jaccard overlap between logged and planned exercises for a plan match.
	planMatchThreshold = 0.5
	// Share of classified sets one group needs to decide a day on its own.
	muscleMajorityShare = 0.6
	// Without such a majority, a day spread over at least two groups (each with this share) is
	// full body, or upper body when legs stay below it.
	fullBodyMinShare = 0.2
)

var dayNamePattern = regexp.MustCompile(`(?i)\b(push|pull|legs?|lower|upper|full[\s-]?body|fullbody|ganzkörper)\b`)

// dayTypeFromName maps a wger routine day name ("Push A", "Leg day", "Full Body", "Lower #1",
// "Upper #2") to a day type.
func dayTypeFromName(name string) string {
	m := dayNamePattern.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	switch n := strings.ToLower(m[1]); {
	case n == "push":
		return dayPush
	case n == "pull":
		return dayPull
	case strings.HasPrefix(n, "leg"), n == "lower":
		return dayLegs
	case n == "upper":
		return dayUpper
	default:
		return dayFull
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

// Exercises whose wger muscles/category point the wrong way: rear-delt work is pulling even though
// wger files it under Shoulders/Chest (often without muscles), and pullovers are a lat movement.
var pullNamePattern = regexp.MustCompile(`(?i)rear[\s-]?delt|reverse[\s-]?(pec|fly|flye)|face[\s-]?pull|pull[\s-]?over`)

// exerciseType classifies an exercise by name override, then its primary muscles (majority),
// then its category.
func exerciseType(ex WorkoutExercise) string {
	if pullNamePattern.MatchString(ex.Name) {
		return dayPull
	}
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

// muscleDayType decides a day from sets per muscle-derived type: a clear majority wins; a spread
// over at least two groups is full body, or upper body if legs are not among them.
func muscleDayType(typeSets map[string]int) string {
	total := 0
	for _, n := range typeSets {
		total += n
	}
	if total == 0 {
		return ""
	}
	top := majority(typeSets)
	if top != "" && float64(typeSets[top])/float64(total) >= muscleMajorityShare {
		return top
	}
	groups, legs := 0, false
	for _, t := range []string{dayPush, dayPull, dayLegs} {
		if float64(typeSets[t])/float64(total) >= fullBodyMinShare {
			groups++
			legs = legs || t == dayLegs
		}
	}
	if groups >= 2 && legs {
		return dayFull
	}
	if groups >= 2 {
		return dayUpper
	}
	return top
}

// planMatch returns the type of the routine day whose planned exercises best match the logged
// ones (Jaccard overlap) and that day, if the overlap reaches planMatchThreshold.
func planMatch(logged map[int]bool, plans []WorkoutPlanDay) (string, *WorkoutPlanDay) {
	bestScore, bestType := 0.0, ""
	var best *WorkoutPlanDay
	for i := range plans {
		p := &plans[i]
		t := dayTypeFromName(p.Name)
		if p.IsRest || t == "" || len(p.ExerciseIDs) == 0 {
			continue
		}
		planned := map[int]bool{}
		for _, id := range p.ExerciseIDs {
			planned[id] = true
		}
		inter := 0
		for id := range logged {
			if planned[id] {
				inter++
			}
		}
		union := len(logged) + len(planned) - inter
		if union == 0 {
			continue
		}
		if score := float64(inter) / float64(union); score > bestScore {
			bestScore, bestType, best = score, t, p
		}
	}
	if bestScore >= planMatchThreshold {
		return bestType, best
	}
	return "", nil
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
