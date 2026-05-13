package models

import "strings"

// PropertyTypeMetadata describes the Foundry-like base type semantics
// that Workshop, Pipeline Builder, object queries, and inline edits need
// to reason about a property without hardcoding raw property_type strings.
type PropertyTypeMetadata struct {
	BaseType               string
	TypeFamily             string
	TypeDisplayName        string
	ValueShape             string
	IsArray                bool
	ArrayItemType          *string
	ArrayAllowed           bool
	Searchable             bool
	Filterable             bool
	Sortable               bool
	Aggregatable           bool
	PrimaryKeyEligible     bool
	TitleKeyEligible       bool
	FormattingEligible     bool
	ObjectSecurityEligible bool
	ProminentEligible      bool
	SemanticHints          []string
}

// EnrichPropertyMetadata attaches canonical base type metadata to a
// direct object property. The persisted PropertyType remains unchanged.
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
	property.PrimaryKeyEligible = metadata.PrimaryKeyEligible
	property.TitleKeyEligible = metadata.TitleKeyEligible
	property.FormattingEligible = metadata.FormattingEligible
	property.ObjectSecurityEligible = metadata.ObjectSecurityEligible
	property.ProminentEligible = metadata.ProminentEligible
	property.SemanticHints = append([]string(nil), metadata.SemanticHints...)
	EnrichPropertyPresentationMetadata(property)
}

func EnrichPropertyPresentationMetadata(property *Property) {
	if property == nil {
		return
	}
	if strings.TrimSpace(property.DisplayMode) == "" {
		property.DisplayMode = "normal"
	}
	if len(property.ValueFormatting) == 0 {
		property.ValueFormatting = []byte(`{}`)
	}
	if len(property.ConditionalFormatting) == 0 {
		property.ConditionalFormatting = []byte(`[]`)
	}
	if len(property.ReducerMetadata) == 0 {
		property.ReducerMetadata = []byte(`{}`)
	}
}

func EnrichSharedPropertyTypeMetadata(property *SharedPropertyType) {
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
	property.PrimaryKeyEligible = metadata.PrimaryKeyEligible
	property.TitleKeyEligible = metadata.TitleKeyEligible
	property.FormattingEligible = metadata.FormattingEligible
	property.ObjectSecurityEligible = metadata.ObjectSecurityEligible
	property.ProminentEligible = metadata.ProminentEligible
	property.SemanticHints = append([]string(nil), metadata.SemanticHints...)
}

