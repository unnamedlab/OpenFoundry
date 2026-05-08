package cassandrakernel

import (
	"encoding/json"
	"testing"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func TestActionLogStoreSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ repos.ActionLogStore = (*ActionLogStore)(nil)
}

// --- Day-bucket round-trip ----------------------------------------------

func TestDayBucketRoundTripsThroughEpochOffset(t *testing.T) {
	t.Parallel()
	// 1970-01-01T00:00:00Z → days_since_epoch=0 → day_bucket=2^31.
	d := msToDayBucket(0)
	assert.Equal(t, cqlDateEpochOffset, d)
	assert.Equal(t, int64(0), dayBucketToTime(d).UnixMilli())
}

func TestDayBucketTruncatesToMidnight(t *testing.T) {
	t.Parallel()
	// 86_400_001 ms = 1 day + 1 ms after epoch → day_bucket should
	// still resolve to day 1 (truncates to midnight UTC).
	ms := int64(86_400_001)
	d := msToDayBucket(ms)
	got := dayBucketToTime(d).UnixMilli()
	assert.Equal(t, int64(86_400_000), got, "day bucket truncates to midnight")
}

func TestDayBucketHandlesNegativeMs(t *testing.T) {
	t.Parallel()
	// 1969-12-31T23:59:59Z → day_bucket should round down (floor),
	// landing on day -1 = 2^31 - 1.
	ms := int64(-1)
	d := msToDayBucket(ms)
	assert.Equal(t, cqlDateEpochOffset-1, d)
}

// --- Token codec --------------------------------------------------------

func TestRecentTokenRoundTrip(t *testing.T) {
	t.Parallel()
	original := actionRecentToken{Day: 1<<31 + 19000, DaysScanned: 7, Paging: nil}
	encoded, err := encodeRecentToken(original)
	require.NoError(t, err)
	got, err := decodeRecentToken(&encoded)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestRecentTokenWithEmbeddedPaging(t *testing.T) {
	t.Parallel()
	paging := "deadbeef=="
	original := actionRecentToken{Day: 1 << 31, DaysScanned: 0, Paging: &paging}
	encoded, err := encodeRecentToken(original)
	require.NoError(t, err)
	got, err := decodeRecentToken(&encoded)
	require.NoError(t, err)
	require.NotNil(t, got.Paging)
	assert.Equal(t, paging, *got.Paging)
}

func TestRecentTokenNilDecodesAsTodayCursor(t *testing.T) {
	t.Parallel()
	got, err := decodeRecentToken(nil)
	require.NoError(t, err)
	assert.Equal(t, uint8(0), got.DaysScanned)
	assert.Nil(t, got.Paging)
	// Day bucket roughly == today; we don't assert exact value
	// because the test must remain stable across the day boundary.
	assert.Greater(t, got.Day, cqlDateEpochOffset)
}

func TestRecentTokenMalformedSurfacesInvalidArgument(t *testing.T) {
	t.Parallel()
	bad := "not-base64@@"
	_, err := decodeRecentToken(&bad)
	require.Error(t, err)
	assert.True(t, repos.IsInvalidArgument(err))
	assert.Contains(t, err.Error(), "malformed action-log page token")
}

// --- Event-id derivation ------------------------------------------------

func TestDeriveEventIDPrefersExplicitNonEmpty(t *testing.T) {
	t.Parallel()
	provided := "explicit-event-id"
	entry := repos.ActionLogEntry{
		EventID: &provided,
		Kind:    "create_aircraft",
		Tenant:  "t",
	}
	assert.Equal(t, "explicit-event-id", deriveEventID(entry, "{}"))
}

func TestDeriveEventIDIgnoresWhitespaceOnlyExplicit(t *testing.T) {
	t.Parallel()
	provided := "   "
	entry := repos.ActionLogEntry{
		EventID: &provided,
		Kind:    "fly",
		Subject: "u1",
		Tenant:  "t",
		Payload: json.RawMessage(`{}`),
	}
	got := deriveEventID(entry, "{}")
	assert.NotEqual(t, "   ", got, "whitespace-only event_id must fall through")
	// Must be a stable UUIDv5.
	assert.Len(t, got, 36)
}

func TestDeriveEventIDExtractsFromIdempotencyKey(t *testing.T) {
	t.Parallel()
	entry := repos.ActionLogEntry{
		Kind:    "execute",
		Tenant:  "t",
		Subject: "u1",
		Payload: json.RawMessage(`{"idempotency_key":"abc-123"}`),
	}
	got := deriveEventID(entry, `{"idempotency_key":"abc-123"}`)
	assert.Equal(t, "execute:abc-123", got)
}

func TestDeriveEventIDExtractsFromExecutionId(t *testing.T) {
	t.Parallel()
	entry := repos.ActionLogEntry{
		Kind:    "run",
		Payload: json.RawMessage(`{"executionId":"xyz"}`),
	}
	got := deriveEventID(entry, `{"executionId":"xyz"}`)
	assert.Equal(t, "run:xyz", got)
}

func TestDeriveEventIDFallsBackToUUIDv5(t *testing.T) {
	t.Parallel()
	entry := repos.ActionLogEntry{
		Kind:    "create_aircraft",
		Tenant:  "t1",
		Subject: "user-1",
		Payload: json.RawMessage(`{"action_type_id":"foo","parameters":{"x":1}}`),
	}
	first := deriveEventID(entry, `{"action_type_id":"foo","parameters":{"x":1}}`)
	second := deriveEventID(entry, `{"action_type_id":"foo","parameters":{"x":1}}`)
	assert.Equal(t, first, second, "deterministic over identical input")
	assert.Len(t, first, 36)
}

func TestDeriveEventIDDiffersOnDifferentInput(t *testing.T) {
	t.Parallel()
	a := repos.ActionLogEntry{
		Kind: "x", Tenant: "t", Subject: "u1",
		Payload: json.RawMessage(`{"action_type_id":"foo","parameters":{"k":1}}`),
	}
	b := repos.ActionLogEntry{
		Kind: "x", Tenant: "t", Subject: "u2", // different subject
		Payload: json.RawMessage(`{"action_type_id":"foo","parameters":{"k":1}}`),
	}
	got1 := deriveEventID(a, `{"action_type_id":"foo","parameters":{"k":1}}`)
	got2 := deriveEventID(b, `{"action_type_id":"foo","parameters":{"k":1}}`)
	assert.NotEqual(t, got1, got2)
}

// --- Stable payload projection -----------------------------------------

func TestStablePayloadProjectionWebhookShape(t *testing.T) {
	t.Parallel()
	got := stablePayloadProjection(json.RawMessage(`{
        "side_effect_type": "webhook",
        "webhook_id": "wh-7",
        "action_type_id": "send_email",
        "status": "applied",
        "private": "discarded"
    }`))
	require.NotNil(t, got)
	assert.Equal(t, "send_email", got["action_type_id"])
	assert.Equal(t, "webhook", got["side_effect_type"])
	assert.Equal(t, "wh-7", got["webhook_id"])
	assert.Equal(t, "applied", got["status"])
	_, hasPrivate := got["private"]
	assert.False(t, hasPrivate, "non-projected keys must be dropped")
}

func TestStablePayloadProjectionActionShape(t *testing.T) {
	t.Parallel()
	got := stablePayloadProjection(json.RawMessage(`{
        "action_type_id": "create_x",
        "target_object_id": "obj-1",
        "parameters": {"p": 1},
        "status": "failed",
        "failure_type": "validation",
        "extra": "discarded"
    }`))
	require.NotNil(t, got)
	assert.Equal(t, "create_x", got["action_type_id"])
	assert.Equal(t, "obj-1", got["target_object_id"])
	assert.Equal(t, map[string]any{"p": float64(1)}, got["parameters"])
	assert.Equal(t, "failed", got["status"])
	assert.Equal(t, "validation", got["failure_type"])
	_, hasExtra := got["extra"]
	assert.False(t, hasExtra)
}

func TestStablePayloadProjectionUnknownShapeReturnsNil(t *testing.T) {
	t.Parallel()
	got := stablePayloadProjection(json.RawMessage(`{"foo": "bar"}`))
	assert.Nil(t, got)
}

// --- Event-id-from-payload ----------------------------------------------

func TestEventIDFromPayloadTriesAllKeys(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		`{"event_id":"e1"}`:        "k:e1",
		`{"idempotency_key":"i1"}`: "k:i1",
		`{"idempotencyKey":"i2"}`:  "k:i2",
		`{"execution_id":"x1"}`:    "k:x1",
		`{"executionId":"x2"}`:     "k:x2",
		`{"run_id":"r1"}`:          "k:r1",
		`{"runId":"r2"}`:           "k:r2",
	}
	for payload, want := range cases {
		got := eventIDFromPayload("k", json.RawMessage(payload))
		assert.Equal(t, want, got, "payload=%s", payload)
	}
}

