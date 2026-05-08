// Package kafkatoiceberg holds the wire-compat constants for the
// lineage Kafka → Iceberg materialisation pipeline. The full
// consumer + writer wiring lands in a follow-up slice; the
// constants ship now so downstream Iceberg readers can rely on
// stable namespace / table names regardless of which mode the
// foundation binary is currently running.
package kafkatoiceberg

// SourceTopic is the Kafka topic the lineage consumer subscribes to.
// Verbatim from Rust src/kafka_to_iceberg.rs.
const SourceTopic = "lineage.events.v1"

// ConsumerGroup is pinned so replicas don't fork rebalance state.
const ConsumerGroup = "lineage-service"

// Iceberg target — preserves the wire-format namespace + table
// names so existing readers continue to find rows after cut-over.
const (
	IcebergCatalog        = "lakekeeper"
	IcebergNamespace      = "of_lineage"
	IcebergTableRuns      = "runs"
	IcebergTableEvents    = "events"
	IcebergTableDatasetsIO = "datasets_io"

	// PartitionTransform applies a `day(event_time)` partition on
	// every materialised table.
	PartitionTransform = "day(event_time)"
)
