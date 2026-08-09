package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
)

//go:embed templates
var templatesFS embed.FS

type App struct {
	store         *sessions.CookieStore
	oauth2Config  *oauth2.Config
	oidcVerifier  *oidc.IDTokenVerifier
	apiURL        string
	adminUsername string
	adminPassword string
	providerName  string
	baseURL       string
}

// --- Types mirroring API responses ---

type NutritionLog struct {
	ID       int     `json:"id"`
	Date     string  `json:"date"`
	Calories float64 `json:"calories"`
	Carbs    float64 `json:"carbs"`
	Fat      float64 `json:"fat"`
	Protein  float64 `json:"protein"`
}

type TrainingLog struct {
	ID   int    `json:"id"`
	Date string `json:"date"`
}

type BodyMeasurement struct {
	ID                 int       `json:"id"`
	MeasuredAt         time.Time `json:"measured_at"`
	Weight             float64   `json:"weight"`
	BMI                float64   `json:"bmi"`
	BodyFat            float64   `json:"body_fat"`
	SkeletalMuscleMass float64   `json:"skeletal_muscle_mass"`
	BoneMass           float64   `json:"bone_mass"`
	BodyWater          float64   `json:"body_water"`
}

type WorkoutExercise struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type WorkoutLogEntry struct {
	ExerciseID  int     `json:"exercise_id"`
	Date        string  `json:"date"`
	Weight      float64 `json:"weight"`
	Repetitions int     `json:"repetitions"`
}

// --- Template rendering ---

var funcMap = template.FuncMap{
	"add": func(a, b int) int { return a + b },
}

func renderTemplate(w http.ResponseWriter, name string, data any) {
	t, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "templates/base.html", "templates/"+name+".html")
	if err != nil {
		slog.Error("parse template", "name", name, "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("execute template", "name", name, "err", err)
	}
}

// --- Internal API client ---

func (a *App) apiGet(ctx context.Context, path string, out any) error {
	resp, err := http.Get(a.apiURL + path)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return json.Unmarshal(body, out)
}

// --- Auth helpers ---

