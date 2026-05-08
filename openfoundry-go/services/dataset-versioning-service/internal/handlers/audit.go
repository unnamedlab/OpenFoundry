package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
)

// AuditEvent mirrors Rust security::emit_audit's structured log payload for
// files/download and files/upload_url actions. Tests can inject AuditSink;
// production falls back to target-style slog fields consumed by audit pipelines.
type AuditEvent struct {
	Actor      string         `json:"actor"`
	Action     string         `json:"action"`
	DatasetRID string         `json:"dataset_rid"`
	Details    map[string]any `json:"details"`
}

func (h *Handlers) emitAudit(ctx context.Context, event AuditEvent) {
	if h.AuditSink != nil {
		h.AuditSink(ctx, event)
		return
	}
	details, _ := json.Marshal(event.Details)
	slog.InfoContext(ctx, "dataset versioning mutation", slog.String("target", "audit"), slog.String("actor", event.Actor), slog.String("action", event.Action), slog.String("dataset_rid", event.DatasetRID), slog.String("details", string(details)))
}
