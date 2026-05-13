package models

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ObjectType mirrors `libs/ontology-kernel/src/models/object_type.rs`
// `struct ObjectType` 1:1.
type ObjectType struct {
	ID                       uuid.UUID       `json:"id"                        db:"id"`
	RID                      string          `json:"rid"                       db:"-"`
	Name                     string          `json:"name"                      db:"name"`
	APIName                  string          `json:"api_name"                  db:"-"`
	DisplayName              string          `json:"display_name"              db:"display_name"`
	PluralDisplayName        *string         `json:"plural_display_name"       db:"plural_display_name"`
	Description              string          `json:"description"               db:"description"`
	PrimaryKeyProperty       *string         `json:"primary_key_property"      db:"primary_key_property"`
	PrimaryKey               string          `json:"primary_key"               db:"-"`
	TitleProperty            *string         `json:"title_property"            db:"title_property"`
	Icon                     *string         `json:"icon"                      db:"icon"`
	Color                    *string         `json:"color"                     db:"color"`
	Status                   string          `json:"status"                    db:"status"`
	Visibility               string          `json:"visibility"                db:"visibility"`
	GroupNames               []string        `json:"group_names,omitempty"     db:"group_names"`
	ObjectDisplayPreferences json.RawMessage `json:"object_display_preferences,omitempty" db:"object_display_preferences"`
	Properties               []Property      `json:"properties"                db:"-"`
	PropertyCount            int             `json:"property_count"            db:"-"`
	SearchablePropertyNames  []string        `json:"searchable_property_names" db:"-"`
	GeoPointPropertyNames    []string        `json:"geopoint_property_names"   db:"-"`
	GeoShapePropertyNames    []string        `json:"geoshape_property_names"   db:"-"`
	OwnerID                  uuid.UUID       `json:"owner_id"                  db:"owner_id"`
	CreatedAt                time.Time       `json:"created_at"                db:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"                db:"updated_at"`
}

// EnrichObjectTypeMetadata fills the compatibility metadata used by
// Workshop widgets and Pipeline Builder outputs. The aliases are derived
// from the existing canonical columns so callers do not need a migration
// before they can render object type metadata consistently.
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
	if len(objectType.ObjectDisplayPreferences) == 0 {
		objectType.ObjectDisplayPreferences = json.RawMessage(`{}`)
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
	if strings.HasSuffix(strings.ToLower(base), "s") {
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

// CreateObjectTypeRequest mirrors `struct CreateObjectTypeRequest`.
type CreateObjectTypeRequest struct {
	Name                     string          `json:"name"`
	DisplayName              *string         `json:"display_name,omitempty"`
	Description              *string         `json:"description,omitempty"`
	PrimaryKeyProperty       *string         `json:"primary_key_property,omitempty"`
	TitleProperty            *string         `json:"title_property,omitempty"`
	PluralDisplayName        *string         `json:"plural_display_name,omitempty"`
	Status                   *string         `json:"status,omitempty"`
	Visibility               *string         `json:"visibility,omitempty"`
	GroupNames               []string        `json:"group_names,omitempty"`
	ObjectDisplayPreferences json.RawMessage `json:"object_display_preferences,omitempty"`
	Icon                     *string         `json:"icon,omitempty"`
	Color                    *string         `json:"color,omitempty"`
}

// UpdateObjectTypeRequest mirrors `struct UpdateObjectTypeRequest`.
type UpdateObjectTypeRequest struct {
	Name                     *string         `json:"name,omitempty"`
	DisplayName              *string         `json:"display_name,omitempty"`
	Description              *string         `json:"description,omitempty"`
	PrimaryKeyProperty       *string         `json:"primary_key_property,omitempty"`
	TitleProperty            *string         `json:"title_property,omitempty"`
	PluralDisplayName        *string         `json:"plural_display_name,omitempty"`
	Status                   *string         `json:"status,omitempty"`
	Visibility               *string         `json:"visibility,omitempty"`
	GroupNames               []string        `json:"group_names,omitempty"`
	ObjectDisplayPreferences json.RawMessage `json:"object_display_preferences,omitempty"`
	Icon                     *string         `json:"icon,omitempty"`
	Color                    *string         `json:"color,omitempty"`
}

// ListObjectTypesQuery mirrors `struct ListObjectTypesQuery`.
type ListObjectTypesQuery struct {
	Page    *int64  `json:"page,omitempty"`
	PerPage *int64  `json:"per_page,omitempty"`
	Search  *string `json:"search,omitempty"`
}

// ListObjectTypesResponse mirrors `struct ListObjectTypesResponse`.
type ListObjectTypesResponse struct {
	Data    []ObjectType `json:"data"`
	Total   int64        `json:"total"`
	Page    int64        `json:"page"`
	PerPage int64        `json:"per_page"`
}
