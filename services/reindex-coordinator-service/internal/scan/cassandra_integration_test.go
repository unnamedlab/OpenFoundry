//go:build integration

// Integration test for the Cassandra paginated scanner. Boots a real
// `cassandra:5.0` testcontainer via libs/testing.BootCassandra, applies
// the ontology schema, seeds a handful of objects, and verifies the
// two-query pattern (objects_by_type → objects_by_id):
//
//   - per-type scan returns only the matching type;
//   - all-types scan (ALLOW FILTERING) returns every type;
//   - paging-state round-trip drives the second page;
//   - soft-deleted rows are filtered out during hydration;
//   - deleted ids still count toward `Scanned` (matches the legacy
//     Go worker, which uses `scanned` for index-row throughput).
//
// Opt-in via `go test -tags=integration ./...`.
package scan

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
)

const integrationKeyspace = "reindex_coordinator_scan_it"

func bootScanner(t *testing.T) (*CassandraScanner, *gocql.Session) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	h := testingx.BootCassandra(ctx, t, integrationKeyspace)
	applyOntologySchema(t, h.Session, integrationKeyspace)
	return NewCassandraScanner(h.Session, integrationKeyspace), h.Session
}

func applyOntologySchema(t *testing.T, session *gocql.Session, keyspace string) {
	t.Helper()
	stmts := []string{
		// `revision_number` is STATIC in the production schema
		// (libs/cassandra-kernel/ontology_migrations.go), but Cassandra
		// 5 rejects STATIC on tables without clustering columns. The
		// scanner only reads `revision_number` per-row, so dropping
		// STATIC here is wire-equivalent for the assertions in this
		// file without depending on a specific Cassandra version's
		// validation rules.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_id (
			tenant text,
			object_id timeuuid,
			type_id text,
			owner_id uuid,
			properties text,
			marking frozen<set<text>>,
			organization_id uuid,
			revision_number bigint,
			created_at timestamp,
			updated_at timestamp,
			deleted boolean,
			PRIMARY KEY ((tenant, object_id))
		)`, keyspace),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_type (
			tenant text,
			type_id text,
			updated_at timestamp,
			object_id timeuuid,
			owner_id uuid,
			marking frozen<set<text>>,
			properties_summary text,
			deleted boolean,
			PRIMARY KEY ((tenant, type_id), updated_at, object_id)
		) WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)`, keyspace),
	}
	for _, stmt := range stmts {
		if err := session.Query(stmt).Exec(); err != nil {
			t.Fatalf("apply schema: %v\n%s", err, stmt)
		}
	}
}

