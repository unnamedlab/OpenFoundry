package domain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storageabstraction "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ---- time_series.go ------------------------------------------------------

// libs/ontology-kernel/src/domain/time_series.rs `normalize_chart_kind`.
func TestNormalizeChartKindAccepted(t *testing.T) {
	for _, k := range []string{"line", "area", "bar", "point"} {
		got, err := NormalizeChartKind(k)
		require.NoError(t, err)
		assert.Equal(t, k, got)
	}
	_, err := NormalizeChartKind("heatmap")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chart_kind must be one of: line, area, bar, point")
	assert.Contains(t, err.Error(), "received heatmap")
}

// libs/ontology-kernel/src/domain/time_series.rs
// `builds_a_vega_lite_template_for_quiver` — pin the schema URL,
// dataset shape, mark type and usermeta blob.
func TestBuildQuiverVegaSpecAreaShape(t *testing.T) {
	secondary := uuid.New()
	selectedGroup := "EMEA"
	draft := models.QuiverVisualFunctionDraft{
		Name:               "Pipeline Throughput",
		Description:        "Tracks daily throughput by team.",
		PrimaryTypeID:      uuid.Nil,
		SecondaryTypeID:    &secondary,
		JoinField:          "order_id",
		SecondaryJoinField: "order_id",
		DateField:          "event_date",
		MetricField:        "throughput",
		GroupField:         "team",
		SelectedGroup:      &selectedGroup,
		ChartKind:          "area",
		Shared:             true,
	}

	out, err := BuildQuiverVegaSpec(draft)
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, json.Unmarshal(out, &spec))
	assert.Equal(t, "https://vega.github.io/schema/vega-lite/v5.json", spec["$schema"])

	datasets := spec["datasets"].(map[string]any)
	require.Empty(t, datasets["timeSeries"])

	vconcat := spec["vconcat"].([]any)
	require.Len(t, vconcat, 2)
	tsMark := vconcat[0].(map[string]any)["mark"].(map[string]any)
	assert.Equal(t, "area", tsMark["type"])

	usermeta := spec["usermeta"].(map[string]any)["quiver"].(map[string]any)
	assert.Equal(t, "area", usermeta["chart_kind"])
	assert.Equal(t, true, usermeta["shared"])

	params := spec["params"].([]any)[0].(map[string]any)
	assert.Equal(t, "EMEA", params["value"])
}

// libs/ontology-kernel/src/domain/time_series.rs
// `rejects_unknown_chart_kind` — propagates the
// normalize_chart_kind error verbatim.
func TestBuildQuiverVegaSpecRejectsUnknownChartKind(t *testing.T) {
	draft := models.QuiverVisualFunctionDraft{
		PrimaryTypeID: uuid.Nil,
		ChartKind:     "heatmap",
	}
	_, err := BuildQuiverVegaSpec(draft)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chart_kind")
}

// libs/ontology-kernel/src/domain/time_series.rs — empty description
// builds the canned fallback `Quiver visual function '<name>'
// generated from ontology analytics.`
func TestBuildQuiverVegaSpecDescriptionFallback(t *testing.T) {
	draft := models.QuiverVisualFunctionDraft{
		Name:          "Daily Health",
		PrimaryTypeID: uuid.Nil,
		ChartKind:     "line",
	}
	out, err := BuildQuiverVegaSpec(draft)
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, json.Unmarshal(out, &spec))
	desc := spec["description"].(string)
	assert.True(t, strings.Contains(desc, "Daily Health"))
	assert.True(t, strings.Contains(desc, "ontology analytics"))
}

// libs/ontology-kernel/src/domain/time_series.rs — when
// secondary_type_id is nil the join_mode subtitle says
// `single_object_set`.
func TestBuildQuiverVegaSpecJoinModeSubtitle(t *testing.T) {
	draft := models.QuiverVisualFunctionDraft{
		Name:          "x",
		PrimaryTypeID: uuid.Nil,
		ChartKind:     "line",
		DateField:     "d",
		MetricField:   "m",
		GroupField:    "g",
	}
	out, err := BuildQuiverVegaSpec(draft)
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, json.Unmarshal(out, &spec))
	subtitle := spec["title"].(map[string]any)["subtitle"].(string)
	assert.Contains(t, subtitle, "single_object_set")
	assert.NotContains(t, subtitle, "joined_object_sets")
}

// ---- writeback.go --------------------------------------------------------

// libs/ontology-kernel/src/domain/writeback.rs `event_id_is_deterministic`
// + `event_id_uses_v5_namespace` + `event_id_differs_when_any_field_changes`.
func TestDeriveEventIDIsDeterministic(t *testing.T) {
	a := DeriveEventID("acme", "object", "obj-123", 7)
	b := DeriveEventID("acme", "object", "obj-123", 7)
	assert.Equal(t, a, b)

	// UUID-v5 has version nibble = 5.
	assert.Equal(t, uuid.Version(5), a.Version())

	base := DeriveEventID("acme", "object", "obj-123", 7)
	assert.NotEqual(t, base, DeriveEventID("other", "object", "obj-123", 7))
	assert.NotEqual(t, base, DeriveEventID("acme", "link", "obj-123", 7))
	assert.NotEqual(t, base, DeriveEventID("acme", "object", "obj-124", 7))
	assert.NotEqual(t, base, DeriveEventID("acme", "object", "obj-123", 8))
}

