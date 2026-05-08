package handlers

// Schema validation + history endpoints (Bloque E2). Mirrors
// services/ingestion-replication-service/src/event_streaming/handlers/schemas.rs.
//
//   POST /api/v1/streaming/streams/{id}/schema:validate
//   GET  /api/v1/streaming/streams/{id}/schema/history
//
// The Rust implementation delegates fingerprinting and compatibility
// checks to the event_bus_control::schema_registry crate. We expose
// the same behaviour through a SchemaRegistry interface so production
// wiring injects the real registry (BusControlSchemaRegistry, backed
// by libs/event-bus-control) and tests use NoopSchemaRegistry without
// pulling in the avro/protobuf dependencies.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	controlbus "github.com/openfoundry/openfoundry-go/libs/event-bus-control"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/repo"
)

// SchemaRegistry abstracts the schema-registry façade.
//
// Implementations may shell out to the central registry
// (BusControlSchemaRegistry), run an in-process Avro parser, or stub
// out the call entirely (NoopSchemaRegistry).
type SchemaRegistry interface {
	// Fingerprint returns the canonical Avro fingerprint for the
	// schema, or a non-nil error if the document does not parse.
	Fingerprint(schemaJSON []byte) (string, error)
	// ValidatePayload returns nil when sample conforms to schemaJSON,
	// or a descriptive error otherwise.
	ValidatePayload(schemaJSON, sample []byte) error
	// CheckCompatibility verifies that candidate is mode-compatible
	// with previous. Mode is the persisted compatibility setting (e.g.
	// BACKWARD, FORWARD, FULL, NONE).
	CheckCompatibility(previous, candidate []byte, mode string) error
}

// SchemaStore persists schema-history rows and returns the currently-
// installed schema for a stream.
type SchemaStore interface {
	StreamExists(ctx context.Context, streamID uuid.UUID) (bool, error)
	CurrentSchema(ctx context.Context, streamID uuid.UUID) (current []byte, mode string, err error)
	ListSchemaHistory(ctx context.Context, streamID uuid.UUID) ([]models.StreamSchemaVersion, error)
}

// SchemasHandler bundles the schema endpoints.
type SchemasHandler struct {
	Store    SchemaStore
	Registry SchemaRegistry
}

// validCompatibilityModes mirrors event_bus_control::schema_registry::CompatibilityMode.
// Keys are upper-case — wire format is Confluent-style ("BACKWARD",
// "FULL", …). The handler accepts case-insensitively but persists the
// canonical upper-case form.
var validCompatibilityModes = map[string]struct{}{
	"NONE":                {},
	"BACKWARD":            {},
	"BACKWARD_TRANSITIVE": {},
	"FORWARD":             {},
	"FORWARD_TRANSITIVE":  {},
	"FULL":                {},
	"FULL_TRANSITIVE":     {},
}

// ValidateSchema is POST /streams/{id}/schema:validate.
func (h *SchemasHandler) ValidateSchema(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	streamID, ok := parseStreamID(w, r)
	if !ok {
		return
	}
	var body models.ValidateSchemaRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.SchemaAvro) == 0 || !json.Valid(body.SchemaAvro) {
		writeJSONErr(w, http.StatusBadRequest, "schema_avro must be a valid JSON document")
		return
	}

	resp := models.ValidateSchemaResponse{Errors: []string{}, Warnings: []string{}}

	if h.Registry != nil {
		if fp, err := h.Registry.Fingerprint(body.SchemaAvro); err != nil {
			resp.Errors = append(resp.Errors, "schema parse failed: "+err.Error())
		} else {
			f := fp
			resp.Fingerprint = &f
		}
		if len(body.Sample) > 0 {
			if err := h.Registry.ValidatePayload(body.SchemaAvro, body.Sample); err != nil {
				resp.Errors = append(resp.Errors, "sample failed validation: "+err.Error())
			}
		}
	}

	current, currentMode, err := h.Store.CurrentSchema(r.Context(), streamID)
	if errors.Is(err, repo.ErrStreamNotFound) {
		writeJSONErr(w, http.StatusNotFound, "stream not found")
		return
	}
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}

	mode := currentMode
	if body.Compatibility != nil && *body.Compatibility != "" {
		mode = *body.Compatibility
	}
	if mode != "" {
		if _, ok := validCompatibilityModes[mode]; !ok {
			writeJSONErr(w, http.StatusBadRequest, "invalid compatibility mode: "+mode)
			return
		}
	}

	if len(current) > 0 && h.Registry != nil {
		if err := h.Registry.CheckCompatibility(current, body.SchemaAvro, mode); err != nil {
			reason := err.Error()
			resp.Compatibility = &models.CompatibilityOutcome{
				Mode: mode, Compatible: false, Reason: &reason,
			}
			resp.Errors = append(resp.Errors, "compatibility violation: "+err.Error())
		} else {
			resp.Compatibility = &models.CompatibilityOutcome{
				Mode: mode, Compatible: true,
			}
		}
	} else {
		resp.Warnings = append(resp.Warnings,
			"no current Avro schema persisted; compatibility check skipped")
	}

	resp.Valid = len(resp.Errors) == 0
	writeJSON(w, http.StatusOK, resp)
}

