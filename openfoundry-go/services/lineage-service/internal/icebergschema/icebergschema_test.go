package icebergschema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunsFieldIDsStable(t *testing.T) {
	t.Parallel()
	// Iceberg field IDs are part of the on-disk format. Changing one
	// is a metadata-rewrite operation. Pin every value here.
	cases := map[string]int32{
		RunsFieldRunID:        1,
		RunsFieldJobNamespace: 2,
		RunsFieldJobName:      3,
		RunsFieldStartedAt:    4,
		RunsFieldCompletedAt:  5,
		RunsFieldState:        6,
		RunsFieldFacets:       7,
	}
	got := map[string]int32{
		RunsFieldRunID:        RunsFieldIDRunID,
		RunsFieldJobNamespace: RunsFieldIDJobNamespace,
		RunsFieldJobName:      RunsFieldIDJobName,
		RunsFieldStartedAt:    RunsFieldIDStartedAt,
		RunsFieldCompletedAt:  RunsFieldIDCompletedAt,
		RunsFieldState:        RunsFieldIDState,
		RunsFieldFacets:       RunsFieldIDFacets,
	}
	assert.Equal(t, cases, got)
}

func TestEventsFieldIDsStable(t *testing.T) {
	t.Parallel()
	got := map[string]int32{
		EventsFieldEventID:   EventsFieldIDEventID,
		EventsFieldRunID:     EventsFieldIDRunID,
		EventsFieldEventTime: EventsFieldIDEventTime,
		EventsFieldEventType: EventsFieldIDEventType,
		EventsFieldProducer:  EventsFieldIDProducer,
		EventsFieldSchemaURL: EventsFieldIDSchemaURL,
		EventsFieldPayload:   EventsFieldIDPayload,
	}
	want := map[string]int32{
		"event_id": 1, "run_id": 2, "event_time": 3, "event_type": 4,
		"producer": 5, "schema_url": 6, "payload": 7,
	}
	assert.Equal(t, want, got)
}

func TestDatasetsIOFieldIDsStable(t *testing.T) {
	t.Parallel()
	got := map[string]int32{
		DatasetsIOFieldRunID:            DatasetsIOFieldIDRunID,
		DatasetsIOFieldEventTime:        DatasetsIOFieldIDEventTime,
		DatasetsIOFieldSide:             DatasetsIOFieldIDSide,
		DatasetsIOFieldDatasetNamespace: DatasetsIOFieldIDDatasetNamespace,
		DatasetsIOFieldDatasetName:      DatasetsIOFieldIDDatasetName,
		DatasetsIOFieldFacets:           DatasetsIOFieldIDFacets,
	}
	want := map[string]int32{
		"run_id": 1, "event_time": 2, "side": 3,
		"dataset_namespace": 4, "dataset_name": 5, "facets": 6,
	}
	assert.Equal(t, want, got)
}

func TestPartitionTransformsAreDay(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Day, RunsPartitionTransform)
	assert.Equal(t, Day, EventsPartitionTransform)
	assert.Equal(t, Day, DatasetsIOPartitionTransform)
}

func TestPartitionSourcesMatchEventTime(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "started_at", RunsPartitionSourceField)
	assert.Equal(t, "event_time", EventsPartitionSourceField)
	assert.Equal(t, "event_time", DatasetsIOPartitionSourceField)
}

func TestRequiredColumnsLocked(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		[]string{"run_id", "job_namespace", "job_name", "started_at", "state"},
		RunsRequired)
	assert.Equal(t,
		[]string{"event_id", "run_id", "event_time", "event_type"},
		EventsRequired)
	assert.Equal(t,
		[]string{"run_id", "event_time", "side", "dataset_namespace", "dataset_name"},
		DatasetsIORequired)
}

func TestDatasetsIOSideValues(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "input", DatasetsIOSideInput)
	assert.Equal(t, "output", DatasetsIOSideOutput)
}

func TestTablePropertiesSet(t *testing.T) {
	t.Parallel()
	props := map[string]string{}
	for _, kv := range TableProperties {
		props[kv[0]] = kv[1]
	}
	assert.Equal(t, "parquet", props["write.format.default"])
	assert.Equal(t, "zstd", props["write.parquet.compression-codec"])
	assert.Equal(t, "7776000000", props["history.expire.max-snapshot-age-ms"])
	assert.Equal(t, "10", props["history.expire.min-snapshots-to-keep"])
}
