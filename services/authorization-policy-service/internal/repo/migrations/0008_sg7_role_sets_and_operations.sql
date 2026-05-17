-- 0008: SG.7 — Role and operation model.
--
-- Slice 0007 shipped tenant-scoped roles/permissions/groups CRUD.
-- SG.7 layers four parity-required surfaces on top:
--
--   1. operations             — low-level capability tokens (extends
--                                the existing permissions table by
--                                widening the action vocabulary
--                                with the standard Foundry verbs).
--   2. role_sets              — a named bundle of roles tied to a
--                                resource context: project, ontology,
--                                restricted_view, platform_admin.
--   3. role_set_roles         — role ∈ set with an integer rank
--                                used by the delegation check
--                                (a grantor can only grant a role
--                                with rank ≤ their own role rank in
--                                the same set).
--   4. seeded default rows   — Discoverer / Viewer / Editor / Owner
--                                in each of the four canonical role
--                                sets, with rank 1..4.
--
-- All schema is additive; existing tenant_id-scoped rows keep
-- working.

-- ─── role_sets ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS role_sets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NULL,
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    context     TEXT NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT role_sets_tenant_slug_unique UNIQUE (tenant_id, slug),
    CONSTRAINT role_sets_context_check
        CHECK (context IN ('project', 'ontology', 'restricted_view', 'platform_admin'))
);

CREATE UNIQUE INDEX IF NOT EXISTS role_sets_global_slug_unique
    ON role_sets (slug)
    WHERE tenant_id IS NULL;

