package cassandrakernel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ActionLogStore (P2.5.6) is the Cassandra-backed implementation of
// repos.ActionLogStore mirroring libs/cassandra-kernel/src/repos.rs::
// CassandraActionLogStore. The largest of the 5 stores — backs the
// `actions_log` keyspace with 4 tables:
//
//   - actions_by_event (tenant, event_id) — idempotent dedupe via
//     INSERT IF NOT EXISTS.
//   - actions_log (tenant, day_bucket, applied_at, action_id) —
//     primary time-ordered log used by ListRecent.
//   - actions_by_object (tenant, target_object_id, applied_at) —
//     used by ListForObject.
//   - actions_by_action (tenant, action_id, day_bucket) — used by
//     ListForAction.
//
// Append is atomic and idempotent on (tenant, event_id):
// implementations may derive a deterministic event_id when the
// caller leaves it empty (UUIDv5 over a stable canonical projection).
//
// ListRecent + ListForAction iterate day buckets backwards across a
// 90-day lookback window, maintaining a JSON continuation token
// that interleaves Cassandra paging-state with day cursor and
// scanned-count.
type ActionLogStore struct {
	session  *gocql.Session
	keyspace string
}

// actionLogLookbackDays mirrors ACTION_LOG_LOOKBACK_DAYS in Rust.
const actionLogLookbackDays uint8 = 90

// cqlDateEpochOffset mirrors CqlDate's "days since 1970-01-01 +
// 2^31" encoding. Used in the day_bucket round-trip.
const cqlDateEpochOffset uint32 = 1 << 31

// actionLogNamespaceUUID mirrors the Rust namespace
// `Uuid::from_u128(0x6d1d_30aa_4d0c_5da9_9d8d_e8280a5a1c3f)` used by
// derive_event_id.
var actionLogNamespaceUUID = uuid.MustParse("6d1d30aa-4d0c-5da9-9d8d-e8280a5a1c3f")

// NewActionLogStore builds a store bound to the standard
// `actions_log` keyspace.
func NewActionLogStore(session *gocql.Session) *ActionLogStore {
	return &ActionLogStore{session: session, keyspace: "actions_log"}
}

// NewActionLogStoreWithKeyspace allows a custom keyspace.
func NewActionLogStoreWithKeyspace(session *gocql.Session, keyspace string) *ActionLogStore {
	return &ActionLogStore{session: session, keyspace: keyspace}
}

// Compile-time interface assertion.
var _ repos.ActionLogStore = (*ActionLogStore)(nil)

// actionRecentToken is the per-call cursor we encode into the
// PagedResult.NextToken when ListRecent / ListForAction need to be
// resumed across day boundaries. JSON+base64 keeps it opaque to
// callers but matches the Rust serde shape so a token issued by
// one runtime can be consumed by the other.
type actionRecentToken struct {
	Day         uint32  `json:"day"`
	DaysScanned uint8   `json:"days_scanned"`
	Paging      *string `json:"paging,omitempty"`
}

// --- prepared CQL strings ----------------------------------------------

func (s *ActionLogStore) cqlInsertEvent() string {
	return fmt.Sprintf(
		`INSERT INTO %s.actions_by_event
            (tenant, event_id, action_id, kind, actor_id, subject,
             target_object_id, payload, applied_at, day_bucket)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS`, s.keyspace)
}

