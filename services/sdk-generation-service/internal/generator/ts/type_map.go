package ts

import "strings"

// MapType collapses an ontology PropertyType wire string into the TS
// type the generator emits in types.ts / actions.ts. Anything we don't
// recognize falls back to `unknown`.
//
// The mapping is deliberately simple in v0: dates and timestamps are
// kept as `string` (ISO-8601) rather than `Date`, because the snapshot
// data is JSON over HTTP and the generated client does not currently
// rehydrate it. Geo and timeseries types are emitted as opaque
// records so the surface compiles today without committing to a
// schema we'd have to break later.
func MapType(propertyType string) string {
	raw := strings.ToLower(strings.TrimSpace(propertyType))
	if raw == "" {
		return "unknown"
	}
	if itemKind, ok := stripArray(raw); ok {
		return MapType(itemKind) + "[]"
	}
	switch raw {
	case "string", "text", "uri", "uuid", "rid":
		return "string"
	case "integer", "int", "long", "double", "float", "decimal", "number":
		return "number"
	case "boolean", "bool":
		return "boolean"
	case "date", "datetime", "timestamp", "instant":
		return "string"
	case "geo_point", "geopoint":
		return "{ readonly type: \"Point\"; readonly coordinates: readonly [number, number] }"
	case "geo_shape", "geoshape":
		return "{ readonly type: string; readonly coordinates: unknown }"
	case "timeseries":
		return "{ readonly seriesId: string }"
	case "attachment":
		return "{ readonly rid: string; readonly filename: string }"
	case "json", "object", "any":
		return "Record<string, unknown>"
	default:
		return "unknown"
	}
}

func stripArray(raw string) (string, bool) {
	if strings.HasPrefix(raw, "array<") && strings.HasSuffix(raw, ">") {
		return raw[len("array<") : len(raw)-1], true
	}
	if strings.HasSuffix(raw, "[]") {
		return strings.TrimSuffix(raw, "[]"), true
	}
	return "", false
}