// libs/ontology-kernel/src/domain/writeback.rs — `WritebackError`
// surfaces variant-shape strings that round-trip Rust's `Display`
// impl byte-for-byte.
func TestWritebackErrorDisplayShapes(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	primary := &WritebackError{Kind: WritebackPrimary, Source: errors.New("nope")}
	assert.Equal(t, "primary store rejected the write: nope", primary.Error())

	commit := &WritebackError{
		Kind:             WritebackCommitAfterPrimary,
		Source:           errors.New("db down"),
		EventID:          id,
		CommittedVersion: 7,
	}
	assert.Equal(t, "commit after primary write failed (event_id=11111111-1111-1111-1111-111111111111, version=7): db down", commit.Error())

	openTx := &WritebackError{
		Kind:             WritebackOpenTxAfterPrimary,
		Source:           errors.New("tx fail"),
		EventID:          id,
		CommittedVersion: 7,
	}
	assert.Equal(t, "could not open pg-policy tx after primary write (event_id=11111111-1111-1111-1111-111111111111, version=7): tx fail", openTx.Error())

	conflict := &WritebackError{Kind: WritebackVersionConflict, ExpectedVersion: 3, ActualVersion: 5}
	assert.Equal(t, "version conflict: expected 3, found 5", conflict.Error())

	assert.True(t, IsVersionConflict(conflict))
	assert.False(t, IsVersionConflict(primary))
	assert.False(t, IsVersionConflict(errors.New("plain")))
}

// libs/ontology-kernel/src/domain/writeback.rs — primary-store
// failure becomes a [WritebackError] of kind WritebackPrimary; the
// PG transaction is never opened.
func TestApplyObjectWithOutboxPrimaryFailure(t *testing.T) {
	objects := stores().objectsRejecting(errors.New("cassandra exploded"))
	_, err := ApplyObjectWithOutbox(
		context.Background(),
		nil, // pg pool is never reached on the primary failure path
		objects,
		storageabstraction.Object{Tenant: "t", ID: "obj"},
		nil,
		"object", "topic", json.RawMessage(`{}`),
	)
	require.Error(t, err)
	var w *WritebackError
	require.True(t, errors.As(err, &w))
	assert.Equal(t, WritebackPrimary, w.Kind)
	assert.Contains(t, w.Error(), "cassandra exploded")
}

// libs/ontology-kernel/src/domain/writeback.rs — VersionConflict
// whose ActualVersion ≠ target_version surfaces a typed version
// conflict error.
func TestApplyObjectWithOutboxVersionConflict(t *testing.T) {
	objects := stores().objectsConflict(3, 99)
	exp := uint64(2)
	_, err := ApplyObjectWithOutbox(
		context.Background(),
		nil,
		objects,
		storageabstraction.Object{Tenant: "t", ID: "obj"},
		&exp,
		"object", "topic", json.RawMessage(`{}`),
	)
	require.Error(t, err)
	var w *WritebackError
	require.True(t, errors.As(err, &w))
	assert.Equal(t, WritebackVersionConflict, w.Kind)
	assert.Equal(t, uint64(3), w.ExpectedVersion)
	assert.Equal(t, uint64(99), w.ActualVersion)
}

// ---- media_reference_validator.go ----------------------------------------

// libs/ontology-kernel/src/domain/media_reference_validator.rs
// `accepts_camel_case_payload` + `accepts_snake_case_payload_too`.
func TestMediaReferenceAcceptsBothCases(t *testing.T) {
	camel := json.RawMessage(`{
        "mediaSetRid": "ri.foundry.main.media_set.abc",
        "mediaItemRid": "ri.foundry.main.media_item.def",
        "branch": "main",
        "schema": "IMAGE"
    }`)
	snake := json.RawMessage(`{
        "media_set_rid": "ri.foundry.main.media_set.abc",
        "media_item_rid": "ri.foundry.main.media_item.def"
    }`)
	ctx := MediaReferenceContextFromMap(map[string]ResolvedMediaSet{
		"ri.foundry.main.media_set.abc": {MediaSetRID: "ri.foundry.main.media_set.abc"},
	}, []string{"public"})

	parsed, err := ValidateMediaReference(camel, ctx)
	require.NoError(t, err)
	require.NotNil(t, parsed.Branch)
	assert.Equal(t, "main", *parsed.Branch)
	require.NotNil(t, parsed.Schema)
	assert.Equal(t, "IMAGE", *parsed.Schema)

	parsed, err = ValidateMediaReference(snake, ctx)
	require.NoError(t, err)
	assert.Nil(t, parsed.Branch)
	assert.Nil(t, parsed.Schema)
}

