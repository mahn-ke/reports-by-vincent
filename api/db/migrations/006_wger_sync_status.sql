-- Progress of the current/last wger sync, written by the jobs worker and polled by the calendar page.
CREATE TABLE IF NOT EXISTS wger_sync_status (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    state       TEXT NOT NULL,             -- queued | running | done | failed | skipped
    phase       TEXT NOT NULL DEFAULT '',
    percent     DOUBLE PRECISION NOT NULL DEFAULT 0,
    message     TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
