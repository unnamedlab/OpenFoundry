package workspace

import (
	"time"

	"github.com/google/uuid"
)

// UserFavorite mirrors the Rust struct — composite primary key on
// (user_id, resource_kind, resource_id).
type UserFavorite struct {
	UserID       uuid.UUID    `json:"user_id"`
	ResourceKind ResourceKind `json:"resource_kind"`
	ResourceID   uuid.UUID    `json:"resource_id"`
	CreatedAt    time.Time    `json:"created_at"`
}

// CreateFavoriteRequest is the body of POST /workspace/favorites.
type CreateFavoriteRequest struct {
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
}

// ListFavoritesResponse pins the {data: [...]} envelope used by the
// Rust impl (matches streaming-monitor and other workspace APIs).
type ListFavoritesResponse struct {
	Data []UserFavorite `json:"data"`
}

// RecentEntry mirrors the dedup'd (kind, id, last_accessed_at) row
// returned by GET /workspace/recents.
type RecentEntry struct {
	ResourceKind   ResourceKind `json:"resource_kind"`
	ResourceID     uuid.UUID    `json:"resource_id"`
	LastAccessedAt time.Time    `json:"last_accessed_at"`
}

// RecordAccessRequest is the body of POST /workspace/recents.
type RecordAccessRequest struct {
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
}

// ListRecentsResponse is the {data: [...]} envelope.
type ListRecentsResponse struct {
	Data []RecentEntry `json:"data"`
}
