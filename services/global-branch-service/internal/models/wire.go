// Package models holds the HTTP wire shapes for global-branch-service.
//
// Kept separate from internal/domain so the domain types can stay free
// of JSON tags and HTTP-specific defaulting. Handlers translate
// between the two.
package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/domain"
)

// CreateBranchRequest is the POST /api/v1/global-branches body.
//
// TenantID is omitted on the wire — handlers fill it from the
// caller's JWT (claims.OrgID). Surfacing it as an input would let a
// caller forge cross-tenant rows.
type CreateBranchRequest struct {
	Name        string `json:"name"`
	BaseRef     string `json:"base_ref"`
	Description string `json:"description,omitempty"`
}

// UpdateBranchRequest is the PATCH body. Pointer fields signal "leave
// unchanged"; an empty-string pointer for Description is allowed and
// means "clear the description".
type UpdateBranchRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// MergeBranchRequest is the optional POST .../merge body. Empty body
// is allowed — Strategy defaults to "coordinated", the only mode
// Milestone A supports.
type MergeBranchRequest struct {
	Strategy string `json:"strategy,omitempty"`
}

// AddParticipantRequest is the POST .../participants body.
type AddParticipantRequest struct {
	ServiceName    string `json:"service_name"`
	LocalBranchRef string `json:"local_branch_ref"`
}

// BranchResponse is the on-wire representation of a global branch.
// Mirrors domain.GlobalBranch but always renders an empty (non-null)
// participating_services slice so frontend consumers can map over it
// unconditionally.
type BranchResponse struct {
	ID                    uuid.UUID           `json:"id"`
	TenantID              uuid.UUID           `json:"tenant_id"`
	Name                  string              `json:"name"`
	BaseRef               string              `json:"base_ref"`
	Status                domain.BranchStatus `json:"status"`
	Description           string              `json:"description"`
	CreatedBy             uuid.UUID           `json:"created_by"`
	CreatedAt             time.Time           `json:"created_at"`
	MergedAt              *time.Time          `json:"merged_at,omitempty"`
	MergedBy              *uuid.UUID          `json:"merged_by,omitempty"`
	ParticipatingServices []string            `json:"participating_services"`
}

// FromDomain projects a domain branch into the wire shape, attaching
// the participating-services list (extracted from the participation
// table by the handler).
func FromDomain(b *domain.GlobalBranch, services []string) BranchResponse {
	if services == nil {
		services = []string{}
	}
	return BranchResponse{
		ID:                    b.ID,
		TenantID:              b.TenantID,
		Name:                  b.Name,
		BaseRef:               b.BaseRef,
		Status:                b.Status,
		Description:           b.Description,
		CreatedBy:             b.CreatedBy,
		CreatedAt:             b.CreatedAt,
		MergedAt:              b.MergedAt,
		MergedBy:              b.MergedBy,
		ParticipatingServices: services,
	}
}

// ListResponse is the standard envelope for index endpoints.
type ListResponse[T any] struct {
	Items []T `json:"items"`
}
