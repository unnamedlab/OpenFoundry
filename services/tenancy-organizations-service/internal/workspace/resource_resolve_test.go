package workspace_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/workspace"
)

// ─── Wire-format pinning ────────────────────────────────────────────

func TestResolvedLabelJSONShapeUnresolved(t *testing.T) {
	t.Parallel()
	rl := workspace.ResolvedLabel{
		ResourceKind: "dataset",
		ResourceID:   uuid.New(),
		Resolved:     false,
	}
	out, err := json.Marshal(rl)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"resource_kind", "resource_id", "resolved", "label", "description"} {
		assert.Contains(t, view, k, "Rust serializes unresolved entries with explicit null label/description; the field MUST be present on the wire")
	}
	assert.Equal(t, "dataset", view["resource_kind"])
	assert.Equal(t, false, view["resolved"])
	assert.Nil(t, view["label"])
	assert.Nil(t, view["description"])
}

func TestResolvedLabelJSONShapeResolved(t *testing.T) {
	t.Parallel()
	label := "My Project"
	desc := "ok"
	rl := workspace.ResolvedLabel{
		ResourceKind: "ontology_project",
		ResourceID:   uuid.New(),
		Resolved:     true,
		Label:        &label,
		Description:  &desc,
	}
	out, err := json.Marshal(rl)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	assert.Equal(t, "My Project", view["label"])
	assert.Equal(t, "ok", view["description"])
	assert.Equal(t, true, view["resolved"])
}

func TestResolveResponseEnvelopeIsData(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(workspace.ResolveResponse{Data: []workspace.ResolvedLabel{}})
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	assert.Contains(t, view, "data")
	// Empty slice must serialize as `[]`, not `null`. The Rust impl
	// builds a Vec::new() and Serde renders it as `[]` — pin that.
	assert.Equal(t, "[]", string(mustMarshalResolve(t, view["data"])))
}

func mustMarshalResolve(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	require.NoError(t, err)
	return out
}

// ─── Handler validation paths (no DB) ────────────────────────────────

func newAuthedResolveReq(_ *testing.T, body string) *httptest.ResponseRecorder {
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/workspace/resources/resolve", strings.NewReader(body))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	(&workspace.Handlers{}).ResolveResources(rec, req)
	return rec
}

func TestResolveResourcesRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("POST", "/workspace/resources/resolve",
		strings.NewReader(`{"items":[]}`))
	rec := httptest.NewRecorder()
	h.ResolveResources(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestResolveResourcesEmptyItemsReturnsEmptyData(t *testing.T) {
	t.Parallel()
	rec := newAuthedResolveReq(t, `{"items":[]}`)
	require.Equal(t, 200, rec.Code)
	var resp workspace.ResolveResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotNil(t, resp.Data)
	assert.Len(t, resp.Data, 0)
	// Surface should serialize as `[]`, not `null`, even on the empty-fast-path.
	var view map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, "[]", string(mustMarshalResolve(t, view["data"])))
}

func TestResolveResourcesRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	rec := newAuthedResolveReq(t, `not json`)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid body")
}

func TestResolveResourcesRejectsOversizedBatch(t *testing.T) {
	t.Parallel()
	// 201 entries — one over MaxResolveBatch.
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := 0; i < workspace.MaxResolveBatch+1; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"resource_kind":"dataset","resource_id":%q}`, uuid.New().String())
	}
	b.WriteString(`]}`)
	rec := newAuthedResolveReq(t, b.String())
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "at most")
	assert.Contains(t, rec.Body.String(), "200")
}

func TestResolveResourcesUnsupportedKindReturnsResolvedFalse(t *testing.T) {
	t.Parallel()
	// `dataset` is a valid kind but unsupported for in-process resolve.
	// The handler MUST still emit an entry with resolved=false rather
	// than dropping it from the response (preserves request order).
	id := uuid.New()
	body := fmt.Sprintf(`{"items":[{"resource_kind":"dataset","resource_id":%q}]}`, id.String())
	rec := newAuthedResolveReq(t, body)
	require.Equal(t, 200, rec.Code)
	var resp workspace.ResolveResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	got := resp.Data[0]
	assert.Equal(t, "dataset", got.ResourceKind)
	assert.Equal(t, id, got.ResourceID)
	assert.False(t, got.Resolved)
	assert.Nil(t, got.Label)
	assert.Nil(t, got.Description)
}

func TestResolveResourcesUnknownKindStringPassesThroughResolvedFalse(t *testing.T) {
	t.Parallel()
	// Even an entirely unknown kind string ("banana") must round-trip
	// as resolved=false — the handler does not 400 on unknown kinds in
	// the items list, mirroring Rust's lenient behaviour for batch
	// resolves (the per-item kind is not bucketed but the entry is
	// still emitted).
	id := uuid.New()
	body := fmt.Sprintf(`{"items":[{"resource_kind":"banana","resource_id":%q}]}`, id.String())
	rec := newAuthedResolveReq(t, body)
	require.Equal(t, 200, rec.Code)
	var resp workspace.ResolveResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "banana", resp.Data[0].ResourceKind)
	assert.False(t, resp.Data[0].Resolved)
}

func TestResolveResourcesPreservesRequestOrder(t *testing.T) {
	t.Parallel()
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()
	body := fmt.Sprintf(
		`{"items":[`+
			`{"resource_kind":"dataset","resource_id":%q},`+
			`{"resource_kind":"pipeline","resource_id":%q},`+
			`{"resource_kind":"notebook","resource_id":%q}`+
			`]}`,
		id1.String(), id2.String(), id3.String(),
	)
	rec := newAuthedResolveReq(t, body)
	require.Equal(t, 200, rec.Code)
	var resp workspace.ResolveResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 3)
	assert.Equal(t, id1, resp.Data[0].ResourceID)
	assert.Equal(t, id2, resp.Data[1].ResourceID)
	assert.Equal(t, id3, resp.Data[2].ResourceID)
	assert.Equal(t, "dataset", resp.Data[0].ResourceKind)
	assert.Equal(t, "pipeline", resp.Data[1].ResourceKind)
	assert.Equal(t, "notebook", resp.Data[2].ResourceKind)
}
