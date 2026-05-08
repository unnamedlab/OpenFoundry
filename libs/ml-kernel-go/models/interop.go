package models

import (
	"encoding/json"
	"strings"
)

// ExternalTrackingSource captures references to an external ML
// tracking system (MLflow, Weights & Biases, …).
type ExternalTrackingSource struct {
	System                  string              `json:"system,omitempty"`
	Project                 string              `json:"project,omitempty"`
	ExperimentName          string              `json:"experiment_name,omitempty"`
	RunID                   string              `json:"run_id,omitempty"`
	RunName                 string              `json:"run_name,omitempty"`
	RunURI                  string              `json:"run_uri,omitempty"`
	ArtifactURI             string              `json:"artifact_uri,omitempty"`
	ModelURI                string              `json:"model_uri,omitempty"`
	RegisteredModelName     string              `json:"registered_model_name,omitempty"`
	RegisteredModelVersion  string              `json:"registered_model_version,omitempty"`
	Framework               string              `json:"framework,omitempty"`
	Flavor                  string              `json:"flavor,omitempty"`
	Stage                   string              `json:"stage,omitempty"`
	Tags                    json.RawMessage     `json:"tags,omitempty"`
	Params                  json.RawMessage     `json:"params,omitempty"`
	Metrics                 []MetricValue       `json:"metrics,omitempty"`
	Artifacts               []ArtifactReference `json:"artifacts,omitempty"`
	Metadata                json.RawMessage     `json:"metadata,omitempty"`
}

// HasSignal returns true when at least one field carries data.
// Mirrors the Rust impl ExternalTrackingSource::has_signal.
func (s *ExternalTrackingSource) HasSignal() bool {
	return strings.TrimSpace(s.System) != "" ||
		strings.TrimSpace(s.Project) != "" ||
		strings.TrimSpace(s.ExperimentName) != "" ||
		strings.TrimSpace(s.RunID) != "" ||
		strings.TrimSpace(s.RunName) != "" ||
		strings.TrimSpace(s.RunURI) != "" ||
		strings.TrimSpace(s.ArtifactURI) != "" ||
		strings.TrimSpace(s.ModelURI) != "" ||
		strings.TrimSpace(s.RegisteredModelName) != "" ||
		strings.TrimSpace(s.RegisteredModelVersion) != "" ||
		strings.TrimSpace(s.Framework) != "" ||
		strings.TrimSpace(s.Flavor) != "" ||
		strings.TrimSpace(s.Stage) != "" ||
		len(s.Metrics) > 0 ||
		len(s.Artifacts) > 0 ||
		!isJSONNull(s.Params) ||
		!isJSONNull(s.Tags) ||
		!isJSONNull(s.Metadata)
}

// ModelAdapterDescriptor describes a custom model adapter (loader).
type ModelAdapterDescriptor struct {
	Kind            string          `json:"kind,omitempty"`
	Framework       string          `json:"framework,omitempty"`
	Flavor          string          `json:"flavor,omitempty"`
	Runtime         string          `json:"runtime,omitempty"`
	Loader          string          `json:"loader,omitempty"`
	ArtifactURI     string          `json:"artifact_uri,omitempty"`
	Entrypoint      string          `json:"entrypoint,omitempty"`
	RequirementsURI string          `json:"requirements_uri,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

// HasSignal mirrors the Rust impl.
func (d *ModelAdapterDescriptor) HasSignal() bool {
	return strings.TrimSpace(d.Kind) != "" ||
		strings.TrimSpace(d.Framework) != "" ||
		strings.TrimSpace(d.Flavor) != "" ||
		strings.TrimSpace(d.Runtime) != "" ||
		strings.TrimSpace(d.Loader) != "" ||
		strings.TrimSpace(d.ArtifactURI) != "" ||
		strings.TrimSpace(d.Entrypoint) != "" ||
		strings.TrimSpace(d.RequirementsURI) != "" ||
		!isJSONNull(d.Metadata)
}

// RegistrySourceDescriptor points at a registry-system reference.
type RegistrySourceDescriptor struct {
	System       string          `json:"system,omitempty"`
	ModelName    string          `json:"model_name,omitempty"`
	ModelVersion string          `json:"model_version,omitempty"`
	Stage        string          `json:"stage,omitempty"`
	URI          string          `json:"uri,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// HasSignal mirrors the Rust impl.
func (r *RegistrySourceDescriptor) HasSignal() bool {
	return strings.TrimSpace(r.System) != "" ||
		strings.TrimSpace(r.ModelName) != "" ||
		strings.TrimSpace(r.ModelVersion) != "" ||
		strings.TrimSpace(r.Stage) != "" ||
		strings.TrimSpace(r.URI) != "" ||
		!isJSONNull(r.Metadata)
}

// isJSONNull treats nil/empty/literal "null" raw messages as null —
// mirrors Rust serde_json::Value::is_null() for the omitempty inputs.
func isJSONNull(b json.RawMessage) bool {
	if len(b) == 0 {
		return true
	}
	s := strings.TrimSpace(string(b))
	return s == "" || s == "null"
}