// libs/ontology-kernel/src/domain/media_reference_validator.rs
// `rejects_non_object_payload`.
func TestMediaReferenceRejectsNonObject(t *testing.T) {
	ctx := MediaReferenceContextFromMap(nil, nil)
	_, err := ValidateMediaReference(json.RawMessage(`"just-a-string"`), ctx)
	require.Error(t, err)
	assert.True(t, IsMediaRefError(err, MediaRefNotAnObject))
	assert.Contains(t, err.Error(), "must be a JSON object on H6 ontology surfaces")
}

// libs/ontology-kernel/src/domain/media_reference_validator.rs
// `rejects_missing_required_field`.
func TestMediaReferenceMissingRequiredField(t *testing.T) {
	ctx := MediaReferenceContextFromMap(map[string]ResolvedMediaSet{
		"rid": {MediaSetRID: "rid"},
	}, []string{})
	_, err := ValidateMediaReference(json.RawMessage(`{"mediaSetRid": "rid"}`), ctx)
	require.Error(t, err)
	var m *MediaReferenceValidationError
	require.True(t, errors.As(err, &m))
	assert.Equal(t, MediaRefMissingField, m.Kind)
	assert.Equal(t, "mediaItemRid", m.Field)
}

// libs/ontology-kernel/src/domain/media_reference_validator.rs
// `rejects_unknown_media_set`.
func TestMediaReferenceUnknownMediaSet(t *testing.T) {
	ctx := MediaReferenceContextFromMap(map[string]ResolvedMediaSet{
		"ri.other": {MediaSetRID: "ri.other"},
	}, []string{"public"})
	_, err := ValidateMediaReference(json.RawMessage(`{
        "mediaSetRid": "ri.absent",
        "mediaItemRid": "x"
    }`), ctx)
	require.Error(t, err)
	assert.True(t, IsMediaRefError(err, MediaRefUnknownMediaSet))
	assert.Contains(t, err.Error(), "media set `ri.absent` does not exist")
}

// libs/ontology-kernel/src/domain/media_reference_validator.rs
// `rejects_when_clearances_miss_a_marking`.
func TestMediaReferenceInsufficientClearance(t *testing.T) {
	ctx := MediaReferenceContextFromMap(map[string]ResolvedMediaSet{
		"ri.classified": {
			MediaSetRID: "ri.classified",
			Markings:    []string{"public", "secret"},
		},
	}, []string{"public"})
	_, err := ValidateMediaReference(json.RawMessage(`{
        "mediaSetRid": "ri.classified",
        "mediaItemRid": "x"
    }`), ctx)
	require.Error(t, err)
	var m *MediaReferenceValidationError
	require.True(t, errors.As(err, &m))
	assert.Equal(t, MediaRefInsufficientClearance, m.Kind)
	assert.Equal(t, "secret", m.Missing)
}

// libs/ontology-kernel/src/domain/media_reference_validator.rs —
// `covers_clearance` — empty markings → unconditional pass.
func TestCoversClearanceEmptyMarkingsAlwaysPass(t *testing.T) {
	assert.True(t, coversClearance([]string{}, []string{}))
	assert.True(t, coversClearance([]string{}, []string{"  ", ""}), "blank markings ignored")
}

// ---- test fakes ---------------------------------------------------------

type writebackFakes struct{}

func stores() writebackFakes { return writebackFakes{} }

func (writebackFakes) objectsRejecting(err error) storageabstraction.ObjectStore {
	return &fakeObjectStore{putErr: err}
}

func (writebackFakes) objectsConflict(expected, actual uint64) storageabstraction.ObjectStore {
	return &fakeObjectStore{putOutcome: storageabstraction.VersionConflict(expected, actual)}
}

type fakeObjectStore struct {
	putOutcome storageabstraction.PutOutcome
	putErr     error
}

func (f *fakeObjectStore) Get(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.ObjectId, _ storageabstraction.ReadConsistency) (*storageabstraction.Object, error) {
	return nil, nil
}
func (f *fakeObjectStore) Put(_ context.Context, _ storageabstraction.Object, _ *uint64) (storageabstraction.PutOutcome, error) {
	return f.putOutcome, f.putErr
}
func (f *fakeObjectStore) Delete(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.ObjectId) (bool, error) {
	return false, nil
}
func (f *fakeObjectStore) ListByType(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.TypeId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.Object], error) {
	return storageabstraction.PagedResult[storageabstraction.Object]{}, nil
}
func (f *fakeObjectStore) ListByOwner(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.OwnerId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.Object], error) {
	return storageabstraction.PagedResult[storageabstraction.Object]{}, nil
}
func (f *fakeObjectStore) ListByMarking(_ context.Context, _ storageabstraction.TenantId, _ storageabstraction.MarkingId, _ storageabstraction.Page, _ storageabstraction.ReadConsistency) (storageabstraction.PagedResult[storageabstraction.Object], error) {
	return storageabstraction.PagedResult[storageabstraction.Object]{}, nil
}

var _ storageabstraction.ObjectStore = (*fakeObjectStore)(nil)
