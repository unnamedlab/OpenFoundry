//! Versioned, idempotent CQL migrations.
//!
//! Cassandra has no native rollback. The kernel models migrations as
//! an append-only ledger keyed by a monotonically increasing integer
//! version. Each migration is one or more CQL statements that must be
//! safely re-runnable: prefer `IF NOT EXISTS` on schema, `IF EXISTS`
//! on drops, and never write data in a migration.
//!
//! The ledger lives in a single table per keyspace:
//!
//! ```cql
//! CREATE TABLE IF NOT EXISTS <keyspace>.cassandra_kernel_migrations (
//!     version    int,
//!     name       text,
//!     applied_at timestamp,
//!     checksum   text,
//!     PRIMARY KEY (version)
//! );
//! ```
//!
//! On each run the kernel:
//!
//! 1. Creates the ledger table if missing.
//! 2. Reads applied versions.
//! 3. For each declared migration whose version is not yet applied,
//!    runs the statements one by one and inserts the ledger row with
//!    a checksum of the source.
//! 4. For migrations already applied, verifies the checksum still
//!    matches and returns [`KernelError::MigrationDrift`] if not.
//!
//! The [`cql_migrate!`] macro is sugar for declaring a `&[Migration]`
//! slice inline in the call site.

use std::time::{SystemTime, UNIX_EPOCH};

use scylla::{Session, frame::value::CqlTimestamp};
use tracing::info;

use crate::error::{KernelError, KernelResult};

/// One migration, materialised as a slice of CQL statements.
#[derive(Debug, Clone)]
pub struct Migration {
    /// Strictly monotonically increasing version. Gaps are allowed
    /// but discouraged.
    pub version: i32,
    /// Human-readable name; appears in the ledger.
    pub name: &'static str,
    /// One or more CQL statements. They are applied in order under
    /// the same migration version. Each statement must be idempotent
    /// (`IF NOT EXISTS`, `IF EXISTS`).
    pub statements: &'static [&'static str],
}

impl Migration {
    /// Stable checksum of the migration source. We use a simple FNV-
    /// 1a 64-bit hash rendered as hex; this is not cryptographic, it
    /// just has to detect accidental edits to a frozen migration.
    pub fn checksum(&self) -> String {
        let mut h: u64 = 0xcbf2_9ce4_8422_2325;
        let mut feed = |s: &[u8]| {
            for b in s {
                h ^= u64::from(*b);
                h = h.wrapping_mul(0x100_0000_01b3);
            }
        };
        feed(self.name.as_bytes());
        for stmt in self.statements {
            feed(b"|");
            feed(stmt.as_bytes());
        }
        format!("{h:016x}")
    }
}

/// Outcome of a single migration in [`apply`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MigrationOutcome {
    /// Migration was newly applied.
    Applied,
    /// Migration was already applied with a matching checksum.
    AlreadyApplied,
}

/// Apply the supplied migrations against `keyspace`.
///
/// Returns the per-version outcome in input order. Stops at the first
/// failure — partial application is tolerated because each statement
/// inside a migration is required to be idempotent.
pub async fn apply(
    session: &Session,
    keyspace: &str,
    migrations: &[Migration],
) -> KernelResult<Vec<(i32, MigrationOutcome)>> {
    ensure_ledger(session, keyspace).await?;

    // Load applied versions and their stored checksums.
    let select = format!(
        "SELECT version, name, checksum FROM {ks}.cassandra_kernel_migrations",
        ks = keyspace
    );
    let rows = session
        .query(select, &[])
        .await?
        .rows_typed::<(i32, String, String)>()
        .map_err(|e| KernelError::Other(anyhow::anyhow!("ledger row decode: {e}")))?
        .collect::<Result<Vec<_>, _>>()
        .map_err(|e| KernelError::Other(anyhow::anyhow!("ledger row decode: {e}")))?;

    let mut outcomes = Vec::with_capacity(migrations.len());
    for m in migrations {
        let current_checksum = m.checksum();

        if let Some((_, name, stored)) = rows.iter().find(|(v, _, _)| *v == m.version).cloned() {
            if stored != current_checksum {
                return Err(KernelError::MigrationDrift {
                    version: m.version,
                    name,
                    stored,
                    current: current_checksum,
                });
            }
            outcomes.push((m.version, MigrationOutcome::AlreadyApplied));
            continue;
        }

        info!(version = m.version, name = m.name, "applying migration");
        for stmt in m.statements {
            session.query(*stmt, &[]).await?;
        }

        let applied_at = CqlTimestamp(
            SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .map(|d| d.as_millis() as i64)
                .unwrap_or(0),
        );
        let insert = format!(
            "INSERT INTO {ks}.cassandra_kernel_migrations \
             (version, name, applied_at, checksum) VALUES (?, ?, ?, ?)",
            ks = keyspace
        );
        session
            .query(
                insert,
                (m.version, m.name.to_string(), applied_at, current_checksum),
            )
            .await?;
        outcomes.push((m.version, MigrationOutcome::Applied));
    }
    Ok(outcomes)
}

async fn ensure_ledger(session: &Session, keyspace: &str) -> KernelResult<()> {
    let cql = format!(
        "CREATE TABLE IF NOT EXISTS {ks}.cassandra_kernel_migrations ( \
            version int, name text, applied_at timestamp, checksum text, \
            PRIMARY KEY (version) \
         )",
        ks = keyspace
    );
    session.query(cql, &[]).await?;
    Ok(())
}

/// Declarative slice-of-`Migration` builder.
///
/// ```ignore
/// use cassandra_kernel::cql_migrate;
///
/// let migrations = cql_migrate![
///     1, "create_objects_table" => &[
///         "CREATE TABLE IF NOT EXISTS ontology_objects.objects ( \
///             tenant_id text, object_id timeuuid, payload blob, \
///             PRIMARY KEY ((tenant_id), object_id) \
///          ) WITH CLUSTERING ORDER BY (object_id DESC)",
///     ],
///     2, "add_objects_index" => &[
///         "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_kind ( \
///             tenant_id text, kind text, object_id timeuuid, \
///             PRIMARY KEY ((tenant_id, kind), object_id) \
///          )",
///     ],
/// ];
///
/// cassandra_kernel::migrate::apply(&session, "ontology_objects", &migrations).await?;
/// ```
#[macro_export]
macro_rules! cql_migrate {
    ( $( $version:expr, $name:expr => $stmts:expr ),* $(,)? ) => {
        &[
            $(
                $crate::migrate::Migration {
                    version: $version,
                    name: $name,
                    statements: $stmts,
                },
            )*
        ]
    };
}
