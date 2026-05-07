package scan

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPageSizeBounds(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int32(1), MinPageSize)
	assert.Equal(t, int32(10000), MaxPageSize)
	assert.Equal(t, int32(1000), DefaultPageSize)
}

func TestDecodeRequestDefaultsAndClamps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want DecodedRequest
	}{
		{"defaults", `{"tenant_id":"t-a"}`, DecodedRequest{TenantID: "t-a", PageSize: DefaultPageSize}},
		{"empty type collapses", `{"tenant_id":"t-a","type_id":""}`, DecodedRequest{TenantID: "t-a", PageSize: DefaultPageSize}},
		{"type with spaces trimmed", `{"tenant_id":"t-a","type_id":"  users "}`, DecodedRequest{TenantID: "t-a", TypeID: ptr("users"), PageSize: DefaultPageSize}},
		{"page_size 0 → default", `{"tenant_id":"t-a","page_size":0}`, DecodedRequest{TenantID: "t-a", PageSize: DefaultPageSize}},
		{"page_size clamped high", `{"tenant_id":"t-a","page_size":99999}`, DecodedRequest{TenantID: "t-a", PageSize: MaxPageSize}},
		{"page_size respected", `{"tenant_id":"t-a","page_size":250}`, DecodedRequest{TenantID: "t-a", PageSize: 250}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeRequest([]byte(c.in))
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestDecodeRequestRejectsEmptyTenant(t *testing.T) {
	t.Parallel()
	_, err := DecodeRequest([]byte(`{"tenant_id":""}`))
	require.Error(t, err)
	var de *DecodeError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, "missing", de.Kind)
	assert.Equal(t, "tenant_id", de.Field)
}

func TestDecodeRequestRejectsNegativePageSize(t *testing.T) {
	t.Parallel()
	_, err := DecodeRequest([]byte(`{"tenant_id":"t","page_size":-1}`))
	require.Error(t, err)
	var de *DecodeError
	require.True(t, errors.As(err, &de))
	assert.Equal(t, "invalid", de.Kind)
	assert.Equal(t, "page_size", de.Field)
}

func TestReindexRecordPartitionKey(t *testing.T) {
	t.Parallel()
	r := ReindexRecord{Tenant: "tenant-a", ID: "obj-1"}
	assert.Equal(t, "tenant-a/obj-1", r.PartitionKey(),
		"partition-key composition must match the legacy Go worker for indexer hash co-location")
}

func TestEncodeBatchRecordExtractsEmbedding(t *testing.T) {
	t.Parallel()
	props := json.RawMessage(`{"name":"alice","embedding":[0.1,0.2,0.3]}`)
	r := EncodeBatchRecord("t-a", "obj-1", "user", 7, props)
	assert.Equal(t, "t-a", r.Tenant)
	assert.Equal(t, "obj-1", r.ID)
	assert.Equal(t, "user", r.TypeID)
	assert.Equal(t, int64(7), r.Version)
	assert.Equal(t, []float64{0.1, 0.2, 0.3}, r.Embedding)
	assert.False(t, r.Deleted, "deleted always false on publish path")
}

func TestEncodeBatchRecordOmitsEmbeddingWhenAbsent(t *testing.T) {
	t.Parallel()
	props := json.RawMessage(`{"name":"alice"}`)
	r := EncodeBatchRecord("t-a", "obj-1", "user", 1, props)
	assert.Nil(t, r.Embedding)

	b, err := json.Marshal(r)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "embedding", "omitempty must elide nil embedding from wire")
}

func ptr[T any](v T) *T { return &v }
