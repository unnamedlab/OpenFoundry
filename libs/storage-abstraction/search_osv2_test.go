package storageabstraction

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemorySearchTextPhrasePrefixAndLanguageSurface(t *testing.T) {
	backend := NewInMemorySearchBackend()
	require.NoError(t, backend.Index(context.Background(), IndexDoc{Tenant: "acme", ID: "obj-1", TypeID: "Aircraft", Version: 1, Payload: json.RawMessage(`{"description":"fast blue aircraft"}`)}))
	require.NoError(t, backend.Index(context.Background(), IndexDoc{Tenant: "acme", ID: "obj-2", TypeID: "Aircraft", Version: 1, Payload: json.RawMessage(`{"description":"slow red truck"}`)}))

	phrase, err := backend.SearchText(context.Background(), TextQuery{Tenant: "acme", TypeID: "Aircraft", Property: "description", Text: "blue aircraft", Phrase: true, Language: "en", Page: Page{Size: 10}}, Strong())
	require.NoError(t, err)
	require.Len(t, phrase.Items, 1)
	assert.Equal(t, ObjectId("obj-1"), phrase.Items[0].ID)

	prefix, err := backend.SearchText(context.Background(), TextQuery{Tenant: "acme", TypeID: "Aircraft", Property: "description", Text: "air", Prefix: true, Page: Page{Size: 10}}, Strong())
	require.NoError(t, err)
	require.Len(t, prefix.Items, 1)
	assert.Equal(t, ObjectId("obj-1"), prefix.Items[0].ID)
}

func TestInMemorySearchHybridSupportsDistanceChoices(t *testing.T) {
	backend := NewInMemorySearchBackend()
	require.NoError(t, backend.Index(context.Background(), IndexDoc{Tenant: "acme", ID: "near", TypeID: "Doc", Version: 1, Payload: json.RawMessage(`{"body":"engine maintenance"}`), Embedding: []float32{1, 0}}))
	require.NoError(t, backend.Index(context.Background(), IndexDoc{Tenant: "acme", ID: "far", TypeID: "Doc", Version: 1, Payload: json.RawMessage(`{"body":"catering"}`), Embedding: []float32{0, 1}}))

	hits, err := backend.SearchHybrid(context.Background(), HybridQuery{Tenant: "acme", TypeID: "Doc", Property: "body", Text: "engine", Embedding: []float32{1, 0}, K: 2, Distance: VectorDistanceCosine}, Strong())
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	assert.Equal(t, ObjectId("near"), hits[0].ID)

	hits, err = backend.SearchHybrid(context.Background(), HybridQuery{Tenant: "acme", TypeID: "Doc", Embedding: []float32{1, 0}, K: 1, Distance: VectorDistanceDot}, Strong())
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, ObjectId("near"), hits[0].ID)
}
