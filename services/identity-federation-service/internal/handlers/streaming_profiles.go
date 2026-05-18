package handlers

// StreamingProfile CRUD parked in identity-federation-service control-panel.
// ADR-0046 explains why the resource lives here instead of inside
// ingestion-replication-service (ADR-0035 P3 placement). Storage is
// process-local; durability arrives with the future control-panel
// persistence RFC.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// StreamingProfile is the wire shape of a reusable streaming-pipeline
// configuration: a named bundle of connector type, runtime knobs and
// source/destination wiring that pipelines reference via
// StreamingConfig.streaming_profile_id (see proto/pipeline/pipeline.proto).
type StreamingProfile struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	ConnectorType        string          `json:"connector_type"`
	Status               string          `json:"status"`
	Parallelism          uint32          `json:"parallelism"`
	WatermarkPolicy      string          `json:"watermark_policy"`
	CheckpointIntervalMs uint32          `json:"checkpoint_interval_ms"`
	SourceConfig         json.RawMessage `json:"source_config"`
	DestinationDatasetID string          `json:"destination_dataset_id,omitempty"`
	LastEventAt          *time.Time      `json:"last_event_at,omitempty"`
	ThroughputEps        *float64        `json:"throughput_eps,omitempty"`
	CreatedBy            *string         `json:"created_by,omitempty"`
	CreatedAt            *time.Time      `json:"created_at,omitempty"`
	UpdatedBy            *string         `json:"updated_by,omitempty"`
	UpdatedAt            *time.Time      `json:"updated_at,omitempty"`
}

// CreateStreamingProfileRequest is the body of POST
// /api/v1/control-panel/streaming-profiles. ID is optional and will be
// minted server-side when omitted.
type CreateStreamingProfileRequest struct {
	ID                   string          `json:"id,omitempty"`
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	ConnectorType        string          `json:"connector_type"`
	Status               string          `json:"status,omitempty"`
	Parallelism          uint32          `json:"parallelism,omitempty"`
	WatermarkPolicy      string          `json:"watermark_policy,omitempty"`
	CheckpointIntervalMs uint32          `json:"checkpoint_interval_ms,omitempty"`
	SourceConfig         json.RawMessage `json:"source_config,omitempty"`
	DestinationDatasetID string          `json:"destination_dataset_id,omitempty"`
}

// UpdateStreamingProfileRequest is the body of PATCH
// /api/v1/control-panel/streaming-profiles/{id}. Every field is a
// pointer so partial updates leave untouched fields alone. Status is
// intentionally read-only here — admins use the :pause / :resume
// endpoints to move a profile through its lifecycle.
type UpdateStreamingProfileRequest struct {
	Name                 *string          `json:"name,omitempty"`
	Description          *string          `json:"description,omitempty"`
	ConnectorType        *string          `json:"connector_type,omitempty"`
	Parallelism          *uint32          `json:"parallelism,omitempty"`
	WatermarkPolicy      *string          `json:"watermark_policy,omitempty"`
	CheckpointIntervalMs *uint32          `json:"checkpoint_interval_ms,omitempty"`
	SourceConfig         *json.RawMessage `json:"source_config,omitempty"`
	DestinationDatasetID *string          `json:"destination_dataset_id,omitempty"`
}

// ListStreamingProfilesResponse wraps the slice so a follow-up can
// add `total` / `next_cursor` without breaking SDKs — same envelope
// shape SG.4 chose for ListUsersResponse.
type ListStreamingProfilesResponse struct {
	Items []StreamingProfile `json:"items"`
	Total int                `json:"total"`
}

// Streaming-profile lifecycle states. Mirrors the Foundry vocabulary
// used by the migration checklist (Active / Paused / Error).
const (
	StreamingProfileStatusActive = "active"
	StreamingProfileStatusPaused = "paused"
	StreamingProfileStatusError  = "error"
	StreamingProfileStatusDraft  = "draft"
)

