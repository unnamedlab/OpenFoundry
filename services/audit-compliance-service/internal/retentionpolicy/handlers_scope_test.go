package retentionpolicy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func mkReq(t *testing.T, claims *authmw.Claims, rawQuery string) *http.Request {
	t.Helper()
	url := "/api/v1/retention/policies"
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	r := httptest.NewRequest(http.MethodGet, url, nil)
	if claims != nil {
		r = r.WithContext(authmw.ContextWithClaims(r.Context(), claims))
	}
	return r
}

func TestResolveOrgScope_NoClaims401(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	_, ok := resolveOrgScope(rec, mkReq(t, nil, ""))
	if ok {
		t.Fatal("scope should not resolve without claims")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestResolveOrgScope_TenantPinnedToOwnOrg(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	claims := &authmw.Claims{Sub: uuid.New(), OrgID: &org, Roles: []string{"viewer"}}
	rec := httptest.NewRecorder()
	scope, ok := resolveOrgScope(rec, mkReq(t, claims, ""))
	if !ok {
		t.Fatalf("scope should resolve, code=%d body=%s", rec.Code, rec.Body.String())
	}
	if scope.AllOrgs {
		t.Fatal("tenant must not get AllOrgs")
	}
	if scope.OrgID == nil || *scope.OrgID != org {
		t.Fatalf("scope.OrgID = %v, want %v", scope.OrgID, org)
	}
}

func TestResolveOrgScope_TenantWithoutOrgRejected(t *testing.T) {
	t.Parallel()
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"viewer"}}
	rec := httptest.NewRecorder()
	_, ok := resolveOrgScope(rec, mkReq(t, claims, ""))
	if ok {
		t.Fatal("scope must not resolve without an org context")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestResolveOrgScope_TenantQueryParamIgnored(t *testing.T) {
	t.Parallel()
	mine := uuid.New()
	someoneElse := uuid.New()
	claims := &authmw.Claims{Sub: uuid.New(), OrgID: &mine, Roles: []string{"viewer"}}
	rec := httptest.NewRecorder()
	_, ok := resolveOrgScope(rec, mkReq(t, claims, "org_id="+someoneElse.String()))
	if ok {
		t.Fatal("cross-org request from non-admin must be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestResolveOrgScope_TenantSameOrgQueryParamAccepted(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	claims := &authmw.Claims{Sub: uuid.New(), OrgID: &org, Roles: []string{"viewer"}}
	rec := httptest.NewRecorder()
	scope, ok := resolveOrgScope(rec, mkReq(t, claims, "org_id="+org.String()))
	if !ok {
		t.Fatalf("self-org query should resolve, body=%s", rec.Body.String())
	}
	if scope.OrgID == nil || *scope.OrgID != org {
		t.Fatalf("scope.OrgID = %v, want %v", scope.OrgID, org)
	}
}

func TestResolveOrgScope_AdminAllOrgs(t *testing.T) {
	t.Parallel()
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rec := httptest.NewRecorder()
	scope, ok := resolveOrgScope(rec, mkReq(t, claims, "all_orgs=true"))
	if !ok {
		t.Fatalf("admin all_orgs should resolve, body=%s", rec.Body.String())
	}
	if !scope.AllOrgs {
		t.Fatal("admin all_orgs must set AllOrgs=true")
	}
}

func TestResolveOrgScope_AdminScopedToSpecificOrg(t *testing.T) {
	t.Parallel()
	target := uuid.New()
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rec := httptest.NewRecorder()
	scope, ok := resolveOrgScope(rec, mkReq(t, claims, "org_id="+target.String()))
	if !ok {
		t.Fatalf("admin scoped query should resolve, body=%s", rec.Body.String())
	}
	if scope.AllOrgs || scope.OrgID == nil || *scope.OrgID != target {
		t.Fatalf("scope = %+v, want pinned to %s", scope, target)
	}
}

func TestResolveOrgScope_AdminWithoutHintDefaultsToOwnOrg(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	claims := &authmw.Claims{Sub: uuid.New(), OrgID: &org, Roles: []string{"admin"}}
	rec := httptest.NewRecorder()
	scope, ok := resolveOrgScope(rec, mkReq(t, claims, ""))
	if !ok {
		t.Fatalf("admin default should resolve, body=%s", rec.Body.String())
	}
	if scope.AllOrgs {
		t.Fatal("admin without hint must default to own org, not AllOrgs")
	}
	if scope.OrgID == nil || *scope.OrgID != org {
		t.Fatalf("scope.OrgID = %v, want %v", scope.OrgID, org)
	}
}

func TestResolveOrgScope_AdminWithoutOrgDefaultsAllOrgs(t *testing.T) {
	t.Parallel()
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rec := httptest.NewRecorder()
	scope, ok := resolveOrgScope(rec, mkReq(t, claims, ""))
	if !ok {
		t.Fatalf("platform admin should resolve, body=%s", rec.Body.String())
	}
	if !scope.AllOrgs {
		t.Fatal("platform admin (no org) must default to AllOrgs")
	}
}

func TestResolveOrgScope_AdminViaPermission(t *testing.T) {
	t.Parallel()
	claims := &authmw.Claims{
		Sub:         uuid.New(),
		Roles:       []string{"viewer"},
		Permissions: []string{"retention-policies:admin"},
	}
	rec := httptest.NewRecorder()
	scope, ok := resolveOrgScope(rec, mkReq(t, claims, "all_orgs=true"))
	if !ok {
		t.Fatalf("permission-bearer admin should resolve, body=%s", rec.Body.String())
	}
	if !scope.AllOrgs {
		t.Fatal("permission-bearer admin must reach AllOrgs")
	}
}

func TestResolveOrgScope_InvalidOrgIDQuery(t *testing.T) {
	t.Parallel()
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rec := httptest.NewRecorder()
	_, ok := resolveOrgScope(rec, mkReq(t, claims, "org_id=not-a-uuid"))
	if ok {
		t.Fatal("malformed org_id must be rejected")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestResolveWriteOrgID(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	cases := []struct {
		name  string
		scope OrgScope
		want  *uuid.UUID
	}{
		{"pinned", OrgScope{OrgID: &org}, &org},
		{"all_orgs", OrgScope{AllOrgs: true}, nil},
	}
	for _, tc := range cases {
		got := resolveWriteOrgID(tc.scope)
		if (got == nil) != (tc.want == nil) {
			t.Fatalf("%s: nullness mismatch got=%v want=%v", tc.name, got, tc.want)
		}
		if got != nil && *got != *tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, *got, *tc.want)
		}
	}
}

func TestOrgScopeAppendOrgFilter(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	scope := OrgScope{OrgID: &org}
	frag, args := scope.appendOrgFilter(nil)
	if frag != "(org_id = $1 OR is_system = TRUE)" {
		t.Fatalf("frag = %q", frag)
	}
	if len(args) != 1 || args[0] != &org {
		t.Fatalf("args = %v", args)
	}

	admin := OrgScope{AllOrgs: true}
	frag, args = admin.appendOrgFilter([]any{42})
	if frag != "" {
		t.Fatalf("admin frag must be empty, got %q", frag)
	}
	if len(args) != 1 || args[0] != 42 {
		t.Fatalf("admin args mutated: %v", args)
	}
}
