//go:build integration

// Integration tests for retention-policy tenant isolation.
//
// Seeds the audit-compliance migration set against an ephemeral
// Postgres, creates two orgs with one policy each, then drives the
// HTTP handlers to confirm:
//
//   - Org A only sees Org A's policy on list, plus the system policy
//     from migration 0005.
//   - Org A receives 404 when fetching/updating/deleting Org B's
//     policy (rather than a 200 leak).
//   - Cross-org access from a non-admin via ?org_id=<other> is 403.
//   - Admin callers see every policy and can target an arbitrary org
//     for create/update/delete.
//
// Run with: `go test -tags=integration -race ./services/audit-compliance-service/...`.
package retentionpolicy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/retentionpolicy"
)

func withChiURLParam(req *http.Request, key, val string) *http.Request {
	rctx := chi.RouteContext(req.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, val)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func mustClaims(orgID *uuid.UUID, roles ...string) *authmw.Claims {
	return &authmw.Claims{Sub: uuid.New(), OrgID: orgID, Roles: roles}
}

func newAuthedRequest(t *testing.T, method, target string, body any, claims *authmw.Claims) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	r := httptest.NewRequest(method, target, &buf)
	if claims != nil {
		r = r.WithContext(authmw.ContextWithClaims(r.Context(), claims))
	}
	return r
}

func seedPolicy(t *testing.T, h *retentionpolicy.Handlers, claims *authmw.Claims, name string) *models.RetentionPolicy {
	t.Helper()
	req := newAuthedRequest(t, http.MethodPost, "/api/v1/retention/policies", retentionpolicy.CreateRetentionPolicyRequest{
		Name:          name,
		TargetKind:    "dataset",
		RetentionDays: 30,
		PurgeMode:     "hard-delete-after-ttl",
		UpdatedBy:     "tenant-admin",
	}, claims)
	rec := httptest.NewRecorder()
	h.CreatePolicyHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed %s failed: status=%d body=%s", name, rec.Code, rec.Body.String())
	}
	var out models.RetentionPolicy
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode seeded policy: %v", err)
	}
	return &out
}

