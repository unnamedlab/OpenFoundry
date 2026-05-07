package handlers_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/models"
)

// Wire-format invariants: PrimaryItem + SecondaryItem JSON shape
// preserved 1:1 with the Rust crate.
func TestPrimaryJSONShape(t *testing.T) {
	t.Parallel()
	p := models.PrimaryItem{
		ID:        uuid.New(),
		Payload:   json.RawMessage(`{"k":"v"}`),
		CreatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(p)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"id", "payload", "created_at"} {
		assert.Contains(t, view, k)
	}
}

func TestSecondaryJSONShape(t *testing.T) {
	t.Parallel()
	s := models.SecondaryItem{
		ID:        uuid.New(),
		ParentID:  uuid.New(),
		Payload:   json.RawMessage(`{"k":"v"}`),
		CreatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(s)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"id", "parent_id", "payload", "created_at"} {
		assert.Contains(t, view, k)
	}
}

func TestAllFeaturesPinned(t *testing.T) {
	t.Parallel()
	features := models.AllFeatures()
	require.Len(t, features, 4, "four foundation features in canonical order")
	assert.Equal(t, "telemetry-exports", features[0].Feature)
	assert.Equal(t, "telemetry_exports", features[0].Primary)
	assert.Equal(t, "telemetry_policies", features[0].Secondary)
	assert.Equal(t, "policies", features[0].SecondaryPath)

	assert.Equal(t, "health-checks", features[1].Feature)
	assert.Equal(t, "health_checks", features[1].Primary)
	assert.Equal(t, "health_check_results", features[1].Secondary)

	assert.Equal(t, "execution-runs", features[2].Feature)
	assert.Equal(t, "execution_runs", features[2].Primary)
	assert.Equal(t, "execution_logs", features[2].Secondary)

	assert.Equal(t, "monitoring-rules", features[3].Feature)
	assert.Equal(t, "monitoring_rules", features[3].Primary)
	assert.Equal(t, "monitoring_subscribers", features[3].Secondary)
}
