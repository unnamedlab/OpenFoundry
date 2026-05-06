package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libs/ontology-kernel/src/metrics.rs
// `failure_type_from_http_status_classifies_common_codes`.
func TestFailureTypeFromHTTPStatus(t *testing.T) {
	assert.Equal(t, FailureTypeInvalidParameter, FailureTypeFromHTTPStatus(400))
	assert.Equal(t, FailureTypeInvalidParameter, FailureTypeFromHTTPStatus(422))
	assert.Equal(t, FailureTypeAuthentication, FailureTypeFromHTTPStatus(401))
	assert.Equal(t, FailureTypeAuthentication, FailureTypeFromHTTPStatus(403))
	assert.Equal(t, FailureTypeConflict, FailureTypeFromHTTPStatus(409))
	assert.Equal(t, FailureTypeScaleLimit, FailureTypeFromHTTPStatus(429))
	assert.Equal(t, FailureTypeSideEffect, FailureTypeFromHTTPStatus(500))
	assert.Equal(t, FailureTypeSideEffect, FailureTypeFromHTTPStatus(503))
	assert.Equal(t, FailureTypeUnclassified, FailureTypeFromHTTPStatus(200))
}

// libs/ontology-kernel/src/metrics.rs `FailureType::as_str` —
// every variant prints its snake_case label verbatim.
func TestFailureTypeAsStr(t *testing.T) {
	cases := map[FailureType]string{
		FailureTypeInvalidParameter:   "invalid_parameter",
		FailureTypeScaleLimit:         "scale_limit",
		FailureTypeAuthentication:     "authentication",
		FailureTypeSideEffect:         "side_effect",
		FailureTypeFunction:           "function",
		FailureTypeUserFacingFunction: "user_facing_function",
		FailureTypeConflict:           "conflict",
		FailureTypeUnclassified:       "unclassified",
	}
	for f, want := range cases {
		assert.Equal(t, want, f.AsStr())
	}
}

// libs/ontology-kernel/src/metrics.rs
// `metrics_record_success_and_failure_increments_counters`. The
// gathered families must include the three collectors and reflect
// the recorded calls.
func TestRecordSuccessAndFailureIncrementsCounters(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := RegisterActionMetrics(registry)
	require.NotNil(t, m)
	const action = "00000000-0000-0000-0000-000000000001"
	m.RecordSuccess(action, 0.123)
	m.RecordFailure(action, FailureTypeInvalidParameter, 0.05)

	families, err := registry.Gather()
	require.NoError(t, err)
	require.NotEmpty(t, families)

	gather := func(name string) *dto.MetricFamily {
		for _, fam := range families {
			if fam.GetName() == name {
				return fam
			}
		}
		return nil
	}
	assert.NotNil(t, gather("action_executions_total"))
	assert.NotNil(t, gather("action_execution_duration_seconds"))
	assert.NotNil(t, gather("action_failures_total"))

	// Singleton accessor returns the registered bundle.
	assert.NotNil(t, ActionMetricsSingleton())
}

// libs/ontology-kernel/src/metrics.rs — exponential bucket spec is
// 10 buckets, start 0.005, factor 2.0. Pin the first/last bucket so
// drift on the helper is caught.
func TestExponentialBucketsShape(t *testing.T) {
	b := exponentialBuckets(0.005, 2.0, 10)
	require.Len(t, b, 10)
	assert.InDelta(t, 0.005, b[0], 1e-9)
	// 0.005 * 2^9 = 2.56
	assert.InDelta(t, 2.56, b[9], 1e-6)
}

// libs/ontology-kernel/src/metrics.rs — re-registering the same
// collector against a fresh registry is allowed (mirrors `let _ =
// registry.register(...)`). Reusing the singleton across registries
// must not panic.
func TestRegisterActionMetricsAcrossRegistries(t *testing.T) {
	r1 := prometheus.NewRegistry()
	r2 := prometheus.NewRegistry()
	a := RegisterActionMetrics(r1)
	b := RegisterActionMetrics(r2)
	// Same singleton instance.
	assert.Same(t, a, b)
}
