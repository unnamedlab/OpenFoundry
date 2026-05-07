package streamingmonitors_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/streamingmonitors"
)

// ─── Wire-format pinning ────────────────────────────────────────────

func TestEnumStringValuesAreScreamingSnakeCase(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		string(streamingmonitors.ResourceStreamingDataset):       "STREAMING_DATASET",
		string(streamingmonitors.ResourceStreamingPipeline):      "STREAMING_PIPELINE",
		string(streamingmonitors.ResourceTimeSeriesSync):         "TIME_SERIES_SYNC",
		string(streamingmonitors.ResourceGeotemporalObservations): "GEOTEMPORAL_OBSERVATIONS",
		string(streamingmonitors.KindIngestRecords):              "INGEST_RECORDS",
		string(streamingmonitors.KindCheckpointLiveness):         "CHECKPOINT_LIVENESS",
		string(streamingmonitors.KindGeotemporalObsSent):         "GEOTEMPORAL_OBS_SENT",
		string(streamingmonitors.CmpLT):                          "LT",
		string(streamingmonitors.CmpGTE):                         "GTE",
		string(streamingmonitors.SeverityWarn):                   "WARN",
		string(streamingmonitors.SeverityCritical):               "CRITICAL",
	}
	for got, want := range cases {
		assert.Equal(t, want, got)
	}
}

func TestMonitorRuleJSONShape(t *testing.T) {
	t.Parallel()
	rule := streamingmonitors.MonitorRule{
		ID: uuid.New(), ViewID: uuid.New(), Name: "lag",
		ResourceType:  streamingmonitors.ResourceStreamingDataset,
		ResourceRID:   "rid.foo",
		MonitorKind:   streamingmonitors.KindTotalLag,
		WindowSeconds: 300,
		Comparator:    streamingmonitors.CmpGT,
		Threshold:     1.5,
		Severity:      streamingmonitors.SeverityWarn,
		Enabled:       true,
		CreatedBy:     "tester",
		CreatedAt:     time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(rule)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "view_id", "name", "resource_type", "resource_rid",
		"monitor_kind", "window_seconds", "comparator", "threshold",
		"severity", "enabled", "created_by", "created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
	// Critically: enums must serialize as their SCREAMING_SNAKE_CASE strings.
	assert.Equal(t, "STREAMING_DATASET", view["resource_type"])
	assert.Equal(t, "TOTAL_LAG", view["monitor_kind"])
	assert.Equal(t, "GT", view["comparator"])
	assert.Equal(t, "WARN", view["severity"])
}

func TestDataEnvelope(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(streamingmonitors.DataEnvelope[streamingmonitors.MonitoringView]{
		Data: []streamingmonitors.MonitoringView{},
	})
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	assert.Contains(t, view, "data", "streaming-monitor surface uses {data: [...]} (NOT {items})")
	assert.NotContains(t, view, "items")
}

// ─── Comparator semantics ───────────────────────────────────────────

func TestComparatorEvaluate(t *testing.T) {
	t.Parallel()
	assert.True(t, streamingmonitors.CmpLT.Evaluate(1, 2))
	assert.False(t, streamingmonitors.CmpLT.Evaluate(2, 2))
	assert.True(t, streamingmonitors.CmpLTE.Evaluate(2, 2))
	assert.True(t, streamingmonitors.CmpGT.Evaluate(3, 2))
	assert.True(t, streamingmonitors.CmpGTE.Evaluate(2, 2))
	assert.True(t, streamingmonitors.CmpEQ.Evaluate(2.0, 2.0))
	// EQ tolerance covers tiny FP noise.
	assert.True(t, streamingmonitors.CmpEQ.Evaluate(2.0, 2.0+math.Nextafter(0, 1)))
}

// ─── Validation ─────────────────────────────────────────────────────

func TestCreateMonitorRuleValidate(t *testing.T) {
	t.Parallel()
	good := streamingmonitors.CreateMonitorRuleRequest{
		ViewID:        uuid.New(),
		ResourceType:  streamingmonitors.ResourceStreamingDataset,
		ResourceRID:   "rid.foo",
		MonitorKind:   streamingmonitors.KindTotalLag,
		WindowSeconds: 300,
		Comparator:    streamingmonitors.CmpGT,
		Threshold:     1.5,
	}
	require.NoError(t, good.Validate())

	tooSmallWindow := good
	tooSmallWindow.WindowSeconds = 30
	assert.ErrorContains(t, tooSmallWindow.Validate(), "window_seconds")

	tooBigWindow := good
	tooBigWindow.WindowSeconds = 90_000
	assert.ErrorContains(t, tooBigWindow.Validate(), "window_seconds")

	emptyRid := good
	emptyRid.ResourceRID = "   "
	assert.ErrorContains(t, emptyRid.Validate(), "resource_rid")

	nanThresh := good
	nanThresh.Threshold = math.NaN()
	assert.ErrorContains(t, nanThresh.Validate(), "threshold")

	invalidEnum := good
	invalidEnum.MonitorKind = "BANANA"
	assert.ErrorContains(t, invalidEnum.Validate(), "monitor_kind")
}

// ─── RBAC ───────────────────────────────────────────────────────────

// withClaims runs `fn` with the given claims attached to the request
// context, mirroring authmw.Middleware's behavior.
func withClaims(t *testing.T, c *authmw.Claims, fn http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if c != nil {
		// Use the public Middleware testing helper: build a tiny in-memory
		// server that injects the claims, then call the handler.
		ctx := authmw.ContextWithClaims(context.Background(), c)
		req = req.WithContext(ctx)
	}
	fn(rec, req)
	return rec
}

func TestCreateViewRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &streamingmonitors.Handlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/monitoring-views",
		strings.NewReader(`{"name":"x","project_rid":"r"}`))
	h.CreateView(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCreateViewAllowsMonitoringWritePerm(t *testing.T) {
	t.Parallel()
	h := &streamingmonitors.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Permissions: []string{"monitoring:write"}}
	req := httptest.NewRequest("POST", "/monitoring-views",
		strings.NewReader(`not-json`)) // we only check the auth gate, not the body
	rec := withClaims(t, c, h.CreateView, req)
	// Past the auth gate the handler reaches body decoding and 400s.
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateViewRejectsEmptyName(t *testing.T) {
	t.Parallel()
	h := &streamingmonitors.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"monitoring_admin"}}
	req := httptest.NewRequest("POST", "/monitoring-views",
		strings.NewReader(`{"name":"   ","project_rid":"   "}`))
	rec := withClaims(t, c, h.CreateView, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "name and project_rid")
}

func TestPatchRuleRejectsBadEnum(t *testing.T) {
	t.Parallel()
	h := &streamingmonitors.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	req := httptest.NewRequest("PATCH", "/monitor-rules/"+uuid.New().String(),
		strings.NewReader(`{"comparator":"NOT_A_REAL_OP"}`))
	rec := withClaims(t, c, h.PatchRule, req)
	// Even with a real path param, the enum check rejects before SQL.
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
