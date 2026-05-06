package event

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func TestReindexNamespaceVerbatimFromRust(t *testing.T) {
	t.Parallel()
	expected := uuid.UUID{
		0x6f, 0x82, 0x4d, 0x6e, 0x71, 0xa1, 0x4b, 0x9b,
		0x9c, 0xfe, 0x9f, 0x4f, 0x07, 0x2c, 0x88, 0x10,
	}
	assert.Equal(t, expected, ReindexNamespace,
		"namespace bytes are part of the wire contract — DO NOT change without a fleet-wide migration")
}

func TestJobIDStableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := DeriveJobID("tenant-a", ptr("users"))
	b := DeriveJobID("tenant-a", ptr("users"))
	assert.Equal(t, a, b)
	assert.Equal(t, uuid.Version(5), a.Version(), "expect UUID version 5")
}

func TestJobIDDistinguishesAllTypesFromPerType(t *testing.T) {
	t.Parallel()
	all := DeriveJobID("tenant-a", nil)
	typed := DeriveJobID("tenant-a", ptr(""))
	users := DeriveJobID("tenant-a", ptr("users"))
	// nil and Some("") collapse to the same key by design.
	assert.Equal(t, all, typed)
	assert.NotEqual(t, all, users)
}

func TestJobIDDistinguishesTenants(t *testing.T) {
	t.Parallel()
	a := DeriveJobID("tenant-a", ptr("users"))
	b := DeriveJobID("tenant-b", ptr("users"))
	assert.NotEqual(t, a, b)
}

func TestBatchEventIDStableAndTokenSensitive(t *testing.T) {
	t.Parallel()
	p0 := DeriveBatchEventID("tenant-a", ptr("users"), "")
	p0b := DeriveBatchEventID("tenant-a", ptr("users"), "")
	p1 := DeriveBatchEventID("tenant-a", ptr("users"), "AAECAw==")
	assert.Equal(t, p0, p0b)
	assert.NotEqual(t, p0, p1)
}

func TestRequestedV1RoundTrip(t *testing.T) {
	t.Parallel()
	r := ReindexRequestedV1{
		TenantID:  "tenant-a",
		TypeID:    ptr("users"),
		PageSize:  ptr(int32(500)),
		RequestID: ptr("req-1"),
	}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	var back ReindexRequestedV1
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, r, back)
}

func TestRequestedV1OmitsOptionalFields(t *testing.T) {
	t.Parallel()
	r := ReindexRequestedV1{TenantID: "tenant-a"}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	assert.JSONEq(t, `{"tenant_id":"tenant-a"}`, string(b),
		"optional fields must omit when nil — pinned wire shape")
}

func TestCompletedV1RoundTrip(t *testing.T) {
	t.Parallel()
	c := ReindexCompletedV1{
		JobID:     DeriveJobID("tenant-a", ptr("users")),
		TenantID:  "tenant-a",
		TypeID:    ptr("users"),
		Scanned:   12345,
		Published: 12000,
		Status:    "completed",
	}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	var back ReindexCompletedV1
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, c, back)
}
