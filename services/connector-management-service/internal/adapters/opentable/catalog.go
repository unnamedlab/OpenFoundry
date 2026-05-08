// Package opentable is the Go port of the Rust
// `services/connector-management-service/src/connectors/open_table_catalog.rs`
// helper.
//
// Foundry-aligned: any object-store backed connector (S3, ADLS / Azure Blob,
// GCS, OneLake, …) can carry inline `iceberg_tables[]` and/or
// `delta_tables[]` arrays in its `connection.config` to advertise tables
// that already live in the lake. Discovery surfaces one [adapters.Source]
// per entry with:
//
//   - SourceKind        = "<store>_<format>_table"  (e.g. "azure_iceberg_table")
//   - SupportsSync      = false  (zero-copy only)
//   - SupportsZeroCopy  = true
//   - SourceSignature   = the entry's `snapshot_id` if present
//   - Metadata.upstream.metadata_location / table_location = upstream pointers
//     the Iceberg REST `LoadTable` handler forwards verbatim — fulfilling the
//     zero-copy promise from
//     `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Core concepts/Virtual tables.md`.
//
// Each store-specific adapter wraps these helpers with its own validator and
// discovery dispatcher entry (see internal/adapters/azure_blob,
// internal/adapters/generic, …).
package opentable

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/openfoundry/openfoundry-go/services/connector-management-service/internal/adapters"
)

// HasCatalog reports whether `config` declares at least one inline open-table
// entry. Mirrors Rust's `has_open_table_catalog`. Used by per-store
// validators to skip the inline HTTP-catalog requirement when the source
// operates exclusively as a metadata pointer for Iceberg / Delta tables.
func HasCatalog(config json.RawMessage) bool {
	if len(config) == 0 {
		return false
	}
	var probe struct {
		IcebergTables []json.RawMessage `json:"iceberg_tables"`
		DeltaTables   []json.RawMessage `json:"delta_tables"`
	}
	if err := json.Unmarshal(config, &probe); err != nil {
		return false
	}
	return len(probe.IcebergTables) > 0 || len(probe.DeltaTables) > 0
}

// Discover builds [adapters.Source] entries for every `iceberg_tables[]` and
// `delta_tables[]` entry declared in `config`. `storePrefix` is prepended to
// the SourceKind so callers can distinguish e.g. "s3_iceberg_table" from
// "azure_iceberg_table" downstream.
//
// Mirrors Rust's `open_table_catalog::discover`. De-duplicates by selector
// (last entry wins) and returns the result sorted by selector for
// deterministic output.
func Discover(config json.RawMessage, storePrefix string) ([]adapters.Source, error) {
	if len(config) == 0 {
		return nil, nil
	}
	var raw struct {
		IcebergTables []tableEntry `json:"iceberg_tables"`
		DeltaTables   []tableEntry `json:"delta_tables"`
	}
	if err := json.Unmarshal(config, &raw); err != nil {
		return nil, err
	}

	dedup := make(map[string]adapters.Source)
	for _, entry := range raw.IcebergTables {
		if src, ok := entry.toSource(storePrefix, "iceberg"); ok {
			dedup[src.Selector] = src
		}
	}
	for _, entry := range raw.DeltaTables {
		if src, ok := entry.toSource(storePrefix, "delta"); ok {
			dedup[src.Selector] = src
		}
	}
	out := make([]adapters.Source, 0, len(dedup))
	for _, src := range dedup {
		out = append(out, src)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Selector < out[j].Selector })
	return out, nil
}

type tableEntry struct {
	Selector         string          `json:"selector"`
	DisplayName      string          `json:"display_name"`
	MetadataLocation string          `json:"metadata_location"`
	TableLocation    string          `json:"table_location"`
	SnapshotID       json.RawMessage `json:"snapshot_id"`
}

func (e tableEntry) toSource(storePrefix, format string) (adapters.Source, bool) {
	if e.Selector == "" {
		return adapters.Source{}, false
	}
	display := e.DisplayName
	if display == "" {
		display = e.Selector
	}
	meta, _ := json.Marshal(map[string]any{
		"format": format,
		"upstream": map[string]any{
			"metadata_location": stringOrNil(e.MetadataLocation),
			"table_location":    stringOrNil(e.TableLocation),
		},
	})
	src := adapters.Source{
		Selector:         e.Selector,
		DisplayName:      display,
		SourceKind:       storePrefix + "_" + format + "_table",
		SupportsSync:     false,
		SupportsZeroCopy: true,
		Metadata:         meta,
	}
	if sig := decodeSignature(e.SnapshotID); sig != "" {
		src.SourceSignature = &sig
	}
	return src, true
}

func stringOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// decodeSignature accepts either a JSON string or a JSON integer, mirroring
// Rust's `v.as_str().or(v.as_i64().map(|n| n.to_string()))`.
func decodeSignature(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil {
		return strconv.FormatInt(asInt, 10)
	}
	return ""
}
