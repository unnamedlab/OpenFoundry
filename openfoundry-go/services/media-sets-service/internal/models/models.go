// Package models holds wire types for media-sets-service.
//
// Foundation slice scope: media_sets only. media_items, branches,
// transactions, retention, item_markings, outbox, access_patterns,
// dicom_schema all land in follow-up slices (~10k LOC of Rust).
package models

import (
	"time"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// MediaSet mirrors `media_sets` rows. RID-addressed, schema-locked.
type MediaSet struct {
	RID                string    `json:"rid"`
	ProjectRID         string    `json:"project_rid"`
	Name               string    `json:"name"`
	Schema             string    `json:"schema"`
	AllowedMimeTypes   []string  `json:"allowed_mime_types"`
	TransactionPolicy  string    `json:"transaction_policy"`
	RetentionSeconds   int64     `json:"retention_seconds"`
	Virtual            bool      `json:"virtual"`
	SourceRID          *string   `json:"source_rid"`
	Markings           []string  `json:"markings"`
	CreatedAt          time.Time `json:"created_at"`
	CreatedBy          string    `json:"created_by"`
}

// CreateMediaSetRequest is POST /api/v1/media-sets.
type CreateMediaSetRequest struct {
	ProjectRID        string   `json:"project_rid"`
	Name              string   `json:"name"`
	Schema            string   `json:"schema"`
	AllowedMimeTypes  []string `json:"allowed_mime_types,omitempty"`
	TransactionPolicy *string  `json:"transaction_policy,omitempty"`
	RetentionSeconds  *int64   `json:"retention_seconds,omitempty"`
	Virtual           *bool    `json:"virtual,omitempty"`
	SourceRID         *string  `json:"source_rid,omitempty"`
	Markings          []string `json:"markings,omitempty"`
}

// UpdateMediaSetRequest mirrors PATCH semantics.
type UpdateMediaSetRequest struct {
	AllowedMimeTypes []string `json:"allowed_mime_types,omitempty"`
	RetentionSeconds *int64   `json:"retention_seconds,omitempty"`
	Markings         []string `json:"markings,omitempty"`
}
