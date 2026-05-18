//go:build integration

// Integration coverage for the T8 audit-outbox closure on the SSO
// callback path. We boot a real Postgres container, run the service
// migrations (which create outbox.events as of 0018), then drive
// emitAuthAudit through the production NewOutboxAuditBatcher and
// assert the WAL trace: the three INSERT-then-DELETE pairs land in
// the same transaction, so the table is empty after commit but the
// underlying replication slot has captured the envelopes.
//
// REPLICA IDENTITY FULL on outbox.events is what makes the WAL
// inspection possible — we read the captured rows through
// `pg_logical_emit_message` is not needed; we sample the captured
// payload from `outbox.events` inside the transaction before commit.

package handlers_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

// TestSSOCallbackEmitsAuditEnvelopesThroughOutbox is the T8
// integration contract: a successful SSO callback enqueues three
// audit envelopes through the local outbox table. The transactional
// outbox writes INSERT-then-DELETE in one tx so the table is empty
// in steady state — Debezium captures the INSERT from the WAL via
// REPLICA IDENTITY FULL. To verify the INSERT actually happened we
// install an AFTER INSERT trigger that mirrors the inserted row into
// a sentinel observed_outbox table the test owns; the production
// batcher is otherwise unmodified.
func TestSSOCallbackEmitsAuditEnvelopesThroughOutbox(t *testing.T) {
	ctx := context.Background()

	h := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, h.Pool))

	// Sentinel table + trigger: every INSERT on outbox.events is
	// copied here before the same-tx DELETE removes it. This is what
	// Debezium would observe in the WAL; we observe it relationally.
	_, err := h.Pool.Exec(ctx, `
		CREATE TABLE _observed_outbox(
			event_id uuid PRIMARY KEY,
			aggregate text NOT NULL,
			aggregate_id text NOT NULL,
			payload jsonb NOT NULL,
			headers jsonb NOT NULL,
			topic text NOT NULL
		);
		CREATE OR REPLACE FUNCTION _observe_outbox() RETURNS trigger AS $$
		BEGIN
			INSERT INTO _observed_outbox(event_id, aggregate, aggregate_id, payload, headers, topic)
			VALUES (NEW.event_id, NEW.aggregate, NEW.aggregate_id, NEW.payload, NEW.headers, NEW.topic);
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER _observe_outbox_t AFTER INSERT ON outbox.events
			FOR EACH ROW EXECUTE FUNCTION _observe_outbox();
	`)
	require.NoError(t, err)

	r := &repo.Repo{Pool: h.Pool}

	jwt := authmw.NewJWTConfig("integration-secret")
	tenantID := uuid.New()
	user := &models.User{
		ID:             uuid.New(),
		Email:          "carol@example.com",
		Name:           "Carol",
		OrganizationID: &tenantID,
	}
	exp := time.Now().Add(time.Hour).Truncate(time.Second).UTC()
	jti := uuid.New()
	access, err := authmw.EncodeToken(jwt, &authmw.Claims{
		Sub: user.ID, IAT: time.Now().Unix(), EXP: exp.Unix(), JTI: jti,
		AuthMethods: []string{"sso", "okta"},
	})
	require.NoError(t, err)

	s := &handlers.SSO{
		Repo:          r,
		Issuer:        &service.Issuer{JWT: jwt, AccessTTL: time.Hour},
		EmitAudit:     handlers.NewOutboxAuditBatcher(r),
		SourceService: "identity-federation-service",
	}

	req := httptest.NewRequest("GET", "/cb", nil)
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("User-Agent", "IntegrationAgent/2.0")

	// Drive the production audit-emit path via the test seam — same
	// code path the SSO Callback executes, sans the OIDC dance.
	handlers.EmitAuthAuditForTest(s, req, user, "okta", "subject-1", "carol@example.com",
		true /*firstLink*/, []string{"sso", "okta"}, access)

	// The AFTER INSERT trigger surfaced each envelope into the
	// sentinel table even though the same-tx DELETE removed the
	// originals from outbox.events.
	rows, err := h.Pool.Query(ctx,
		`SELECT aggregate, topic, payload FROM _observed_outbox ORDER BY topic, aggregate`)
	require.NoError(t, err)
	defer rows.Close()

	type captured struct {
		aggregate string
		topic     string
		payload   []byte
	}
	var seen []captured
	for rows.Next() {
		c := captured{}
		require.NoError(t, rows.Scan(&c.aggregate, &c.topic, &c.payload))
		seen = append(seen, c)
	}
	require.NoError(t, rows.Err())
	require.Len(t, seen, 3, "all 3 audit envelopes must reach the outbox INSERT path")

	kinds := make(map[audittrail.EventKind]bool)
	for _, row := range seen {
		assert.Equal(t, "audit_event", row.aggregate)
		assert.Equal(t, audittrail.TopicAuditEvents, row.topic)

		var env audittrail.AuditEnvelope
		require.NoError(t, json.Unmarshal(row.payload, &env))
		kinds[env.Kind] = true
		assert.Equal(t, "198.51.100.7", env.IP)
		assert.Equal(t, "IntegrationAgent/2.0", env.UserAgent)
		assert.Equal(t, "identity-federation-service", env.SourceService)
	}
	assert.True(t, kinds[audittrail.KindAuthLogin], "auth.login envelope missing")
	assert.True(t, kinds[audittrail.KindIdentityLinked], "auth.identity_linked envelope missing")
	assert.True(t, kinds[audittrail.KindTokenIssued], "auth.token_issued envelope missing")

	// Once the batcher's tx committed, outbox.events should be empty
	// (Debezium would have picked them up from the WAL in prod).
	var post int
	require.NoError(t, h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox.events`).Scan(&post))
	assert.Zero(t, post, "outbox INSERT+DELETE leaves the table empty post-commit")
}
