package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DefaultFunctionPackageVersion mirrors `pub const
// DEFAULT_FUNCTION_PACKAGE_VERSION` in
// `libs/ontology-kernel/src/models/function_package.rs`.
const DefaultFunctionPackageVersion = "0.1.0"

// FunctionPackageDefaultVersion mirrors `default_function_package_version()`.
func FunctionPackageDefaultVersion() string { return DefaultFunctionPackageVersion }

// ParseFunctionPackageVersion mirrors
// `parse_function_package_version`. Trims leading/trailing whitespace
// then validates the value as semver `MAJOR.MINOR.PATCH(-pre)?(+meta)?`.
// Errors carry the verbatim Rust prefix `function package version
// must be valid semver: ...` so wire-compat callers see the same
// message body.
func ParseFunctionPackageVersion(version string) (string, error) {
	v := strings.TrimSpace(version)
	if !semverShallowOK(v) {
		return "", fmt.Errorf("function package version must be valid semver: invalid: %s", version)
	}
	return v, nil
}

// semverShallowOK is a deliberately small port of `semver::Version::parse`.
// It accepts `MAJOR.MINOR.PATCH` with optional `-prerelease` and `+build`
// suffixes; identifiers are restricted to `[0-9A-Za-z-]+` and dot-separated.
// The Rust crate is more permissive in error messages than in shape, so
// matching the shape is enough for wire-compat tests.
func semverShallowOK(s string) bool {
	if s == "" {
		return false
	}
	core := s
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		core = s[:i]
		// rest must be valid identifiers separated by `.`, `-`, `+`.
		rest := s[i:]
		for _, c := range rest {
			switch {
			case c >= '0' && c <= '9':
			case c >= 'a' && c <= 'z':
			case c >= 'A' && c <= 'Z':
			case c == '-' || c == '+' || c == '.':
			default:
				return false
			}
		}
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// FunctionCapabilities mirrors `struct FunctionCapabilities`.
//
// Note the divergence between the per-field `#[serde(default)]` (which
// applies on JSON deserialisation) and the `impl Default` (which is
// invoked by `unwrap_or_default` in the row→package conversion):
//
//   * deserialise: allow_ontology_read defaults to `true`, the other
//     three booleans default to `false` (Rust bool default).
//   * `impl Default`: all four booleans are `true`.
//
// The two paths are exposed via [DefaultFunctionCapabilities] and the
// custom [FunctionCapabilities.UnmarshalJSON] respectively.
type FunctionCapabilities struct {
	AllowOntologyRead  bool   `json:"allow_ontology_read"`
	AllowOntologyWrite bool   `json:"allow_ontology_write"`
	AllowAI            bool   `json:"allow_ai"`
	AllowNetwork       bool   `json:"allow_network"`
	TimeoutSeconds     uint64 `json:"timeout_seconds"`
	MaxSourceBytes     uint64 `json:"max_source_bytes"`
}

// DefaultFunctionCapabilities mirrors `impl Default for FunctionCapabilities`.
func DefaultFunctionCapabilities() FunctionCapabilities {
	return FunctionCapabilities{
		AllowOntologyRead:  true,
		AllowOntologyWrite: true,
		AllowAI:            true,
		AllowNetwork:       true,
		TimeoutSeconds:     15,
		MaxSourceBytes:     64 * 1024,
	}
}

// UnmarshalJSON mirrors per-field `#[serde(default = ...)]`.
func (c *FunctionCapabilities) UnmarshalJSON(b []byte) error {
	type alias FunctionCapabilities
	defaults := alias{
		AllowOntologyRead: true,
		TimeoutSeconds:    15,
		MaxSourceBytes:    64 * 1024,
	}
	a := defaults
	// Decode into a map first so we can detect explicit absence and only
	// then overlay onto the defaults.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if v, ok := raw["allow_ontology_read"]; ok {
		if err := json.Unmarshal(v, &a.AllowOntologyRead); err != nil {
			return err
		}
	}
	if v, ok := raw["allow_ontology_write"]; ok {
		if err := json.Unmarshal(v, &a.AllowOntologyWrite); err != nil {
			return err
		}
	}
	if v, ok := raw["allow_ai"]; ok {
		if err := json.Unmarshal(v, &a.AllowAI); err != nil {
			return err
		}
	}
	if v, ok := raw["allow_network"]; ok {
		if err := json.Unmarshal(v, &a.AllowNetwork); err != nil {
			return err
		}
	}
	if v, ok := raw["timeout_seconds"]; ok {
		if err := json.Unmarshal(v, &a.TimeoutSeconds); err != nil {
			return err
		}
	}
	if v, ok := raw["max_source_bytes"]; ok {
		if err := json.Unmarshal(v, &a.MaxSourceBytes); err != nil {
			return err
		}
	}
	*c = FunctionCapabilities(a)
	return nil
}

