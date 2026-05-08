// Package models ports services/sql-bi-gateway-service/src/models.rs:
// persistent models for the saved-queries side router.
package models

import (
	"time"

	"github.com/google/uuid"
)

// SavedQuery is a single saved-query row in the gateway's CNPG cluster.
// JSON shape mirrors the Rust struct exactly so existing BI dashboards
// see no change.
type SavedQuery struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SQL         string    `json:"sql"`
	OwnerID     uuid.UUID `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateSavedQueryRequest is the JSON body for `POST /api/v1/queries/saved`.
// `sql` may be omitted when `?seed_dataset_rid=` is set on the query
// string — the handler then auto-fills with `SELECT * FROM <rid>` to
// match Foundry's "Open in SQL workbench" path.
type CreateSavedQueryRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	SQL         *string `json:"sql,omitempty"`
}

// CreateSavedQueryParams is the query string for
// `POST /api/v1/queries/saved`. Setting `seed_dataset_rid` is the
// "Open in SQL workbench" path: when the body's `sql` is empty the
// handler auto-fills it with `SELECT * FROM <dataset>` so the user
// lands on a runnable query.
type CreateSavedQueryParams struct {
	SeedDatasetRID string
}

// ListQueriesQuery is the query string for `GET /api/v1/queries/saved`.
type ListQueriesQuery struct {
	Page    *int64
	PerPage *int64
	Search  *string
}
