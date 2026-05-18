// Package audit centralises the cipher-service's audit-trail
// emissions. It maps lifecycle moments (create / rotate / retire /
// bulk_decrypt) to typed audittrail.EventKind values and emits them
// through the shared libs/audit-trail Emitter interface.
//
// The cipher-specific EventKind constants are declared locally
// because Milestone A does not yet extend libs/audit-trail's variant
// set (that's a coordinated rollout with audit-compliance-service
// and audit-sink). The shared EventKind type is a string alias, so
// the audit envelope still publishes correctly and downstream
// consumers can filter on the dotted name.
package audit

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
)

// EventKind values emitted by the cipher service. Pinned strings so
// SIEM filters never need to chase a rename.
const (
	EventCipherKeyCreated  audittrail.EventKind = "cipher.key.created"
	EventCipherKeyRotated  audittrail.EventKind = "cipher.key.rotated"
	EventCipherKeyRetired  audittrail.EventKind = "cipher.key.retired"
	EventCipherKeyRevoked  audittrail.EventKind = "cipher.key.revoked"
	EventCipherBulkDecrypt audittrail.EventKind = "cipher.bulk_decrypt"
	EventCipherEncrypt     audittrail.EventKind = "cipher.encrypt"
	EventCipherDecrypt     audittrail.EventKind = "cipher.decrypt"
	EventCipherTokenize    audittrail.EventKind = "cipher.tokenize"
	EventCipherBatch       audittrail.EventKind = "cipher.batch"
)

// SourceService is the canonical OpenLineage `producer` facet for
// cipher-service emissions. Stable so dashboards can pin the producer
// without grepping config.
const SourceService = "cipher-service"

// Recorder is the thin wrapper handlers call. Lifecycle events remain
// best-effort because the corresponding state changes are not emitted through
// a transactional outbox here. Critical data operations (encrypt, decrypt,
// tokenize, and batch summaries) return audit delivery errors so production
// handlers can fail closed before releasing sensitive material.
type Recorder struct {
	Emitter         audittrail.Emitter
	Log             *slog.Logger
	RequireDelivery bool
}

// NewRecorder wires a Recorder against the shared audit emitter.
// `log` is optional — when nil emissions still succeed but failures
// are dropped silently. Callers in main() should pass the service
// logger so a saturated bus surfaces in service logs.
func NewRecorder(e audittrail.Emitter, log *slog.Logger) *Recorder {
	return &Recorder{Emitter: e, Log: log}
}

// NewRecorderWithPolicy wires a Recorder and controls whether critical audit
// operations must be delivered. Production callers should set
// requireDelivery=true; dev/test may explicitly opt into fail-open behavior.
func NewRecorderWithPolicy(e audittrail.Emitter, log *slog.Logger, requireDelivery bool) *Recorder {
	return &Recorder{Emitter: e, Log: log, RequireDelivery: requireDelivery}
}

// keyResourceRID builds the canonical RID for a cipher key. Wire
// shape matches the Compass RID style used elsewhere in the platform
// so downstream lineage tooling can stitch events by RID.
func keyResourceRID(keyID uuid.UUID) string {
	return "ri.cipher.main.key." + keyID.String()
}

// KeyCreated records a CIP.2 lifecycle event for a freshly-minted key.
// `markings` is the marking set declared at key creation time (empty
// when the caller did not request any).
func (r *Recorder) KeyCreated(ctx context.Context, actorID uuid.UUID, tenantID, keyID uuid.UUID, alias string, markings []string) {
	r.emit(ctx, actorID, audittrail.AuditEvent{
		Kind:            EventCipherKeyCreated,
		ResourceRID:     keyResourceRID(keyID),
		ProjectRID:      tenantProjectRID(tenantID),
		MarkingsAtEvent: markings,
		// `Name` is the only existing payload slot that fits the
		// alias semantically; we re-purpose it here rather than
		// touching the libs/audit-trail wire shape.
		Name: alias,
	})
}

// KeyRotated records a CIP.16 rotation event.
func (r *Recorder) KeyRotated(ctx context.Context, actorID uuid.UUID, tenantID, keyID uuid.UUID, newVersion uint32, markings []string) {
	r.emit(ctx, actorID, audittrail.AuditEvent{
		Kind:            EventCipherKeyRotated,
		ResourceRID:     keyResourceRID(keyID),
		ProjectRID:      tenantProjectRID(tenantID),
		MarkingsAtEvent: markings,
		// We surface the new version in `Branch` for the same
		// reason — keeps the wire shape unchanged while still
		// carrying the data SIEM needs.
		Branch: "v" + uintToStr(newVersion),
	})
}

// KeyRetired records a CIP.17 retire event. Decrypt-only state is
// reversible only by admins; auditors care most about who flipped the
// switch.
func (r *Recorder) KeyRetired(ctx context.Context, actorID uuid.UUID, tenantID, keyID uuid.UUID, markings []string) {
	r.emit(ctx, actorID, audittrail.AuditEvent{
		Kind:            EventCipherKeyRetired,
		ResourceRID:     keyResourceRID(keyID),
		ProjectRID:      tenantProjectRID(tenantID),
		MarkingsAtEvent: markings,
	})
}

