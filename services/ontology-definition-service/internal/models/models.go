// Package models holds wire types for ontology-definition-service.
//
// Foundation slice scope: object_types only. Properties, link_types,
// action_types, interfaces, shared property types, ontology_projects
// (~600 LOC of consolidated DDL) all land in follow-up slices once
// the Rust kernel handlers themselves migrate.
package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// ObjectType mirrors `ontology_schema.object_types` rows.
type ObjectType struct {
	ID                      uuid.UUID  `json:"id"`
	RID                     string     `json:"rid"`
	Name                    string     `json:"name"`
	APIName                 string     `json:"api_name"`
	DisplayName             string     `json:"display_name"`
	PluralDisplayName       *string    `json:"plural_display_name"`
	Description             string     `json:"description"`
	PrimaryKeyProperty      *string    `json:"primary_key_property"`
	PrimaryKey              string     `json:"primary_key"`
	TitleProperty           *string    `json:"title_property"`
	Icon                    *string    `json:"icon"`
	Color                   *string    `json:"color"`
	Status                  string     `json:"status"`
	Visibility              string     `json:"visibility"`
	Editable                bool       `json:"editable"`
	BackingDatasetID        *uuid.UUID `json:"backing_dataset_id,omitempty"`
	BackingDatasetRID       *string    `json:"backing_dataset_rid,omitempty"`
	PipelineRID             *string    `json:"pipeline_rid,omitempty"`
	ManagedBy               *string    `json:"managed_by,omitempty"`
	Properties              []Property `json:"properties"`
	PropertyCount           int        `json:"property_count"`
	SearchablePropertyNames []string   `json:"searchable_property_names"`
	GeoPointPropertyNames   []string   `json:"geopoint_property_names"`
	GeoShapePropertyNames   []string   `json:"geoshape_property_names"`
	OwnerID                 uuid.UUID  `json:"owner_id"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// EnrichObjectTypeMetadata fills the Foundry-like metadata fields that
// Workshop and Pipeline Builder consume, without requiring extra persisted
// columns for stable aliases.
func EnrichObjectTypeMetadata(objectType *ObjectType, properties []Property) {
	if objectType == nil {
		return
	}
	if objectType.RID == "" && objectType.ID != uuid.Nil {
		objectType.RID = "ri.ontology.main.object-type." + objectType.ID.String()
	}
	if objectType.APIName == "" {
		objectType.APIName = objectType.Name
	}
	if objectType.Status == "" {
		objectType.Status = "active"
	}
	if objectType.Visibility == "" {
		objectType.Visibility = "normal"
	}
	if objectType.PluralDisplayName == nil || strings.TrimSpace(*objectType.PluralDisplayName) == "" {
		plural := defaultPluralDisplayName(objectType.DisplayName, objectType.Name)
		objectType.PluralDisplayName = &plural
	}
	objectType.Properties = append([]Property(nil), properties...)
	objectType.PropertyCount = len(properties)

	primaryKey := ""
	if objectType.PrimaryKeyProperty != nil {
		primaryKey = strings.TrimSpace(*objectType.PrimaryKeyProperty)
	}
	if primaryKey == "" {
		primaryKey = "id"
	}
	objectType.PrimaryKey = primaryKey

	titleProperty := ""
	if objectType.TitleProperty != nil {
		titleProperty = strings.TrimSpace(*objectType.TitleProperty)
	}
	if titleProperty == "" {
		titleProperty = chooseTitleProperty(properties, primaryKey)
	}
	if titleProperty != "" {
		objectType.TitleProperty = &titleProperty
	}

	searchable := make([]string, 0, len(properties)+2)
	if titleProperty != "" {
		searchable = append(searchable, titleProperty)
	}
	if primaryKey != "" {
		searchable = append(searchable, primaryKey)
	}
	geoPoints := []string{}
	geoShapes := []string{}
	for _, property := range properties {
		kind := normalizePropertyType(property.PropertyType)
		switch {
		case isStringLikeType(kind):
			searchable = append(searchable, property.Name)
		case isGeoPointType(kind):
			geoPoints = append(geoPoints, property.Name)
		case isGeoShapeType(kind):
			geoShapes = append(geoShapes, property.Name)
		}
	}
	objectType.SearchablePropertyNames = uniqueNonEmptyStrings(searchable)
	objectType.GeoPointPropertyNames = uniqueNonEmptyStrings(geoPoints)
	objectType.GeoShapePropertyNames = uniqueNonEmptyStrings(geoShapes)
}

func defaultPluralDisplayName(displayName, name string) string {
	base := strings.TrimSpace(displayName)
	if base == "" {
		base = strings.TrimSpace(name)
	}
	if base == "" {
		return "Objects"
	}
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, "s") {
		return base
	}
	return base + "s"
}

func chooseTitleProperty(properties []Property, primaryKey string) string {
	candidates := []string{"label", "title", "name", "display_name", "trail_name"}
	for _, candidate := range candidates {
		if propertyName := findPropertyName(properties, candidate); propertyName != "" {
			return propertyName
		}
	}
	for _, property := range properties {
		if isStringLikeType(normalizePropertyType(property.PropertyType)) {
			return property.Name
		}
	}
	return strings.TrimSpace(primaryKey)
}

func findPropertyName(properties []Property, name string) string {
	for _, property := range properties {
		if strings.EqualFold(property.Name, name) {
			return property.Name
		}
	}
	return ""
}

func normalizePropertyType(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	kind = strings.ReplaceAll(kind, "-", "_")
	kind = strings.ReplaceAll(kind, " ", "_")
	return kind
}

func isStringLikeType(kind string) bool {
	switch kind {
	case "string", "str", "text", "uuid", "rid", "object_id":
		return true
	default:
		return strings.Contains(kind, "string") || strings.Contains(kind, "text")
	}
}

func isGeoPointType(kind string) bool {
	switch kind {
	case "geopoint", "geo_point", "point", "latlon", "lat_lon", "latitude_longitude":
		return true
	default:
		return strings.Contains(kind, "geopoint") || strings.Contains(kind, "geo_point")
	}
}

func isGeoShapeType(kind string) bool {
	switch kind {
	case "geoshape", "geo_shape", "geometry", "geojson", "linestring", "line_string", "polygon", "multipolygon":
		return true
	default:
		return strings.Contains(kind, "geoshape") || strings.Contains(kind, "geo_shape") ||
			strings.Contains(kind, "geometry") || strings.Contains(kind, "geojson")
	}
}

func uniqueNonEmptyStrings(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

type CreateObjectTypeRequest struct {
	ID                 *uuid.UUID `json:"id,omitempty"`
	Name               string     `json:"name"`
	DisplayName        string     `json:"display_name"`
	PluralDisplayName  *string    `json:"plural_display_name,omitempty"`
	Description        string     `json:"description,omitempty"`
	PrimaryKeyProperty *string    `json:"primary_key_property,omitempty"`
	Icon               *string    `json:"icon,omitempty"`
	Color              *string    `json:"color,omitempty"`
	Editable           *bool      `json:"editable,omitempty"`
	BackingDatasetID   *uuid.UUID `json:"backing_dataset_id,omitempty"`
	BackingDatasetRID  *string    `json:"backing_dataset_rid,omitempty"`
	PipelineRID        *string    `json:"pipeline_rid,omitempty"`
	ManagedBy          *string    `json:"managed_by,omitempty"`
}

type UpdateObjectTypeRequest struct {
	DisplayName        *string    `json:"display_name,omitempty"`
	PluralDisplayName  *string    `json:"plural_display_name,omitempty"`
	Description        *string    `json:"description,omitempty"`
	PrimaryKeyProperty *string    `json:"primary_key_property,omitempty"`
	Icon               *string    `json:"icon,omitempty"`
	Color              *string    `json:"color,omitempty"`
	Editable           *bool      `json:"editable,omitempty"`
	BackingDatasetID   *uuid.UUID `json:"backing_dataset_id,omitempty"`
	BackingDatasetRID  *string    `json:"backing_dataset_rid,omitempty"`
	PipelineRID        *string    `json:"pipeline_rid,omitempty"`
	ManagedBy          *string    `json:"managed_by,omitempty"`
}

// Property mirrors `ontology_schema.properties` rows.
type Property struct {
	ID               uuid.UUID `json:"id"`
	ObjectTypeID     uuid.UUID `json:"object_type_id"`
	Name             string    `json:"name"`
	DisplayName      string    `json:"display_name"`
	Description      string    `json:"description"`
	PropertyType     string    `json:"property_type"`
	BaseType         string    `json:"base_type,omitempty"`
	TypeFamily       string    `json:"type_family,omitempty"`
	TypeDisplayName  string    `json:"type_display_name,omitempty"`
	ValueShape       string    `json:"value_shape,omitempty"`
	IsArray          bool      `json:"is_array"`
	ArrayItemType    *string   `json:"array_item_type,omitempty"`
	ArrayAllowed     bool      `json:"array_allowed"`
	Searchable       bool      `json:"searchable"`
	Filterable       bool      `json:"filterable"`
	Sortable         bool      `json:"sortable"`
	Aggregatable     bool      `json:"aggregatable"`
	SemanticHints    []string  `json:"semantic_hints,omitempty"`
	Required         bool      `json:"required"`
	UniqueConstraint bool      `json:"unique_constraint"`
	TimeDependent    bool      `json:"time_dependent"`
	DefaultValue     any       `json:"default_value"`
	ValidationRules  any       `json:"validation_rules"`
	InlineEditConfig any       `json:"inline_edit_config"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type PropertyTypeMetadata struct {
	BaseType        string
	TypeFamily      string
	TypeDisplayName string
	ValueShape      string
	IsArray         bool
	ArrayItemType   *string
	ArrayAllowed    bool
	Searchable      bool
	Filterable      bool
	Sortable        bool
	Aggregatable    bool
	SemanticHints   []string
}

func EnrichPropertyMetadata(property *Property) {
	if property == nil {
		return
	}
	metadata := PropertyTypeMetadataFor(property.PropertyType)
	property.BaseType = metadata.BaseType
	property.TypeFamily = metadata.TypeFamily
	property.TypeDisplayName = metadata.TypeDisplayName
	property.ValueShape = metadata.ValueShape
	property.IsArray = metadata.IsArray
	property.ArrayItemType = metadata.ArrayItemType
	property.ArrayAllowed = metadata.ArrayAllowed
	property.Searchable = metadata.Searchable
	property.Filterable = metadata.Filterable
	property.Sortable = metadata.Sortable
	property.Aggregatable = metadata.Aggregatable
	property.SemanticHints = append([]string(nil), metadata.SemanticHints...)
}

func ValidatePropertyType(propertyType string) error {
	metadata := PropertyTypeMetadataFor(propertyType)
	if strings.TrimSpace(metadata.BaseType) == "" || metadata.TypeFamily == "unknown" {
		return fmt.Errorf("invalid property type %q", propertyType)
	}
	if metadata.IsArray && metadata.ArrayItemType != nil {
		itemMetadata := PropertyTypeMetadataFor(*metadata.ArrayItemType)
		if itemMetadata.TypeFamily == "unknown" {
			return fmt.Errorf("invalid property type %q", propertyType)
		}
		if !itemMetadata.ArrayAllowed {
			return fmt.Errorf("property type %q cannot be used as an array item", *metadata.ArrayItemType)
		}
	}
	return nil
}

func PropertyTypeMetadataFor(propertyType string) PropertyTypeMetadata {
	isArray, itemType := parseArrayPropertyType(propertyType)
	base := CanonicalPropertyBaseType(propertyType)
	if isArray && itemType != nil {
		base = CanonicalPropertyBaseType(*itemType)
	}
	metadata := metadataForBaseType(base)
	metadata.IsArray = isArray
	if isArray {
		itemHint := ""
		if itemType != nil {
			item := base
			metadata.ArrayItemType = &item
			itemHint = item
		}
		metadata.BaseType = "array"
		metadata.TypeFamily = "collection"
		metadata.TypeDisplayName = "Array"
		metadata.ValueShape = "array"
		metadata.Searchable = itemType != nil && metadata.Searchable && isStringLikeType(base)
		metadata.Filterable = true
		metadata.Sortable = false
		metadata.Aggregatable = false
		metadata.SemanticHints = uniqueNonEmptyStrings(append([]string{"array", itemHint}, metadata.SemanticHints...))
	}
	return metadata
}

func CanonicalPropertyBaseType(propertyType string) string {
	isArray, itemType := parseArrayPropertyType(propertyType)
	if isArray {
		if itemType == nil {
			return "array"
		}
		return CanonicalPropertyBaseType(*itemType)
	}
	kind := normalizePropertyType(propertyType)
	switch {
	case kind == "string" || kind == "str" || kind == "text":
		return "string"
	case kind == "integer" || kind == "int" || kind == "long" || kind == "short" || kind == "byte":
		return "integer"
	case kind == "float" || kind == "double" || kind == "decimal" || kind == "number" || kind == "numeric":
		return "float"
	case kind == "boolean" || kind == "bool":
		return "boolean"
	case kind == "date":
		return "date"
	case kind == "timestamp" || kind == "datetime":
		return "timestamp"
	case kind == "json":
		return "json"
	case kind == "array":
		return "array"
	case kind == "vector" || kind == "embedding":
		return "vector"
	case kind == "reference" || kind == "object_reference":
		return "reference"
	case isGeoPointType(kind):
		return "geopoint"
	case isGeoShapeType(kind):
		return "geoshape"
	case kind == "media_reference" || kind == "media" || kind == "mediaref":
		return "media_reference"
	case kind == "attachment" || kind == "file":
		return "attachment"
	case kind == "time_series" || kind == "timeseries" || kind == "time_series_reference":
		return "time_series"
	case kind == "struct":
		return "struct"
	default:
		return kind
	}
}

func metadataForBaseType(base string) PropertyTypeMetadata {
	switch base {
	case "string":
		return propertyTypeMetadata(base, "primitive", "String", "string", true, true, true, false, true, "string")
	case "integer":
		return propertyTypeMetadata(base, "numeric", "Integer", "integer", true, true, true, true, false, "numeric")
	case "float":
		return propertyTypeMetadata(base, "numeric", "Float", "number", true, true, true, true, false, "numeric")
	case "boolean":
		return propertyTypeMetadata(base, "primitive", "Boolean", "boolean", true, true, true, false, false, "boolean")
	case "date":
		return propertyTypeMetadata(base, "temporal", "Date", "date-string", true, true, true, false, false, "temporal")
	case "timestamp":
		return propertyTypeMetadata(base, "temporal", "Timestamp", "timestamp-string", true, true, true, false, false, "temporal")
	case "json":
		return propertyTypeMetadata(base, "structured", "JSON", "json", true, false, false, false, false, "json")
	case "array":
		return propertyTypeMetadata(base, "collection", "Array", "array", true, true, false, false, false, "array")
	case "vector":
		return propertyTypeMetadata(base, "semantic", "Vector", "numeric-array", false, false, false, false, false, "vector", "embedding")
	case "reference":
		return propertyTypeMetadata(base, "reference", "Object reference", "string", true, true, true, false, true, "reference")
	case "geopoint":
		return propertyTypeMetadata(base, "geospatial", "Geopoint", "lat-lon-object", true, true, false, false, false, "geospatial", "point")
	case "geoshape":
		return propertyTypeMetadata(base, "geospatial", "Geoshape", "geojson-object-or-string", true, true, false, false, false, "geospatial", "shape")
	case "media_reference":
		return propertyTypeMetadata(base, "media", "Media reference", "media-reference", true, true, false, false, false, "media")
	case "attachment":
		return propertyTypeMetadata(base, "file", "Attachment", "attachment-reference", true, true, false, false, false, "attachment")
	case "time_series":
		return propertyTypeMetadata(base, "timeseries", "Time series", "time-series", false, false, false, false, false, "time_series", "temporal")
	case "struct":
		return propertyTypeMetadata(base, "structured", "Struct", "object", true, false, false, false, false, "struct")
	default:
		return propertyTypeMetadata(base, "unknown", strings.TrimSpace(base), "unknown", true, false, false, false, false)
	}
}

func propertyTypeMetadata(base, family, displayName, valueShape string, arrayAllowed, filterable, sortable, aggregatable, searchable bool, hints ...string) PropertyTypeMetadata {
	return PropertyTypeMetadata{
		BaseType:        base,
		TypeFamily:      family,
		TypeDisplayName: displayName,
		ValueShape:      valueShape,
		ArrayAllowed:    arrayAllowed,
		Searchable:      searchable,
		Filterable:      filterable,
		Sortable:        sortable,
		Aggregatable:    aggregatable,
		SemanticHints:   uniqueNonEmptyStrings(hints),
	}
}

func parseArrayPropertyType(propertyType string) (bool, *string) {
	raw := strings.TrimSpace(propertyType)
	if raw == "" {
		return false, nil
	}
	kind := normalizePropertyType(raw)
	if kind == "array" {
		return true, nil
	}
	if strings.HasSuffix(kind, "[]") {
		item := strings.TrimSuffix(kind, "[]")
		return true, &item
	}
	for _, prefix := range []string{"array<", "array_of_"} {
		if strings.HasPrefix(kind, prefix) {
			item := strings.TrimPrefix(kind, prefix)
			item = strings.TrimSuffix(item, ">")
			if item != "" {
				return true, &item
			}
		}
	}
	return false, nil
}

type CreatePropertyRequest struct {
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	Description      string `json:"description,omitempty"`
	PropertyType     string `json:"property_type"`
	Required         bool   `json:"required,omitempty"`
	UniqueConstraint bool   `json:"unique_constraint,omitempty"`
	TimeDependent    bool   `json:"time_dependent,omitempty"`
	DefaultValue     any    `json:"default_value,omitempty"`
	ValidationRules  any    `json:"validation_rules,omitempty"`
	InlineEditConfig any    `json:"inline_edit_config,omitempty"`
}

// LinkType mirrors `ontology_schema.link_types` rows.
type LinkType struct {
	ID                    uuid.UUID      `json:"id"`
	Name                  string         `json:"name"`
	DisplayName           string         `json:"display_name"`
	Description           string         `json:"description"`
	SourceTypeID          uuid.UUID      `json:"source_type_id"`
	TargetTypeID          uuid.UUID      `json:"target_type_id"`
	Cardinality           string         `json:"cardinality"`
	Label                 string         `json:"label"`
	ReverseLabel          string         `json:"reverse_label"`
	Visibility            string         `json:"visibility"`
	LinkDatasourceMapping map[string]any `json:"link_datasource_mapping"`
	OwnerID               uuid.UUID      `json:"owner_id"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type CreateLinkTypeRequest struct {
	ID                    *uuid.UUID     `json:"id,omitempty"`
	Name                  string         `json:"name"`
	DisplayName           string         `json:"display_name"`
	Description           string         `json:"description,omitempty"`
	SourceTypeID          uuid.UUID      `json:"source_type_id"`
	TargetTypeID          uuid.UUID      `json:"target_type_id"`
	Cardinality           string         `json:"cardinality,omitempty"`
	Label                 string         `json:"label,omitempty"`
	ReverseLabel          string         `json:"reverse_label,omitempty"`
	Visibility            string         `json:"visibility,omitempty"`
	LinkDatasourceMapping map[string]any `json:"link_datasource_mapping,omitempty"`
}

type UpdateLinkTypeRequest struct {
	DisplayName           *string        `json:"display_name,omitempty"`
	Description           *string        `json:"description,omitempty"`
	Cardinality           *string        `json:"cardinality,omitempty"`
	Label                 *string        `json:"label,omitempty"`
	ReverseLabel          *string        `json:"reverse_label,omitempty"`
	Visibility            *string        `json:"visibility,omitempty"`
	LinkDatasourceMapping map[string]any `json:"link_datasource_mapping,omitempty"`
}

// OntologyInterface mirrors `ontology_schema.ontology_interfaces` rows.
// The Ontology Manager UI reads the catalog on first paint to populate
// the "interfaces" tab; CRUD lives in a follow-up slice.
type OntologyInterface struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description"`
	OwnerID     uuid.UUID `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SharedPropertyType mirrors `ontology_schema.shared_property_types`
// rows. Same scope as `OntologyInterface`: read-only for the catalog
// view, CRUD pending.
type SharedPropertyType struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	DisplayName      string    `json:"display_name"`
	Description      string    `json:"description"`
	PropertyType     string    `json:"property_type"`
	Required         bool      `json:"required"`
	UniqueConstraint bool      `json:"unique_constraint"`
	TimeDependent    bool      `json:"time_dependent"`
	DefaultValue     any       `json:"default_value"`
	ValidationRules  any       `json:"validation_rules"`
	OwnerID          uuid.UUID `json:"owner_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
