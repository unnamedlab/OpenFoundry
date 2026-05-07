package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PackageType is the marketplace listing kind discriminator. Values mirror
// Rust marketplace::models::package::PackageType and the package_kind column.
type PackageType string

const (
	PackageTypeConnector   PackageType = "connector"
	PackageTypeTransform   PackageType = "transform"
	PackageTypeWidget      PackageType = "widget"
	PackageTypeAppTemplate PackageType = "app_template"
	PackageTypeMLModel     PackageType = "ml_model"
	PackageTypeAIAgent     PackageType = "ai_agent"
	PackageTypeMediaSet    PackageType = "media_set"
)

func (p PackageType) Valid() bool {
	switch p {
	case PackageTypeConnector, PackageTypeTransform, PackageTypeWidget, PackageTypeAppTemplate, PackageTypeMLModel, PackageTypeAIAgent, PackageTypeMediaSet:
		return true
	default:
		return false
	}
}

// ListingDefinition is the public marketplace listing JSON shape.
type ListingDefinition struct {
	ID             uuid.UUID   `json:"id"`
	Name           string      `json:"name"`
	Slug           string      `json:"slug"`
	Summary        string      `json:"summary"`
	Description    string      `json:"description"`
	Publisher      string      `json:"publisher"`
	CategorySlug   string      `json:"category_slug"`
	PackageKind    PackageType `json:"package_kind"`
	RepositorySlug string      `json:"repository_slug"`
	Visibility     string      `json:"visibility"`
	Tags           []string    `json:"tags"`
	Capabilities   []string    `json:"capabilities"`
	InstallCount   int64       `json:"install_count"`
	AverageRating  float64     `json:"average_rating"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

type CreateListingRequest struct {
	Name           string      `json:"name"`
	Slug           string      `json:"slug"`
	Summary        string      `json:"summary"`
	Description    string      `json:"description"`
	Publisher      string      `json:"publisher"`
	CategorySlug   string      `json:"category_slug"`
	PackageKind    PackageType `json:"package_kind"`
	RepositorySlug string      `json:"repository_slug"`
	Visibility     string      `json:"visibility"`
	Tags           []string    `json:"tags"`
	Capabilities   []string    `json:"capabilities"`
}

type UpdateListingRequest struct {
	Name           *string   `json:"name"`
	Summary        *string   `json:"summary"`
	Description    *string   `json:"description"`
	CategorySlug   *string   `json:"category_slug"`
	RepositorySlug *string   `json:"repository_slug"`
	Visibility     *string   `json:"visibility"`
	Tags           *[]string `json:"tags"`
	Capabilities   *[]string `json:"capabilities"`
}

// PackageVersion mirrors the package version payload returned in listing detail
// and by publish-version.
type PackageVersion struct {
	ID                uuid.UUID       `json:"id"`
	ListingID         uuid.UUID       `json:"listing_id"`
	Version           string          `json:"version"`
	ReleaseChannel    string          `json:"release_channel"`
	Changelog         string          `json:"changelog"`
	DependencyMode    string          `json:"dependency_mode"`
	Dependencies      json.RawMessage `json:"dependencies"`
	PackagedResources json.RawMessage `json:"packaged_resources"`
	Manifest          json.RawMessage `json:"manifest"`
	PublishedAt       time.Time       `json:"published_at"`
}

type PublishVersionRequest struct {
	Version           string          `json:"version"`
	ReleaseChannel    string          `json:"release_channel"`
	Changelog         string          `json:"changelog"`
	DependencyMode    string          `json:"dependency_mode"`
	Dependencies      json.RawMessage `json:"dependencies"`
	PackagedResources json.RawMessage `json:"packaged_resources"`
	Manifest          json.RawMessage `json:"manifest"`
}

// DependencyRequirement describes a marketplace package dependency and the
// version range required by the package version being installed.
type DependencyRequirement struct {
	PackageSlug string `json:"package_slug"`
	VersionReq  string `json:"version_req"`
	Required    bool   `json:"required"`
}

// DependencyPlanResponse is returned by dependency preview and install
// planning. Conflicts are blocking for create-install.
type DependencyPlanResponse struct {
	ListingID      uuid.UUID               `json:"listing_id"`
	ListingSlug    string                  `json:"listing_slug"`
	Version        string                  `json:"version"`
	ReleaseChannel string                  `json:"release_channel"`
	WorkspaceName  string                  `json:"workspace_name"`
	Items          []DependencyRequirement `json:"items"`
	Conflicts      []DependencyConflict    `json:"conflicts"`
}

// DependencyConflict captures an already-installed package that does not
// satisfy a requested dependency version constraint.
type DependencyConflict struct {
	PackageSlug      string `json:"package_slug"`
	VersionReq       string `json:"version_req"`
	InstalledVersion string `json:"installed_version"`
	Message          string `json:"message"`
}

// InstallActivation records the service-side activation outcome. Go currently
// mirrors the Rust default marketplace-record activation while retaining the
// JSON shape for future activation hooks.
type InstallActivation struct {
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	ResourceID   *uuid.UUID `json:"resource_id"`
	ResourceSlug *string    `json:"resource_slug"`
	PublicURL    *string    `json:"public_url"`
	Notes        *string    `json:"notes"`
}

// InstallRecord is the persisted marketplace install shape.
type InstallRecord struct {
	ID                 uuid.UUID               `json:"id"`
	ListingID          uuid.UUID               `json:"listing_id"`
	ListingName        string                  `json:"listing_name"`
	Version            string                  `json:"version"`
	ReleaseChannel     string                  `json:"release_channel"`
	WorkspaceName      string                  `json:"workspace_name"`
	Status             string                  `json:"status"`
	DependencyPlan     []DependencyRequirement `json:"dependency_plan"`
	Activation         InstallActivation       `json:"activation"`
	FleetID            *uuid.UUID              `json:"fleet_id"`
	FleetName          *string                 `json:"fleet_name"`
	AutoUpgradeEnabled bool                    `json:"auto_upgrade_enabled"`
	MaintenanceWindow  json.RawMessage         `json:"maintenance_window,omitempty"`
	EnrollmentBranch   *string                 `json:"enrollment_branch"`
	InstalledAt        time.Time               `json:"installed_at"`
	ReadyAt            *time.Time              `json:"ready_at"`
}

type CreateInstallRequest struct {
	ListingID        uuid.UUID  `json:"listing_id"`
	Version          string     `json:"version"`
	WorkspaceName    string     `json:"workspace_name"`
	ReleaseChannel   string     `json:"release_channel"`
	FleetID          *uuid.UUID `json:"fleet_id"`
	EnrollmentBranch *string    `json:"enrollment_branch"`
}

type DependencyPlanRequest struct {
	ListingID      uuid.UUID `json:"listing_id"`
	Version        string    `json:"version"`
	WorkspaceName  string    `json:"workspace_name"`
	ReleaseChannel string    `json:"release_channel"`
}

type ListingDetail struct {
	Listing       ListingDefinition `json:"listing"`
	LatestVersion *PackageVersion   `json:"latest_version"`
	Versions      []PackageVersion  `json:"versions"`
	Reviews       []ListingReview   `json:"reviews"`
}

type ListingReview struct {
	ID          uuid.UUID `json:"id"`
	ListingID   uuid.UUID `json:"listing_id"`
	Author      string    `json:"author"`
	Rating      int       `json:"rating"`
	Headline    string    `json:"headline"`
	Body        string    `json:"body"`
	Recommended bool      `json:"recommended"`
	CreatedAt   time.Time `json:"created_at"`
}

type Pagination struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

type PaginatedListResponse[T any] struct {
	Items      []T        `json:"items"`
	Pagination Pagination `json:"pagination"`
}