// KeyRevoked records a CIP.17 revoke event.
func (r *Recorder) KeyRevoked(ctx context.Context, actorID uuid.UUID, tenantID, keyID uuid.UUID, markings []string) {
	r.emit(ctx, actorID, audittrail.AuditEvent{
		Kind:            EventCipherKeyRevoked,
		ResourceRID:     keyResourceRID(keyID),
		ProjectRID:      tenantProjectRID(tenantID),
		MarkingsAtEvent: markings,
	})
}

// BulkDecrypt records a CIP.8 audit event for a multi-item decrypt
// batch. Per-item events would amplify volume past audit-sink's
// budget; the aggregate form keeps the actor + key + count.
func (r *Recorder) BulkDecrypt(ctx context.Context, actorID uuid.UUID, tenantID uuid.UUID, items int) error {
	return r.emit(ctx, actorID, audittrail.AuditEvent{
		Kind:        EventCipherBulkDecrypt,
		ResourceRID: tenantProjectRID(tenantID),
		ProjectRID:  tenantProjectRID(tenantID),
		// We use Path to record the request shape (item count) so the
		// envelope round-trips through the existing audittrail.Build
		// path without a schema change.
		Path: "items=" + uintToStr(uint32(items)),
	})
}

// Batch records CIP.21 aggregate batch summaries without emitting one audit envelope per item.
func (r *Recorder) Batch(ctx context.Context, actorID uuid.UUID, tenantID uuid.UUID, operation string, items int, failures int, requestID string) error {
	return r.emitWithRequest(ctx, actorID, requestID, audittrail.AuditEvent{
		Kind:        EventCipherBatch,
		ResourceRID: tenantProjectRID(tenantID),
		ProjectRID:  tenantProjectRID(tenantID),
		Name:        operation,
		Path:        "items=" + uintToStr(uint32(items)) + ";failures=" + uintToStr(uint32(failures)),
	})
}

// Operation records CIP.8 per-operation encrypt/decrypt audit details.
func (r *Recorder) Operation(ctx context.Context, actorID uuid.UUID, tenantID uuid.UUID, keyID uuid.UUID, operation string, algorithm string, resourceRID string, success bool, markingResult string, requestID string, markings []string) error {
	kind := EventCipherEncrypt
	switch operation {
	case "decrypt":
		kind = EventCipherDecrypt
	case "tokenize":
		kind = EventCipherTokenize
	}
	if resourceRID == "" {
		resourceRID = keyResourceRID(keyID)
	}
	status := "failure"
	if success {
		status = "success"
	}
	return r.emitWithRequest(ctx, actorID, requestID, audittrail.AuditEvent{
		Kind:            kind,
		ResourceRID:     resourceRID,
		ProjectRID:      tenantProjectRID(tenantID),
		MarkingsAtEvent: markings,
		Name:            algorithm,
		AccessPattern:   status,
		Path:            "key=" + keyID.String() + ";marking=" + markingResult,
	})
}

// emit is the single point of contact with audittrail.Emitter so
// failure handling and actor injection stays consistent across events.
func (r *Recorder) emit(ctx context.Context, actorID uuid.UUID, event audittrail.AuditEvent) error {
	return r.emitWithRequest(ctx, actorID, "", event)
}

func (r *Recorder) emitWithRequest(ctx context.Context, actorID uuid.UUID, requestID string, event audittrail.AuditEvent) error {
	if r == nil || r.Emitter == nil {
		return r.deliveryError(errors.New("audit emitter is not configured"), event)
	}
	auditCtx := audittrail.AuditContext{
		ActorID:       actorID.String(),
		RequestID:     requestID,
		SourceService: SourceService,
	}
	if err := r.Emitter.Emit(ctx, event, auditCtx); err != nil {
		return r.deliveryError(err, event)
	}
	return nil
}

func (r *Recorder) deliveryError(err error, event audittrail.AuditEvent) error {
	if err != nil && r != nil && r.Log != nil {
		r.Log.Warn("audit emit failed",
			slog.String("kind", string(event.Kind)),
			slog.String("resource", event.ResourceRID),
			slog.String("error", err.Error()))
	}
	if r != nil && r.RequireDelivery {
		return err
	}
	return nil
}

// tenantProjectRID is the placeholder RID we stamp into ProjectRID
// until the cipher service learns about projects (CIP.13). Stable so
// audit-sink groups events by tenant in the meantime.
func tenantProjectRID(tenantID uuid.UUID) string {
	return "ri.cipher.main.tenant." + tenantID.String()
}

// uintToStr is a tiny zero-alloc-when-small uint stringifier; the
// emit path is hot, so we avoid fmt.Sprintf here.
func uintToStr(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
