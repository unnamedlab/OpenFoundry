CREATE TABLE IF NOT EXISTS modeling_objectives (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    success_criteria JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS model_submissions (
    id UUID PRIMARY KEY,
    model_id UUID NOT NULL,
    version TEXT NOT NULL,
    stage TEXT NOT NULL DEFAULT 'submitted',
    status TEXT NOT NULL DEFAULT 'pending',
    objective_id UUID REFERENCES modeling_objectives(id) ON DELETE SET NULL,
    release_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (model_id, version)
);

CREATE INDEX IF NOT EXISTS idx_model_submissions_model_id ON model_submissions(model_id);
CREATE INDEX IF NOT EXISTS idx_model_submissions_status ON model_submissions(status);

CREATE TABLE IF NOT EXISTS model_lifecycle_events (
    id UUID PRIMARY KEY,
    submission_id UUID NOT NULL REFERENCES model_submissions(id) ON DELETE CASCADE,
    stage TEXT NOT NULL,
    status TEXT NOT NULL,
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_model_lifecycle_events_submission_id ON model_lifecycle_events(submission_id);
