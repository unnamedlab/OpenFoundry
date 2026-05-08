package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// FeatureSample is a single (entity_key, value) observation.
type FeatureSample struct {
	EntityKey  string          `json:"entity_key"`
	Value      json.RawMessage `json:"value"`
	ObservedAt *time.Time      `json:"observed_at"`
}

// FeatureDefinition is the catalog row.
type FeatureDefinition struct {
	ID                   uuid.UUID       `json:"id"`
	Name                 string          `json:"name"`
	EntityName           string          `json:"entity_name"`
	DataType             string          `json:"data_type"`
	Description          string          `json:"description"`
	Status               string          `json:"status"`
	OfflineSource        string          `json:"offline_source"`
	Transformation       string          `json:"transformation"`
	OnlineEnabled        bool            `json:"online_enabled"`
	OnlineNamespace      string          `json:"online_namespace"`
	BatchSchedule        string          `json:"batch_schedule"`
	FreshnessSLAMinutes  int32           `json:"freshness_sla_minutes"`
	Tags                 []string        `json:"tags"`
	Samples              []FeatureSample `json:"samples"`
	LastMaterializedAt   *time.Time      `json:"last_materialized_at"`
	LastOnlineSyncAt     *time.Time      `json:"last_online_sync_at"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type ListFeaturesResponse struct {
	Data []FeatureDefinition `json:"data"`
}

// CreateFeatureRequest defaults: batch_schedule="0 * * * *",
// freshness_sla_minutes=60.
type CreateFeatureRequest struct {
	Name                string          `json:"name"`
	EntityName          string          `json:"entity_name"`
	DataType            string          `json:"data_type"`
	Description         string          `json:"description,omitempty"`
	OfflineSource       string          `json:"offline_source,omitempty"`
	Transformation      string          `json:"transformation,omitempty"`
	OnlineEnabled       bool            `json:"online_enabled,omitempty"`
	OnlineNamespace     string          `json:"online_namespace,omitempty"`
	BatchSchedule       string          `json:"batch_schedule,omitempty"`
	FreshnessSLAMinutes int32           `json:"freshness_sla_minutes,omitempty"`
	Tags                []string        `json:"tags,omitempty"`
	Samples             []FeatureSample `json:"samples,omitempty"`
}

type UpdateFeatureRequest struct {
	Name                *string   `json:"name,omitempty"`
	EntityName          *string   `json:"entity_name,omitempty"`
	DataType            *string   `json:"data_type,omitempty"`
	Description         *string   `json:"description,omitempty"`
	Status              *string   `json:"status,omitempty"`
	OfflineSource       *string   `json:"offline_source,omitempty"`
	Transformation      *string   `json:"transformation,omitempty"`
	OnlineEnabled       *bool     `json:"online_enabled,omitempty"`
	OnlineNamespace     *string   `json:"online_namespace,omitempty"`
	BatchSchedule       *string   `json:"batch_schedule,omitempty"`
	FreshnessSLAMinutes *int32    `json:"freshness_sla_minutes,omitempty"`
	Tags                *[]string `json:"tags,omitempty"`
}

type MaterializeFeatureRequest struct {
	Samples []FeatureSample `json:"samples,omitempty"`
	Mode    *string         `json:"mode,omitempty"`
}

type OnlineFeatureSnapshot struct {
	FeatureID uuid.UUID       `json:"feature_id"`
	Namespace string          `json:"namespace"`
	Source    string          `json:"source"`
	Values    []FeatureSample `json:"values"`
	FetchedAt time.Time       `json:"fetched_at"`
}

const (
	DefaultBatchSchedule       = "0 * * * *"
	DefaultFreshnessSLAMinutes int32 = 60
)
