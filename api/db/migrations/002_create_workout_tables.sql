CREATE TABLE IF NOT EXISTS workout_exercises (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workout_log_entries (
    id          SERIAL PRIMARY KEY,
    exercise_id INTEGER NOT NULL REFERENCES workout_exercises(id),
    date        DATE NOT NULL,
    weight      DOUBLE PRECISION NOT NULL DEFAULT 0,
    repetitions INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (exercise_id, date, weight, repetitions)
);
