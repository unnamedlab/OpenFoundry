package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultBlockingStrategyMatchesRust(t *testing.T) {
	t.Parallel()
	got := DefaultBlockingStrategy()
	assert.Equal(t, "key-based", got.StrategyType)
	assert.Equal(t, []string{"email", "phone", "display_name"}, got.KeyFields)
	assert.Equal(t, int32(5), got.WindowSize)
	assert.Equal(t, int32(24), got.BucketCount)
}

func TestListResponseEnvelope(t *testing.T) {
	t.Parallel()
	resp := ListResponse[MatchCondition]{
		Data: []MatchCondition{{Field: "email", Comparator: "exact", Weight: 1, Threshold: 0.9, Required: true}},
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"data":[{"field":"email","comparator":"exact","weight":1,"threshold":0.9,"required":true}]}`,
		string(b))
}

func TestErrorResponseEnvelope(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(ErrorResponse{Error: "rule name required"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"error":"rule name required"}`, string(b))
}

func TestMatchConditionRoundTrip(t *testing.T) {
	t.Parallel()
	in := MatchCondition{
		Field: "email", Comparator: "exact", Weight: 0.7, Threshold: 0.85, Required: false,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out MatchCondition
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}

func TestSurvivorshipRuleRoundTrip(t *testing.T) {
	t.Parallel()
	in := SurvivorshipRule{
		Field:          "name",
		Strategy:       "longest_non_empty",
		SourcePriority: []string{"crm", "manual"},
		Fallback:       "first",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out SurvivorshipRule
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, out)
}

func TestFusionOverviewFields(t *testing.T) {
	t.Parallel()
	o := FusionOverview{RuleCount: 3, ActiveJobCount: 1, CompletedJobCount: 7,
		ClusterCount: 12, PendingReviewCount: 2, GoldenRecordCount: 9, AutoMergedClusterCount: 5}
	b, err := json.Marshal(o)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"rule_count":3,"active_job_count":1,"completed_job_count":7,"cluster_count":12,"pending_review_count":2,"golden_record_count":9,"auto_merged_cluster_count":5}`,
		string(b))
}
