//go:build integration

// Ephemeral Cassandra 5 harness for integration tests.
//
// BootCassandra starts a single-node `cassandra:5.0` container, waits
// until CQL is reachable on 9042, builds a gocql session against it,
// optionally creates a keyspace with RF=1, and returns everything the
// caller needs.
//
// Keep the returned CassandraHarness alive for the duration of the test
// (call Stop or rely on t.Cleanup) — terminating the container ends
// the harness.
//
// Why a single node? Production runs RF=3 in three racks per DC; tests
// run with RF=1 because spinning up a multi-node cluster per test
// costs ~30s and adds zero coverage. Tests that need to validate
// replica placement should target the dev cluster (`just dev-up-cassandra`)
// instead.
//
// Boot timing: Cassandra startup is slow (~25s on a warm Docker
// daemon). The container is considered healthy when
// `Starting listening for CQL clients on /0.0.0.0:9042` appears in
// stdout. On top of that we retry the gocql session up to 60 times at
// 1s intervals to absorb the post-startup gossip settle window.
package testingx

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const cassandraCQLPort = "9042/tcp"

// CassandraHarness is the live `cassandra:5.0` container plus the
// connected gocql session.
//
// Keyspace is empty when BootCassandra was called with an empty
// keyspace argument; otherwise it is the keyspace created with RF=1
// and bound to the session as its default.
type CassandraHarness struct {
	Container    testcontainers.Container
	Session      *gocql.Session
	ContactPoint string // host:port of the CQL endpoint
	Keyspace     string // empty if no keyspace was created
}

// Stop terminates the container and closes the session. Safe to call
// multiple times.
func (h *CassandraHarness) Stop(ctx context.Context) {
	if h.Session != nil {
		h.Session.Close()
		h.Session = nil
	}
	if h.Container != nil {
		_ = h.Container.Terminate(ctx)
		h.Container = nil
	}
}

// BootCassandra starts a `cassandra:5.0` container and connects a gocql
// session.
//
// Pass a non-empty `keyspace` to have the harness create that keyspace
// with RF=1 ({ 'class': 'NetworkTopologyStrategy', 'dc1': 1 }) and bind
// it as the session's default. Pass "" to get a session with no
// default keyspace.
//
// Wires t.Cleanup so the container disappears with the test even when
// the test panics.
func BootCassandra(ctx context.Context, t *testing.T, keyspace string) *CassandraHarness {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "cassandra:5.0",
		ExposedPorts: []string{cassandraCQLPort},
		Env: map[string]string{
			// Single-node cluster overrides — Cassandra refuses to
			// start a one-node SimpleSnitch cluster without these.
			"CASSANDRA_CLUSTER_NAME":    "of-test",
			"CASSANDRA_DC":              "dc1",
			"CASSANDRA_RACK":            "rack1",
			"CASSANDRA_ENDPOINT_SNITCH": "GossipingPropertyFileSnitch",
			// Cassandra 5 ships with a generous default heap; cap it
			// so CI runners do not OOM.
			"HEAP_NEWSIZE":  "128M",
			"MAX_HEAP_SIZE": "512M",
		},
		WaitingFor: wait.ForLog("Starting listening for CQL clients").
			WithStartupTimeout(120 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("cassandra container start: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("container host: %v", err)
	}
	mapped, err := container.MappedPort(ctx, "9042")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("container port: %v", err)
	}
	contactPoint := fmt.Sprintf("%s:%s", host, mapped.Port())

	cluster := gocql.NewCluster(contactPoint)
	cluster.ProtoVersion = 4
	cluster.ConnectTimeout = 5 * time.Second
	cluster.Timeout = 10 * time.Second
	cluster.Consistency = gocql.LocalOne

	var session *gocql.Session
	for attempt := 1; attempt <= 60; attempt++ {
		session, err = cluster.CreateSession()
		if err == nil {
			break
		}
		if attempt == 60 {
			_ = container.Terminate(ctx)
			t.Fatalf("cassandra never became reachable: %v", err)
		}
		t.Logf("waiting for cassandra (%d): %v", attempt, err)
		time.Sleep(time.Second)
	}

	created := ""
	if ks := strings.TrimSpace(keyspace); ks != "" {
		ddl := fmt.Sprintf(
			`CREATE KEYSPACE IF NOT EXISTS %s WITH replication = `+
				`{ 'class': 'NetworkTopologyStrategy', 'dc1': 1 } AND durable_writes = true`,
			ks,
		)
		if err := session.Query(ddl).Exec(); err != nil {
			session.Close()
			_ = container.Terminate(ctx)
			t.Fatalf("create keyspace: %v", err)
		}
		// gocql binds Keyspace at session-creation time; reopen the
		// session against the freshly-created keyspace so it becomes
		// the session default (mirrors `Session::use_keyspace` on the
		// Rust side).
		session.Close()
		cluster.Keyspace = ks
		session, err = cluster.CreateSession()
		if err != nil {
			_ = container.Terminate(ctx)
			t.Fatalf("session with keyspace %q: %v", ks, err)
		}
		created = ks
	}

	h := &CassandraHarness{
		Container:    container,
		Session:      session,
		ContactPoint: contactPoint,
		Keyspace:     created,
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		h.Stop(stopCtx)
	})

	t.Logf("cassandra harness ready at %s (keyspace=%q)", h.ContactPoint, h.Keyspace)
	return h
}
