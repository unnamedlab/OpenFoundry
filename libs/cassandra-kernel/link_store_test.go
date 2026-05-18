package cassandrakernel

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func TestLinkStoreSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ repos.LinkStore = (*LinkStore)(nil)
}

func TestDecodeLinkPayloadNilReturnsNil(t *testing.T) {
	t.Parallel()
	got, err := decodeLinkPayload(nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeLinkPayloadValidPassesThrough(t *testing.T) {
	t.Parallel()
	raw := `{"weight": 0.7, "kind": "primary"}`
	got, err := decodeLinkPayload(&raw)
	require.NoError(t, err)
	assert.JSONEq(t, raw, string(got))
}

func TestDecodeLinkPayloadInvalidSurfacesBackend(t *testing.T) {
	t.Parallel()
	bad := `{"oops":`
	_, err := decodeLinkPayload(&bad)
	require.Error(t, err)
	assert.True(t, repos.IsBackendError(err))
	assert.Contains(t, err.Error(), "invalid stored link JSON")
}

func TestNewLinkStoreUsesDefaultKeyspace(t *testing.T) {
	t.Parallel()
	s := &LinkStore{keyspace: "ontology_indexes"}
	assert.Contains(t, s.cqlInsertOutgoing(), "ontology_indexes.links_outgoing")
	assert.Contains(t, s.cqlInsertIncoming(), "ontology_indexes.links_incoming")
	assert.Contains(t, s.cqlDeleteOutgoing(), "DELETE FROM ontology_indexes.links_outgoing")
	assert.Contains(t, s.cqlDeleteIncoming(), "DELETE FROM ontology_indexes.links_incoming")
	assert.Contains(t, s.cqlSelectOutgoing(), "FROM ontology_indexes.links_outgoing")
	assert.Contains(t, s.cqlSelectOutgoing(), "LIMIT ?")
	assert.Contains(t, s.cqlSelectOutgoingAfter(), "target_rid > ?")
	assert.Contains(t, s.cqlSelectIncoming(), "FROM ontology_indexes.links_incoming")
	assert.Contains(t, s.cqlSelectIncoming(), "LIMIT ?")
	assert.Contains(t, s.cqlSelectIncomingAfter(), "source_rid > ?")
	assert.Contains(t, s.cqlSelectOutgoingExact(), "WHERE tenant = ? AND link_type_id = ? AND source_rid = ?")
}

func TestLinkStoreCustomKeyspace(t *testing.T) {
	t.Parallel()
	s := &LinkStore{keyspace: "custom_ks"}
	assert.Contains(t, s.cqlInsertOutgoing(), "custom_ks.links_outgoing")
}

func TestCQLInsertStatementsHaveLwtClause(t *testing.T) {
	t.Parallel()
	s := &LinkStore{keyspace: "ontology_indexes"}
	// Both outgoing and incoming inserts MUST be LWT (IF NOT EXISTS)
	// so re-puts of the same triple are no-ops, mirroring the Rust
	// "links are immutable" contract.
	assert.Contains(t, s.cqlInsertOutgoing(), "IF NOT EXISTS")
	assert.Contains(t, s.cqlInsertIncoming(), "IF NOT EXISTS")
}

func TestLinkPayloadCanonicalisation(t *testing.T) {
	t.Parallel()
	// Round-trip through canonicalJSON sorts keys alphabetically —
	// guarantees byte-stable storage so cache-key fingerprints
	// downstream are deterministic.
	got, err := canonicalJSON(json.RawMessage(`{"z":1,"a":2}`))
	require.NoError(t, err)
	assert.Equal(t, `{"a":2,"z":1}`, got)
}

func TestLinkCursorRoundTrip(t *testing.T) {
	t.Parallel()
	id, err := parseUUID("id", "018f3f37-6c7c-7d89-9abc-def012345678")
	require.NoError(t, err)
	token := encodeLinkCursor(id)
	got, ok, err := decodeLinkCursor(token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, id, got)
}

func TestLinkCursorMalformed(t *testing.T) {
	t.Parallel()
	bad := "not-base64"
	_, _, err := decodeLinkCursor(&bad)
	require.Error(t, err)
	assert.True(t, repos.IsInvalidArgument(err))
}
