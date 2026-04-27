CREATE TABLE IF NOT EXISTS network_boundary_policies (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    direction TEXT NOT NULL,
    boundary_kind TEXT NOT NULL,
    allowed_hosts JSONB NOT NULL DEFAULT '[]'::jsonb,
    blocked_hosts JSONB NOT NULL DEFAULT '[]'::jsonb,
    allow_private_networks BOOLEAN NOT NULL DEFAULT FALSE,
    allow_insecure_http BOOLEAN NOT NULL DEFAULT FALSE,
    proxy_mode TEXT NOT NULL DEFAULT 'direct',
    private_link_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS network_private_links (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    target_host TEXT NOT NULL,
    transport TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS network_proxy_definitions (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    proxy_url TEXT NOT NULL,
    mode TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO network_boundary_policies (
    id, name, direction, boundary_kind, allowed_hosts, blocked_hosts, allow_private_networks, allow_insecure_http, proxy_mode, private_link_enabled, updated_by
)
VALUES
    (
        '01968522-2f75-7f3a-bb5d-000000000801',
        'Gateway ingress baseline',
        'ingress',
        'proxy',
        '["app.openfoundry.example","localhost"]'::jsonb,
        '[]'::jsonb,
        FALSE,
        FALSE,
        'reverse-proxy',
        FALSE,
        'network-boundary-service'
    ),
    (
        '01968522-2f75-7f3a-bb5d-000000000802',
        'Connector egress baseline',
        'egress',
        'private-link',
        '[]'::jsonb,
        '[]'::jsonb,
        TRUE,
        FALSE,
        'direct',
        TRUE,
        'network-boundary-service'
    )
ON CONFLICT (id) DO NOTHING;

INSERT INTO network_private_links (id, name, target_host, transport, enabled)
VALUES
    ('01968522-2f75-7f3a-bb5d-000000000803', 'default-private-link', '10.0.0.4', 'https', TRUE)
ON CONFLICT (id) DO NOTHING;

INSERT INTO network_proxy_definitions (id, name, proxy_url, mode, enabled)
VALUES
    ('01968522-2f75-7f3a-bb5d-000000000804', 'gateway-edge-proxy', 'http://edge-gateway-service:8080', 'reverse-proxy', TRUE)
ON CONFLICT (id) DO NOTHING;