-- ─── role_set_roles ───────────────────────────────────────────────────
--
-- Membership of a role in a role set, plus the integer rank that
-- governs delegation. Rank is open-ended (positive integers, ascending
-- = more authority); the seeded Foundry-parity rows use 1..4.
CREATE TABLE IF NOT EXISTS role_set_roles (
    role_set_id UUID NOT NULL REFERENCES role_sets(id) ON DELETE CASCADE,
    role_id     UUID NOT NULL REFERENCES roles(id)     ON DELETE CASCADE,
    rank        INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (role_set_id, role_id),
    CONSTRAINT role_set_roles_rank_positive CHECK (rank > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS role_set_roles_unique_rank
    ON role_set_roles (role_set_id, rank);

CREATE INDEX IF NOT EXISTS role_set_roles_role_idx
    ON role_set_roles (role_id);

-- ─── Operation catalog widening ───────────────────────────────────────
--
-- SG.7 calls out the low-level operations every service should check:
-- create / read / edit / delete / manage / build / apply / share. The
-- existing permissions table already stores resource:action; here we
-- seed the platform-scoped (tenant_id = NULL) rows that role sets
-- attach to.
INSERT INTO permissions (id, resource, action, description) VALUES
    ('0196c3f1-7000-7000-8000-000000000001', 'project',         'discover', 'See that a project exists (Discoverer floor)'),
    ('0196c3f1-7000-7000-8000-000000000002', 'project',         'read',     'Read project metadata and resources'),
    ('0196c3f1-7000-7000-8000-000000000003', 'project',         'edit',     'Edit project resources'),
    ('0196c3f1-7000-7000-8000-000000000004', 'project',         'manage',   'Manage project roles, references, contact'),
    ('0196c3f1-7000-7000-8000-000000000005', 'project',         'build',    'Trigger builds on project resources'),
    ('0196c3f1-7000-7000-8000-000000000006', 'project',         'apply',    'Apply actions on project objects'),
    ('0196c3f1-7000-7000-8000-000000000007', 'project',         'share',    'Share project content with other principals'),

    ('0196c3f1-7000-7000-8000-000000000011', 'ontology',        'discover', 'See ontology objects exist'),
    ('0196c3f1-7000-7000-8000-000000000012', 'ontology',        'read',     'Read ontology types and objects'),
    ('0196c3f1-7000-7000-8000-000000000013', 'ontology',        'edit',     'Edit ontology object instances'),
    ('0196c3f1-7000-7000-8000-000000000014', 'ontology',        'manage',   'Manage ontology types and actions'),
    ('0196c3f1-7000-7000-8000-000000000015', 'ontology',        'apply',    'Apply ontology actions'),

    ('0196c3f1-7000-7000-8000-000000000021', 'restricted_view', 'read',     'Read restricted view rows'),
    ('0196c3f1-7000-7000-8000-000000000022', 'restricted_view', 'edit',     'Edit restricted view policy'),
    ('0196c3f1-7000-7000-8000-000000000023', 'restricted_view', 'manage',   'Manage restricted view permissions and lifecycle'),

    ('0196c3f1-7000-7000-8000-000000000031', 'platform',        'read',     'Read platform admin configuration'),
    ('0196c3f1-7000-7000-8000-000000000032', 'platform',        'manage',   'Manage platform admin configuration'),
    ('0196c3f1-7000-7000-8000-000000000033', 'platform',        'audit',    'Read audit logs and security findings')
ON CONFLICT (id) DO NOTHING;

-- ─── Default roles per context ────────────────────────────────────────
INSERT INTO roles (id, name, description) VALUES
    -- project
    ('0196c3f1-7100-7000-8000-000000000001', 'project_discoverer', 'Default Discoverer role on a project'),
    ('0196c3f1-7100-7000-8000-000000000002', 'project_viewer',     'Default Viewer role on a project'),
    ('0196c3f1-7100-7000-8000-000000000003', 'project_editor',     'Default Editor role on a project'),
    ('0196c3f1-7100-7000-8000-000000000004', 'project_owner',      'Default Owner role on a project'),
    -- ontology
    ('0196c3f1-7100-7000-8000-000000000011', 'ontology_discoverer', 'Default Discoverer role on ontology'),
    ('0196c3f1-7100-7000-8000-000000000012', 'ontology_viewer',     'Default Viewer role on ontology'),
    ('0196c3f1-7100-7000-8000-000000000013', 'ontology_editor',     'Default Editor role on ontology'),
    ('0196c3f1-7100-7000-8000-000000000014', 'ontology_owner',      'Default Owner role on ontology'),
    -- restricted view
    ('0196c3f1-7100-7000-8000-000000000022', 'restricted_view_viewer', 'Default Viewer role on a restricted view'),
    ('0196c3f1-7100-7000-8000-000000000023', 'restricted_view_editor', 'Default Editor role on a restricted view'),
    ('0196c3f1-7100-7000-8000-000000000024', 'restricted_view_owner',  'Default Owner role on a restricted view'),
    -- platform admin
    ('0196c3f1-7100-7000-8000-000000000031', 'platform_viewer', 'Default Viewer role on platform admin'),
    ('0196c3f1-7100-7000-8000-000000000033', 'platform_admin',  'Default platform administrator')
ON CONFLICT (id) DO NOTHING;

-- ─── Role-set seeds ───────────────────────────────────────────────────
INSERT INTO role_sets (id, slug, name, context, description) VALUES
    ('0196c3f1-7200-7000-8000-000000000001',
     'project-default', 'Project default roles', 'project',
     'Owner / Editor / Viewer / Discoverer lattice applied on a project'),
    ('0196c3f1-7200-7000-8000-000000000002',
     'ontology-default', 'Ontology default roles', 'ontology',
     'Owner / Editor / Viewer / Discoverer lattice applied on ontology'),
    ('0196c3f1-7200-7000-8000-000000000003',
     'restricted-view-default', 'Restricted view default roles', 'restricted_view',
     'Owner / Editor / Viewer lattice applied on a restricted view'),
    ('0196c3f1-7200-7000-8000-000000000004',
     'platform-admin-default', 'Platform admin default roles', 'platform_admin',
     'Viewer / Admin lattice for platform administration')
ON CONFLICT (id) DO NOTHING;

-- ─── role_set_roles seeds (rank: 1..N, ascending = more authority) ────
INSERT INTO role_set_roles (role_set_id, role_id, rank) VALUES
    -- project: discoverer=1, viewer=2, editor=3, owner=4
    ('0196c3f1-7200-7000-8000-000000000001', '0196c3f1-7100-7000-8000-000000000001', 1),
    ('0196c3f1-7200-7000-8000-000000000001', '0196c3f1-7100-7000-8000-000000000002', 2),
    ('0196c3f1-7200-7000-8000-000000000001', '0196c3f1-7100-7000-8000-000000000003', 3),
    ('0196c3f1-7200-7000-8000-000000000001', '0196c3f1-7100-7000-8000-000000000004', 4),
    -- ontology: discoverer=1, viewer=2, editor=3, owner=4
    ('0196c3f1-7200-7000-8000-000000000002', '0196c3f1-7100-7000-8000-000000000011', 1),
    ('0196c3f1-7200-7000-8000-000000000002', '0196c3f1-7100-7000-8000-000000000012', 2),
    ('0196c3f1-7200-7000-8000-000000000002', '0196c3f1-7100-7000-8000-000000000013', 3),
    ('0196c3f1-7200-7000-8000-000000000002', '0196c3f1-7100-7000-8000-000000000014', 4),
    -- restricted view: viewer=1, editor=2, owner=3
    ('0196c3f1-7200-7000-8000-000000000003', '0196c3f1-7100-7000-8000-000000000022', 1),
    ('0196c3f1-7200-7000-8000-000000000003', '0196c3f1-7100-7000-8000-000000000023', 2),
    ('0196c3f1-7200-7000-8000-000000000003', '0196c3f1-7100-7000-8000-000000000024', 3),
    -- platform admin: viewer=1, admin=2
    ('0196c3f1-7200-7000-8000-000000000004', '0196c3f1-7100-7000-8000-000000000031', 1),
    ('0196c3f1-7200-7000-8000-000000000004', '0196c3f1-7100-7000-8000-000000000033', 2)
ON CONFLICT (role_set_id, role_id) DO NOTHING;

-- ─── role_permissions seeds — bind each default role to its operations
INSERT INTO role_permissions (role_id, permission_id) VALUES
    -- project_discoverer
    ('0196c3f1-7100-7000-8000-000000000001', '0196c3f1-7000-7000-8000-000000000001'),
    -- project_viewer
    ('0196c3f1-7100-7000-8000-000000000002', '0196c3f1-7000-7000-8000-000000000001'),
    ('0196c3f1-7100-7000-8000-000000000002', '0196c3f1-7000-7000-8000-000000000002'),
    -- project_editor
    ('0196c3f1-7100-7000-8000-000000000003', '0196c3f1-7000-7000-8000-000000000001'),
    ('0196c3f1-7100-7000-8000-000000000003', '0196c3f1-7000-7000-8000-000000000002'),
    ('0196c3f1-7100-7000-8000-000000000003', '0196c3f1-7000-7000-8000-000000000003'),
    ('0196c3f1-7100-7000-8000-000000000003', '0196c3f1-7000-7000-8000-000000000005'),
    ('0196c3f1-7100-7000-8000-000000000003', '0196c3f1-7000-7000-8000-000000000006'),
    -- project_owner
    ('0196c3f1-7100-7000-8000-000000000004', '0196c3f1-7000-7000-8000-000000000001'),
    ('0196c3f1-7100-7000-8000-000000000004', '0196c3f1-7000-7000-8000-000000000002'),
    ('0196c3f1-7100-7000-8000-000000000004', '0196c3f1-7000-7000-8000-000000000003'),
    ('0196c3f1-7100-7000-8000-000000000004', '0196c3f1-7000-7000-8000-000000000004'),
    ('0196c3f1-7100-7000-8000-000000000004', '0196c3f1-7000-7000-8000-000000000005'),
    ('0196c3f1-7100-7000-8000-000000000004', '0196c3f1-7000-7000-8000-000000000006'),
    ('0196c3f1-7100-7000-8000-000000000004', '0196c3f1-7000-7000-8000-000000000007'),
    -- ontology_discoverer
    ('0196c3f1-7100-7000-8000-000000000011', '0196c3f1-7000-7000-8000-000000000011'),
    -- ontology_viewer
    ('0196c3f1-7100-7000-8000-000000000012', '0196c3f1-7000-7000-8000-000000000011'),
    ('0196c3f1-7100-7000-8000-000000000012', '0196c3f1-7000-7000-8000-000000000012'),
    -- ontology_editor
    ('0196c3f1-7100-7000-8000-000000000013', '0196c3f1-7000-7000-8000-000000000011'),
    ('0196c3f1-7100-7000-8000-000000000013', '0196c3f1-7000-7000-8000-000000000012'),
    ('0196c3f1-7100-7000-8000-000000000013', '0196c3f1-7000-7000-8000-000000000013'),
    ('0196c3f1-7100-7000-8000-000000000013', '0196c3f1-7000-7000-8000-000000000015'),
    -- ontology_owner
    ('0196c3f1-7100-7000-8000-000000000014', '0196c3f1-7000-7000-8000-000000000011'),
    ('0196c3f1-7100-7000-8000-000000000014', '0196c3f1-7000-7000-8000-000000000012'),
    ('0196c3f1-7100-7000-8000-000000000014', '0196c3f1-7000-7000-8000-000000000013'),
    ('0196c3f1-7100-7000-8000-000000000014', '0196c3f1-7000-7000-8000-000000000014'),
    ('0196c3f1-7100-7000-8000-000000000014', '0196c3f1-7000-7000-8000-000000000015'),
    -- restricted_view_viewer
    ('0196c3f1-7100-7000-8000-000000000022', '0196c3f1-7000-7000-8000-000000000021'),
    -- restricted_view_editor
    ('0196c3f1-7100-7000-8000-000000000023', '0196c3f1-7000-7000-8000-000000000021'),
    ('0196c3f1-7100-7000-8000-000000000023', '0196c3f1-7000-7000-8000-000000000022'),
    -- restricted_view_owner
    ('0196c3f1-7100-7000-8000-000000000024', '0196c3f1-7000-7000-8000-000000000021'),
    ('0196c3f1-7100-7000-8000-000000000024', '0196c3f1-7000-7000-8000-000000000022'),
    ('0196c3f1-7100-7000-8000-000000000024', '0196c3f1-7000-7000-8000-000000000023'),
    -- platform_viewer
    ('0196c3f1-7100-7000-8000-000000000031', '0196c3f1-7000-7000-8000-000000000031'),
    -- platform_admin
    ('0196c3f1-7100-7000-8000-000000000033', '0196c3f1-7000-7000-8000-000000000031'),
    ('0196c3f1-7100-7000-8000-000000000033', '0196c3f1-7000-7000-8000-000000000032'),
    ('0196c3f1-7100-7000-8000-000000000033', '0196c3f1-7000-7000-8000-000000000033')
ON CONFLICT (role_id, permission_id) DO NOTHING;
