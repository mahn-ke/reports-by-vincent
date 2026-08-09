CREATE TABLE IF NOT EXISTS nutrition_logs (
    id         SERIAL PRIMARY KEY,
    date       DATE NOT NULL UNIQUE,
    calories   DOUBLE PRECISION,
    carbs      DOUBLE PRECISION,
    fat        DOUBLE PRECISION,
    protein    DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS training_logs (
    id         SERIAL PRIMARY KEY,
    date       DATE NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS body_measurements (
    id                   SERIAL PRIMARY KEY,
    measured_at          TIMESTAMPTZ NOT NULL UNIQUE,
    weight               DOUBLE PRECISION,
    bmi                  DOUBLE PRECISION,
    body_fat             DOUBLE PRECISION,
    skeletal_muscle_mass DOUBLE PRECISION,
    bone_mass            DOUBLE PRECISION,
    body_water           DOUBLE PRECISION,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
