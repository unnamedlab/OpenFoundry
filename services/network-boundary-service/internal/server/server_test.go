package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/handler"
)

// TestMountAPIRoutes_StubReturns501ForBoundaryPrefixes keeps the legacy
// boundary placeholders while egress policies graduate to SG.34 handlers.
func TestMountAPIRoutes_StubReturns501ForBoundaryPrefixes(t *testing.T) {
	t.Parallel()
	caps := capabilities.New("network-boundary-service", "test")
	r := chi.NewRouter()
	caps.Mount(r)
	mountAPIRoutes(r, caps, handler.NewMemoryEgressPolicyStore())

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/network-boundaries"},
		{http.MethodGet, "/api/v1/network-boundaries/abc"},
		{http.MethodPost, "/api/v1/network-boundaries"},
		{http.MethodGet, "/api/v1/network-boundary"},
		{http.MethodPut, "/api/v1/network-boundary/xyz"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != http.StatusNotImplemented {
				t.Fatalf("%s %s = %d, want 501", tc.method, tc.path, w.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (body=%q)", err, w.Body.String())
			}
			if body["code"] != "not_implemented" || body["service"] != "network-boundary-service" || body["milestone"] == "" {
				t.Fatalf("unexpected envelope: %+v", body)
			}
		})
	}
}

func TestEgressPoliciesLifecycleAndRuntimeEnforcement(t *testing.T) {
	t.Parallel()
	r := testRouter()
	claims := adminClaims()

	create := `{
		"name":"warehouse api",
		"kind":"direct",
		"address":{"kind":"host","value":"api.example.com"},
		"port":{"kind":"single","value":"443"},
		"protocol":"https",
		"sni_behavior":"verify",
		"allowed_organizations":["org-main"],
		"importer_grants":["group:warehouse-importers"],
		"viewer_grants":["group:warehouse-viewers"],
		"reason":"SG.34 test"
	}`
	rec := serveJSON(r, claims, http.MethodPost, "/api/v1/data-connection/egress-policies", create)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var policy map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &policy))
	require.Equal(t, "pending_approval", policy["state"])
	require.Equal(t, true, policy["importer_grants_high_risk"])
	require.Contains(t, rec.Body.String(), `"potential_data_export":true`)
	require.Contains(t, rec.Body.String(), `"dataExport"`)
	id, _ := policy["id"].(string)
	require.NotEmpty(t, id)

	runtime := `{
		"workload_id":"build-1",
		"policy_ids":["` + id + `"],
		"destination":{"kind":"host","value":"api.example.com"},
		"port":443,
		"organization_id":"org-main",
		"actor_grants":["group:warehouse-importers"]
	}`
	rec = serveJSON(r, claims, http.MethodPost, "/api/v1/data-connection/egress-policies:evaluate-workload", runtime)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "policy_state_pending_approval")

	rec = serveJSON(r, claims, http.MethodPatch, "/api/v1/data-connection/egress-policies/"+id+"/state", `{"state":"active","reason":"approved"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "network_egress.policy.activated")

	rec = serveJSON(r, claims, http.MethodPost, "/api/v1/data-connection/egress-policies:evaluate-workload", runtime)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"allowed":true`)

	rec = serveJSON(r, claims, http.MethodPatch, "/api/v1/data-connection/egress-policies/"+id+"/state", `{"state":"paused","reason":"maintenance"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "network_egress.policy.paused")
	rec = serveJSON(r, claims, http.MethodPost, "/api/v1/data-connection/egress-policies:evaluate-workload", runtime)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "policy_state_paused")

	rec = serveJSON(r, claims, http.MethodPatch, "/api/v1/data-connection/egress-policies/"+id+"/state", `{"state":"revoked","reason":"no longer approved"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "network_egress.policy.revoked")
	rec = serveJSON(r, claims, http.MethodPatch, "/api/v1/data-connection/egress-policies/"+id+"/state", `{"state":"active","reason":"undo"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestEgressPolicyKindsAndImmutableDelete(t *testing.T) {
	t.Parallel()
	r := testRouter()
	claims := adminClaims()

	rec := serveJSON(r, claims, http.MethodPost, "/api/v1/data-connection/egress-policies", `{
		"name":"agent tunnel",
		"kind":"agent_proxy",
		"address":{"kind":"host","value":"private.example.com"},
		"port":{"kind":"single","value":"8443"},
		"protocol":"tls",
		"proxy_mode":"http_connect"
	}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "connector agent")

	rec = serveJSON(r, claims, http.MethodPost, "/api/v1/data-connection/egress-policies", `{
		"name":"agent tunnel",
		"kind":"agent_proxy",
		"address":{"kind":"host","value":"private.example.com"},
		"port":{"kind":"single","value":"8443"},
		"protocol":"tls",
		"proxy_mode":"http_connect",
		"agents":["agent-east-1"],
		"importer_grants":["role:editor"]
	}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = serveJSON(r, claims, http.MethodPost, "/api/v1/data-connection/egress-policies", `{
		"name":"same region bucket",
		"kind":"same_region_bucket",
		"address":{"kind":"host","value":"s3.us-east-1.amazonaws.com"},
		"port":{"kind":"single","value":"443"},
		"protocol":"https",
		"bucket_name":"analytics-staging",
		"bucket_access_level":"read_write",
		"importer_grants":["role:editor"]
	}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var bucket map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bucket))
	require.Equal(t, "same_region_bucket", bucket["kind"])
	require.Contains(t, rec.Body.String(), "same-region-s3-bucket-policy-required")
	require.Contains(t, rec.Body.String(), "VPC endpoint")

	id, _ := bucket["id"].(string)
	rec = serveJSON(r, claims, http.MethodDelete, "/api/v1/data-connection/egress-policies/"+id, ``)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "revoke")
}

