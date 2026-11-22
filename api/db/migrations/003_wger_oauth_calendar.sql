-- Key workout sets by their wger log id so identical sets on one day are kept
-- as separate rows (the old natural key collapsed e.g. 3x10@60kg into one set).
ALTER TABLE workout_log_entries ADD COLUMN IF NOT EXISTS wger_id INTEGER UNIQUE;
ALTER TABLE workout_log_entries ADD COLUMN IF NOT EXISTS session_id INTEGER;
ALTER TABLE workout_log_entries DROP CONSTRAINT IF EXISTS workout_log_entries_exercise_id_date_weight_repetitions_key;
-- Rows from before wger ids were stored are collapsed duplicates; the next sync refills them.
DELETE FROM workout_log_entries WHERE wger_id IS NULL;

ALTER TABLE workout_exercises ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT '';
ALTER TABLE workout_exercises ADD COLUMN IF NOT EXISTS muscles TEXT[] NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS workout_sessions (
    id       INTEGER PRIMARY KEY,
    date     DATE NOT NULL,
    day_name TEXT NOT NULL DEFAULT ''
);

-- OAuth tokens reports holds for wger (single row).
CREATE TABLE IF NOT EXISTS wger_oauth (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    access_token  TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
