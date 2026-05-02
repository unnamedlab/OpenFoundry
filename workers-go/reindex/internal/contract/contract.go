// Package contract pins the task queue, workflow names and search
// attributes shared between the Rust `temporal-client` and the Go
// `reindex` worker. A typo here is a silent rebalance failure.
//
// Mirror of `libs/temporal-client/src/lib.rs::task_queues::REINDEX`
// (added in the same PR that wires this worker into the cluster).
package contract

const (
	// TaskQueue is the Temporal task queue name. Must match the
	// Rust constant byte-for-byte.
	TaskQueue = "openfoundry.reindex"

	// WorkflowOntologyReindex is the canonical reindex-everything
	// workflow. Reads from Cassandra, publishes to Kafka.
	WorkflowOntologyReindex = "OntologyReindex"

	// ActivityScanCassandra activity reads a page of objects from
	// Cassandra. The activity body lives in the Rust side; the
	// worker invokes it via a heartbeat-bounded local activity
	// proxy.
	ActivityScanCassandra = "ScanCassandraObjects"

	// ActivityPublishReindexBatch publishes a batch of objects
	// to the dedicated `ontology.reindex.v1` topic. Same payload
	// shape as `ontology.object.changed.v1` so the indexer can
	// reuse its decoder; separate topic so backfill traffic does
	// not starve the live consumer group.
	ActivityPublishReindexBatch = "PublishReindexBatch"

	// TopicReindex is the Kafka topic dedicated to backfill /
	// re-index runs. Must match
	// `services/ontology-indexer::topics::ONTOLOGY_REINDEX_V1`.
	TopicReindex = "ontology.reindex.v1"

	// HeaderAuditCorrelation is the gRPC metadata key used when
	// activities call back into the Rust services.
	HeaderAuditCorrelation = "x-audit-correlation-id"
)

// OntologyReindexInput is the workflow input. A single tenant is
// reindexed per workflow execution; system-wide reindex fans out
// one workflow per tenant.
type OntologyReindexInput struct {
	TenantID string `json:"tenant_id"`
	// Optional type_id filter. Empty string means "all types".
	TypeID string `json:"type_id,omitempty"`
	// Page size for the Cassandra scan activity. Defaults to 1000
	// at activity boundary if zero.
	PageSize int `json:"page_size,omitempty"`
	// Optional resume token from a previous run.
	ResumeToken string `json:"resume_token,omitempty"`
}

// OntologyReindexResult summarises one reindex run.
type OntologyReindexResult struct {
	TenantID  string `json:"tenant_id"`
	Scanned   int64  `json:"scanned"`
	Published int64  `json:"published"`
	Status    string `json:"status"` // "completed" | "failed" | "cancelled"
	Error     string `json:"error,omitempty"`
}
