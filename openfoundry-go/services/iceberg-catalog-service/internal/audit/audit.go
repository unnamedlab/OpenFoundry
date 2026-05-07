// Package audit emits structured slog events for iceberg-catalog-service.
//
// Mirrors the event vocabulary from the Rust `crate::audit` module. Each
// event carries a stable `audit_event=<name>` attribute that the Foundry
// audit collector drains into `audit-compliance-service`.
package audit

import (
	"log/slog"

	"github.com/google/uuid"
)

// OAuthTokenIssued logs an `iceberg.oauth_token.issued` event. `actor` is
// optional — for `client_credentials` grants the issued JWT identifies
// the service principal directly.
func OAuthTokenIssued(actor *uuid.UUID, grantType, scope string) {
	actorStr := ""
	if actor != nil {
		actorStr = actor.String()
	}
	slog.Info("iceberg oauth token issued",
		slog.String("audit_event", "iceberg.oauth_token.issued"),
		slog.String("actor", actorStr),
		slog.String("grant_type", grantType),
		slog.String("scope", scope),
	)
}

// APITokenCreated logs the issuance of a long-lived `ofty_*` API token.
func APITokenCreated(actor, tokenID uuid.UUID, scopes []string) {
	slog.Info("iceberg api token created",
		slog.String("audit_event", "iceberg.api_token.created"),
		slog.String("actor", actor.String()),
		slog.String("token_id", tokenID.String()),
		slog.Any("scopes", scopes),
	)
}

// MarkingsUpdated logs a transition of effective markings on a namespace
// or a table. `scope` is "namespace" or "table"; `before`/`after` are the
// effective marking-name lists.
func MarkingsUpdated(actor uuid.UUID, targetRID, scope string, before, after []string) {
	slog.Info("iceberg markings updated",
		slog.String("audit_event", "iceberg.markings.updated"),
		slog.String("actor", actor.String()),
		slog.String("target_rid", targetRID),
		slog.String("scope", scope),
		slog.Any("before", before),
		slog.Any("after", after),
	)
}

// MarkingsOverrideCreated fires once per newly-introduced *explicit*
// marking on a table (i.e. one not present in the inherited set).
func MarkingsOverrideCreated(actor uuid.UUID, tableRID, marking string) {
	slog.Info("iceberg markings override created",
		slog.String("audit_event", "iceberg.markings.override_created"),
		slog.String("actor", actor.String()),
		slog.String("table_rid", tableRID),
		slog.String("marking", marking),
	)
}

// AccessDenied logs a 403 emitted by the authz engine. Reason mirrors
// `authz.DenialReason.String()` so dashboards can split by cause.
func AccessDenied(actor uuid.UUID, targetRID, attemptedAction, reason string) {
	slog.Warn("iceberg access denied",
		slog.String("audit_event", "iceberg.access.denied"),
		slog.String("actor", actor.String()),
		slog.String("target_rid", targetRID),
		slog.String("attempted_action", attemptedAction),
		slog.String("reason", reason),
	)
}
