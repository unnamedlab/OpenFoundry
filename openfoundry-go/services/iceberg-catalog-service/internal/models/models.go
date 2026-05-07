// Package models holds wire types for iceberg-catalog-service.
//
// Foundation slice scope: iceberg_namespaces only. iceberg_tables,
// iceberg_snapshots, iceberg_branches, metadata files, REST Catalog
// OpenAPI surface (10k LOC of Rust handlers) all land in follow-up
// slices.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// IcebergNamespace mirrors `iceberg_namespaces` rows.
type IcebergNamespace struct {
	ID                uuid.UUID       `json:"id"`
	ProjectRID        string          `json:"project_rid"`
	Name              string          `json:"name"`
	ParentNamespaceID *uuid.UUID      `json:"parent_namespace_id"`
	Properties        json.RawMessage `json:"properties"`
	CreatedAt         time.Time       `json:"created_at"`
	CreatedBy         uuid.UUID       `json:"created_by"`
}

// CreateNamespaceRequest is the body of POST /api/v1/namespaces.
type CreateNamespaceRequest struct {
	ProjectRID        string          `json:"project_rid"`
	Name              string          `json:"name"`
	ParentNamespaceID *uuid.UUID      `json:"parent_namespace_id,omitempty"`
	Properties        json.RawMessage `json:"properties,omitempty"`
}

// UpdateNamespaceRequest mirrors PATCH semantics — `properties` is the
// only mutable field (name/parent_namespace_id are immutable per the
// Iceberg REST spec).
type UpdateNamespaceRequest struct {
	Properties json.RawMessage `json:"properties,omitempty"`
}
