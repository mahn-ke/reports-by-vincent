package workers

// Primary muscles for wger exercises that have none, keyed by exercise UUID. They are used only
// while wger lists no primary muscles: these exercises come from wger.de, and wger's weekly
// exercise sync resets local edits, so the muscles live here instead of in wger. Once wger.de tags
// an exercise itself, its own muscles win and the override is logged as obsolete.
//
// Names are wger's muscle names as the sync stores them (English name, else Latin), so the
// calendar groups them like any other exercise. Secondary muscles are kept for reference only;
// the calendar scores primary muscles.
type muscleOverride struct {
	Name      string
	Primary   []string
	Secondary []string
	Sources   []string
}

var muscleOverrides = map[string]muscleOverride{
	"e797ca42-f9fc-480a-8f48-7f6905978c72": {
		Name:      "Pec deck rear delt fly",
		Primary:   []string{"Shoulders"}, // wger has one deltoid entry; here the posterior head
		Secondary: []string{"Trapezius"},
		Sources: []string{
			"Franke et al. 2015, J Sports Med Phys Fitness, PMID 24947920 (reverse pec deck: highest posterior deltoid activation)",
			"Ekstrom et al. 2003, J Orthop Sports Phys Ther, doi:10.2519/jospt.2003.33.5.247 (middle trapezius in horizontal abduction)",
			"wger: Chest-Supported Rear Delt Raise is tagged Shoulders + Trapezius (secondary)",
		},
	},
	"3f93459a-d06e-465c-85db-2137da759440": {
		Name:      "Rear Delt Raises",
		Primary:   []string{"Shoulders"},
		Secondary: []string{"Trapezius"},
		Sources: []string{
			"same horizontal abduction as the reverse pec deck (Franke et al. 2015, PMID 24947920); no exercise-specific EMG study found",
			"Ekstrom et al. 2003, doi:10.2519/jospt.2003.33.5.247",
		},
	},
	"e7a964cd-e68e-4926-bc7c-577065137e18": {
		Name:      "Deficit Deadlift",
		Primary:   []string{"Glutes", "Hamstrings", "Lower back", "Quads"},
		Secondary: []string{"Trapezius", "Lats", "Calves", "Abs", "Obliquus externus abdominis"},
		Sources: []string{
			"Escamilla et al. 2002, Med Sci Sports Exerc, doi:10.1097/00005768-200204000-00019 (16-muscle deadlift EMG)",
			"Martín-Fuentes et al. 2020, PLoS One, doi:10.1371/journal.pone.0229507 (systematic review of deadlift EMG)",
			"Rogacion et al. 2024, Int J Exerc Sci Conf Proc 14(4) (deficit: higher peak gluteus max and vastus lateralis; abstract)",
		},
	},
	"3c33c9fd-20cc-4dff-ac88-c992bb9b2e33": {
		Name:      "Extreme Pec Stretch",
		Primary:   []string{"Chest"},
		Secondary: []string{"Shoulders"},
		Sources: []string{
			"Wohlann et al. 2024, Eur J Appl Physiol, doi:10.1007/s00421-023-05413-y (pectoral stretching: thickness and strength gains)",
			"Warneke et al. 2023, J Sports Sci Med, doi:10.52082/jssm.2023.597",
			"wger: Doorway Pectoral Stretch is tagged Chest",
		},
	},
	"ab808c2c-2e84-42bb-a65d-287a06ca7aa2": {
		Name:    "Extreme Lat Stretch",
		Primary: []string{"Lats"},
		Sources: []string{
			"no human study found; target by anatomy (lengthened in shoulder flexion/elevation)",
			"Antonio & Gonyea 1993, J Appl Physiol, doi:10.1152/jappl.1993.75.3.1263 (loaded stretch hypertrophy, quail latissimus)",
			"wger: Butchers Block Stretch is tagged Lats",
		},
	},
}