// seedObject writes one row to objects_by_id + the matching index row
// in objects_by_type. We use raw CQL rather than the production
// ObjectStore so the scanner under test has zero shared code with the
// seeder — important for confidence that the two-query pattern hits
// the real tables.
func seedObject(
	t *testing.T,
	session *gocql.Session,
	keyspace, tenant, typeID string,
	objectID gocql.UUID,
	revision int64,
	properties string,
	deleted bool,
) {
	t.Helper()
	now := time.Now().UTC()
	owner := gocql.UUID(uuid.New())

	insertID := fmt.Sprintf(`INSERT INTO %s.objects_by_id
		(tenant, object_id, type_id, owner_id, properties, marking,
		 organization_id, revision_number, created_at, updated_at, deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, keyspace)
	if err := session.Query(insertID,
		tenant, objectID, typeID, owner, properties, []string{},
		gocql.UUID(uuid.New()), revision, now, now, deleted,
	).Exec(); err != nil {
		t.Fatalf("seed objects_by_id: %v", err)
	}

	insertIdx := fmt.Sprintf(`INSERT INTO %s.objects_by_type
		(tenant, type_id, updated_at, object_id, owner_id, marking,
		 properties_summary, deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, keyspace)
	if err := session.Query(insertIdx,
		tenant, typeID, now, objectID, owner, []string{}, "{}", deleted,
	).Exec(); err != nil {
		t.Fatalf("seed objects_by_type: %v", err)
	}
}

func TestScannerPerTypeHitsObjectsByTypeAndObjectsByID(t *testing.T) {
	scanner, session := bootScanner(t)

	tenant := uuid.NewString()
	usersA := gocql.TimeUUID()
	usersB := gocql.TimeUUID()
	docsA := gocql.TimeUUID()

	seedObject(t, session, integrationKeyspace, tenant, "users", usersA, 1,
		`{"name":"alice","embedding":[0.1,0.2]}`, false)
	seedObject(t, session, integrationKeyspace, tenant, "users", usersB, 5,
		`{"name":"bob"}`, false)
	seedObject(t, session, integrationKeyspace, tenant, "docs", docsA, 2,
		`{"title":"draft"}`, false)

	users := "users"
	page, err := scanner.ScanPage(context.Background(), tenant, &users, 100, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, page.Scanned, "per-type scan must only see ids from that type's partition")
	require.Len(t, page.Records, 2)

	gotIDs := map[string]ReindexRecord{}
	for _, r := range page.Records {
		assert.Equal(t, tenant, r.Tenant)
		assert.Equal(t, "users", r.TypeID, "fetchObject must use the type_id stored in objects_by_id")
		assert.False(t, r.Deleted, "deleted=false on the publish path")
		gotIDs[r.ID] = r
	}
	assert.Contains(t, gotIDs, usersA.String())
	assert.Contains(t, gotIDs, usersB.String())

	rec := gotIDs[usersA.String()]
	assert.Equal(t, int64(1), rec.Version, "version must come from objects_by_id.revision_number")
	assert.Equal(t, []float64{0.1, 0.2}, rec.Embedding,
		"embedding extraction must apply to records hydrated from objects_by_id.properties")
}

func TestScannerAllTypesUsesAllowFiltering(t *testing.T) {
	scanner, session := bootScanner(t)

	tenant := uuid.NewString()
	usersA := gocql.TimeUUID()
	docsA := gocql.TimeUUID()
	seedObject(t, session, integrationKeyspace, tenant, "users", usersA, 1, `{}`, false)
	seedObject(t, session, integrationKeyspace, tenant, "docs", docsA, 1, `{}`, false)

	page, err := scanner.ScanPage(context.Background(), tenant, nil, 100, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, page.Scanned)

	types := map[string]int{}
	for _, r := range page.Records {
		types[r.TypeID]++
	}
	assert.Equal(t, 1, types["users"], "all-types scan must yield each type once per inserted row")
	assert.Equal(t, 1, types["docs"])
}

func TestScannerPaginatesViaOpaqueToken(t *testing.T) {
	scanner, session := bootScanner(t)

	tenant := uuid.NewString()
	const total = 6
	for i := 0; i < total; i++ {
		seedObject(t, session, integrationKeyspace, tenant, "users",
			gocql.TimeUUID(), int64(i+1), `{}`, false)
	}

	users := "users"
	first, err := scanner.ScanPage(context.Background(), tenant, &users, 4, nil)
	require.NoError(t, err)
	require.NotNil(t, first.NextToken, "page 1 must surface a continuation token")
	assert.Equal(t, 4, first.Scanned)
	require.Len(t, first.Records, 4)

	// Token MUST be base64 of the gocql paging-state — round-trip the
	// raw page state through the scanner and confirm we land the
	// remaining rows on page 2.
	second, err := scanner.ScanPage(context.Background(), tenant, &users, 4, first.NextToken)
	require.NoError(t, err)
	assert.Equal(t, total-4, second.Scanned, "page 2 must drain the remaining ids")
	assert.Len(t, second.Records, total-4)

	seen := map[string]bool{}
	for _, r := range first.Records {
		seen[r.ID] = true
	}
	for _, r := range second.Records {
		assert.False(t, seen[r.ID], "no id must appear on both pages")
	}
}

func TestScannerFiltersDeletedRowsOnHydration(t *testing.T) {
	scanner, session := bootScanner(t)

	tenant := uuid.NewString()
	live := gocql.TimeUUID()
	dead := gocql.TimeUUID()
	seedObject(t, session, integrationKeyspace, tenant, "users", live, 1, `{}`, false)
	seedObject(t, session, integrationKeyspace, tenant, "users", dead, 1, `{}`, true)

	users := "users"
	page, err := scanner.ScanPage(context.Background(), tenant, &users, 100, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, page.Scanned,
		"deleted ids must still count toward Scanned — matches the legacy Go worker")
	require.Len(t, page.Records, 1, "soft-deleted rows must be filtered out before publish")
	assert.Equal(t, live.String(), page.Records[0].ID)
}

func TestScannerSurvivesIndexEntryWithoutPrimaryRow(t *testing.T) {
	// Insert directly into objects_by_type without a matching
	// objects_by_id row. The scanner must skip it (Cassandra returns
	// no rows on hydration) without surfacing an error.
	scanner, session := bootScanner(t)

	tenant := uuid.NewString()
	orphan := gocql.TimeUUID()
	live := gocql.TimeUUID()
	now := time.Now().UTC()

	stmt := fmt.Sprintf(`INSERT INTO %s.objects_by_type
		(tenant, type_id, updated_at, object_id, owner_id, marking,
		 properties_summary, deleted)
		VALUES (?, 'users', ?, ?, ?, ?, '{}', false)`, integrationKeyspace)
	require.NoError(t, session.Query(stmt,
		tenant, now, orphan, gocql.UUID(uuid.New()), []string{},
	).Exec())

	seedObject(t, session, integrationKeyspace, tenant, "users", live, 1, `{}`, false)

	users := "users"
	page, err := scanner.ScanPage(context.Background(), tenant, &users, 100, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, page.Scanned)
	require.Len(t, page.Records, 1, "orphan index rows must be silently skipped")
	assert.Equal(t, live.String(), page.Records[0].ID)
}

func TestScannerRejectsMalformedResumeToken(t *testing.T) {
	scanner, _ := bootScanner(t)

	tenant := uuid.NewString()
	users := "users"
	bad := "this-is-not-base64@@@"
	_, err := scanner.ScanPage(context.Background(), tenant, &users, 10, &bad)
	require.Error(t, err)
	assert.True(t, IsScanError(err))
	assert.True(t, strings.Contains(err.Error(), "invalid resume token"))
}
