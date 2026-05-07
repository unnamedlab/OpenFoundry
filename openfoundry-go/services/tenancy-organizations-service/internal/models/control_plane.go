package models

import "github.com/google/uuid"

// IdentityProviderOrganizationRule mirrors the Rust struct of the same
// name: a control-plane mapping that pins a JWT identity-provider claim
// to an organization + workspace + role bundle.
type IdentityProviderOrganizationRule struct {
	Name           string    `json:"name"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Workspace      *string   `json:"workspace"`
	Roles          []string  `json:"roles"`
	TenantTier     *string   `json:"tenant_tier"`
}

// IdentityProviderMapping mirrors Rust `IdentityProviderMapping`.
// Stored as JSONB in `control_panel_settings.identity_provider_mappings`.
type IdentityProviderMapping struct {
	ProviderSlug          string                             `json:"provider_slug"`
	DefaultOrganizationID *uuid.UUID                         `json:"default_organization_id"`
	DefaultWorkspace      *string                            `json:"default_workspace"`
	DefaultRoles          []string                           `json:"default_roles"`
	AllowedEmailDomains   []string                           `json:"allowed_email_domains"`
	OrganizationRules     []IdentityProviderOrganizationRule `json:"organization_rules"`
}

// ResourceQuotaSettings mirrors Rust `ResourceQuotaSettings`. Numeric
// types match the Go `authmw.TenantQuotaPolicy` widths so quotas flow
// across packages without lossy casts. JSON shape is byte-exact with
// the Rust serde repr.
type ResourceQuotaSettings struct {
	MaxQueryLimit              uint32 `json:"max_query_limit"`
	MaxDistributedQueryWorkers uint32 `json:"max_distributed_query_workers"`
	MaxPipelineWorkers         uint32 `json:"max_pipeline_workers"`
	MaxRequestBodyBytes        uint64 `json:"max_request_body_bytes"`
	RequestsPerMinute          uint32 `json:"requests_per_minute"`
	MaxStorageGB               uint32 `json:"max_storage_gb"`
	MaxSharedSpaces            uint32 `json:"max_shared_spaces"`
	MaxGuestSessions           uint32 `json:"max_guest_sessions"`
}

// ResourceManagementPolicy mirrors Rust `ResourceManagementPolicy`.
// Stored as JSONB in `control_panel_settings.resource_management_policies`.
type ResourceManagementPolicy struct {
	Name                string                `json:"name"`
	TenantTier          string                `json:"tenant_tier"`
	AppliesToOrgIDs     []uuid.UUID           `json:"applies_to_org_ids"`
	AppliesToWorkspaces []string              `json:"applies_to_workspaces"`
	Quota               ResourceQuotaSettings `json:"quota"`
}
