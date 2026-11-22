-- wger routine days with the exercises they plan; the calendar matches logged days against them
-- (a session's own day link follows wger's schedule, not what was actually trained).
CREATE TABLE IF NOT EXISTS workout_plan_days (
    day_id       INTEGER PRIMARY KEY,
    routine_id   INTEGER NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    is_rest      BOOLEAN NOT NULL DEFAULT FALSE,
    exercise_ids INTEGER[] NOT NULL DEFAULT '{}'
);
