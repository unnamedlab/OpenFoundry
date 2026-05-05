package cassandrakernel

import (
	"fmt"

	"github.com/gocql/gocql"
)

// Migration is one DDL statement (CREATE TABLE IF NOT EXISTS / ALTER /
// CREATE INDEX). Mirrors the Rust `cassandra_kernel::Migration` shape.
//
// Ordering matters: migrations are applied in the order given. The CQL
// `IF NOT EXISTS` makes them idempotent on re-runs.
type Migration struct {
	Name string
	DDL  string
}

// Apply runs every DDL statement in order against `session`. Returns
// the first error encountered and short-circuits.
//
// `keyspace` is set by the caller via gocql.NewCluster.Keyspace — this
// helper does not bootstrap the keyspace; that's the operator's job
// (typically a one-off Job that runs CREATE KEYSPACE per cluster
// deployment).
func Apply(session *gocql.Session, keyspace string, migrations []Migration) error {
	for _, m := range migrations {
		if err := session.Query(m.DDL).Exec(); err != nil {
			return fmt.Errorf("apply migration %q (keyspace=%s): %w", m.Name, keyspace, err)
		}
	}
	return nil
}

// MustApply panics on error. Use only at startup where a migration
// failure is fatal anyway.
func MustApply(session *gocql.Session, keyspace string, migrations []Migration) {
	if err := Apply(session, keyspace, migrations); err != nil {
		panic(err)
	}
}