func (s *ActionLogStore) cqlInsertLog() string {
	return fmt.Sprintf(
		`INSERT INTO %s.actions_log
            (tenant, day_bucket, applied_at, action_id, kind, actor_id,
             subject, target_object_id, target_type_id, payload, status,
             failure_type, duration_ms, event_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.keyspace)
}

func (s *ActionLogStore) cqlInsertByObject() string {
	return fmt.Sprintf(
		`INSERT INTO %s.actions_by_object
            (tenant, target_object_id, applied_at, action_id, kind,
             actor_id, subject, payload, event_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.keyspace)
}

func (s *ActionLogStore) cqlInsertByAction() string {
	return fmt.Sprintf(
		`INSERT INTO %s.actions_by_action
            (tenant, action_id, day_bucket, applied_at, event_id, kind,
             actor_id, subject, target_object_id, payload)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.keyspace)
}

func (s *ActionLogStore) cqlSelectEvent() string {
	return fmt.Sprintf(
		`SELECT action_id, kind, actor_id, subject, target_object_id,
                payload, applied_at, day_bucket
           FROM %s.actions_by_event WHERE tenant = ? AND event_id = ?`, s.keyspace)
}

func (s *ActionLogStore) cqlSelectRecent() string {
	return fmt.Sprintf(
		`SELECT applied_at, action_id, kind, actor_id, subject,
                target_object_id, payload, event_id
           FROM %s.actions_log WHERE tenant = ? AND day_bucket = ?`, s.keyspace)
}

func (s *ActionLogStore) cqlSelectByObject() string {
	return fmt.Sprintf(
		`SELECT applied_at, action_id, kind, actor_id, subject,
                payload, event_id
           FROM %s.actions_by_object WHERE tenant = ? AND target_object_id = ?`, s.keyspace)
}

func (s *ActionLogStore) cqlSelectByAction() string {
	return fmt.Sprintf(
		`SELECT applied_at, event_id, kind, actor_id, subject,
                target_object_id, payload
           FROM %s.actions_by_action WHERE tenant = ? AND action_id = ? AND day_bucket = ?`, s.keyspace)
}

// --- token codec --------------------------------------------------------

func encodeRecentToken(t actionRecentToken) (string, error) {
	raw, err := json.Marshal(t)
	if err != nil {
		return "", repos.Backendf("recent token encode failed: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// decodeRecentToken returns the parsed token or — on a nil input —
// a fresh cursor positioned at today's day bucket with 0 scanned.
func decodeRecentToken(token *string) (actionRecentToken, error) {
	if token == nil {
		return actionRecentToken{
			Day:         msToDayBucket(time.Now().UnixMilli()),
			DaysScanned: 0,
		}, nil
	}
	raw, err := base64.StdEncoding.DecodeString(*token)
	if err != nil {
		return actionRecentToken{}, repos.Invalidf("malformed action-log page token: %v", err)
	}
	var out actionRecentToken
	if err := json.Unmarshal(raw, &out); err != nil {
		return actionRecentToken{}, repos.Invalidf("malformed action-log page token: %v", err)
	}
	return out, nil
}

// --- day bucket round-trip ---------------------------------------------

func msToDayBucket(ms int64) uint32 {
	days := ms / 86_400_000
	if ms < 0 && ms%86_400_000 != 0 {
		days--
	}
	return uint32(int64(cqlDateEpochOffset) + days)
}

func dayBucketToTime(d uint32) time.Time {
	days := int64(d) - int64(cqlDateEpochOffset)
	return time.Unix(days*86_400, 0).UTC()
}

// --- entry ↔ row plumbing ----------------------------------------------

// actionLogRow is the canonical representation of an action-log row
// that flows through Append's idempotent write path.
type actionLogRow struct {
	tenant         repos.TenantId
	eventID        string
	actionID       gocql.UUID
	kind           string
	actorID        *gocql.UUID
	subject        string
	targetObjectID *gocql.UUID
	payload        string // canonicalised JSON
	appliedAt      time.Time
	dayBucket      uint32
}

func (s *ActionLogStore) rowFromEntry(entry repos.ActionLogEntry) (*actionLogRow, error) {
	payload, err := canonicalJSON(entry.Payload)
	if err != nil {
		return nil, invalidArgf("action payload is not serialisable: %v", err)
	}
	eventID := deriveEventID(entry, payload)
	actionID, err := parseUUID("action_id", entry.ActionID)
	if err != nil {
		return nil, err
	}
	var targetObjectID *gocql.UUID
	if entry.Object != nil {
		v, err := parseUUID("object", string(*entry.Object))
		if err != nil {
			return nil, err
		}
		targetObjectID = &v
	}
	// Try to pluck a UUID actor_id from the subject string — the
	// Rust impl falls back to actor=Some(uuid) when subject parses.
	var actorID *gocql.UUID
	if u, err := uuid.Parse(entry.Subject); err == nil {
		v := gocql.UUID(u)
		actorID = &v
	}
	return &actionLogRow{
		tenant:         entry.Tenant,
		eventID:        eventID,
		actionID:       actionID,
		kind:           entry.Kind,
		actorID:        actorID,
		subject:        entry.Subject,
		targetObjectID: targetObjectID,
		payload:        payload,
		appliedAt:      time.UnixMilli(entry.RecordedAtMs).UTC(),
		dayBucket:      msToDayBucket(entry.RecordedAtMs),
	}, nil
}

// deriveEventID mirrors fn derive_event_id. Priority:
//  1. entry.event_id (trimmed, when non-empty).
//  2. payload.event_id / idempotency_key / executionId / runId
//     (any non-empty string), prefixed with `<kind>:`.
//  3. UUIDv5 over a stable canonical projection of the entry +
//     payload (so the SAME action issued twice with the same body
//     deduplicates even when the caller didn't supply an event_id).
func deriveEventID(entry repos.ActionLogEntry, canonicalPayload string) string {
	if entry.EventID != nil {
		trimmed := strings.TrimSpace(*entry.EventID)
		if trimmed != "" {
			return trimmed
		}
	}
	if id := eventIDFromPayload(entry.Kind, entry.Payload); id != "" {
		return id
	}
	objectStr := ""
	if entry.Object != nil {
		objectStr = string(*entry.Object)
	}
	stable := stablePayloadProjection(entry.Payload)
	payloadMaterial := canonicalPayload
	if stable != nil {
		if b, err := json.Marshal(stable); err == nil {
			payloadMaterial = string(b)
		}
	}
	material := fmt.Sprintf("of-action-log-v1\x00%s\x00%s\x00%s\x00%s\x00%s",
		entry.Tenant, entry.Kind, entry.Subject, objectStr, payloadMaterial)
	return uuid.NewSHA1(actionLogNamespaceUUID, []byte(material)).String()
}

// eventIDFromPayload tries the well-known idempotency keys and
// returns "<kind>:<value>" when one is present. Mirrors fn
// event_id_from_payload.
func eventIDFromPayload(kind string, payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return ""
	}
	for _, key := range []string{"event_id", "idempotency_key", "idempotencyKey", "execution_id", "executionId", "run_id", "runId"} {
		if raw, ok := obj[key]; ok {
			if s, ok := raw.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					return kind + ":" + trimmed
				}
			}
		}
	}
	return ""
}

// stablePayloadProjection mirrors fn stable_payload_projection —
// for the two known shapes (webhook-effect / action-execution),
// project a deterministic subset that's safe to hash for UUIDv5
// derivation. Returns nil when neither shape matches.
func stablePayloadProjection(payload json.RawMessage) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return nil
	}
	_, hasSideEffect := obj["side_effect_type"]
	_, hasWebhook := obj["webhook_id"]
	if hasSideEffect && hasWebhook {
		return map[string]any{
			"action_type_id":   obj["action_type_id"],
			"side_effect_type": obj["side_effect_type"],
			"webhook_id":       obj["webhook_id"],
			"status":           obj["status"],
		}
	}
	_, hasActionType := obj["action_type_id"]
	_, hasParameters := obj["parameters"]
	if hasActionType && hasParameters {
		return map[string]any{
			"action_type_id":   obj["action_type_id"],
			"target_object_id": obj["target_object_id"],
			"parameters":       obj["parameters"],
			"status":           obj["status"],
			"failure_type":     obj["failure_type"],
		}
	}
	return nil
}

// subjectFrom prefers the explicit subject string; falls back to
// actor_id.String() when subject is empty. Mirrors fn subject_from.
func subjectFrom(actorID *gocql.UUID, subject *string) string {
	if subject != nil {
		trimmed := strings.TrimSpace(*subject)
		if trimmed != "" {
			return *subject
		}
	}
	if actorID != nil {
		return actorID.String()
	}
	return ""
}

// entryFromRow assembles an ActionLogEntry from the projected
// columns of any of the read-side tables. Mirrors fn entry_from_row.
func entryFromRow(
	tenant repos.TenantId,
	eventID *string,
	actionID gocql.UUID,
	kind string,
	actorID *gocql.UUID,
	subject *string,
	objectID *gocql.UUID,
	payload string,
	appliedAt time.Time,
) (repos.ActionLogEntry, error) {
	if !json.Valid([]byte(payload)) {
		return repos.ActionLogEntry{}, repos.Backendf("invalid stored action payload JSON: not parseable")
	}
	var object *repos.ObjectId
	if objectID != nil {
		oid := repos.ObjectId(objectID.String())
		object = &oid
	}
	return repos.ActionLogEntry{
		Tenant:       tenant,
		EventID:      eventID,
		ActionID:     actionID.String(),
		Kind:         kind,
		Subject:      subjectFrom(actorID, subject),
		Object:       object,
		Payload:      json.RawMessage([]byte(payload)),
		RecordedAtMs: appliedAt.UnixMilli(),
	}, nil
}

// --- write path --------------------------------------------------------

func (s *ActionLogStore) putEventRow(ctx context.Context, row *actionLogRow) (bool, error) {
	q := s.session.Query(s.cqlInsertEvent(),
		tenantStr(row.tenant), row.eventID, row.actionID, row.kind,
		row.actorID, row.subject, row.targetObjectID, row.payload,
		row.appliedAt, dayBucketToTime(row.dayBucket)).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).
		SerialConsistency(gocql.LocalSerial)
	rowMap := map[string]any{}
	applied, err := q.MapScanCAS(rowMap)
	if err != nil {
		return false, driverErr(err)
	}
	return applied, nil
}

// readEventRow re-reads the canonical row that was inserted by a
// previous Append call (idempotent path). Surfaces RepoBackend if
// the row is mysteriously missing.
func (s *ActionLogStore) readEventRow(
	ctx context.Context,
	tenant repos.TenantId,
	eventID string,
) (*actionLogRow, error) {
	q := s.session.Query(s.cqlSelectEvent(), tenantStr(tenant), eventID).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum)
	var (
		actionID       gocql.UUID
		kind           string
		actorID        *gocql.UUID
		subject        *string
		targetObjectID *gocql.UUID
		payload        string
		appliedAt      time.Time
		dayBucketTime  time.Time
	)
	if err := q.Scan(&actionID, &kind, &actorID, &subject, &targetObjectID, &payload, &appliedAt, &dayBucketTime); err != nil {
		if err == gocql.ErrNotFound {
			return nil, repos.Backendf("action event %s was not readable after idempotent insert", eventID)
		}
		return nil, driverErr(err)
	}
	return &actionLogRow{
		tenant:         tenant,
		eventID:        eventID,
		actionID:       actionID,
		kind:           kind,
		actorID:        actorID,
		subject:        subjectFrom(actorID, subject),
		targetObjectID: targetObjectID,
		payload:        payload,
		appliedAt:      appliedAt,
		dayBucket:      msToDayBucket(dayBucketTime.UnixMilli()),
	}, nil
}

// writeActionFanout fans the canonical row out to actions_log,
// actions_by_action and (when target_object_id is set)
// actions_by_object. Status / failure_type / duration_ms are pulled
// out of the canonical payload object and projected into the
// dedicated actions_log columns.
func (s *ActionLogStore) writeActionFanout(ctx context.Context, row *actionLogRow) error {
	var parsedPayload map[string]any
	if err := json.Unmarshal([]byte(row.payload), &parsedPayload); err != nil {
		return repos.Backendf("invalid stored action payload JSON: %v", err)
	}
	status := "applied"
	if v, ok := parsedPayload["status"].(string); ok && v != "" {
		status = v
	}
	var failureType *string
	if v, ok := parsedPayload["failure_type"].(string); ok && v != "" {
		failureType = &v
	}
	var durationMs *int32
	if v, ok := parsedPayload["duration_ms"].(float64); ok {
		// JSON numbers decode as float64; clamp to int32 range.
		if v >= float64(-maxInt32) && v <= float64(maxInt32) {
			d := int32(v)
			durationMs = &d
		}
	}
	var targetTypeID *string

	if err := s.session.Query(s.cqlInsertLog(),
		tenantStr(row.tenant), dayBucketToTime(row.dayBucket), row.appliedAt,
		row.actionID, row.kind, row.actorID, row.subject,
		row.targetObjectID, targetTypeID, row.payload,
		&status, failureType, durationMs, row.eventID).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).Exec(); err != nil {
		return driverErr(err)
	}
	if err := s.session.Query(s.cqlInsertByAction(),
		tenantStr(row.tenant), row.actionID, dayBucketToTime(row.dayBucket),
		row.appliedAt, row.eventID, row.kind, row.actorID, row.subject,
		row.targetObjectID, row.payload).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).Exec(); err != nil {
		return driverErr(err)
	}
	if row.targetObjectID != nil {
		if err := s.session.Query(s.cqlInsertByObject(),
			tenantStr(row.tenant), *row.targetObjectID, row.appliedAt,
			row.actionID, row.kind, row.actorID, row.subject,
			row.payload, row.eventID).
			WithContext(ctx).
			Consistency(gocql.LocalQuorum).Exec(); err != nil {
			return driverErr(err)
		}
	}
	return nil
}

// Append is the only write entry point — atomic, idempotent on
// (tenant, event_id). On dedup-hit we read the canonical row back
// and re-fan-out to keep the secondary tables consistent.
func (s *ActionLogStore) Append(ctx context.Context, entry repos.ActionLogEntry) error {
	row, err := s.rowFromEntry(entry)
	if err != nil {
		return err
	}
	applied, err := s.putEventRow(ctx, row)
	if err != nil {
		return err
	}
	canonical := row
	if !applied {
		canonical, err = s.readEventRow(ctx, entry.Tenant, row.eventID)
		if err != nil {
			return err
		}
	}
	return s.writeActionFanout(ctx, canonical)
}

// --- read path: ListRecent ---------------------------------------------

// ListRecent walks day buckets backwards from today across the
// 90-day lookback window, filling up to page.Size entries. The
// continuation token interleaves Cassandra paging-state with the
// day cursor and scanned-count.
func (s *ActionLogStore) ListRecent(
	ctx context.Context,
	tenant repos.TenantId,
	page repos.Page,
	consistency repos.ReadConsistency,
) (repos.PagedResult[repos.ActionLogEntry], error) {
	token, err := decodeRecentToken(page.Token)
	if err != nil {
		return repos.PagedResult[repos.ActionLogEntry]{}, err
	}
	limit := clampPageSize(page.Size)
	items := make([]repos.ActionLogEntry, 0, min(limit, 128))
	var nextToken *string

	for len(items) < limit && token.DaysScanned < actionLogLookbackDays {
		q := s.session.Query(s.cqlSelectRecent(),
			tenantStr(tenant), dayBucketToTime(token.Day)).
			WithContext(ctx).
			Consistency(cqlConsistency(consistency)).
			PageSize(clampPageSize(uint32(limit - len(items))))
		if token.Paging != nil {
			pageBytes, err := decodePagingState(token.Paging)
			if err != nil {
				return repos.PagedResult[repos.ActionLogEntry]{}, err
			}
			if len(pageBytes) > 0 {
				q = q.PageState(pageBytes)
			}
			token.Paging = nil
		}
		iter := q.Iter()

		var (
			appliedAt      time.Time
			actionID       gocql.UUID
			kind           string
			actorID        *gocql.UUID
			subject        *string
			targetObjectID *gocql.UUID
			payload        string
			eventID        *string
		)
		for iter.Scan(&appliedAt, &actionID, &kind, &actorID, &subject, &targetObjectID, &payload, &eventID) {
			entry, err := entryFromRow(tenant, eventID, actionID, kind, actorID, subject, targetObjectID, payload, appliedAt)
			if err != nil {
				iter.Close()
				return repos.PagedResult[repos.ActionLogEntry]{}, err
			}
			items = append(items, entry)
		}
		pageState := iter.PageState()
		if err := iter.Close(); err != nil {
			return repos.PagedResult[repos.ActionLogEntry]{}, driverErr(err)
		}

		if len(pageState) > 0 {
			token.Paging = encodePagingState(pageState)
			encoded, err := encodeRecentToken(token)
			if err != nil {
				return repos.PagedResult[repos.ActionLogEntry]{}, err
			}
			nextToken = &encoded
			break
		}
		if token.Day == 0 {
			break
		}
		token.Day--
		token.DaysScanned++
		token.Paging = nil

		if len(items) >= limit && token.DaysScanned < actionLogLookbackDays {
			encoded, err := encodeRecentToken(token)
			if err != nil {
				return repos.PagedResult[repos.ActionLogEntry]{}, err
			}
			nextToken = &encoded
			break
		}
	}

	return repos.PagedResult[repos.ActionLogEntry]{Items: items, NextToken: nextToken}, nil
}

// --- read path: ListForObject ------------------------------------------

func (s *ActionLogStore) ListForObject(
	ctx context.Context,
	tenant repos.TenantId,
	object repos.ObjectId,
	page repos.Page,
	consistency repos.ReadConsistency,
) (repos.PagedResult[repos.ActionLogEntry], error) {
	objectID, err := parseUUID("object", string(object))
	if err != nil {
		return repos.PagedResult[repos.ActionLogEntry]{}, err
	}
	pagingState, err := decodePagingState(page.Token)
	if err != nil {
		return repos.PagedResult[repos.ActionLogEntry]{}, err
	}

	q := s.session.Query(s.cqlSelectByObject(), tenantStr(tenant), objectID).
		WithContext(ctx).
		Consistency(cqlConsistency(consistency)).
		PageSize(clampPageSize(page.Size))
	if len(pagingState) > 0 {
		q = q.PageState(pagingState)
	}
	iter := q.Iter()

	out := repos.PagedResult[repos.ActionLogEntry]{Items: []repos.ActionLogEntry{}}
	var (
		appliedAt time.Time
		actionID  gocql.UUID
		kind      string
		actorID   *gocql.UUID
		subject   *string
		payload   string
		eventID   *string
	)
	for iter.Scan(&appliedAt, &actionID, &kind, &actorID, &subject, &payload, &eventID) {
		entry, err := entryFromRow(tenant, eventID, actionID, kind, actorID, subject, &objectID, payload, appliedAt)
		if err != nil {
			iter.Close()
			return repos.PagedResult[repos.ActionLogEntry]{}, err
		}
		out.Items = append(out.Items, entry)
	}
	if pageBytes := iter.PageState(); len(pageBytes) > 0 {
		out.NextToken = encodePagingState(pageBytes)
	}
	if err := iter.Close(); err != nil {
		return repos.PagedResult[repos.ActionLogEntry]{}, driverErr(err)
	}
	return out, nil
}

// --- read path: ListForAction ------------------------------------------

func (s *ActionLogStore) ListForAction(
	ctx context.Context,
	tenant repos.TenantId,
	actionIDStr string,
	page repos.Page,
	consistency repos.ReadConsistency,
) (repos.PagedResult[repos.ActionLogEntry], error) {
	actionID, err := parseUUID("action_id", actionIDStr)
	if err != nil {
		return repos.PagedResult[repos.ActionLogEntry]{}, err
	}
	token, err := decodeRecentToken(page.Token)
	if err != nil {
		return repos.PagedResult[repos.ActionLogEntry]{}, err
	}
	limit := clampPageSize(page.Size)
	items := make([]repos.ActionLogEntry, 0, min(limit, 128))
	var nextToken *string

	for len(items) < limit && token.DaysScanned < actionLogLookbackDays {
		q := s.session.Query(s.cqlSelectByAction(),
			tenantStr(tenant), actionID, dayBucketToTime(token.Day)).
			WithContext(ctx).
			Consistency(cqlConsistency(consistency)).
			PageSize(clampPageSize(uint32(limit - len(items))))
		if token.Paging != nil {
			pageBytes, err := decodePagingState(token.Paging)
			if err != nil {
				return repos.PagedResult[repos.ActionLogEntry]{}, err
			}
			if len(pageBytes) > 0 {
				q = q.PageState(pageBytes)
			}
			token.Paging = nil
		}
		iter := q.Iter()

		var (
			appliedAt      time.Time
			eventID        string
			kind           string
			actorID        *gocql.UUID
			subject        *string
			targetObjectID *gocql.UUID
			payload        string
		)
		for iter.Scan(&appliedAt, &eventID, &kind, &actorID, &subject, &targetObjectID, &payload) {
			eventIDCopy := eventID
			entry, err := entryFromRow(tenant, &eventIDCopy, actionID, kind, actorID, subject, targetObjectID, payload, appliedAt)
			if err != nil {
				iter.Close()
				return repos.PagedResult[repos.ActionLogEntry]{}, err
			}
			items = append(items, entry)
		}
		pageState := iter.PageState()
		if err := iter.Close(); err != nil {
			return repos.PagedResult[repos.ActionLogEntry]{}, driverErr(err)
		}

		if len(pageState) > 0 {
			token.Paging = encodePagingState(pageState)
			encoded, err := encodeRecentToken(token)
			if err != nil {
				return repos.PagedResult[repos.ActionLogEntry]{}, err
			}
			nextToken = &encoded
			break
		}
		if token.Day == 0 {
			break
		}
		token.Day--
		token.DaysScanned++
		token.Paging = nil

		if len(items) >= limit && token.DaysScanned < actionLogLookbackDays {
			encoded, err := encodeRecentToken(token)
			if err != nil {
				return repos.PagedResult[repos.ActionLogEntry]{}, err
			}
			nextToken = &encoded
			break
		}
	}

	return repos.PagedResult[repos.ActionLogEntry]{Items: items, NextToken: nextToken}, nil
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
