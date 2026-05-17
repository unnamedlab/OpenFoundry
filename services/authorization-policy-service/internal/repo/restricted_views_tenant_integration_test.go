//go:build integration

// Integration coverage for the cross-tenant restricted_views isolation
// fix in this service's read path. The schema is owned by
// identity-federation-service (slice 7a + 0017 follow-up); the
// authorization-policy-service repo is read-only against the same
// table. We can't import the identity-federation internal package
// from here, so the test stands up the minimal schema inline — close
// enough to the live shape to exercise
// ListEnabledRestrictedViewsMatching with realistic data.
//
// Asserts the tenancy contract: a row authored by tenant A must not
// appear in a tenant B evaluation, even when (resource, action)
// matches via wildcards. Pre-0017 the query ignored tenant_id and
// returned every enabled row — this test fails loudly if anyone
// reintroduces that bug.

package repo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/repo"
)

// restrictedViewsSchema mirrors the columns the read path in
// repo.go needs. Indexes are included to keep the planner shape
// representative of production.
const restrictedViewsSchema = `
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE restricted_views (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID NOT NULL,
    name                  TEXT NOT NULL,
    description           TEXT,
    resource              TEXT NOT NULL,
    action                TEXT NOT NULL,
    conditions            JSONB NOT NULL DEFAULT '{}'::jsonb,
    row_filter            TEXT,
    hidden_columns        JSONB NOT NULL DEFAULT '[]'::jsonb,
    marking_columns       JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_org_ids       JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_markings      JSONB NOT NULL DEFAULT '[]'::jsonb,
    consumer_mode_enabled BOOLEAN NOT NULL DEFAULT false,
    allow_guest_access    BOOLEAN NOT NULL DEFAULT false,
    enabled               BOOLEAN NOT NULL DEFAULT true,
    created_by            UUID,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_restricted_views_lookup
    ON restricted_views (resource, action, enabled, created_at DESC);
CREATE INDEX idx_restricted_views_tenant_enabled
    ON restricted_views (tenant_id, enabled);
`

func TestListEnabledRestrictedViewsMatchingFiltersByTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := testingx.BootPostgres(ctx, t)
	h.MustExec(ctx, restrictedViewsSchema)

	r := &repo.Repo{Pool: h.Pool}

	tenantA := uuid.New()
	tenantB := uuid.New()
	viewA := insertView(t, ctx, h, tenantA, "datasets", "read", true)
	viewB := insertView(t, ctx, h, tenantB, "datasets", "read", true)
	insertView(t, ctx, h, tenantA, "datasets", "read", false) // disabled in A
	wildcardB := insertView(t, ctx, h, tenantB, "*", "*", true)

	t.Run("tenantA sees only its enabled view", func(t *testing.T) {
		got, err := r.ListEnabledRestrictedViewsMatching(ctx, tenantA, "datasets", "read")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, viewA, got[0].ID)
		assert.Equal(t, tenantA, got[0].TenantID)
	})

	t.Run("tenantB sees only its rows including wildcard", func(t *testing.T) {
		got, err := r.ListEnabledRestrictedViewsMatching(ctx, tenantB, "datasets", "read")
		require.NoError(t, err)
		ids := idsOf(got)
		assert.ElementsMatch(t, []uuid.UUID{viewB, wildcardB}, ids)
		for _, v := range got {
			assert.Equal(t, tenantB, v.TenantID)
		}
	})

	t.Run("tenantA does not see tenantB wildcard on an unrelated resource", func(t *testing.T) {
		got, err := r.ListEnabledRestrictedViewsMatching(ctx, tenantA, "anything", "read")
		require.NoError(t, err)
		assert.Empty(t, got, "tenantA must not inherit tenantB's wildcard")
	})

	t.Run("uuid.Nil tenant matches no rows (default deny)", func(t *testing.T) {
		got, err := r.ListEnabledRestrictedViewsMatching(ctx, uuid.Nil, "datasets", "read")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func insertView(t *testing.T, ctx context.Context, h *testingx.PostgresHarness, tenantID uuid.UUID, resource, action string, enabled bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO restricted_views (
		    id, tenant_id, name, resource, action, conditions, row_filter,
		    hidden_columns, allowed_org_ids, allowed_markings,
		    consumer_mode_enabled, allow_guest_access, enabled
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		id, tenantID, "rv-"+id.String()[:8], resource, action,
		json.RawMessage(`{}`), nil,
		json.RawMessage(`["ssn"]`), json.RawMessage(`[]`), json.RawMessage(`["public"]`),
		false, false, enabled,
	)
	require.NoError(t, err)
	return id
}

func idsOf(views []repo.RestrictedView) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(views))
	for _, v := range views {
		out = append(out, v.ID)
	}
	return out
}
