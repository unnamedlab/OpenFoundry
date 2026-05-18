package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kernelstores "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/stores"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func TestProcessMessageWithStoresProjectsObjectRowsAndDedupesEventID(t *testing.T) {
	objects := kernelstores.NewInMemoryObjectStore()
	backend := &fakeBackend{}
	projector := NewProjectionIndex()
	msg := KafkaMessage{Topic: TopicObjectChangedV1, Time: time.Unix(10, 0), Value: mustJSON(t, map[string]any{
		"event_id": "evt-1", "tenant": "acme", "id": "obj-1", "type_id": "Aircraft", "version": 1,
		"payload": map[string]any{"tail_number": "EC-123"},
	})}

	outcome, err := ProcessMessageWithStores(context.Background(), backend, StorageProjector{Objects: objects}, projector, msg, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeIndexed, outcome)
	stored, err := objects.Get(context.Background(), "acme", "obj-1", repos.Strong())
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, repos.TypeId("Aircraft"), stored.TypeID)
	assert.JSONEq(t, `{"tail_number":"EC-123"}`, string(stored.Payload))
	require.Len(t, backend.indexed, 1)

	outcome, err = ProcessMessageWithStores(context.Background(), backend, StorageProjector{Objects: objects}, projector, msg, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeSkippedStale, outcome)
	assert.Len(t, backend.indexed, 1)
}

func TestProcessMessageWithStoresProjectsLinkRows(t *testing.T) {
	links := kernelstores.NewInMemoryLinkStore()
	backend := &fakeBackend{}
	projector := NewProjectionIndex()
	msg := KafkaMessage{Topic: TopicLinkChangedV1, Time: time.Unix(20, 0), Value: mustJSON(t, map[string]any{
		"event_id": "link-evt-1", "tenant": "acme", "link_type": "owns.asset", "from": "owner-1", "to": "asset-1", "version": 1,
		"payload": map[string]any{"role": "operator"},
	})}

	outcome, err := ProcessMessageWithStores(context.Background(), backend, StorageProjector{Links: links}, projector, msg, nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeIndexed, outcome)
	page, err := links.ListOutgoing(context.Background(), "acme", "owns.asset", "owner-1", repos.Page{Size: 10}, repos.Strong())
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, repos.ObjectId("asset-1"), page.Items[0].To)
	assert.JSONEq(t, `{"role":"operator"}`, string(page.Items[0].Payload))
}

func TestEventIDFromMessageIgnoresMalformedJSON(t *testing.T) {
	assert.Empty(t, eventIDFromMessage(KafkaMessage{Topic: TopicObjectChangedV1, Value: json.RawMessage(`{"tenant":`)}))
}