func TestEgressRuntimeRequiresExplicitImporterGrant(t *testing.T) {
	t.Parallel()
	r := testRouter()
	claims := adminClaims()
	rec := serveJSON(r, claims, http.MethodPost, "/api/v1/data-connection/egress-policies", `{
		"name":"active api",
		"kind":"direct",
		"address":{"kind":"host","value":"api.example.com"},
		"port":{"kind":"single","value":"443"},
		"status":"active",
		"importer_grants":["group:egress-importers"]
	}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var policy map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &policy))
	id, _ := policy["id"].(string)

	caller := &authmw.Claims{Sub: uuid.New(), Roles: []string{"viewer"}}
	rec = serveJSON(r, caller, http.MethodPost, "/api/v1/data-connection/egress-policies:evaluate-workload", `{
		"workload_id":"transform-1",
		"policy_ids":["`+id+`"],
		"destination":{"kind":"host","value":"api.example.com"},
		"port":443
	}`)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "importer_grant_required_high_risk")
}

func TestEgressApprovalWorkflowAuditAndInventoryWarnings(t *testing.T) {
	t.Parallel()
	r := testRouter()
	proposer := &authmw.Claims{Sub: uuid.New(), Email: "dev@example.com", Roles: []string{"viewer"}}
	admin := adminClaims()

	rec := serveJSON(r, proposer, http.MethodPost, "/api/v1/data-connection/egress-policies", `{
		"name":"proposed external api",
		"kind":"direct",
		"address":{"kind":"host","value":"api.vendor.example.com"},
		"port":{"kind":"single","value":"443"},
		"protocol":"https",
		"importer_grants":["group:trusted-builders"],
		"reason":"needed for vendor sync"
	}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var proposed map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &proposed))
	require.Equal(t, "pending_approval", proposed["state"])
	require.Contains(t, rec.Body.String(), "network_egress.approval.requested")
	require.Contains(t, rec.Body.String(), "Information Security Officer")
	proposedID, _ := proposed["id"].(string)

	rec = serveJSON(r, proposer, http.MethodGet, "/api/v1/data-connection/egress-policies/approvals?status=pending", "")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = serveJSON(r, admin, http.MethodGet, "/api/v1/data-connection/egress-policies/approvals?status=pending", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var approvals []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &approvals))
	require.Len(t, approvals, 1)
	taskID, _ := approvals[0]["id"].(string)
	require.NotEmpty(t, taskID)

	rec = serveJSON(r, admin, http.MethodPost, "/api/v1/data-connection/egress-policies/approvals/"+taskID+"/decision", `{"decision":"approved","reason":"ISO reviewed trusted destination"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"state":"active"`)
	require.Contains(t, rec.Body.String(), "network_egress.approval.approved")

	runtime := `{
		"workload_id":"export-job-1",
		"workload_kind":"data_export",
		"policy_ids":["` + proposedID + `"],
		"destination":{"kind":"host","value":"api.vendor.example.com"},
		"port":443,
		"actor_grants":["group:trusted-builders"]
	}`
	rec = serveJSON(r, proposer, http.MethodPost, "/api/v1/data-connection/egress-policies:evaluate-workload", runtime)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = serveJSON(r, admin, http.MethodGet, "/api/v1/data-connection/egress-policies/"+proposedID, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "potential_data_export")
	require.Contains(t, rec.Body.String(), "dataExport")
	require.Contains(t, rec.Body.String(), "export-job-1")

	rec = serveJSON(r, admin, http.MethodPost, "/api/v1/data-connection/egress-policies", `{
		"name":"cidr route",
		"kind":"direct",
		"address":{"kind":"cidr","value":"10.20.0.0/16"},
		"port":{"kind":"range","value":"8000-9000"},
		"status":"active"
	}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var cidr map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cidr))
	cidrID, _ := cidr["id"].(string)

	rec = serveJSON(r, admin, http.MethodPost, "/api/v1/data-connection/egress-policies", `{
		"name":"single ip route",
		"kind":"direct",
		"address":{"kind":"ip","value":"10.20.30.40"},
		"port":{"kind":"single","value":"8443"},
		"status":"active"
	}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), cidrID)
	require.Contains(t, rec.Body.String(), "overlapping-egress-policy")
	require.Contains(t, rec.Body.String(), "egress_ip_ranges")
}

func testRouter() http.Handler {
	caps := capabilities.New("network-boundary-service", "test")
	r := chi.NewRouter()
	caps.Mount(r)
	mountAPIRoutes(r, caps, handler.NewMemoryEgressPolicyStore())
	return r
}

func adminClaims() *authmw.Claims {
	org := uuid.New()
	return &authmw.Claims{
		Sub:         uuid.New(),
		Email:       "admin@example.com",
		Roles:       []string{"admin"},
		Permissions: []string{"network-egress:manage", "network-egress:approve"},
		OrgID:       &org,
	}
}

func serveJSON(r http.Handler, claims *authmw.Claims, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if claims != nil {
		req = req.WithContext(authmw.ContextWithClaims(req.Context(), claims))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
