package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mahn-ke/reports-by-vincent/jobs/workers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"riverqueue.com/riverui"
)

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Run River schema migrations.
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		slog.Error("river migrator create", "err", err)
		os.Exit(1)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		slog.Error("river migrate", "err", err)
		os.Exit(1)
	}

	apiURL := os.Getenv("API_INTERNAL_URL")
	if apiURL == "" {
		apiURL = "http://api:8080"
	}
	garminURL := os.Getenv("GARMIN_URL")
	if garminURL == "" {
		garminURL = "http://garmin:8080"
	}

	// Warn at startup if fetcher credentials are absent.
	if os.Getenv("MFP_COOKIE") == "" {
		slog.Warn("MFP_COOKIE not set — MyFitnessPal job will be a no-op")
	}
	if os.Getenv("FITX_EMAIL") == "" || os.Getenv("FITX_PASSWORD") == "" {
		slog.Warn("FITX_EMAIL/FITX_PASSWORD not set — FitX job will be a no-op")
	}

	workerPool := river.NewWorkers()
	river.AddWorker(workerPool, &workers.MyFitnessPalWorker{
		MFPCookie:  os.Getenv("MFP_COOKIE"),
		APIBaseURL: apiURL,
	})
	river.AddWorker(workerPool, &workers.FitXWorker{
		Email:      os.Getenv("FITX_EMAIL"),
		Password:   os.Getenv("FITX_PASSWORD"),
		APIBaseURL: apiURL,
	})
	river.AddWorker(workerPool, &workers.GarminWorker{
		GarminURL:  garminURL,
		APIBaseURL: apiURL,
	})

	periodicJobs := []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return workers.MyFitnessPalArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return workers.FitXArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return workers.GarminArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}

	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 5},
		},
		Workers:      workerPool,
		PeriodicJobs: periodicJobs,
	})
	if err != nil {
		slog.Error("river client create", "err", err)
		os.Exit(1)
	}

	if err := riverClient.Start(ctx); err != nil {
		slog.Error("river start", "err", err)
		os.Exit(1)
	}

	uiSrv, err := riverui.NewHandler(&riverui.HandlerOpts{
		Endpoints: riverui.NewEndpoints(riverClient, nil),
		Logger:    slog.Default(),
		Prefix:    "/",
	})
	if err != nil {
		slog.Error("riverui handler create", "err", err)
		os.Exit(1)
	}

	go func() {
		slog.Info("River UI listening on :8080")
		if err := http.ListenAndServe(":8080", uiSrv); err != nil {
			slog.Error("River UI server error", "err", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down River worker...")
	if err := riverClient.Stop(ctx); err != nil {
		slog.Error("river stop", "err", err)
	}
}
