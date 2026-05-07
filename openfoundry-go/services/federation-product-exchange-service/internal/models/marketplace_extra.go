package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type CategoryDefinition struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	ListingCount int    `json:"listing_count"`
}

type MarketplaceOverview struct {
	ListingCount  int                 `json:"listing_count"`
	CategoryCount int                 `json:"category_count"`
	Featured      []ListingDefinition `json:"featured"`
	TotalInstalls int64               `json:"total_installs"`
}

type SearchResult struct {
	Listing ListingDefinition `json:"listing"`
	Score   float64           `json:"score"`
}

type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

type ActionTypeDependencies struct {
	ObjectTypeIDs      []uuid.UUID `json:"object_type_ids"`
	FunctionPackageIDs []uuid.UUID `json:"function_package_ids"`
	Webhooks           []uuid.UUID `json:"webhooks"`
}

type IncludeActionRequest struct {
	VersionID    *uuid.UUID             `json:"version_id"`
	ActionType   json.RawMessage        `json:"action_type"`
	Dependencies ActionTypeDependencies `json:"dependencies"`
}

type DatasetProductManifest struct {
	Entity          string                  `json:"entity"`
	Version         string                  `json:"version"`
	Schema          json.RawMessage         `json:"schema,omitempty"`
	Retention       json.RawMessage         `json:"retention"`
	BranchingPolicy json.RawMessage         `json:"branching_policy,omitempty"`
	Schedules       []string                `json:"schedules"`
	Bootstrap       DatasetProductBootstrap `json:"bootstrap"`
}

type DatasetProductBootstrap struct {
	Mode string `json:"mode"`
}

type DatasetProduct struct {
	ID                 uuid.UUID              `json:"id"`
	Name               string                 `json:"name"`
	SourceDatasetRID   string                 `json:"source_dataset_rid"`
	EntityType         string                 `json:"entity_type"`
	Version            string                 `json:"version"`
	ProjectID          *uuid.UUID             `json:"project_id"`
	PublishedBy        *uuid.UUID             `json:"published_by"`
	ExportIncludesData bool                   `json:"export_includes_data"`
	IncludeSchema      bool                   `json:"include_schema"`
	IncludeBranches    bool                   `json:"include_branches"`
	IncludeRetention   bool                   `json:"include_retention"`
	IncludeSchedules   bool                   `json:"include_schedules"`
	Manifest           DatasetProductManifest `json:"manifest"`
	BootstrapMode      string                 `json:"bootstrap_mode"`
	PublishedAt        time.Time              `json:"published_at"`
	CreatedAt          time.Time              `json:"created_at"`
}

type CreateDatasetProductRequest struct {
	Name               string          `json:"name"`
	Version            string          `json:"version"`
	ProjectID          *uuid.UUID      `json:"project_id"`
	PublishedBy        *uuid.UUID      `json:"published_by"`
	ExportIncludesData bool            `json:"export_includes_data"`
	IncludeSchema      bool            `json:"include_schema"`
	IncludeBranches    bool            `json:"include_branches"`
	IncludeRetention   bool            `json:"include_retention"`
	IncludeSchedules   bool            `json:"include_schedules"`
	BootstrapMode      string          `json:"bootstrap_mode"`
	Schema             json.RawMessage `json:"schema"`
	Retention          json.RawMessage `json:"retention"`
	BranchingPolicy    json.RawMessage `json:"branching_policy"`
	Schedules          []string        `json:"schedules"`
}

type InstallDatasetProductRequest struct {
	TargetProjectID  uuid.UUID  `json:"target_project_id"`
	TargetDatasetRID string     `json:"target_dataset_rid"`
	BootstrapMode    *string    `json:"bootstrap_mode"`
	InstalledBy      *uuid.UUID `json:"installed_by"`
}

type DatasetProductInstall struct {
	ID               uuid.UUID       `json:"id"`
	ProductID        uuid.UUID       `json:"product_id"`
	TargetProjectID  uuid.UUID       `json:"target_project_id"`
	TargetDatasetRID string          `json:"target_dataset_rid"`
	BootstrapMode    string          `json:"bootstrap_mode"`
	Status           string          `json:"status"`
	Details          json.RawMessage `json:"details"`
	InstalledBy      *uuid.UUID      `json:"installed_by"`
	CreatedAt        time.Time       `json:"created_at"`
	CompletedAt      *time.Time      `json:"completed_at"`
}

type ScheduleDefaults struct {
	TimeZone         *string `json:"time_zone"`
	TimezoneOverride *string `json:"timezone_override"`
	ForceBuild       *bool   `json:"force_build"`
}

type ScheduleManifest struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Trigger     json.RawMessage  `json:"trigger"`
	Target      json.RawMessage  `json:"target"`
	ScopeKind   string           `json:"scope_kind"`
	Defaults    ScheduleDefaults `json:"defaults"`
}

type AddScheduleManifestRequest struct {
	ProductVersionID uuid.UUID        `json:"product_version_id"`
	Manifest         ScheduleManifest `json:"manifest"`
}

type AddScheduleManifestResponse struct {
	ID               uuid.UUID `json:"id"`
	ProductVersionID uuid.UUID `json:"product_version_id"`
	Name             string    `json:"name"`
}

type RidMapping struct {
	Pipeline map[string]string `json:"pipeline"`
	Dataset  map[string]string `json:"dataset"`
}

type InstallSchedulesRequest struct {
	ProductVersionID  uuid.UUID  `json:"product_version_id"`
	RidMapping        RidMapping `json:"rid_mapping"`
	ActivateManifests []string   `json:"activate_manifests"`
}

type MaterialisedSchedule struct {
	Name      string           `json:"name"`
	Trigger   json.RawMessage  `json:"trigger"`
	Target    json.RawMessage  `json:"target"`
	ScopeKind string           `json:"scope_kind"`
	Defaults  ScheduleDefaults `json:"defaults"`
}

type InstallSchedulesResponse struct {
	ProductVersionID uuid.UUID              `json:"product_version_id"`
	Materialised     []MaterialisedSchedule `json:"materialised"`
}

func (r SearchResult) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{r.Listing, r.Score})
}

func (r *SearchResult) UnmarshalJSON(data []byte) error {
	var tuple []json.RawMessage
	if err := json.Unmarshal(data, &tuple); err == nil && len(tuple) == 2 {
		if err := json.Unmarshal(tuple[0], &r.Listing); err != nil {
			return err
		}
		return json.Unmarshal(tuple[1], &r.Score)
	}
	var object struct {
		Listing ListingDefinition `json:"listing"`
		Score   float64           `json:"score"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	r.Listing = object.Listing
	r.Score = object.Score
	return nil
}
