package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/mahn-ke/reports-by-vincent/api/db"
	"github.com/mahn-ke/reports-by-vincent/api/handlers"
	apimw "github.com/mahn-ke/reports-by-vincent/api/middleware"
)

func main() {
	ctx := context.Background()

	pool, err := db.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		slog.Error("failed to run migrations", "err", err)
		os.Exit(1)
	}

	h := handlers.New(pool)
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	// Internal routes — accessible only within the Docker network, no auth required.
	r.Route("/internal", func(r chi.Router) {
		r.Get("/nutrition", h.ListNutrition)
		r.Post("/nutrition", h.UpsertNutrition)
		r.Get("/training", h.ListTraining)
		r.Post("/training", h.UpsertTraining)
		r.Get("/body", h.ListBody)
		r.Post("/body", h.UpsertBody)
	})

	// OIDC-protected routes for external clients. Skipped if OIDC_ISSUER is not configured.
	issuer := os.Getenv("OIDC_ISSUER")
	clientID := os.Getenv("OIDC_CLIENT_ID")
	if issuer != "" && clientID != "" {
		verifier, err := apimw.NewOIDCVerifier(ctx, issuer, clientID)
		if err != nil {
			slog.Error("OIDC provider discovery failed", "err", err)
			os.Exit(1)
		}
		r.Route("/api", func(r chi.Router) {
			r.Use(apimw.OIDCMiddleware(verifier))
			r.Get("/nutrition", h.ListNutrition)
			r.Get("/training", h.ListTraining)
			r.Get("/body", h.ListBody)
		})
	} else {
		slog.Warn("OIDC_ISSUER not set — /api/* routes disabled")
	}

	slog.Info("API listening on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
