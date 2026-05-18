package models

import (
	"encoding/json"
	"time"
)

// ProductResourceType is the discriminator for bundle-eligible
// product and governance resource kinds. Mirrors proto/marketplace/v1/product.proto:ResourceType.
type ProductResourceType string

const (
	ProductResourceOntologyType              ProductResourceType = "ONTOLOGY_TYPE"
	ProductResourceActionType                ProductResourceType = "ACTION_TYPE"
	ProductResourcePipeline                  ProductResourceType = "PIPELINE"
	ProductResourceApp                       ProductResourceType = "APP"
	ProductResourceRestrictedView            ProductResourceType = "RESTRICTED_VIEW"
	ProductResourceProjectTemplate           ProductResourceType = "PROJECT_TEMPLATE"
	ProductResourceApplicationAccessMetadata ProductResourceType = "APPLICATION_ACCESS_METADATA"
	ProductResourceDashboard                 ProductResourceType = "DASHBOARD"
	ProductResourceGovernanceConfig          ProductResourceType = "GOVERNANCE_CONFIG"
)

// Valid reports whether t is one of the supported resource kinds.
func (t ProductResourceType) Valid() bool {
	switch t {
	case ProductResourceOntologyType, ProductResourceActionType,
		ProductResourcePipeline, ProductResourceApp,
		ProductResourceRestrictedView, ProductResourceProjectTemplate,
		ProductResourceApplicationAccessMetadata, ProductResourceDashboard,
		ProductResourceGovernanceConfig:
		return true
	default:
		return false
	}
}

// ProductStatus mirrors proto:ProductStatus.
type ProductStatus string

const (
	ProductStatusDraft     ProductStatus = "DRAFT"
	ProductStatusPublished ProductStatus = "PUBLISHED"
	ProductStatusArchived  ProductStatus = "ARCHIVED"
)

// InstallationStatus mirrors proto:InstallationStatus.
type InstallationStatus string

const (
	InstallationStatusPending     InstallationStatus = "PENDING"
	InstallationStatusInstalling  InstallationStatus = "INSTALLING"
	InstallationStatusInstalled   InstallationStatus = "INSTALLED"
	InstallationStatusFailed      InstallationStatus = "FAILED"
	InstallationStatusUninstalled InstallationStatus = "UNINSTALLED"
)

// ProductResource is a typed pointer to a resource owned by another
// service. `Ref` is the rid/id understood by that service (UUID string).
type ProductResource struct {
	Type ProductResourceType `json:"type"`
	Ref  string              `json:"ref"`
}

// Product is the bundle definition.
type Product struct {
	RID         string            `json:"rid"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	ManifestURL string            `json:"manifest_url"`
	Signature   string            `json:"signature"`
	Status      ProductStatus     `json:"status"`
	Resources   []ProductResource `json:"resources"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// ProductVersion is the snapshot row. `BundlePath` is the storage key
// (relative to the bundle storage root), not a presigned URL.
type ProductVersion struct {
	RID         string          `json:"rid"`
	ProductRID  string          `json:"product_rid"`
	Version     string          `json:"version"`
	Manifest    json.RawMessage `json:"manifest"`
	BundlePath  string          `json:"bundle_path"`
	Signature   string          `json:"signature"`
	PublishedAt time.Time       `json:"published_at"`
}

// ResourceMapping records that the source `SrcRef` (referenced in the
// bundle manifest) was created in the target workspace as `DstRID`.
type ResourceMapping struct {
	Type   ProductResourceType `json:"type"`
	SrcRef string              `json:"src_ref"`
	DstRID string              `json:"dst_rid"`
}

// Installation records one install attempt.
type Installation struct {
	RID                string             `json:"rid"`
	ProductRID         string             `json:"product_rid"`
	Version            string             `json:"version"`
	TargetWorkspaceRID string             `json:"target_workspace_rid"`
	Status             InstallationStatus `json:"status"`
	ResourceMappings   []ResourceMapping  `json:"resource_mappings"`
	FailureReason      string             `json:"failure_reason,omitempty"`
	InstalledAt        time.Time          `json:"installed_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

// ── Request / response envelopes ───────────────────────────────────────

type CreateProductRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	Resources   []ProductResource `json:"resources"`
}

type PublishProductVersionRequest struct {
	Version string `json:"version"`
}

type InstallProductRequest struct {
	Version            string          `json:"version"`
	TargetWorkspaceRID string          `json:"target_workspace_rid"`
	ParameterMap       json.RawMessage `json:"parameter_map,omitempty"`
}

// ProductManifest is the JSON blob written to manifest.json at the root
// of the tar.gz bundle. It enumerates every resource snapshot included
// in the bundle and records the signature that the install path uses
// to authenticate the payload.
type ProductManifest struct {
	ProductRID    string                 `json:"product_rid"`
	ProductName   string                 `json:"product_name"`
	Version       string                 `json:"version"`
	Author        string                 `json:"author"`
	Description   string                 `json:"description"`
	SignAlgorithm string                 `json:"sign_algorithm"`
	SignedAt      time.Time              `json:"signed_at"`
	Resources     []ProductManifestEntry `json:"resources"`
}

// ProductManifestEntry pairs the bundle-relative path of a resource
// snapshot file with its src ref so the install path can re-create the
// resource on the receiving service.
type ProductManifestEntry struct {
	Type ProductResourceType `json:"type"`
	Ref  string              `json:"ref"`
	Path string              `json:"path"`
}