// allowedStreamingConnectorTypes is the curated allowlist of connector
// kinds a profile can reference. Mirrors the catalogue exposed by
// connector-management-service at GET /api/v1/data-connection/streaming-sources;
// if that catalogue grows, this list must be updated in lockstep
// (see ADR-0046, "manual sync point").
var allowedStreamingConnectorTypes = map[string]struct{}{
	"streaming_kafka":    {},
	"streaming_kinesis":  {},
	"streaming_sqs":      {},
	"streaming_pubsub":   {},
	"streaming_aveva_pi": {},
	"streaming_external": {},
}

var allowedStreamingProfileStatuses = map[string]struct{}{
	StreamingProfileStatusActive: {},
	StreamingProfileStatusPaused: {},
	StreamingProfileStatusError:  {},
	StreamingProfileStatusDraft:  {},
}

var allowedWatermarkPolicies = map[string]struct{}{
	"none":                     {},
	"bounded_out_of_orderness": {},
	"monotonic_event_time":     {},
	"ingestion_time":           {},
}

// ListStreamingProfiles is GET /api/v1/control-panel/streaming-profiles.
// Supports optional `status` and `connector_type` filters via query
// string. Returns the items sorted by name (case-insensitive).
func (h *ControlPanel) ListStreamingProfiles(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireControlPanelRead(w, r); !ok {
		return
	}
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	connectorFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("connector_type")))
	h.mu.RLock()
	items := make([]StreamingProfile, 0, len(h.streamingProfiles))
	for _, p := range h.streamingProfiles {
		if statusFilter != "" && p.Status != statusFilter {
			continue
		}
		if connectorFilter != "" && p.ConnectorType != connectorFilter {
			continue
		}
		items = append(items, cloneStreamingProfile(p))
	}
	h.mu.RUnlock()
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	writeJSON(w, http.StatusOK, ListStreamingProfilesResponse{Items: items, Total: len(items)})
}

// GetStreamingProfile is GET /api/v1/control-panel/streaming-profiles/{id}.
func (h *ControlPanel) GetStreamingProfile(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireControlPanelRead(w, r); !ok {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "id is required")
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	idx := indexOfStreamingProfile(h.streamingProfiles, id)
	if idx < 0 {
		writeJSONErr(w, http.StatusNotFound, "streaming profile not found")
		return
	}
	writeJSON(w, http.StatusOK, cloneStreamingProfile(h.streamingProfiles[idx]))
}

// CreateStreamingProfile is POST /api/v1/control-panel/streaming-profiles.
func (h *ControlPanel) CreateStreamingProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireControlPanelWrite(w, r)
	if !ok {
		return
	}
	var body CreateStreamingProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	profile, err := buildStreamingProfileFromCreate(body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC()
	actor := applicationAccessActor(claims)
	profile.CreatedBy = &actor
	profile.CreatedAt = &now
	profile.UpdatedBy = &actor
	profile.UpdatedAt = &now

	h.mu.Lock()
	defer h.mu.Unlock()
	if indexOfStreamingProfile(h.streamingProfiles, profile.ID) >= 0 {
		writeJSONErr(w, http.StatusConflict, "streaming profile id already exists")
		return
	}
	if nameTaken(h.streamingProfiles, profile.Name, "") {
		writeJSONErr(w, http.StatusConflict, "streaming profile name already exists")
		return
	}
	h.streamingProfiles = append(h.streamingProfiles, profile)
	writeJSON(w, http.StatusCreated, cloneStreamingProfile(profile))
}

// UpdateStreamingProfile is PATCH /api/v1/control-panel/streaming-profiles/{id}.
func (h *ControlPanel) UpdateStreamingProfile(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireControlPanelWrite(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "id is required")
		return
	}
	var body UpdateStreamingProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	idx := indexOfStreamingProfile(h.streamingProfiles, id)
	if idx < 0 {
		writeJSONErr(w, http.StatusNotFound, "streaming profile not found")
		return
	}
	merged := h.streamingProfiles[idx]
	if err := applyStreamingProfileUpdate(&merged, body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if nameTaken(h.streamingProfiles, merged.Name, merged.ID) {
		writeJSONErr(w, http.StatusConflict, "streaming profile name already exists")
		return
	}
	now := time.Now().UTC()
	actor := applicationAccessActor(claims)
	merged.UpdatedBy = &actor
	merged.UpdatedAt = &now
	h.streamingProfiles[idx] = merged
	writeJSON(w, http.StatusOK, cloneStreamingProfile(merged))
}

