CREATE TABLE IF NOT EXISTS cipher_permissions (
    id UUID PRIMARY KEY,
    resource TEXT NOT NULL,
    action TEXT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cipher_channels (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    release_channel TEXT NOT NULL,
    allowed_operations JSONB NOT NULL DEFAULT '[]'::jsonb,
    license_tier TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cipher_licenses (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    tier TEXT NOT NULL,
    features JSONB NOT NULL DEFAULT '[]'::jsonb,
    issued_by UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO cipher_permissions (id, resource, action, description) VALUES
    ('01968522-2f75-7f3a-bb5d-000000000501', 'cipher', 'use', 'Use cryptographic operations'),
    ('01968522-2f75-7f3a-bb5d-000000000502', 'cipher', 'govern', 'Manage channels and licenses')
ON CONFLICT (id) DO NOTHING;

INSERT INTO cipher_channels (id, name, release_channel, allowed_operations, license_tier) VALUES
    ('01968522-2f75-7f3a-bb5d-000000000503', 'default', 'stable', '["hash","sign","verify","encrypt","decrypt"]'::jsonb, 'enterprise'),
    ('01968522-2f75-7f3a-bb5d-000000000504', 'compliance', 'regulated', '["hash","sign","verify"]'::jsonb, 'regulated')
ON CONFLICT (id) DO NOTHING;

INSERT INTO cipher_licenses (id, name, tier, features) VALUES
    ('01968522-2f75-7f3a-bb5d-000000000505', 'Enterprise Cipher', 'enterprise', '["hash","sign","verify","encrypt","decrypt"]'::jsonb),
    ('01968522-2f75-7f3a-bb5d-000000000506', 'Regulated Cipher', 'regulated', '["hash","sign","verify","channel_controls"]'::jsonb)
ON CONFLICT (id) DO NOTHING;