func randomState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// hasClientRole parses the access token payload (trusted via ID token verification)
// and checks for a named role under resource_access.<client>.roles.
func hasClientRole(accessToken, client, role string) bool {
	parts := strings.SplitN(accessToken, ".", 3)
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		ResourceAccess map[string]struct {
			Roles []string `json:"roles"`
		} `json:"resource_access"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false
	}
	for _, r := range claims.ResourceAccess[client].Roles {
		if r == role {
			return true
		}
	}
	return false
}

func (a *App) isAuthenticated(r *http.Request) bool {
	sess, err := a.store.Get(r, "session")
	if err != nil {
		return false
	}
	auth, _ := sess.Values["authenticated"].(bool)
	return auth
}

func (a *App) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.isAuthenticated(r) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Auth handlers ---

type loginPageData struct {
	Error        string
	ProviderName string
	OIDCEnabled  bool
}

func (a *App) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if a.isAuthenticated(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	renderTemplate(w, "login", loginPageData{
		ProviderName: a.providerName,
		OIDCEnabled:  a.oauth2Config != nil,
	})
}

func (a *App) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	uMatch := subtle.ConstantTimeCompare([]byte(username), []byte(a.adminUsername)) == 1
	pMatch := subtle.ConstantTimeCompare([]byte(password), []byte(a.adminPassword)) == 1
	if !uMatch || !pMatch {
		renderTemplate(w, "login", loginPageData{
			Error:        "Invalid username or password.",
			ProviderName: a.providerName,
			OIDCEnabled:  a.oauth2Config != nil,
		})
		return
	}

	sess, _ := a.store.Get(r, "session")
	sess.Values["authenticated"] = true
	sess.Values["username"] = username
	sess.Values["auth_type"] = "basic"
	sess.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	if a.oauth2Config == nil {
		http.Error(w, "OIDC not configured", http.StatusServiceUnavailable)
		return
	}
	state := randomState()
	verifier := oauth2.GenerateVerifier()
	sess, _ := a.store.Get(r, "oauth-state")
	sess.Values["state"] = state
	sess.Values["pkce_verifier"] = verifier
	sess.Save(r, w)
	http.Redirect(w, r, a.oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (a *App) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if a.oauth2Config == nil || a.oidcVerifier == nil {
		http.Error(w, "OIDC not configured", http.StatusServiceUnavailable)
		return
	}

	stateSess, _ := a.store.Get(r, "oauth-state")
	expected, _ := stateSess.Values["state"].(string)
	if r.URL.Query().Get("state") != expected || expected == "" {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	verifier, _ := stateSess.Values["pkce_verifier"].(string)

	token, err := a.oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token", http.StatusInternalServerError)
		return
	}
	idToken, err := a.oidcVerifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
	}
	idToken.Claims(&claims)
	username := claims.PreferredUsername
	if username == "" {
		username = claims.Email
	}

	if !hasClientRole(token.AccessToken, "realm-management", "manage-users") {
		w.WriteHeader(http.StatusForbidden)
		renderTemplate(w, "forbidden", nil)
		return
	}

	sess, _ := a.store.Get(r, "session")
	sess.Values["authenticated"] = true
	sess.Values["username"] = username
	sess.Values["auth_type"] = "oidc"
	sess.Save(r, w)

	stateSess.Options.MaxAge = -1
	stateSess.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	sess, _ := a.store.Get(r, "session")
	sess.Options.MaxAge = -1
	sess.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// --- Page handlers ---

type dashboardData struct {
	Username       string
	LastCalories   float64
	LastCalDate    string
	LastWeight     float64
	LastWeightDate string
	TrainingTotal  int
	LastTrainDate  string
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	sess, _ := a.store.Get(r, "session")
	username, _ := sess.Values["username"].(string)

	var nutrition []NutritionLog
	var body []BodyMeasurement
	var training []TrainingLog
	a.apiGet(r.Context(), "/internal/nutrition", &nutrition)
	a.apiGet(r.Context(), "/internal/body", &body)
	a.apiGet(r.Context(), "/internal/training", &training)

	data := dashboardData{Username: username, TrainingTotal: len(training)}
	if len(nutrition) > 0 {
		last := nutrition[len(nutrition)-1]
		data.LastCalories = last.Calories
		data.LastCalDate = last.Date
	}
	if len(body) > 0 {
		last := body[len(body)-1]
		data.LastWeight = last.Weight
		data.LastWeightDate = last.MeasuredAt.Format("2006-01-02")
	}
	if len(training) > 0 {
		data.LastTrainDate = training[len(training)-1].Date
	}
	renderTemplate(w, "dashboard", data)
}

type nutritionPageData struct {
	Entries  []NutritionLog
	Labels   template.JS
	Calories template.JS
	Carbs    template.JS
	Fat      template.JS
	Protein  template.JS
}

func (a *App) handleNutrition(w http.ResponseWriter, r *http.Request) {
	var entries []NutritionLog
	if err := a.apiGet(r.Context(), "/internal/nutrition", &entries); err != nil {
		http.Error(w, "failed to load data", http.StatusInternalServerError)
		return
	}

	labels := make([]string, len(entries))
	cals := make([]float64, len(entries))
	carbs := make([]float64, len(entries))
	fats := make([]float64, len(entries))
	proteins := make([]float64, len(entries))
	for i, e := range entries {
		labels[i] = e.Date
		cals[i] = e.Calories
		carbs[i] = e.Carbs
		fats[i] = e.Fat
		proteins[i] = e.Protein
	}

	labelsJ, _ := json.Marshal(labels)
	calsJ, _ := json.Marshal(cals)
	carbsJ, _ := json.Marshal(carbs)
	fatsJ, _ := json.Marshal(fats)
	protsJ, _ := json.Marshal(proteins)

	renderTemplate(w, "nutrition", nutritionPageData{
		Entries:  entries,
		Labels:   template.JS(labelsJ),
		Calories: template.JS(calsJ),
		Carbs:    template.JS(carbsJ),
		Fat:      template.JS(fatsJ),
		Protein:  template.JS(protsJ),
	})
}

type trainingPageData struct {
	Entries []TrainingLog
	Dates   template.JS
}

func (a *App) handleTraining(w http.ResponseWriter, r *http.Request) {
	var entries []TrainingLog
	if err := a.apiGet(r.Context(), "/internal/training", &entries); err != nil {
		http.Error(w, "failed to load data", http.StatusInternalServerError)
		return
	}
	dates := make([]string, len(entries))
	for i, e := range entries {
		dates[i] = e.Date
	}
	datesJ, _ := json.Marshal(dates)
	renderTemplate(w, "training", trainingPageData{Entries: entries, Dates: template.JS(datesJ)})
}

type bodyPageData struct {
	Entries    []BodyMeasurement
	Labels     template.JS
	Weights    template.JS
	BodyFats   template.JS
	MuscleMass template.JS
}

func (a *App) handleBody(w http.ResponseWriter, r *http.Request) {
	var entries []BodyMeasurement
	if err := a.apiGet(r.Context(), "/internal/body", &entries); err != nil {
		http.Error(w, "failed to load data", http.StatusInternalServerError)
		return
	}
	labels := make([]string, len(entries))
	weights := make([]float64, len(entries))
	bodyFats := make([]float64, len(entries))
	muscleMass := make([]float64, len(entries))
	for i, e := range entries {
		labels[i] = e.MeasuredAt.Format("2006-01-02")
		weights[i] = e.Weight
		bodyFats[i] = e.BodyFat
		muscleMass[i] = e.SkeletalMuscleMass
	}
	labelsJ, _ := json.Marshal(labels)
	weightsJ, _ := json.Marshal(weights)
	bfJ, _ := json.Marshal(bodyFats)
	mmJ, _ := json.Marshal(muscleMass)
	renderTemplate(w, "body", bodyPageData{
		Entries:    entries,
		Labels:     template.JS(labelsJ),
		Weights:    template.JS(weightsJ),
		BodyFats:   template.JS(bfJ),
		MuscleMass: template.JS(mmJ),
	})
}

type workoutPageData struct {
	// JSON object: {exerciseId: {date: totalKg, ...}, ...}
	ExerciseDailyTotals template.JS
	// JSON object: {exerciseId: "name", ...}
	ExerciseNames template.JS
}

func (a *App) handleWorkout(w http.ResponseWriter, r *http.Request) {
	var exercises []WorkoutExercise
	var entries []WorkoutLogEntry
	a.apiGet(r.Context(), "/internal/workout/exercises", &exercises)
	a.apiGet(r.Context(), "/internal/workout/entries", &entries)

	// Build name map
	nameMap := make(map[int]string, len(exercises))
	for _, e := range exercises {
		nameMap[e.ID] = e.Name
	}

	// Aggregate: exerciseID -> date -> sum(weight * repetitions)
	totals := make(map[int]map[string]float64)
	for _, e := range entries {
		if _, ok := totals[e.ExerciseID]; !ok {
			totals[e.ExerciseID] = make(map[string]float64)
		}
		totals[e.ExerciseID][e.Date] += e.Weight * float64(e.Repetitions)
	}

	totalsJ, _ := json.Marshal(totals)
	namesJ, _ := json.Marshal(nameMap)
	renderTemplate(w, "workout", workoutPageData{
		ExerciseDailyTotals: template.JS(totalsJ),
		ExerciseNames:       template.JS(namesJ),
	})
}

// --- main ---

func main() {
	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		slog.Error("SESSION_SECRET is required")
		os.Exit(1)
	}

	store := sessions.NewCookieStore([]byte(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(os.Getenv("APP_BASE_URL"), "https://"),
	}

	app := &App{
		store:         store,
		apiURL:        os.Getenv("API_URL"),
		adminUsername: os.Getenv("ADMIN_USERNAME"),
		adminPassword: os.Getenv("ADMIN_PASSWORD"),
		providerName:  os.Getenv("OIDC_PROVIDER_NAME"),
		baseURL:       os.Getenv("APP_BASE_URL"),
	}
	if app.apiURL == "" {
		app.apiURL = "http://api:8080"
	}
	if app.baseURL == "" {
		app.baseURL = "http://localhost:8082"
	}

	issuer := os.Getenv("OIDC_ISSUER")
	clientID := os.Getenv("OIDC_CLIENT_ID")
	clientSecret := os.Getenv("OIDC_CLIENT_SECRET")

	if issuer != "" && clientID != "" && clientSecret != "" {
		provider, err := oidc.NewProvider(context.Background(), issuer)
		if err != nil {
			slog.Warn("OIDC provider discovery failed — OIDC login disabled", "err", err)
		} else {
			app.oauth2Config = &oauth2.Config{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				RedirectURL:  app.baseURL + "/oauth/callback",
				Endpoint:     provider.Endpoint(),
				Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
			}
			app.oidcVerifier = provider.Verifier(&oidc.Config{ClientID: clientID})
			slog.Info("OIDC enabled", "issuer", issuer)
		}
	} else {
		slog.Warn("OIDC env vars missing — OIDC login disabled")
	}

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	r.Get("/login", app.handleLoginGet)
	r.Post("/login", app.handleLoginPost)
	r.Get("/oauth/login", app.handleOAuthLogin)
	r.Get("/oauth/callback", app.handleOAuthCallback)
	r.Get("/logout", app.handleLogout)

	r.Group(func(r chi.Router) {
		r.Use(app.requireAuth)
		r.Get("/", app.handleDashboard)
		r.Get("/nutrition", app.handleNutrition)
		r.Get("/training", app.handleTraining)
		r.Get("/body", app.handleBody)
		r.Get("/workout", app.handleWorkout)
	})

	slog.Info("App listening on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