func listNames(t *testing.T, h *retentionpolicy.Handlers, claims *authmw.Claims, query string) (int, []string) {
	t.Helper()
	target := "/api/v1/retention/policies"
	if query != "" {
		target += "?" + query
	}
	rec := httptest.NewRecorder()
	h.ListPolicies(rec, newAuthedRequest(t, http.MethodGet, target, nil, claims))
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	var policies []models.RetentionPolicy
	if err := json.NewDecoder(rec.Body).Decode(&policies); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	out := make([]string, 0, len(policies))
	for _, p := range policies {
		out = append(out, p.Name)
	}
	return rec.Code, out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestTenantIsolation_TwoOrgsListGetUpdateDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pg := testingx.BootPostgres(ctx, t)
	if err := repo.Migrate(ctx, pg.Pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	h := retentionpolicy.New(pg.Pool)

	orgA := uuid.New()
	orgB := uuid.New()
	tenantA := mustClaims(&orgA, "viewer")
	tenantB := mustClaims(&orgB, "viewer")
	platformAdmin := mustClaims(nil, "admin")

	policyA := seedPolicy(t, h, tenantA, "policy-A")
	policyB := seedPolicy(t, h, tenantB, "policy-B")

	if policyA.OrgID == nil || *policyA.OrgID != orgA {
		t.Fatalf("policy-A org_id = %v, want %v", policyA.OrgID, orgA)
	}
	if policyB.OrgID == nil || *policyB.OrgID != orgB {
		t.Fatalf("policy-B org_id = %v, want %v", policyB.OrgID, orgB)
	}

	// ── List: each tenant sees its own + system, never the other ──
	codeA, namesA := listNames(t, h, tenantA, "")
	if codeA != http.StatusOK {
		t.Fatalf("tenant A list status %d", codeA)
	}
	if !contains(namesA, "policy-A") {
		t.Fatalf("tenant A list missing own policy: %v", namesA)
	}
	if contains(namesA, "policy-B") {
		t.Fatalf("tenant A list leaks policy-B: %v", namesA)
	}
	if !contains(namesA, "DELETE_ABORTED_TRANSACTIONS") {
		t.Fatalf("tenant A list missing system policy: %v", namesA)
	}

	codeB, namesB := listNames(t, h, tenantB, "")
	if codeB != http.StatusOK {
		t.Fatalf("tenant B list status %d", codeB)
	}
	if contains(namesB, "policy-A") {
		t.Fatalf("tenant B list leaks policy-A: %v", namesB)
	}

	// ── Cross-org via query is 403 for non-admin ──
	rec := httptest.NewRecorder()
	h.ListPolicies(rec, newAuthedRequest(t, http.MethodGet,
		"/api/v1/retention/policies?org_id="+orgB.String(), nil, tenantA))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant A cross-org list status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	// ── Get cross-org returns 404 for non-admin (looks like missing) ──
	rec = httptest.NewRecorder()
	r := newAuthedRequest(t, http.MethodGet, "/api/v1/retention/policies/"+policyB.ID.String(), nil, tenantA)
	r = withChiURLParam(r, "id", policyB.ID.String())
	h.GetPolicyHandler(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tenant A get policy-B status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	// ── Update cross-org returns 404 ──
	updateBody := retentionpolicy.UpdateRetentionPolicyRequest{
		RetentionDays: ptrInt32(7),
	}
	rec = httptest.NewRecorder()
	r = newAuthedRequest(t, http.MethodPatch, "/api/v1/retention/policies/"+policyB.ID.String(), updateBody, tenantA)
	r = withChiURLParam(r, "id", policyB.ID.String())
	h.UpdatePolicyHandler(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tenant A update policy-B status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	// Confirm policy-B retention_days is still 30 (unchanged).
	var rd int32
	if err := pg.Pool.QueryRow(ctx, `SELECT retention_days FROM retention_policies WHERE id = $1`, policyB.ID).Scan(&rd); err != nil {
		t.Fatalf("read policy-B: %v", err)
	}
	if rd != 30 {
		t.Fatalf("policy-B retention_days = %d, want 30 (tenant A must not mutate)", rd)
	}

	// ── Delete cross-org returns 404 ──
	rec = httptest.NewRecorder()
	r = newAuthedRequest(t, http.MethodDelete, "/api/v1/retention/policies/"+policyB.ID.String(), nil, tenantA)
	r = withChiURLParam(r, "id", policyB.ID.String())
	h.DeletePolicyHandler(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tenant A delete policy-B status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var stillThere int
	if err := pg.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM retention_policies WHERE id = $1`, policyB.ID).Scan(&stillThere); err != nil {
		t.Fatalf("count policy-B: %v", err)
	}
	if stillThere != 1 {
		t.Fatalf("policy-B was deleted by tenant A (count=%d)", stillThere)
	}

	// ── Admin sees both via AllOrgs ──
	codeAdmin, namesAdmin := listNames(t, h, platformAdmin, "all_orgs=true")
	if codeAdmin != http.StatusOK {
		t.Fatalf("admin list status %d", codeAdmin)
	}
	if !contains(namesAdmin, "policy-A") || !contains(namesAdmin, "policy-B") {
		t.Fatalf("admin must see both, got %v", namesAdmin)
	}

	// ── Admin scoped to a specific org sees only that org + system ──
	codeAdminA, namesAdminA := listNames(t, h, platformAdmin, "org_id="+orgA.String())
	if codeAdminA != http.StatusOK {
		t.Fatalf("admin scoped list status %d", codeAdminA)
	}
	if !contains(namesAdminA, "policy-A") {
		t.Fatalf("admin scoped to A missing policy-A: %v", namesAdminA)
	}
	if contains(namesAdminA, "policy-B") {
		t.Fatalf("admin scoped to A leaked policy-B: %v", namesAdminA)
	}

	// ── Admin can cross-org update + delete ──
	updateBody = retentionpolicy.UpdateRetentionPolicyRequest{
		RetentionDays: ptrInt32(11),
	}
	rec = httptest.NewRecorder()
	r = newAuthedRequest(t, http.MethodPatch,
		"/api/v1/retention/policies/"+policyB.ID.String()+"?org_id="+orgB.String(),
		updateBody, platformAdmin)
	r = withChiURLParam(r, "id", policyB.ID.String())
	h.UpdatePolicyHandler(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin update policy-B status = %d; body=%s", rec.Code, rec.Body.String())
	}

	if err := pg.Pool.QueryRow(ctx, `SELECT retention_days FROM retention_policies WHERE id = $1`, policyB.ID).Scan(&rd); err != nil {
		t.Fatalf("read policy-B post-admin-update: %v", err)
	}
	if rd != 11 {
		t.Fatalf("admin update did not persist (got %d, want 11)", rd)
	}

	rec = httptest.NewRecorder()
	r = newAuthedRequest(t, http.MethodDelete,
		"/api/v1/retention/policies/"+policyA.ID.String()+"?org_id="+orgA.String(),
		nil, platformAdmin)
	r = withChiURLParam(r, "id", policyA.ID.String())
	h.DeletePolicyHandler(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete policy-A status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var leftover int
	if err := pg.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM retention_policies WHERE id = $1`, policyA.ID).Scan(&leftover); err != nil {
		t.Fatalf("count policy-A: %v", err)
	}
	if leftover != 0 {
		t.Fatalf("admin delete did not remove policy-A (count=%d)", leftover)
	}

	// ── Tenant B can still see its own policy after admin actions ──
	_, namesAfter := listNames(t, h, tenantB, "")
	if !contains(namesAfter, "policy-B") {
		t.Fatalf("tenant B lost its own policy: %v", namesAfter)
	}
}

func TestTenantIsolation_NonAdminCreateGetsOwnOrg(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pg := testingx.BootPostgres(ctx, t)
	if err := repo.Migrate(ctx, pg.Pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	h := retentionpolicy.New(pg.Pool)
	orgA := uuid.New()
	orgB := uuid.New()
	tenantA := mustClaims(&orgA, "viewer")

	policy := seedPolicy(t, h, tenantA, "policy-write-test")
	if policy.OrgID == nil || *policy.OrgID != orgA {
		t.Fatalf("created policy org_id = %v, want %v", policy.OrgID, orgA)
	}

	// Even when the tenant tries to inject ?org_id=<other>, the
	// create must land in their own org (and the query first triggers
	// a 403 from resolveOrgScope).
	rec := httptest.NewRecorder()
	body := retentionpolicy.CreateRetentionPolicyRequest{
		Name:          "trespasser",
		TargetKind:    "dataset",
		RetentionDays: 90,
		PurgeMode:     "hard-delete-after-ttl",
		UpdatedBy:     "tenant-admin",
	}
	r := newAuthedRequest(t, http.MethodPost,
		fmt.Sprintf("/api/v1/retention/policies?org_id=%s", orgB),
		body, tenantA)
	h.CreatePolicyHandler(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tenant A cross-org create status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}

	var trespasserCount int
	if err := pg.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM retention_policies WHERE name = 'trespasser'`).Scan(&trespasserCount); err != nil {
		t.Fatalf("count trespasser: %v", err)
	}
	if trespasserCount != 0 {
		t.Fatalf("trespasser policy was created (count=%d)", trespasserCount)
	}
}

func ptrInt32(v int32) *int32 { return &v }
