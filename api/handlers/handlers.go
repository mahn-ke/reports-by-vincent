package handlers

import "github.com/jackc/pgx/v5/pgxpool"

// Handler holds shared dependencies for all route handlers.
type Handler struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}
