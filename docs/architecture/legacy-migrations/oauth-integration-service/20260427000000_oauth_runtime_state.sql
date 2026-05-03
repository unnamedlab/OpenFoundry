CREATE TABLE IF NOT EXISTS oauth_pending_auth (
    authorization_code    TEXT PRIMARY KEY,
    client_id             TEXT NOT NULL,
    redirect_uri          TEXT NOT NULL,
    scopes                JSONB NOT NULL DEFAULT '[]'::jsonb,
    user_id               UUID NOT NULL,
    code_challenge        TEXT NOT NULL,
    code_challenge_method TEXT NOT NULL,
    issued_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS oauth_token_exchange (
    access_token   TEXT PRIMARY KEY,
    client_id      TEXT NOT NULL,
    user_id        UUID NOT NULL,
    scopes         JSONB NOT NULL DEFAULT '[]'::jsonb,
    expires_at     TIMESTAMPTZ NOT NULL,
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_oauth_pending_auth_client
    ON oauth_pending_auth (client_id, issued_at DESC);

CREATE INDEX IF NOT EXISTS idx_oauth_token_exchange_expires_at
    ON oauth_token_exchange (expires_at);