// FunctionPackageRow mirrors `struct FunctionPackageRow`.
type FunctionPackageRow struct {
	ID           uuid.UUID       `db:"id"`
	Name         string          `db:"name"`
	Version      string          `db:"version"`
	DisplayName  string          `db:"display_name"`
	Description  string          `db:"description"`
	Runtime      string          `db:"runtime"`
	Source       string          `db:"source"`
	Entrypoint   string          `db:"entrypoint"`
	Capabilities json.RawMessage `db:"capabilities"`
	OwnerID      uuid.UUID       `db:"owner_id"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
}

// FunctionPackage mirrors `struct FunctionPackage`.
type FunctionPackage struct {
	ID           uuid.UUID            `json:"id"`
	Name         string               `json:"name"`
	Version      string               `json:"version"`
	DisplayName  string               `json:"display_name"`
	Description  string               `json:"description"`
	Runtime      string               `json:"runtime"`
	Source       string               `json:"source"`
	Entrypoint   string               `json:"entrypoint"`
	Capabilities FunctionCapabilities `json:"capabilities"`
	OwnerID      uuid.UUID            `json:"owner_id"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// IntoPackage mirrors `TryFrom<FunctionPackageRow> for FunctionPackage`.
// On capability JSON parse failure falls back to
// [DefaultFunctionCapabilities] (the `impl Default`, all-true booleans).
func (row FunctionPackageRow) IntoPackage() FunctionPackage {
	caps := DefaultFunctionCapabilities()
	if len(row.Capabilities) > 0 {
		var parsed FunctionCapabilities
		if err := json.Unmarshal(row.Capabilities, &parsed); err == nil {
			caps = parsed
		}
	}
	return FunctionPackage{
		ID:           row.ID,
		Name:         row.Name,
		Version:      row.Version,
		DisplayName:  row.DisplayName,
		Description:  row.Description,
		Runtime:      row.Runtime,
		Source:       row.Source,
		Entrypoint:   row.Entrypoint,
		Capabilities: caps,
		OwnerID:      row.OwnerID,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

// FunctionPackageSummary mirrors `struct FunctionPackageSummary`.
type FunctionPackageSummary struct {
	ID           uuid.UUID            `json:"id"`
	Name         string               `json:"name"`
	Version      string               `json:"version"`
	DisplayName  string               `json:"display_name"`
	Runtime      string               `json:"runtime"`
	Entrypoint   string               `json:"entrypoint"`
	Capabilities FunctionCapabilities `json:"capabilities"`
}

// Summary mirrors `From<&FunctionPackage> for FunctionPackageSummary`.
func (p FunctionPackage) Summary() FunctionPackageSummary {
	return FunctionPackageSummary{
		ID:           p.ID,
		Name:         p.Name,
		Version:      p.Version,
		DisplayName:  p.DisplayName,
		Runtime:      p.Runtime,
		Entrypoint:   p.Entrypoint,
		Capabilities: p.Capabilities,
	}
}

// CreateFunctionPackageRequest mirrors `struct CreateFunctionPackageRequest`.
type CreateFunctionPackageRequest struct {
	Name         string                `json:"name"`
	Version      *string               `json:"version,omitempty"`
	DisplayName  *string               `json:"display_name,omitempty"`
	Description  *string               `json:"description,omitempty"`
	Runtime      string                `json:"runtime"`
	Source       string                `json:"source"`
	Entrypoint   *string               `json:"entrypoint,omitempty"`
	Capabilities *FunctionCapabilities `json:"capabilities,omitempty"`
}

// UpdateFunctionPackageRequest mirrors `struct UpdateFunctionPackageRequest`.
type UpdateFunctionPackageRequest struct {
	DisplayName  *string               `json:"display_name,omitempty"`
	Description  *string               `json:"description,omitempty"`
	Runtime      *string               `json:"runtime,omitempty"`
	Source       *string               `json:"source,omitempty"`
	Entrypoint   *string               `json:"entrypoint,omitempty"`
	Capabilities *FunctionCapabilities `json:"capabilities,omitempty"`
}

// ListFunctionPackagesQuery mirrors `struct ListFunctionPackagesQuery`.
type ListFunctionPackagesQuery struct {
	Runtime *string `json:"runtime,omitempty"`
	Search  *string `json:"search,omitempty"`
	Page    *int64  `json:"page,omitempty"`
	PerPage *int64  `json:"per_page,omitempty"`
}

// ListFunctionPackagesResponse mirrors `struct ListFunctionPackagesResponse`.
type ListFunctionPackagesResponse struct {
	Data    []FunctionPackage `json:"data"`
	Total   int64             `json:"total"`
	Page    int64             `json:"page"`
	PerPage int64             `json:"per_page"`
}

// ValidateFunctionPackageRequest mirrors `struct ValidateFunctionPackageRequest`.
// `parameters` is `#[serde(default)]` — null when missing.
type ValidateFunctionPackageRequest struct {
	ObjectTypeID   *uuid.UUID      `json:"object_type_id,omitempty"`
	TargetObjectID *uuid.UUID      `json:"target_object_id,omitempty"`
	Parameters     json.RawMessage `json:"parameters"`
	Justification  *string         `json:"justification,omitempty"`
}

// ValidateFunctionPackageResponse mirrors `struct ValidateFunctionPackageResponse`.
type ValidateFunctionPackageResponse struct {
	Valid   bool                   `json:"valid"`
	Package FunctionPackageSummary `json:"package"`
	Preview json.RawMessage        `json:"preview"`
	Errors  []string               `json:"errors"`
}

// SimulateFunctionPackageRequest mirrors `struct SimulateFunctionPackageRequest`.
type SimulateFunctionPackageRequest struct {
	ObjectTypeID   uuid.UUID       `json:"object_type_id"`
	TargetObjectID *uuid.UUID      `json:"target_object_id,omitempty"`
	Parameters     json.RawMessage `json:"parameters"`
	Justification  *string         `json:"justification,omitempty"`
}

// SimulateFunctionPackageResponse mirrors `struct SimulateFunctionPackageResponse`.
type SimulateFunctionPackageResponse struct {
	Package FunctionPackageSummary `json:"package"`
	Preview json.RawMessage        `json:"preview"`
	Result  json.RawMessage        `json:"result"`
}
