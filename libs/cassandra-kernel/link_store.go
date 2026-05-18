package cassandrakernel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// LinkStore (OSV2.5) is the Cassandra-backed implementation of
// repos.LinkStore mirroring libs/cassandra-kernel/src/repos.rs::
// CassandraLinkStore. It writes against the `links_outgoing` and
// `links_incoming` tables in the `ontology_indexes` keyspace.
//
// Links are immutable: a second Put of the same (tenant, link_type,
// from, to) triple is a no-op (LWT INSERT IF NOT EXISTS on both
// outgoing and incoming sides).
type LinkStore struct {
	session  *gocql.Session
	keyspace string
}

// NewLinkStore builds a store bound to the standard
// `ontology_indexes` keyspace.
func NewLinkStore(session *gocql.Session) *LinkStore {
	return &LinkStore{session: session, keyspace: "ontology_indexes"}
}

// NewLinkStoreWithKeyspace allows a custom keyspace.
func NewLinkStoreWithKeyspace(session *gocql.Session, keyspace string) *LinkStore {
	return &LinkStore{session: session, keyspace: keyspace}
}

// Compile-time interface assertion.
var _ repos.LinkStore = (*LinkStore)(nil)

func (s *LinkStore) cqlInsertOutgoing() string {
	return fmt.Sprintf(
		`INSERT INTO %s.links_outgoing
            (tenant, link_type_id, source_rid, target_rid, target_type,
             properties_blob, markings_blob, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS`, s.keyspace)
}

func (s *LinkStore) cqlInsertIncoming() string {
	return fmt.Sprintf(
		`INSERT INTO %s.links_incoming
            (tenant, link_type_id, target_rid, source_rid, source_type,
             properties_blob, markings_blob, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS`, s.keyspace)
}

func (s *LinkStore) cqlDeleteOutgoing() string {
	return fmt.Sprintf(
		`DELETE FROM %s.links_outgoing
          WHERE tenant = ? AND link_type_id = ? AND source_rid = ? AND target_rid = ?`,
		s.keyspace)
}

func (s *LinkStore) cqlDeleteIncoming() string {
	return fmt.Sprintf(
		`DELETE FROM %s.links_incoming
          WHERE tenant = ? AND link_type_id = ? AND target_rid = ? AND source_rid = ?`,
		s.keyspace)
}

func (s *LinkStore) cqlSelectOutgoing() string {
	return fmt.Sprintf(
		`SELECT target_rid, properties_blob, created_at
           FROM %s.links_outgoing
          WHERE tenant = ? AND link_type_id = ? AND source_rid = ?
          LIMIT ?`, s.keyspace)
}

func (s *LinkStore) cqlSelectOutgoingAfter() string {
	return fmt.Sprintf(
		`SELECT target_rid, properties_blob, created_at
           FROM %s.links_outgoing
          WHERE tenant = ? AND link_type_id = ? AND source_rid = ? AND target_rid > ?
          LIMIT ?`, s.keyspace)
}

func (s *LinkStore) cqlSelectIncoming() string {
	return fmt.Sprintf(
		`SELECT source_rid, properties_blob, created_at
           FROM %s.links_incoming
          WHERE tenant = ? AND link_type_id = ? AND target_rid = ?
          LIMIT ?`, s.keyspace)
}

func (s *LinkStore) cqlSelectIncomingAfter() string {
	return fmt.Sprintf(
		`SELECT source_rid, properties_blob, created_at
           FROM %s.links_incoming
          WHERE tenant = ? AND link_type_id = ? AND target_rid = ? AND source_rid > ?
          LIMIT ?`, s.keyspace)
}

func (s *LinkStore) cqlSelectOutgoingExact() string {
	return fmt.Sprintf(
		`SELECT target_rid FROM %s.links_outgoing
          WHERE tenant = ? AND link_type_id = ? AND source_rid = ? AND target_rid = ?`,
		s.keyspace)
}

