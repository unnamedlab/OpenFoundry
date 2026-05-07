// Package icebergschema pins the column set, partition transform
// and sort order for the three Iceberg tables behind the
// OpenLineage materialisation pipeline. The writer ports in a
// follow-up slice; pinning these constants now keeps the wire-
// compat tests honest.
//
// Table layout follows OpenLineage 1.x event conventions
// (https://openlineage.io/spec/) trimmed to what the Foundry-parity
// UI actually queries.
package icebergschema

// --- Common ---------------------------------------------------------------

const (
	Day       = "day"
	Asc       = "asc"
	NullsLast = "nulls-last"
)

// TableProperties are applied to every materialised lineage table.
// Lineage history is regenerable from the Cassandra hot path, so
// 90-day snapshot expiry is allowed (contrast with audit_sink which
// forbids expiry entirely).
var TableProperties = [][2]string{
	{"write.format.default", "parquet"},
	{"write.parquet.compression-codec", "zstd"},
	{"history.expire.max-snapshot-age-ms", "7776000000"}, // 90d
	{"history.expire.min-snapshots-to-keep", "10"},
}

// --- Runs table -----------------------------------------------------------

const RunsTable = "runs"

// Field names. Pinned because downstream readers (UI, lineage-graph
// query layer) reference them by literal string.
const (
	RunsFieldRunID        = "run_id"
	RunsFieldJobNamespace = "job_namespace"
	RunsFieldJobName      = "job_name"
	RunsFieldStartedAt    = "started_at"
	RunsFieldCompletedAt  = "completed_at"
	RunsFieldState        = "state"
	RunsFieldFacets       = "facets"
)

// Field IDs. Iceberg field IDs are part of the on-disk format —
// changing one is a metadata-rewrite operation.
const (
	RunsFieldIDRunID        int32 = 1
	RunsFieldIDJobNamespace int32 = 2
	RunsFieldIDJobName      int32 = 3
	RunsFieldIDStartedAt    int32 = 4
	RunsFieldIDCompletedAt  int32 = 5
	RunsFieldIDState        int32 = 6
	RunsFieldIDFacets       int32 = 7
)

// Partition + sort.
const (
	RunsPartitionSourceField = RunsFieldStartedAt
	RunsPartitionTransform   = Day
	RunsSortField            = RunsFieldStartedAt
	RunsSortDirection        = Asc
)

// RunsRequired pins the NOT NULL columns.
var RunsRequired = []string{
	RunsFieldRunID,
	RunsFieldJobNamespace,
	RunsFieldJobName,
	RunsFieldStartedAt,
	RunsFieldState,
}

// --- Events table ---------------------------------------------------------

const EventsTable = "events"

const (
	EventsFieldEventID   = "event_id"
	EventsFieldRunID     = "run_id"
	EventsFieldEventTime = "event_time"
	EventsFieldEventType = "event_type"
	EventsFieldProducer  = "producer"
	EventsFieldSchemaURL = "schema_url"
	EventsFieldPayload   = "payload"
)

const (
	EventsFieldIDEventID   int32 = 1
	EventsFieldIDRunID     int32 = 2
	EventsFieldIDEventTime int32 = 3
	EventsFieldIDEventType int32 = 4
	EventsFieldIDProducer  int32 = 5
	EventsFieldIDSchemaURL int32 = 6
	EventsFieldIDPayload   int32 = 7
)

const (
	EventsPartitionSourceField = EventsFieldEventTime
	EventsPartitionTransform   = Day
	EventsSortField            = EventsFieldEventTime
	EventsSortDirection        = Asc
)

var EventsRequired = []string{
	EventsFieldEventID,
	EventsFieldRunID,
	EventsFieldEventTime,
	EventsFieldEventType,
}

// --- Datasets I/O table ---------------------------------------------------

const DatasetsIOTable = "datasets_io"

const (
	DatasetsIOFieldRunID            = "run_id"
	DatasetsIOFieldEventTime        = "event_time"
	DatasetsIOFieldSide             = "side"
	DatasetsIOFieldDatasetNamespace = "dataset_namespace"
	DatasetsIOFieldDatasetName      = "dataset_name"
	DatasetsIOFieldFacets           = "facets"

	DatasetsIOSideInput  = "input"
	DatasetsIOSideOutput = "output"
)

const (
	DatasetsIOFieldIDRunID            int32 = 1
	DatasetsIOFieldIDEventTime        int32 = 2
	DatasetsIOFieldIDSide             int32 = 3
	DatasetsIOFieldIDDatasetNamespace int32 = 4
	DatasetsIOFieldIDDatasetName      int32 = 5
	DatasetsIOFieldIDFacets           int32 = 6
)

const (
	DatasetsIOPartitionSourceField = DatasetsIOFieldEventTime
	DatasetsIOPartitionTransform   = Day
	DatasetsIOSortField            = DatasetsIOFieldEventTime
	DatasetsIOSortDirection        = Asc
)

var DatasetsIORequired = []string{
	DatasetsIOFieldRunID,
	DatasetsIOFieldEventTime,
	DatasetsIOFieldSide,
	DatasetsIOFieldDatasetNamespace,
	DatasetsIOFieldDatasetName,
}
