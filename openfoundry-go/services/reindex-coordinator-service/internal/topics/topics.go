// Package topics holds pinned Kafka topic constants for
// reindex-coordinator-service. Topic names are part of the wire
// contract with services/ontology-indexer (consumer of
// ontology.reindex.v1) and the control plane that dispatches
// reindex requests (producer of ontology.reindex.requested.v1).
// Pinning them as constants makes a typo a compile error.
package topics

// OntologyReindexRequestedV1 is the input topic. The coordinator
// subscribes here. Payload is JSON-serialised event.ReindexRequestedV1.
const OntologyReindexRequestedV1 = "ontology.reindex.requested.v1"

// OntologyReindexV1 is the output (data plane) topic. One Kafka
// record per re-indexed object. Same payload shape as
// ontology.object.changed.v1 so ontology-indexer can ingest both
// with the same decoder.
const OntologyReindexV1 = "ontology.reindex.v1"

// OntologyReindexCompletedV1 is the output (control plane) topic.
// One record per terminal job transition (completed/failed/cancelled).
// Payload is JSON-serialised event.ReindexCompletedV1.
const OntologyReindexCompletedV1 = "ontology.reindex.completed.v1"

// ConsumerGroup pinned so replicas don't fork rebalance state.
const ConsumerGroup = "reindex-coordinator"