// Put persists a link to both the outgoing and incoming index
// tables. Idempotent: a re-insert of the same triple is a no-op
// thanks to IF NOT EXISTS on both sides.
func (s *LinkStore) Put(ctx context.Context, link repos.Link) error {
	sourceID, err := parseUUID("from", string(link.From))
	if err != nil {
		return err
	}
	targetID, err := parseUUID("to", string(link.To))
	if err != nil {
		return err
	}
	createdAt := time.UnixMilli(link.CreatedAtMs).UTC()

	propertiesBlob, err := encodePropertiesBlob(link.Payload)
	if err != nil {
		return invalidArgf("link payload is not serialisable as OSV2 properties_blob: %v", err)
	}
	markingsBlob, err := encodeMarkingsBlob(nil)
	if err != nil {
		return invalidArgf("link markings are not serialisable as OSV2 markings_blob: %v", err)
	}

	tenant := tenantStr(link.Tenant)
	linkType := string(link.LinkType)

	// outgoing side
	q1 := s.session.Query(s.cqlInsertOutgoing(),
		tenant, linkType, sourceID, targetID, "", propertiesBlob, markingsBlob, createdAt).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).
		SerialConsistency(gocql.LocalSerial)
	// We only care that the write happened; LWT IF NOT EXISTS makes
	// the operation idempotent.
	row := map[string]any{}
	if _, err := q1.MapScanCAS(row); err != nil {
		return driverErr(err)
	}

	q2 := s.session.Query(s.cqlInsertIncoming(),
		tenant, linkType, targetID, sourceID, "", propertiesBlob, markingsBlob, createdAt).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).
		SerialConsistency(gocql.LocalSerial)
	row = map[string]any{}
	if _, err := q2.MapScanCAS(row); err != nil {
		return driverErr(err)
	}
	return nil
}

// Delete removes the link from both index tables. Returns true
// when the row existed at the moment of the SELECT probe, false
// otherwise. The probe-then-delete pattern matches the Rust impl
// (Cassandra DELETE doesn't surface rows_affected on non-LWT writes).
func (s *LinkStore) Delete(
	ctx context.Context,
	tenant repos.TenantId,
	linkType repos.LinkTypeId,
	from, to repos.ObjectId,
) (bool, error) {
	sourceID, err := parseUUID("from", string(from))
	if err != nil {
		return false, err
	}
	targetID, err := parseUUID("to", string(to))
	if err != nil {
		return false, err
	}

	probe := s.session.Query(s.cqlSelectOutgoingExact(),
		tenantStr(tenant), string(linkType), sourceID, targetID).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum)
	var probeTarget gocql.UUID
	existed := true
	if err := probe.Scan(&probeTarget); err != nil {
		if err == gocql.ErrNotFound {
			existed = false
		} else {
			return false, driverErr(err)
		}
	}

	if err := s.session.Query(s.cqlDeleteOutgoing(),
		tenantStr(tenant), string(linkType), sourceID, targetID).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).Exec(); err != nil {
		return false, driverErr(err)
	}
	if err := s.session.Query(s.cqlDeleteIncoming(),
		tenantStr(tenant), string(linkType), targetID, sourceID).
		WithContext(ctx).
		Consistency(gocql.LocalQuorum).Exec(); err != nil {
		return false, driverErr(err)
	}
	return existed, nil
}

// ListOutgoing yields every outgoing link of `linkType` from `from`.
func (s *LinkStore) ListOutgoing(
	ctx context.Context,
	tenant repos.TenantId,
	linkType repos.LinkTypeId,
	from repos.ObjectId,
	page repos.Page,
	consistency repos.ReadConsistency,
) (repos.PagedResult[repos.Link], error) {
	sourceID, err := parseUUID("from", string(from))
	if err != nil {
		return repos.PagedResult[repos.Link]{}, err
	}
	after, hasAfter, err := decodeLinkCursor(page.Token)
	if err != nil {
		return repos.PagedResult[repos.Link]{}, err
	}

	limit := clampPageSize(page.Size)
	queryLimit := limit + 1
	var q *gocql.Query
	if hasAfter {
		q = s.session.Query(s.cqlSelectOutgoingAfter(), tenantStr(tenant), string(linkType), sourceID, after, queryLimit)
	} else {
		q = s.session.Query(s.cqlSelectOutgoing(), tenantStr(tenant), string(linkType), sourceID, queryLimit)
	}
	iter := q.WithContext(ctx).Consistency(cqlConsistency(consistency)).Iter()

	out := repos.PagedResult[repos.Link]{Items: []repos.Link{}}
	var lastTarget gocql.UUID
	var (
		targetID       gocql.UUID
		propertiesBlob []byte
		createdAt      time.Time
	)
	for iter.Scan(&targetID, &propertiesBlob, &createdAt) {
		if len(out.Items) >= limit {
			out.NextToken = encodeLinkCursor(lastTarget)
			break
		}
		payload, err := decodePropertiesBlob(propertiesBlob)
		if err != nil {
			iter.Close()
			return repos.PagedResult[repos.Link]{}, err
		}
		out.Items = append(out.Items, repos.Link{
			Tenant:      tenant,
			LinkType:    linkType,
			From:        from,
			To:          repos.ObjectId(targetID.String()),
			Payload:     payload,
			CreatedAtMs: createdAt.UnixMilli(),
		})
		lastTarget = targetID
	}
	if err := iter.Close(); err != nil {
		return repos.PagedResult[repos.Link]{}, driverErr(err)
	}
	return out, nil
}

