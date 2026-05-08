package domain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/models"
)

// NormalizeRegistrationMode mirrors Rust's registration-mode gate.
func NormalizeRegistrationMode(mode *string) (string, error) {
	if mode == nil || strings.TrimSpace(*mode) == "" {
		return "sync", nil
	}
	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "sync", "zero_copy":
		return strings.ToLower(strings.TrimSpace(*mode)), nil
	default:
		return "", fmt.Errorf("registration_mode must be sync or zero_copy")
	}
}

// DiscoverConnectionSources is the Go inline discovery shim used by
// registration handlers and background workers until adapter-backed discovery
// is wired end-to-end.
func DiscoverConnectionSources(c *models.Connection) []models.DiscoveredSource {
	zeroCopyTypes := map[string]bool{"adls": true, "azure_blob": true, "bigquery": true, "csv": true, "databricks": true, "gcs": true, "generic": true, "google_cloud_storage": true, "json": true, "mysql": true, "open_table_catalog": true, "postgresql": true, "s3": true, "snowflake": true}
	var cfg map[string]json.RawMessage
	_ = json.Unmarshal(c.Config, &cfg)
	for _, key := range []string{"tables", "datasets", "iceberg_tables", "delta_tables", "topics", "streams", "entities", "objects"} {
		if raw, ok := cfg[key]; ok {
			if out := discoveredFromConfigArray(raw, key, c.ConnectorType, zeroCopyTypes[c.ConnectorType]); len(out) > 0 {
				return out
			}
		}
	}
	meta, _ := json.Marshal(map[string]any{"connection_type": c.ConnectorType, "supports_zero_copy": zeroCopyTypes[c.ConnectorType]})
	return []models.DiscoveredSource{{Selector: c.Name, DisplayName: c.Name, SourceKind: c.ConnectorType, SupportsSync: true, SupportsZeroCopy: zeroCopyTypes[c.ConnectorType], Metadata: meta}}
}

// SelectSources applies the auto-registration selector allow-list.
func SelectSources(discovered []models.DiscoveredSource, selectors []string) []models.DiscoveredSource {
	if len(selectors) == 0 {
		return append([]models.DiscoveredSource(nil), discovered...)
	}
	allowed := make(map[string]struct{}, len(selectors))
	for _, selector := range selectors {
		allowed[selector] = struct{}{}
	}
	out := make([]models.DiscoveredSource, 0, len(discovered))
	for _, source := range discovered {
		if _, ok := allowed[source.Selector]; ok {
			out = append(out, source)
		}
	}
	return out
}

func discoveredFromConfigArray(raw json.RawMessage, collection, connectorType string, zeroCopy bool) []models.DiscoveredSource {
	var entries []map[string]any
	if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
		return nil
	}
	out := make([]models.DiscoveredSource, 0, len(entries))
	for _, t := range entries {
		selector := stringValue(t, "selector", stringValue(t, "name", stringValue(t, "table", stringValue(t, "dataset", stringValue(t, "topic", stringValue(t, "stream", stringValue(t, "entity", "")))))))
		if selector == "" {
			continue
		}
		display := stringValue(t, "display_name", selector)
		kind := stringValue(t, "source_kind", defaultSourceKind(connectorType, collection))
		metadata, _ := json.Marshal(t)
		signature := optionalStringValue(t, "source_signature", optionalStringValue(t, "schema_signature", optionalStringValue(t, "version", nil)))
		out = append(out, models.DiscoveredSource{Selector: selector, DisplayName: display, SourceKind: kind, SupportsSync: true, SupportsZeroCopy: boolValue(t, "supports_zero_copy", zeroCopy), SourceSignature: signature, Metadata: metadata})
	}
	return out
}

func defaultSourceKind(connectorType, collection string) string {
	switch collection {
	case "topics":
		return "topic"
	case "streams":
		return "stream"
	case "iceberg_tables":
		return "iceberg_table"
	case "delta_tables":
		return "delta_table"
	case "datasets":
		if connectorType == "parquet" {
			return "parquet_file"
		}
	}
	return connectorType
}

func stringValue(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func optionalStringValue(m map[string]any, key string, fallback *string) *string {
	if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
		value := v
		return &value
	}
	return fallback
}

func boolValue(m map[string]any, key string, fallback bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return fallback
}