// DeleteStreamingProfile is DELETE /api/v1/control-panel/streaming-profiles/{id}.
func (h *ControlPanel) DeleteStreamingProfile(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireControlPanelWrite(w, r); !ok {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "id is required")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	idx := indexOfStreamingProfile(h.streamingProfiles, id)
	if idx < 0 {
		writeJSONErr(w, http.StatusNotFound, "streaming profile not found")
		return
	}
	h.streamingProfiles = append(h.streamingProfiles[:idx], h.streamingProfiles[idx+1:]...)
	w.WriteHeader(http.StatusNoContent)
}

// PauseStreamingProfile is POST /api/v1/control-panel/streaming-profiles/{id}:pause.
// Idempotent: pausing an already-paused profile is a no-op success.
func (h *ControlPanel) PauseStreamingProfile(w http.ResponseWriter, r *http.Request) {
	h.transitionStreamingProfile(w, r, StreamingProfileStatusPaused)
}

// ResumeStreamingProfile is POST /api/v1/control-panel/streaming-profiles/{id}:resume.
// Idempotent: resuming an active profile is a no-op success.
func (h *ControlPanel) ResumeStreamingProfile(w http.ResponseWriter, r *http.Request) {
	h.transitionStreamingProfile(w, r, StreamingProfileStatusActive)
}

func (h *ControlPanel) transitionStreamingProfile(w http.ResponseWriter, r *http.Request, target string) {
	claims, ok := requireControlPanelWrite(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSONErr(w, http.StatusBadRequest, "id is required")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	idx := indexOfStreamingProfile(h.streamingProfiles, id)
	if idx < 0 {
		writeJSONErr(w, http.StatusNotFound, "streaming profile not found")
		return
	}
	merged := h.streamingProfiles[idx]
	if merged.Status == target {
		writeJSON(w, http.StatusOK, cloneStreamingProfile(merged))
		return
	}
	if merged.Status == StreamingProfileStatusError {
		writeJSONErr(w, http.StatusConflict, "profile is in error state; update configuration before resuming")
		return
	}
	merged.Status = target
	now := time.Now().UTC()
	actor := applicationAccessActor(claims)
	merged.UpdatedBy = &actor
	merged.UpdatedAt = &now
	h.streamingProfiles[idx] = merged
	writeJSON(w, http.StatusOK, cloneStreamingProfile(merged))
}

func buildStreamingProfileFromCreate(body CreateStreamingProfileRequest) (StreamingProfile, error) {
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return StreamingProfile{}, fmt.Errorf("name is required")
	}
	connector := strings.ToLower(strings.TrimSpace(body.ConnectorType))
	if connector == "" {
		return StreamingProfile{}, fmt.Errorf("connector_type is required")
	}
	if _, ok := allowedStreamingConnectorTypes[connector]; !ok {
		return StreamingProfile{}, fmt.Errorf("connector_type %q is not supported", body.ConnectorType)
	}

	status := strings.ToLower(strings.TrimSpace(body.Status))
	if status == "" {
		status = StreamingProfileStatusDraft
	}
	if _, ok := allowedStreamingProfileStatuses[status]; !ok {
		return StreamingProfile{}, fmt.Errorf("status %q is not supported", body.Status)
	}

	watermark := strings.ToLower(strings.TrimSpace(body.WatermarkPolicy))
	if watermark == "" {
		watermark = "none"
	}
	if _, ok := allowedWatermarkPolicies[watermark]; !ok {
		return StreamingProfile{}, fmt.Errorf("watermark_policy %q is not supported", body.WatermarkPolicy)
	}

	sourceConfig, err := normalizeStreamingSourceConfig(body.SourceConfig)
	if err != nil {
		return StreamingProfile{}, err
	}

	id := strings.TrimSpace(body.ID)
	if id == "" {
		id = uuid.NewString()
	}

	return StreamingProfile{
		ID:                   id,
		Name:                 name,
		Description:          strings.TrimSpace(body.Description),
		ConnectorType:        connector,
		Status:               status,
		Parallelism:          body.Parallelism,
		WatermarkPolicy:      watermark,
		CheckpointIntervalMs: body.CheckpointIntervalMs,
		SourceConfig:         sourceConfig,
		DestinationDatasetID: strings.TrimSpace(body.DestinationDatasetID),
	}, nil
}

