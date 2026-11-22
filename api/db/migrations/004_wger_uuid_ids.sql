-- wger 2.7 identifies workout logs and sessions by UUID (PowerSync), not by integer.
-- Idempotent: altering a TEXT column to TEXT is a no-op.
ALTER TABLE workout_log_entries ALTER COLUMN wger_id TYPE TEXT USING wger_id::text;
ALTER TABLE workout_log_entries ALTER COLUMN session_id TYPE TEXT USING session_id::text;
ALTER TABLE workout_sessions ALTER COLUMN id TYPE TEXT USING id::text;
