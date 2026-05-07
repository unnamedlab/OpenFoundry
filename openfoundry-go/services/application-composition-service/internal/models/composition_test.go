package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimaryItemJSONShape(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	at := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	p := PrimaryItem{ID: id, Payload: json.RawMessage(`{"layout":{}}`), CreatedAt: at}
	b, err := json.Marshal(p)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, id.String(), got["id"])
	assert.NotNil(t, got["payload"])
	assert.NotNil(t, got["created_at"])
}

func TestSecondaryItemJSONShape(t *testing.T) {
	t.Parallel()
	id, parent := uuid.New(), uuid.New()
	s := SecondaryItem{ID: id, ParentID: parent, Payload: json.RawMessage(`{"binding":"x"}`), CreatedAt: time.Now()}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, id.String(), got["id"])
	assert.Equal(t, parent.String(), got["parent_id"])
}