func EnrichInterfacePropertyMetadata(property *InterfaceProperty) {
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
	property.PrimaryKeyEligible = metadata.PrimaryKeyEligible
	property.TitleKeyEligible = metadata.TitleKeyEligible
	property.FormattingEligible = metadata.FormattingEligible
	property.ObjectSecurityEligible = metadata.ObjectSecurityEligible
	property.ProminentEligible = metadata.ProminentEligible
	property.SemanticHints = append([]string(nil), metadata.SemanticHints...)
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
		metadata.PrimaryKeyEligible = false
		metadata.TitleKeyEligible = false
		metadata.ObjectSecurityEligible = false
		metadata.ProminentEligible = metadata.Filterable
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
	case kind == "integer" || kind == "int" || kind == "short" || kind == "byte":
		return "integer"
	case kind == "long":
		return "long"
	case kind == "float" || kind == "double":
		return "float"
	case kind == "decimal" || kind == "number" || kind == "numeric":
		return "decimal"
	case kind == "boolean" || kind == "bool":
		return "boolean"
	case kind == "date":
		return "date"
	case kind == "time":
		return "time"
	case kind == "timestamp" || kind == "datetime":
		return "timestamp"
	case kind == "json":
		return "json"
	case kind == "array":
		return "array"
	case kind == "vector" || kind == "embedding":
		return "vector"
	case kind == "reference" || kind == "object_reference" || kind == "object_ref":
		return "reference"
	case kind == "geohash":
		return "geohash"
	case isGeoPointType(kind):
		return "geopoint"
	case isGeoShapeType(kind):
		return "geoshape"
	case kind == "media_reference" || kind == "media" || kind == "mediaref":
		return "media_reference"
	case kind == "attachment" || kind == "file" || kind == "file_reference":
		return "attachment"
	case kind == "binary" || kind == "bytes":
		return "binary"
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
		return propertyTypeMetadata(base, "primitive", "String", "string", true, true, true, false, true, true, true, true, true, true, "string")
	case "integer":
		return propertyTypeMetadata(base, "numeric", "Integer", "integer", true, true, true, true, false, true, false, true, true, true, "numeric")
	case "long":
		return propertyTypeMetadata(base, "numeric", "Long", "integer", true, true, true, true, false, true, false, true, true, true, "numeric")
	case "float":
		return propertyTypeMetadata(base, "numeric", "Float", "number", true, true, true, true, false, false, false, true, false, true, "numeric")
	case "decimal":
		return propertyTypeMetadata(base, "numeric", "Decimal", "decimal", true, true, true, true, false, false, false, true, false, true, "numeric", "decimal")
	case "boolean":
		return propertyTypeMetadata(base, "primitive", "Boolean", "boolean", true, true, true, false, false, false, false, true, true, true, "boolean")
	case "date":
		return propertyTypeMetadata(base, "temporal", "Date", "date-string", true, true, true, false, false, false, false, true, true, true, "temporal")
	case "time":
		return propertyTypeMetadata(base, "temporal", "Time", "time-string", true, true, true, false, false, false, false, true, true, true, "temporal", "time")
	case "timestamp":
		return propertyTypeMetadata(base, "temporal", "Timestamp", "timestamp-string", true, true, true, false, false, false, false, true, true, true, "temporal")
	case "json":
		return propertyTypeMetadata(base, "structured", "JSON", "json", true, false, false, false, false, false, false, true, false, false, "json")
	case "array":
		return propertyTypeMetadata(base, "collection", "Array", "array", true, true, false, false, false, false, false, true, false, false, "array")
	case "vector":
		return propertyTypeMetadata(base, "semantic", "Vector", "numeric-array", false, false, false, false, false, false, false, false, false, false, "vector", "embedding")
	case "reference":
		return propertyTypeMetadata(base, "reference", "Object reference", "string", true, true, true, false, true, false, true, true, true, true, "reference", "object_reference")
	case "geohash":
		return propertyTypeMetadata(base, "geospatial", "Geohash", "string", true, true, true, false, true, false, true, true, true, true, "geospatial", "geohash")
	case "geopoint":
		return propertyTypeMetadata(base, "geospatial", "Geopoint", "lat-lon-object", true, true, false, false, false, false, false, true, false, true, "geospatial", "point")
	case "geoshape":
		return propertyTypeMetadata(base, "geospatial", "Geoshape", "geojson-object-or-string", true, true, false, false, false, false, false, true, false, true, "geospatial", "shape")
	case "media_reference":
		return propertyTypeMetadata(base, "media", "Media reference", "media-reference", true, true, false, false, false, false, false, true, false, true, "media")
	case "attachment":
		return propertyTypeMetadata(base, "file", "Attachment", "attachment-reference", true, true, false, false, false, false, false, true, false, true, "attachment", "file")
	case "binary":
		return propertyTypeMetadata(base, "file", "Binary", "base64-string", false, false, false, false, false, false, false, false, false, false, "binary", "file")
	case "time_series":
		return propertyTypeMetadata(base, "timeseries", "Time series", "time-series", false, false, false, false, false, false, false, false, false, false, "time_series", "temporal")
	case "struct":
		return propertyTypeMetadata(base, "structured", "Struct", "object", true, false, false, false, false, false, false, true, false, false, "struct")
	default:
		return propertyTypeMetadata(base, "unknown", strings.TrimSpace(base), "unknown", true, false, false, false, false, false, false, false, false, false)
	}
}

func propertyTypeMetadata(base, family, displayName, valueShape string, arrayAllowed, filterable, sortable, aggregatable, searchable, primaryKeyEligible, titleKeyEligible, formattingEligible, objectSecurityEligible, prominentEligible bool, hints ...string) PropertyTypeMetadata {
	return PropertyTypeMetadata{
		BaseType:               base,
		TypeFamily:             family,
		TypeDisplayName:        displayName,
		ValueShape:             valueShape,
		ArrayAllowed:           arrayAllowed,
		Searchable:             searchable,
		Filterable:             filterable,
		Sortable:               sortable,
		Aggregatable:           aggregatable,
		PrimaryKeyEligible:     primaryKeyEligible,
		TitleKeyEligible:       titleKeyEligible,
		FormattingEligible:     formattingEligible,
		ObjectSecurityEligible: objectSecurityEligible,
		ProminentEligible:      prominentEligible,
		SemanticHints:          uniqueNonEmptyStrings(hints),
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
