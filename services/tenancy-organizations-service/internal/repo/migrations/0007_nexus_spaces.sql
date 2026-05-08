-- Phase 1 (TO-2/TO-4): Nexus federation foundation tables.
--
-- The Rust crate `services/tenancy-organizations-service` defines the
-- nexus_peers + nexus_spaces tables across two source migrations:
--   migrations/20260423091500_nexus_foundation.sql       (peers + foundation)
--   migrations/20260425223000_spaces_and_admin_lifecycle.sql (peer extras + spaces)
--
-- The Go port consolidates both into a single migration so the schema
-- arrives in lock-step with the spaces handler. Columns and types are
-- byte-identical to the Rust definition; the FK on owner_peer_id and the
-- JSONB shapes for member_peer_ids / governance_tags are preserved
-- verbatim because the spaces handler validates peer references against
-- nexus_peers and serialises both arrays via serde_json::to_value.

CREATE TABLE IF NOT EXISTS nexus_peers (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    organization_type TEXT NOT NULL DEFAULT 'partner',
    region TEXT NOT NULL,
    endpoint_url TEXT NOT NULL,
    auth_mode TEXT NOT NULL,
    trust_level TEXT NOT NULL,
    public_key_fingerprint TEXT NOT NULL,
    shared_scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL,
    lifecycle_stage TEXT NOT NULL DEFAULT 'onboarding',
    admin_contacts JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_handshake_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS nexus_spaces (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL,
    space_kind TEXT NOT NULL,
    owner_peer_id UUID REFERENCES nexus_peers(id) ON DELETE SET NULL,
    region TEXT NOT NULL,
    member_peer_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    governance_tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