// ListSchemaHistory is GET /streams/{id}/schema/history.
func (h *SchemasHandler) ListSchemaHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	streamID, ok := parseStreamID(w, r)
	if !ok {
		return
	}
	exists, err := h.Store.StreamExists(r.Context(), streamID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if !exists {
		writeJSONErr(w, http.StatusNotFound, "stream not found")
		return
	}
	items, err := h.Store.ListSchemaHistory(r.Context(), streamID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if items == nil {
		items = []models.StreamSchemaVersion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func parseStreamID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

// ─── BusControlSchemaRegistry ──────────────────────────────────────────

// BusControlSchemaRegistry is the production SchemaRegistry that
// delegates to libs/event-bus-control (which already ports the Rust
// event_bus_control::schema_registry crate verbatim).
//
// The wire surface for the streams endpoints is Avro, so the adapter
// hard-codes SchemaTypeAvro. If/when the streaming control plane
// admits JSON or Protobuf streams we can promote SchemaType to a
// per-call argument; until then this matches the Rust handler that
// also passes SchemaType::Avro for every call.
type BusControlSchemaRegistry struct{}

// Fingerprint returns the controlbus.Fingerprint over the schema bytes.
func (BusControlSchemaRegistry) Fingerprint(schemaJSON []byte) (string, error) {
	return controlbus.Fingerprint(controlbus.SchemaTypeAvro, string(schemaJSON))
}

// ValidatePayload validates a raw JSON sample against the schema.
func (BusControlSchemaRegistry) ValidatePayload(schemaJSON, sample []byte) error {
	return controlbus.ValidatePayload(controlbus.SchemaTypeAvro, string(schemaJSON), sample)
}

// CheckCompatibility checks Confluent-style compatibility between
// previous and candidate under mode. Mode is matched case-insensitively
// — production callers persist upper-case but the Rust impl is also
// permissive so we match that behaviour.
func (BusControlSchemaRegistry) CheckCompatibility(previous, candidate []byte, mode string) error {
	parsedMode, err := controlbus.ParseCompatibilityMode(mode)
	if err != nil {
		return err
	}
	return controlbus.CheckCompatibility(controlbus.SchemaTypeAvro, string(previous), string(candidate), parsedMode)
}

// ─── NoopSchemaRegistry ────────────────────────────────────────────────

// NoopSchemaRegistry is a permissive default kept for tests and
// environments that haven't wired the event-bus-control registry yet.
// Fingerprint hashes the JSON bytes with SHA-256, sample validation is
// a no-op, and compatibility checks succeed in NONE mode and otherwise
// require the schemas to round-trip equally — a pragmatic stand-in for
// the Rust implementation when the central registry isn't reachable.
type NoopSchemaRegistry struct{}

// Fingerprint returns a SHA-256 over the raw JSON bytes (or an error
// when the document is not valid JSON).
func (NoopSchemaRegistry) Fingerprint(schemaJSON []byte) (string, error) {
	if !json.Valid(schemaJSON) {
		return "", errors.New("schema is not valid JSON")
	}
	sum := sha256.Sum256(schemaJSON)
	return hex.EncodeToString(sum[:]), nil
}

// ValidatePayload is intentionally permissive in the noop registry.
func (NoopSchemaRegistry) ValidatePayload(_, _ []byte) error { return nil }

// CheckCompatibility returns nil in NONE mode, otherwise treats any
// structural drift as incompatible.
func (NoopSchemaRegistry) CheckCompatibility(previous, candidate []byte, mode string) error {
	if mode == "NONE" || mode == "none" || mode == "" {
		return nil
	}
	var p, c any
	if err := json.Unmarshal(previous, &p); err != nil {
		return errors.New("previous schema unparsable")
	}
	if err := json.Unmarshal(candidate, &c); err != nil {
		return errors.New("candidate schema unparsable")
	}
	pb, _ := json.Marshal(p)
	cb, _ := json.Marshal(c)
	if string(pb) != string(cb) {
		return errors.New("structural difference detected (Noop registry)")
	}
	return nil
}
