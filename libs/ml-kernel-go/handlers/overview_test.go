package handlers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorResponseEnvelope(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(ErrorResponse{Error: "boom"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"error":"boom"}`, string(b))
}
