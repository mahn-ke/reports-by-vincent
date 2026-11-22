-- For the app's overload baseline ("the first N planned sets of the previous session"):
-- when each set was logged (set order within a day) and the planned sets of each routine slot.
ALTER TABLE workout_log_entries ADD COLUMN IF NOT EXISTS logged_at TIMESTAMPTZ;
-- [{slot_entry_id, exercise_id, sets: [{iteration, value}]}]
ALTER TABLE workout_plan_days ADD COLUMN IF NOT EXISTS entries JSONB NOT NULL DEFAULT '[]';