// ListIncoming yields every incoming link of `linkType` into `to`.
func (s *LinkStore) ListIncoming(
	ctx context.Context,
	tenant repos.TenantId,
	linkType repos.LinkTypeId,
	to repos.ObjectId,
	page repos.Page,
	consistency repos.ReadConsistency,
) (repos.PagedResult[repos.Link], error) {
	targetID, err := parseUUID("to", string(to))
	if err != nil {
		return repos.PagedResult[repos.Link]{}, err
	}
	after, hasAfter, err := decodeLinkCursor(page.Token)
	if err != nil {
		return repos.PagedResult[repos.Link]{}, err
	}

	limit := clampPageSize(page.Size)
	queryLimit := limit + 1
	var q *gocql.Query
	if hasAfter {
		q = s.session.Query(s.cqlSelectIncomingAfter(), tenantStr(tenant), string(linkType), targetID, after, queryLimit)
	} else {
		q = s.session.Query(s.cqlSelectIncoming(), tenantStr(tenant), string(linkType), targetID, queryLimit)
	}
	iter := q.WithContext(ctx).Consistency(cqlConsistency(consistency)).Iter()

	out := repos.PagedResult[repos.Link]{Items: []repos.Link{}}
	var lastSource gocql.UUID
	var (
		sourceID       gocql.UUID
		propertiesBlob []byte
		createdAt      time.Time
	)
	for iter.Scan(&sourceID, &propertiesBlob, &createdAt) {
		if len(out.Items) >= limit {
			out.NextToken = encodeLinkCursor(lastSource)
			break
		}
		payload, err := decodePropertiesBlob(propertiesBlob)
		if err != nil {
			iter.Close()
			return repos.PagedResult[repos.Link]{}, err
		}
		out.Items = append(out.Items, repos.Link{
			Tenant:      tenant,
			LinkType:    linkType,
			From:        repos.ObjectId(sourceID.String()),
			To:          to,
			Payload:     payload,
			CreatedAtMs: createdAt.UnixMilli(),
		})
		lastSource = sourceID
	}
	if err := iter.Close(); err != nil {
		return repos.PagedResult[repos.Link]{}, driverErr(err)
	}
	return out, nil
}

func encodeLinkCursor(id gocql.UUID) *string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("link:v1:" + id.String()))
	return &encoded
}

func decodeLinkCursor(token *string) (gocql.UUID, bool, error) {
	if token == nil || strings.TrimSpace(*token) == "" {
		return gocql.UUID{}, false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(*token)
	if err != nil {
		return gocql.UUID{}, false, repos.Invalidf("malformed link page token: %v", err)
	}
	value := strings.TrimSpace(string(raw))
	value = strings.TrimPrefix(value, "link:v1:")
	id, err := parseUUID("link page token", value)
	if err != nil {
		return gocql.UUID{}, false, err
	}
	return id, true, nil
}

// decodeLinkPayload converts a possibly-nil text column into a
// json.RawMessage. Nil text → nil payload; valid JSON → raw bytes;
// invalid JSON → RepoBackend (storage corruption).
func decodeLinkPayload(raw *string) (json.RawMessage, error) {
	if raw == nil {
		return nil, nil
	}
	if !json.Valid([]byte(*raw)) {
		return nil, repos.Backendf("invalid stored link JSON: not parseable")
	}
	return json.RawMessage([]byte(*raw)), nil
}