func applyStreamingProfileUpdate(target *StreamingProfile, body UpdateStreamingProfileRequest) error {
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" {
			return fmt.Errorf("name cannot be empty")
		}
		target.Name = name
	}
	if body.Description != nil {
		target.Description = strings.TrimSpace(*body.Description)
	}
	if body.ConnectorType != nil {
		connector := strings.ToLower(strings.TrimSpace(*body.ConnectorType))
		if _, ok := allowedStreamingConnectorTypes[connector]; !ok {
			return fmt.Errorf("connector_type %q is not supported", *body.ConnectorType)
		}
		target.ConnectorType = connector
	}
	if body.Parallelism != nil {
		target.Parallelism = *body.Parallelism
	}
	if body.WatermarkPolicy != nil {
		watermark := strings.ToLower(strings.TrimSpace(*body.WatermarkPolicy))
		if _, ok := allowedWatermarkPolicies[watermark]; !ok {
			return fmt.Errorf("watermark_policy %q is not supported", *body.WatermarkPolicy)
		}
		target.WatermarkPolicy = watermark
	}
	if body.CheckpointIntervalMs != nil {
		target.CheckpointIntervalMs = *body.CheckpointIntervalMs
	}
	if body.SourceConfig != nil {
		sourceConfig, err := normalizeStreamingSourceConfig(*body.SourceConfig)
		if err != nil {
			return err
		}
		target.SourceConfig = sourceConfig
	}
	if body.DestinationDatasetID != nil {
		target.DestinationDatasetID = strings.TrimSpace(*body.DestinationDatasetID)
	}
	return nil
}

// normalizeStreamingSourceConfig enforces that the source-config blob
// is either absent or a JSON object — accepting arbitrary scalars
// would let admins post nonsense the runtime cannot consume.
func normalizeStreamingSourceConfig(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage(`{}`), nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("source_config must be a JSON object")
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("source_config is not valid JSON: %w", err)
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out, nil
}

func indexOfStreamingProfile(items []StreamingProfile, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

func nameTaken(items []StreamingProfile, name, excludeID string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, item := range items {
		if item.ID == excludeID {
			continue
		}
		if strings.ToLower(item.Name) == lower {
			return true
		}
	}
	return false
}

func cloneStreamingProfile(p StreamingProfile) StreamingProfile {
	out := p
	if len(p.SourceConfig) > 0 {
		buf := make([]byte, len(p.SourceConfig))
		copy(buf, p.SourceConfig)
		out.SourceConfig = buf
	}
	if p.LastEventAt != nil {
		t := *p.LastEventAt
		out.LastEventAt = &t
	}
	if p.ThroughputEps != nil {
		v := *p.ThroughputEps
		out.ThroughputEps = &v
	}
	if p.CreatedBy != nil {
		v := *p.CreatedBy
		out.CreatedBy = &v
	}
	if p.CreatedAt != nil {
		v := *p.CreatedAt
		out.CreatedAt = &v
	}
	if p.UpdatedBy != nil {
		v := *p.UpdatedBy
		out.UpdatedBy = &v
	}
	if p.UpdatedAt != nil {
		v := *p.UpdatedAt
		out.UpdatedAt = &v
	}
	return out
}

