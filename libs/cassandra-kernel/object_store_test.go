package cassandrakernel

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// Pure-logic tests pinning the helpers ObjectStore relies on. The
// network-bound Get / Put / Delete / List* paths land with a
// testcontainers-backed integration suite when the consuming
// service (object-database) wires Cassandra in.

func TestCanonicalJSONHandlesEmpty(t *testing.T) {
	t.Parallel()
	for _, raw := range []json.RawMessage{nil, []byte(""), []byte("   "), json.RawMessage("null")} {
		got, err := canonicalJSON(raw)
		require.NoError(t, err)
		// null is valid JSON and round-trips as "null".
		if string(raw) == "null" {
			assert.Equal(t, "null", got)
			continue
		}
		assert.Equal(t, "{}", got)
	}
}

func TestCanonicalJSONNormalisesKeyOrder(t *testing.T) {
	t.Parallel()
	got, err := canonicalJSON(json.RawMessage(`{"z": 1, "a": 2}`))
	require.NoError(t, err)
	// encoding/json sorts map keys alphabetically on Marshal.
	assert.Equal(t, `{"a":2,"z":1}`, got)
}

func TestCanonicalJSONRejectsInvalid(t *testing.T) {
	t.Parallel()
	_, err := canonicalJSON(json.RawMessage(`{"a":`))
	require.Error(t, err)
}

func TestTruncateSummaryClampsAt1024(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ab", truncateSummary("ab"))
	long := strings.Repeat("x", 1100)
	assert.Len(t, truncateSummary(long), 1024)
}

func TestClampPageSizeBounds(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1, clampPageSize(0))
	assert.Equal(t, 1, clampPageSize(1))
	assert.Equal(t, 5000, clampPageSize(5000))
	assert.Equal(t, 5000, clampPageSize(99999))
	assert.Equal(t, 250, clampPageSize(250))
}

func TestPagingStateEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()
	original := []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x12}
	encoded := encodePagingState(original)
	require.NotNil(t, encoded)
	require.Equal(t, base64.StdEncoding.EncodeToString(original), *encoded)
	decoded, err := decodePagingState(encoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestPagingStateEncodeNilEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, encodePagingState(nil))
	assert.Nil(t, encodePagingState([]byte{}))

	got, err := decodePagingState(nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestPagingStateDecodeRejectsMalformed(t *testing.T) {
	t.Parallel()
	bad := "not!base64"
	_, err := decodePagingState(&bad)
	require.Error(t, err)
	assert.True(t, repos.IsInvalidArgument(err), "must surface as RepoInvalidArgument")
}

func TestParseUUIDRejectsBadInput(t *testing.T) {
	t.Parallel()
	_, err := parseUUID("object_id", "not-a-uuid")
	require.Error(t, err)
	assert.True(t, repos.IsInvalidArgument(err))
	assert.Contains(t, err.Error(), "object_id is not a valid UUID")
}

func TestParseUUIDOptHandlesNil(t *testing.T) {
	t.Parallel()
	got, err := parseUUIDOpt("organization_id", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestParseUUIDOptParsesValue(t *testing.T) {
	t.Parallel()
	id := uuid.NewString()
	got, err := parseUUIDOpt("organization_id", &id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, id, got.String())
}

func TestOrganizationIDFromTenantParsesUUID(t *testing.T) {
	t.Parallel()
	tenantUUID := uuid.NewString()
	got := organizationIDFromTenant(repos.TenantId(tenantUUID))
	require.NotNil(t, got)
	assert.Equal(t, tenantUUID, *got)
}

func TestOrganizationIDFromTenantReturnsNilForNonUUID(t *testing.T) {
	t.Parallel()
	got := organizationIDFromTenant(repos.TenantId("not-uuid-shape"))
	assert.Nil(t, got)
}

func TestRevisionFromCASExtractsTypedColumn(t *testing.T) {
	t.Parallel()
	row := map[string]any{"revision_number": int64(7), "[applied]": false}
	assert.Equal(t, uint64(7), revisionFromCAS(row))
}

func TestRevisionFromCASFallsBackToAnyInt64(t *testing.T) {
	t.Parallel()
	// gocql sometimes labels the column differently; the Rust impl
	// also fell back to "first bigint column". Verify that path.
	row := map[string]any{"some_int": int64(42)}
	assert.Equal(t, uint64(42), revisionFromCAS(row))
}

func TestRevisionFromCASNoIntegerReturnsZero(t *testing.T) {
	t.Parallel()
	row := map[string]any{"name": "alice"}
	assert.Equal(t, uint64(0), revisionFromCAS(row))
}

func TestObjectStoreSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ repos.ObjectStore = (*ObjectStore)(nil)
}

func TestOSV2PrimaryKeyHashBucketStableAndBounded(t *testing.T) {
	t.Parallel()
	got := primaryKeyHashBucket("customer-123")
	assert.GreaterOrEqual(t, got, 0)
	assert.Less(t, got, objectHashBuckets)
	assert.Equal(t, got, primaryKeyHashBucket("customer-123"))
}

func TestOSV2PropertyIDDoesNotExposeName(t *testing.T) {
	t.Parallel()
	got := propertyID("sensitive_property_name")
	assert.NotContains(t, got, "sensitive_property_name")
	assert.Equal(t, got, propertyID("sensitive_property_name"))
}

func TestOSV2PropertiesBlobUsesPropertyIDs(t *testing.T) {
	t.Parallel()
	blob, err := encodePropertiesBlob(json.RawMessage(`{"status":"OPEN","score":7,"empty":null}`))
	require.NoError(t, err)
	assert.Contains(t, string(blob), "of.osv2.properties.v1")
	assert.NotContains(t, string(blob), "status")
	assert.Contains(t, string(blob), propertyID("status"))
	terms, err := propertyIndexTerms(json.RawMessage(`{"status":"OPEN","score":7,"empty":null}`))
	require.NoError(t, err)
	require.Len(t, terms, 3)
	assert.Contains(t, []string{terms[0].PropertyID, terms[1].PropertyID, terms[2].PropertyID}, propertyID("status"))
}
