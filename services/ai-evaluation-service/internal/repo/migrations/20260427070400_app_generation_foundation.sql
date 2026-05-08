CREATE TABLE IF NOT EXISTS app_generation_seeds (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    template JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS app_generation_sessions (
    id UUID PRIMARY KEY,
    seed_id UUID REFERENCES app_generation_seeds(id) ON DELETE SET NULL,
    goal TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    context JSONB NOT NULL DEFAULT '{}'::jsonb,
    generated_app_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_app_gen_sessions_status ON app_generation_sessions(status);

CREATE TABLE IF NOT EXISTS app_generation_events (
    id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES app_generation_sessions(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_app_gen_events_session_id ON app_generation_events(session_id);
