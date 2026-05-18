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
	GroupID      *uuid.UUID   `json:"group_id"`
	DisplayOrder int          `json:"display_order"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// CreateFavoriteRequest is the body of POST /workspace/favorites.
type CreateFavoriteRequest struct {
	ResourceKind string     `json:"resource_kind"`
	ResourceID   uuid.UUID  `json:"resource_id"`
	GroupID      *uuid.UUID `json:"group_id"`
	DisplayOrder *int       `json:"display_order"`
}

// FavoriteGroup is a per-user profile grouping used to render favorites
// consistently across devices.
type FavoriteGroup struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	Name         string    `json:"name"`
	DisplayOrder int       `json:"display_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateFavoriteGroupRequest creates or reuses a named favorite group.
type CreateFavoriteGroupRequest struct {
	Name         string `json:"name"`
	DisplayOrder *int   `json:"display_order"`
}

// FavoriteOrderItem updates one favorite's visible group and ordering slot.
type FavoriteOrderItem struct {
	ResourceKind string     `json:"resource_kind"`
	ResourceID   uuid.UUID  `json:"resource_id"`
	GroupID      *uuid.UUID `json:"group_id"`
	DisplayOrder int        `json:"display_order"`
}

// UpdateFavoriteOrderRequest is the body for PUT /workspace/favorites/order.
type UpdateFavoriteOrderRequest struct {
	Items []FavoriteOrderItem `json:"items"`
}

// FavoriteGroupOrderItem updates one favorite group's display slot.
type FavoriteGroupOrderItem struct {
	ID           uuid.UUID `json:"id"`
	DisplayOrder int       `json:"display_order"`
}

// UpdateFavoriteGroupsOrderRequest is the body for
// PUT /workspace/favorites/groups/order.
type UpdateFavoriteGroupsOrderRequest struct {
	Groups []FavoriteGroupOrderItem `json:"groups"`
}

// ListFavoritesResponse pins the {data: [...]} envelope used by the
// Rust impl (matches streaming-monitor and other workspace APIs).
type ListFavoritesResponse struct {
	Data   []UserFavorite  `json:"data"`
	Groups []FavoriteGroup `json:"groups"`
}

// ListFavoriteGroupsResponse is the {data: [...]} envelope for favorite
// groups, matching the broader workspace API style.
type ListFavoriteGroupsResponse struct {
	Data []FavoriteGroup `json:"data"`
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
