// Package models holds wire types for ingestion-replication-service.
//
// Foundation slice scope: ingest_jobs only. Streaming + cdc_metadata
// sub-modules (~30k LOC across 20+ submodule migrations) land in
// follow-up slices.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// IngestJob mirrors `ingest_jobs` rows. Each job materializes a Kafka
// connector + Flink deployment pair tracked by status.
type IngestJob struct {
	ID                  uuid.UUID       `json:"id"`
	Name                string          `json:"name"`
	Namespace           string          `json:"namespace"`
	Spec                json.RawMessage `json:"spec"`
	Status              string          `json:"status"`
	KafkaConnectorName  *string         `json:"kafka_connector_name"`
	FlinkDeploymentName *string         `json:"flink_deployment_name"`
	Error               *string         `json:"error"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// CreateIngestJobRequest is POST /api/v1/ingest-jobs.
type CreateIngestJobRequest struct {
	Name      string          `json:"name"`
	Namespace string          `json:"namespace"`
	Spec      json.RawMessage `json:"spec"`
}

// UpdateIngestJobRequest mirrors PATCH semantics — used by the runtime
// to advance status and stamp connector/deployment names.
type UpdateIngestJobRequest struct {
	Status              *string `json:"status,omitempty"`
	KafkaConnectorName  *string `json:"kafka_connector_name,omitempty"`
	FlinkDeploymentName *string `json:"flink_deployment_name,omitempty"`
	Error               *string `json:"error,omitempty"`
}