func TestEventIDFromPayloadIgnoresWhitespace(t *testing.T) {
	t.Parallel()
	got := eventIDFromPayload("k", json.RawMessage(`{"event_id":"   "}`))
	assert.Empty(t, got)
}

// --- Subject reconciliation --------------------------------------------

func TestSubjectFromPrefersExplicit(t *testing.T) {
	t.Parallel()
	subj := "alice"
	assert.Equal(t, "alice", subjectFrom(nil, &subj))
}

func TestSubjectFromFallsBackToActorID(t *testing.T) {
	t.Parallel()
	// gocql.UUID stringifies as the canonical 36-char form.
	emptySubj := "  "
	got := subjectFrom(testGocqlUUID(), &emptySubj)
	assert.Len(t, got, 36)
}

func TestSubjectFromHandlesAllNil(t *testing.T) {
	t.Parallel()
	assert.Empty(t, subjectFrom(nil, nil))
}

// testGocqlUUID returns a deterministic gocql.UUID for tests.
func testGocqlUUID() *gocql.UUID {
	parsed := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	g := gocql.UUID(parsed)
	return &g
}

// --- CQL keyspace interpolation ----------------------------------------

func TestActionLogCQLReferencesKeyspace(t *testing.T) {
	t.Parallel()
	s := &ActionLogStore{keyspace: "actions_log"}
	assert.Contains(t, s.cqlInsertEvent(), "actions_log.actions_by_event")
	assert.Contains(t, s.cqlInsertEvent(), "IF NOT EXISTS")
	assert.Contains(t, s.cqlInsertLog(), "actions_log.actions_log")
	assert.Contains(t, s.cqlInsertByObject(), "actions_log.actions_by_object")
	assert.Contains(t, s.cqlInsertByAction(), "actions_log.actions_by_action")
	assert.Contains(t, s.cqlSelectRecent(), "FROM actions_log.actions_log")
	assert.Contains(t, s.cqlSelectByObject(), "FROM actions_log.actions_by_object")
	assert.Contains(t, s.cqlSelectByAction(), "FROM actions_log.actions_by_action")
}
