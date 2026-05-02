//! Cassandra-backed implementations of the repository traits defined
//! in [`storage_abstraction::repositories`].
//!
//! ## Status (S1.3)
//!
//! [`CassandraObjectStore`] is **fully implemented** against the
//! `ontology_objects` keyspace materialised by S1.1.b. Prepared
//! statements are cached at startup via [`ObjectPreparedStatements`]
//! and re-used for every call. Optimistic concurrency is enforced
//! with **single-row LWTs** (`UPDATE ... IF revision_number = ?`)
//! per ADR-0020. Listing methods page transparently and surface the
//! Cassandra `paging_state` to callers as an opaque, base64-encoded
//! continuation token.
//!
//! [`CassandraLinkStore`] and [`CassandraActionLogStore`] are wired
//! against the S1.1 Cassandra tables. [`CassandraSchemaStore`] and
//! [`CassandraSessionStore`] remain typed scaffolds returning
//! [`RepoError::Backend`]; schema/session data stay in PostgreSQL
//! until the schema-cluster migration lands.
//!
//! ## Object ↔ Row mapping
//!
//! The `properties` column is `text` (canonical JSON). Versioned
//! schemas live elsewhere; `CassandraObjectStore` is intentionally
//! schema-free — it rejects only invalid JSON. Markings are mapped
//! to `frozen<set<text>>`. Owner is mapped to `uuid` and is required
//! by the table definition; objects without an `owner` are rejected
//! with [`RepoError::InvalidArgument`].

use std::collections::HashSet;
use std::sync::Arc;

use async_trait::async_trait;
use base64::{Engine as _, engine::general_purpose::STANDARD as B64};
use bytes::Bytes;
use chrono::Utc;
use scylla::Session;
use scylla::frame::value::{CqlDate, CqlTimestamp};
use scylla::prepared_statement::PreparedStatement;
use scylla::statement::{Consistency, SerialConsistency};
use tokio::sync::OnceCell;
use uuid::Uuid;

use storage_abstraction::repositories::{
    ActionLogEntry, ActionLogStore, Link, LinkStore, LinkTypeId, MarkingId, Object, ObjectId,
    ObjectStore, OwnerId, Page, PagedResult, PutOutcome, ReadConsistency, RepoError, RepoResult,
    Schema, SchemaStore, Session as RepoSession, SessionStore, TenantId, TypeId,
};

const PENDING: &str = "cassandra impl pending; see migration-plan §S1.4–S1.6";
const ACTION_LOG_LOOKBACK_DAYS: u8 = 90;

fn pending<T>() -> RepoResult<T> {
    Err(RepoError::Backend(PENDING.to_string()))
}

fn driver_err<E: std::fmt::Display>(e: E) -> RepoError {
    RepoError::Backend(e.to_string())
}

fn invalid<S: Into<String>>(s: S) -> RepoError {
    RepoError::InvalidArgument(s.into())
}

/// Translate the abstract [`ReadConsistency`] hint into a CQL
/// consistency. `Strong` ⇒ `LOCAL_QUORUM`; everything else ⇒
/// `LOCAL_ONE` (per ADR-0020 §"opt-in strong reads").
fn cql_consistency(c: ReadConsistency) -> Consistency {
    match c {
        ReadConsistency::Strong => Consistency::LocalQuorum,
        ReadConsistency::Eventual | ReadConsistency::BoundedStaleness(_) => Consistency::LocalOne,
    }
}

fn encode_paging_state(bytes: Option<Bytes>) -> Option<String> {
    bytes.map(|b| B64.encode(b))
}

fn decode_paging_state(token: Option<&str>) -> RepoResult<Option<Bytes>> {
    match token {
        None => Ok(None),
        Some(s) => {
            let raw = B64
                .decode(s.as_bytes())
                .map_err(|e| invalid(format!("malformed page token: {e}")))?;
            Ok(Some(Bytes::from(raw)))
        }
    }
}

fn parse_uuid(field: &str, raw: &str) -> RepoResult<Uuid> {
    Uuid::parse_str(raw).map_err(|e| invalid(format!("{field} is not a valid UUID: {e}")))
}

fn ms_to_ts(ms: i64) -> CqlTimestamp {
    CqlTimestamp(ms)
}

fn cql_ts_to_ms(ts: CqlTimestamp) -> i64 {
    ts.0
}

fn ms_to_day(ms: i64) -> CqlDate {
    let days_since_epoch = ms.div_euclid(86_400_000);
    CqlDate(((1_i64 << 31) + days_since_epoch) as u32)
}

fn tenant_str(t: &TenantId) -> &str {
    t.0.as_str()
}

fn clamp_page_size(n: u32) -> i32 {
    n.clamp(1, 5_000) as i32
}

fn truncate_summary(json: &str) -> &str {
    if json.len() <= 1024 {
        json
    } else {
        &json[..1024]
    }
}

/// Read `[applied]` boolean from an LWT result.
fn lwt_applied(res: &scylla::QueryResult) -> bool {
    let Some(rows) = res.rows.as_ref() else {
        return false;
    };
    let Some(row) = rows.first() else {
        return false;
    };
    row.columns
        .first()
        .and_then(|c| c.as_ref())
        .and_then(|v| v.as_boolean())
        .unwrap_or(false)
}

/// On a failed LWT, Cassandra returns the current row alongside
/// `[applied] = false`. Pull the first `bigint` column, which is the
/// `revision_number` projection in our `IF` clauses. Best-effort —
/// returns `None` if no `bigint` column is present.
fn read_revision(res: &scylla::QueryResult) -> Option<i64> {
    let row = res.rows.as_ref()?.first()?;
    for col in row.columns.iter().flatten() {
        if let Some(n) = col.as_bigint() {
            return Some(n);
        }
    }
    None
}

// ---------------------------------------------------------------------------
// Common construction
// ---------------------------------------------------------------------------

/// Shared construction parameters for every Cassandra-backed store.
#[derive(Clone)]
pub struct CassandraRepoCtx {
    /// Live `scylla::Session` (typically obtained via `SharedSession`).
    pub session: Arc<Session>,
    /// Logical keyspace prefix (`ontology_objects`, `auth_runtime`, …).
    pub keyspace: String,
}

impl CassandraRepoCtx {
    /// Build a context bound to a keyspace.
    pub fn new(session: Arc<Session>, keyspace: impl Into<String>) -> Self {
        Self {
            session,
            keyspace: keyspace.into(),
        }
    }
}

// ---------------------------------------------------------------------------
// ObjectStore
// ---------------------------------------------------------------------------

/// Lazily-prepared statements used by [`CassandraObjectStore`]. Built
/// once per process and shared via the embedded `OnceCell`s.
struct ObjectPreparedStatements {
    insert_if_not_exists: OnceCell<PreparedStatement>,
    update_if_version: OnceCell<PreparedStatement>,
    select_by_id: OnceCell<PreparedStatement>,
    delete_by_id: OnceCell<PreparedStatement>,
    insert_index_by_type: OnceCell<PreparedStatement>,
    insert_index_by_owner: OnceCell<PreparedStatement>,
    insert_index_by_marking: OnceCell<PreparedStatement>,
    select_by_type: OnceCell<PreparedStatement>,
    select_by_owner: OnceCell<PreparedStatement>,
    select_by_marking: OnceCell<PreparedStatement>,
}

impl ObjectPreparedStatements {
    fn new() -> Self {
        Self {
            insert_if_not_exists: OnceCell::new(),
            update_if_version: OnceCell::new(),
            select_by_id: OnceCell::new(),
            delete_by_id: OnceCell::new(),
            insert_index_by_type: OnceCell::new(),
            insert_index_by_owner: OnceCell::new(),
            insert_index_by_marking: OnceCell::new(),
            select_by_type: OnceCell::new(),
            select_by_owner: OnceCell::new(),
            select_by_marking: OnceCell::new(),
        }
    }

    async fn get_or_prepare<'a>(
        cell: &'a OnceCell<PreparedStatement>,
        session: &Session,
        cql: &str,
    ) -> RepoResult<&'a PreparedStatement> {
        cell.get_or_try_init(|| async { session.prepare(cql).await.map_err(driver_err) })
            .await
    }
}

/// `ObjectStore` backed by `ontology_objects.objects_by_id` plus the
/// three secondary index tables (`objects_by_type`, `objects_by_owner`,
/// `objects_by_marking`).
pub struct CassandraObjectStore {
    ctx: CassandraRepoCtx,
    prepared: ObjectPreparedStatements,
}

impl CassandraObjectStore {
    /// Build with the standard `ontology_objects` keyspace.
    pub fn new(session: Arc<Session>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, "ontology_objects"),
            prepared: ObjectPreparedStatements::new(),
        }
    }

    /// Build with a custom keyspace (multi-tenant override).
    pub fn with_keyspace(session: Arc<Session>, keyspace: impl Into<String>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, keyspace),
            prepared: ObjectPreparedStatements::new(),
        }
    }

    /// Eagerly prepare every statement this store may issue. Call
    /// from service startup to fold the prepare round-trips into
    /// cold-start time rather than the first request.
    pub async fn warm_up(&self) -> RepoResult<()> {
        self.stmt_insert_if_not_exists().await?;
        self.stmt_update_if_version().await?;
        self.stmt_select_by_id().await?;
        self.stmt_delete_by_id().await?;
        self.stmt_insert_index_by_type().await?;
        self.stmt_insert_index_by_owner().await?;
        self.stmt_insert_index_by_marking().await?;
        self.stmt_select_by_type().await?;
        self.stmt_select_by_owner().await?;
        self.stmt_select_by_marking().await?;
        Ok(())
    }

    async fn stmt_insert_if_not_exists(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.objects_by_id \
             (tenant, object_id, type_id, owner_id, properties, marking, revision_number, created_at, updated_at, deleted) \
             VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, false) IF NOT EXISTS",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_if_not_exists,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_update_if_version(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "UPDATE {ks}.objects_by_id \
             SET type_id = ?, owner_id = ?, properties = ?, marking = ?, \
                 revision_number = ?, updated_at = ?, deleted = false \
             WHERE tenant = ? AND object_id = ? IF revision_number = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.update_if_version,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_by_id(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT type_id, owner_id, properties, marking, revision_number, updated_at, deleted \
             FROM {ks}.objects_by_id WHERE tenant = ? AND object_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_by_id,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_delete_by_id(&self) -> RepoResult<&PreparedStatement> {
        // Soft-delete: mark `deleted = true`. Index tombstoning
        // happens lazily via the `deleted` column on the index rows.
        let cql = format!(
            "UPDATE {ks}.objects_by_id SET deleted = true, updated_at = ? \
             WHERE tenant = ? AND object_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.delete_by_id,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_insert_index_by_type(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.objects_by_type \
             (tenant, type_id, updated_at, object_id, owner_id, marking, properties_summary, deleted) \
             VALUES (?, ?, ?, ?, ?, ?, ?, false)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_index_by_type,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_insert_index_by_owner(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.objects_by_owner \
             (tenant, owner_id, type_id, object_id, updated_at, deleted) \
             VALUES (?, ?, ?, ?, ?, false)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_index_by_owner,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_insert_index_by_marking(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.objects_by_marking \
             (tenant, marking_id, object_id, type_id, owner_id, updated_at, deleted) \
             VALUES (?, ?, ?, ?, ?, ?, false)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_index_by_marking,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_by_type(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT object_id, owner_id, marking, properties_summary, updated_at, deleted \
             FROM {ks}.objects_by_type WHERE tenant = ? AND type_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_by_type,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_by_owner(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT object_id, type_id, updated_at, deleted \
             FROM {ks}.objects_by_owner WHERE tenant = ? AND owner_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_by_owner,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_by_marking(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT object_id, type_id, owner_id, updated_at, deleted \
             FROM {ks}.objects_by_marking WHERE tenant = ? AND marking_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_by_marking,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    /// Best-effort fan-out write to the three index tables. We do
    /// not LOGGED-batch them because they sit on different partitions
    /// across tables, which is the worst case for LOGGED per
    /// ADR-0020. Drift is repaired by the `tools/of-cli reindex` job.
    #[allow(clippy::too_many_arguments)]
    async fn write_indexes(
        &self,
        tenant: &str,
        object_uuid: Uuid,
        type_id: &str,
        owner_uuid: Uuid,
        markings: &HashSet<String>,
        properties_summary: &str,
        updated_at: CqlTimestamp,
    ) -> RepoResult<()> {
        let s = &self.ctx.session;
        let p_type = self.stmt_insert_index_by_type().await?.clone();
        let p_owner = self.stmt_insert_index_by_owner().await?.clone();
        let p_mark = self.stmt_insert_index_by_marking().await?.clone();

        s.execute(
            &p_type,
            (
                tenant,
                type_id,
                updated_at,
                object_uuid,
                owner_uuid,
                markings.clone(),
                properties_summary,
            ),
        )
        .await
        .map_err(driver_err)?;

        s.execute(
            &p_owner,
            (tenant, owner_uuid, type_id, object_uuid, updated_at),
        )
        .await
        .map_err(driver_err)?;

        for m in markings {
            s.execute(
                &p_mark,
                (
                    tenant,
                    m.as_str(),
                    object_uuid,
                    type_id,
                    owner_uuid,
                    updated_at,
                ),
            )
            .await
            .map_err(driver_err)?;
        }
        Ok(())
    }

    fn require_owner(o: &Object) -> RepoResult<Uuid> {
        let owner = o
            .owner
            .as_ref()
            .ok_or_else(|| invalid("Object.owner is required by the Cassandra schema"))?;
        parse_uuid("owner", &owner.0)
    }
}

#[async_trait]
impl ObjectStore for CassandraObjectStore {
    async fn get(
        &self,
        tenant: &TenantId,
        id: &ObjectId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<Object>> {
        let object_uuid = parse_uuid("object_id", &id.0)?;
        let mut prep = self.stmt_select_by_id().await?.clone();
        prep.set_consistency(cql_consistency(consistency));

        let result = self
            .ctx
            .session
            .execute(&prep, (tenant_str(tenant), object_uuid))
            .await
            .map_err(driver_err)?;

        let mut iter = result.rows_typed_or_empty::<(
            String,
            Uuid,
            String,
            Option<HashSet<String>>,
            i64,
            CqlTimestamp,
            Option<bool>,
        )>();
        let row = match iter.next() {
            Some(r) => r.map_err(driver_err)?,
            None => return Ok(None),
        };

        let (type_id, owner_uuid, properties, marking, revision, updated_at, deleted) = row;
        if deleted.unwrap_or(false) {
            return Ok(None);
        }

        let payload = serde_json::from_str(&properties)
            .map_err(|e| RepoError::Backend(format!("invalid stored JSON: {e}")))?;
        let markings = marking
            .unwrap_or_default()
            .into_iter()
            .map(MarkingId)
            .collect();

        Ok(Some(Object {
            tenant: tenant.clone(),
            id: id.clone(),
            type_id: TypeId(type_id),
            version: revision as u64,
            payload,
            updated_at_ms: cql_ts_to_ms(updated_at),
            owner: Some(OwnerId(owner_uuid.to_string())),
            markings,
        }))
    }

    async fn put(&self, obj: Object, expected_version: Option<u64>) -> RepoResult<PutOutcome> {
        let object_uuid = parse_uuid("object_id", &obj.id.0)?;
        let owner_uuid = Self::require_owner(&obj)?;
        let properties = serde_json::to_string(&obj.payload)
            .map_err(|e| invalid(format!("payload is not serialisable: {e}")))?;
        let markings: HashSet<String> = obj.markings.iter().map(|m| m.0.clone()).collect();
        let updated_at = ms_to_ts(obj.updated_at_ms);

        match expected_version {
            None => {
                let mut prep = self.stmt_insert_if_not_exists().await?.clone();
                prep.set_serial_consistency(Some(SerialConsistency::LocalSerial));
                let res = self
                    .ctx
                    .session
                    .execute(
                        &prep,
                        (
                            tenant_str(&obj.tenant),
                            object_uuid,
                            obj.type_id.0.as_str(),
                            owner_uuid,
                            properties.as_str(),
                            markings.clone(),
                            updated_at,
                            updated_at,
                        ),
                    )
                    .await
                    .map_err(driver_err)?;

                if !lwt_applied(&res) {
                    let actual = read_revision(&res).unwrap_or(0) as u64;
                    return Ok(PutOutcome::VersionConflict {
                        expected_version: 0,
                        actual_version: actual,
                    });
                }
                self.write_indexes(
                    tenant_str(&obj.tenant),
                    object_uuid,
                    &obj.type_id.0,
                    owner_uuid,
                    &markings,
                    truncate_summary(&properties),
                    updated_at,
                )
                .await?;
                Ok(PutOutcome::Inserted)
            }
            Some(expected) => {
                let new_version = (expected as i64) + 1;
                let mut prep = self.stmt_update_if_version().await?.clone();
                prep.set_serial_consistency(Some(SerialConsistency::LocalSerial));
                let res = self
                    .ctx
                    .session
                    .execute(
                        &prep,
                        (
                            obj.type_id.0.as_str(),
                            owner_uuid,
                            properties.as_str(),
                            markings.clone(),
                            new_version,
                            updated_at,
                            tenant_str(&obj.tenant),
                            object_uuid,
                            expected as i64,
                        ),
                    )
                    .await
                    .map_err(driver_err)?;

                if !lwt_applied(&res) {
                    let actual = read_revision(&res).unwrap_or(expected as i64) as u64;
                    return Ok(PutOutcome::VersionConflict {
                        expected_version: expected,
                        actual_version: actual,
                    });
                }
                self.write_indexes(
                    tenant_str(&obj.tenant),
                    object_uuid,
                    &obj.type_id.0,
                    owner_uuid,
                    &markings,
                    truncate_summary(&properties),
                    updated_at,
                )
                .await?;
                Ok(PutOutcome::Updated {
                    previous_version: expected,
                    new_version: new_version as u64,
                })
            }
        }
    }

    async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool> {
        let object_uuid = parse_uuid("object_id", &id.0)?;
        let prep = self.stmt_delete_by_id().await?.clone();
        let now = ms_to_ts(Utc::now().timestamp_millis());
        self.ctx
            .session
            .execute(&prep, (now, tenant_str(tenant), object_uuid))
            .await
            .map_err(driver_err)?;
        // Cassandra does not surface `rows_affected`. Caller is
        // responsible for treating double-deletes as no-ops.
        Ok(true)
    }

    async fn list_by_type(
        &self,
        tenant: &TenantId,
        type_id: &TypeId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        let mut prep = self.stmt_select_by_type().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        prep.set_page_size(clamp_page_size(page.size));

        let paging = decode_paging_state(page.token.as_deref())?;
        let result = self
            .ctx
            .session
            .execute_paged(&prep, (tenant_str(tenant), type_id.0.as_str()), paging)
            .await
            .map_err(driver_err)?;

        let next_token = encode_paging_state(result.paging_state.clone());
        let mut items = Vec::new();
        for row in result.rows_typed_or_empty::<(
            Uuid,
            Uuid,
            Option<HashSet<String>>,
            Option<String>,
            CqlTimestamp,
            Option<bool>,
        )>() {
            let (object_id, owner_id, marking, summary, updated_at, deleted) =
                row.map_err(driver_err)?;
            if deleted.unwrap_or(false) {
                continue;
            }
            items.push(Object {
                tenant: tenant.clone(),
                id: ObjectId(object_id.to_string()),
                type_id: type_id.clone(),
                version: 0, // index row does not carry the revision; caller `get`s for that.
                payload: summary
                    .as_deref()
                    .and_then(|s| serde_json::from_str(s).ok())
                    .unwrap_or_else(|| serde_json::json!({})),
                updated_at_ms: cql_ts_to_ms(updated_at),
                owner: Some(OwnerId(owner_id.to_string())),
                markings: marking
                    .unwrap_or_default()
                    .into_iter()
                    .map(MarkingId)
                    .collect(),
            });
        }
        Ok(PagedResult { items, next_token })
    }

    async fn list_by_owner(
        &self,
        tenant: &TenantId,
        owner: &OwnerId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        let owner_uuid = parse_uuid("owner_id", &owner.0)?;
        let mut prep = self.stmt_select_by_owner().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        prep.set_page_size(clamp_page_size(page.size));

        let paging = decode_paging_state(page.token.as_deref())?;
        let result = self
            .ctx
            .session
            .execute_paged(&prep, (tenant_str(tenant), owner_uuid), paging)
            .await
            .map_err(driver_err)?;

        let next_token = encode_paging_state(result.paging_state.clone());
        let mut items = Vec::new();
        for row in result.rows_typed_or_empty::<(Uuid, String, CqlTimestamp, Option<bool>)>() {
            let (object_id, type_id, updated_at, deleted) = row.map_err(driver_err)?;
            if deleted.unwrap_or(false) {
                continue;
            }
            items.push(Object {
                tenant: tenant.clone(),
                id: ObjectId(object_id.to_string()),
                type_id: TypeId(type_id),
                version: 0,
                payload: serde_json::json!({}),
                updated_at_ms: cql_ts_to_ms(updated_at),
                owner: Some(owner.clone()),
                markings: Vec::new(),
            });
        }
        Ok(PagedResult { items, next_token })
    }

    async fn list_by_marking(
        &self,
        tenant: &TenantId,
        marking: &MarkingId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        let mut prep = self.stmt_select_by_marking().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        prep.set_page_size(clamp_page_size(page.size));

        let paging = decode_paging_state(page.token.as_deref())?;
        let result = self
            .ctx
            .session
            .execute_paged(&prep, (tenant_str(tenant), marking.0.as_str()), paging)
            .await
            .map_err(driver_err)?;

        let next_token = encode_paging_state(result.paging_state.clone());
        let mut items = Vec::new();
        for row in result.rows_typed_or_empty::<(Uuid, String, Uuid, CqlTimestamp, Option<bool>)>()
        {
            let (object_id, type_id, owner_id, updated_at, deleted) = row.map_err(driver_err)?;
            if deleted.unwrap_or(false) {
                continue;
            }
            items.push(Object {
                tenant: tenant.clone(),
                id: ObjectId(object_id.to_string()),
                type_id: TypeId(type_id),
                version: 0,
                payload: serde_json::json!({}),
                updated_at_ms: cql_ts_to_ms(updated_at),
                owner: Some(OwnerId(owner_id.to_string())),
                markings: vec![marking.clone()],
            });
        }
        Ok(PagedResult { items, next_token })
    }
}

// ---------------------------------------------------------------------------
// LinkStore (S1.4 wiring)
// ---------------------------------------------------------------------------

/// `LinkStore` backed by `ontology_indexes.links_by_source` and
/// `links_by_target`.
pub struct CassandraLinkStore {
    ctx: CassandraRepoCtx,
    prepared: LinkPreparedStatements,
}

struct LinkPreparedStatements {
    insert_outgoing: OnceCell<PreparedStatement>,
    insert_incoming: OnceCell<PreparedStatement>,
    delete_outgoing: OnceCell<PreparedStatement>,
    delete_incoming: OnceCell<PreparedStatement>,
    select_outgoing: OnceCell<PreparedStatement>,
    select_incoming: OnceCell<PreparedStatement>,
    select_outgoing_exact: OnceCell<PreparedStatement>,
}

impl LinkPreparedStatements {
    fn new() -> Self {
        Self {
            insert_outgoing: OnceCell::new(),
            insert_incoming: OnceCell::new(),
            delete_outgoing: OnceCell::new(),
            delete_incoming: OnceCell::new(),
            select_outgoing: OnceCell::new(),
            select_incoming: OnceCell::new(),
            select_outgoing_exact: OnceCell::new(),
        }
    }
}

impl CassandraLinkStore {
    /// Build with the standard `ontology_indexes` keyspace.
    pub fn new(session: Arc<Session>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, "ontology_indexes"),
            prepared: LinkPreparedStatements::new(),
        }
    }

    /// Build with a custom keyspace.
    pub fn with_keyspace(session: Arc<Session>, keyspace: impl Into<String>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, keyspace),
            prepared: LinkPreparedStatements::new(),
        }
    }

    /// Eagerly prepare every statement this store may issue.
    pub async fn warm_up(&self) -> RepoResult<()> {
        self.stmt_insert_outgoing().await?;
        self.stmt_insert_incoming().await?;
        self.stmt_delete_outgoing().await?;
        self.stmt_delete_incoming().await?;
        self.stmt_select_outgoing().await?;
        self.stmt_select_incoming().await?;
        self.stmt_select_outgoing_exact().await?;
        Ok(())
    }

    async fn stmt_insert_outgoing(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.links_outgoing \
             (tenant, source_id, link_type, target_id, target_type, properties, created_at) \
             VALUES (?, ?, ?, ?, ?, ?, ?)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_outgoing,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_insert_incoming(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.links_incoming \
             (tenant, target_id, link_type, source_id, source_type, properties, created_at) \
             VALUES (?, ?, ?, ?, ?, ?, ?)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_incoming,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_delete_outgoing(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "DELETE FROM {ks}.links_outgoing \
             WHERE tenant = ? AND source_id = ? AND link_type = ? AND target_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.delete_outgoing,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_delete_incoming(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "DELETE FROM {ks}.links_incoming \
             WHERE tenant = ? AND target_id = ? AND link_type = ? AND source_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.delete_incoming,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_outgoing(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT target_id, properties, created_at \
             FROM {ks}.links_outgoing \
             WHERE tenant = ? AND source_id = ? AND link_type = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_outgoing,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_incoming(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT source_id, properties, created_at \
             FROM {ks}.links_incoming \
             WHERE tenant = ? AND target_id = ? AND link_type = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_incoming,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_outgoing_exact(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT target_id FROM {ks}.links_outgoing \
             WHERE tenant = ? AND source_id = ? AND link_type = ? AND target_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_outgoing_exact,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    fn payload_to_cql(payload: Option<serde_json::Value>) -> RepoResult<Option<String>> {
        payload
            .map(|value| {
                serde_json::to_string(&value)
                    .map_err(|e| invalid(format!("link payload is not serialisable: {e}")))
            })
            .transpose()
    }

    fn payload_from_cql(raw: Option<String>) -> RepoResult<Option<serde_json::Value>> {
        raw.map(|s| {
            serde_json::from_str(&s)
                .map_err(|e| RepoError::Backend(format!("invalid stored link JSON: {e}")))
        })
        .transpose()
    }
}

#[async_trait]
impl LinkStore for CassandraLinkStore {
    async fn put(&self, link: Link) -> RepoResult<()> {
        let source_id = parse_uuid("from", &link.from.0)?;
        let target_id = parse_uuid("to", &link.to.0)?;
        let created_at = ms_to_ts(link.created_at_ms);
        let payload = Self::payload_to_cql(link.payload)?;
        let outgoing = self.stmt_insert_outgoing().await?.clone();
        let incoming = self.stmt_insert_incoming().await?.clone();

        self.ctx
            .session
            .execute(
                &outgoing,
                (
                    tenant_str(&link.tenant),
                    source_id,
                    link.link_type.0.as_str(),
                    target_id,
                    "",
                    payload.as_deref(),
                    created_at,
                ),
            )
            .await
            .map_err(driver_err)?;

        self.ctx
            .session
            .execute(
                &incoming,
                (
                    tenant_str(&link.tenant),
                    target_id,
                    link.link_type.0.as_str(),
                    source_id,
                    "",
                    payload.as_deref(),
                    created_at,
                ),
            )
            .await
            .map_err(driver_err)?;

        Ok(())
    }

    async fn delete(
        &self,
        tenant: &TenantId,
        link_type: &LinkTypeId,
        from: &ObjectId,
        to: &ObjectId,
    ) -> RepoResult<bool> {
        let source_id = parse_uuid("from", &from.0)?;
        let target_id = parse_uuid("to", &to.0)?;
        let mut exact = self.stmt_select_outgoing_exact().await?.clone();
        exact.set_consistency(Consistency::LocalQuorum);
        let existed = self
            .ctx
            .session
            .execute(
                &exact,
                (
                    tenant_str(tenant),
                    source_id,
                    link_type.0.as_str(),
                    target_id,
                ),
            )
            .await
            .map_err(driver_err)?
            .rows
            .as_ref()
            .map(|rows| !rows.is_empty())
            .unwrap_or(false);

        let outgoing = self.stmt_delete_outgoing().await?.clone();
        let incoming = self.stmt_delete_incoming().await?.clone();
        self.ctx
            .session
            .execute(
                &outgoing,
                (
                    tenant_str(tenant),
                    source_id,
                    link_type.0.as_str(),
                    target_id,
                ),
            )
            .await
            .map_err(driver_err)?;
        self.ctx
            .session
            .execute(
                &incoming,
                (
                    tenant_str(tenant),
                    target_id,
                    link_type.0.as_str(),
                    source_id,
                ),
            )
            .await
            .map_err(driver_err)?;
        Ok(existed)
    }

    async fn list_outgoing(
        &self,
        tenant: &TenantId,
        link_type: &LinkTypeId,
        from: &ObjectId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Link>> {
        let source_id = parse_uuid("from", &from.0)?;
        let mut prep = self.stmt_select_outgoing().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        prep.set_page_size(clamp_page_size(page.size));
        let paging = decode_paging_state(page.token.as_deref())?;
        let result = self
            .ctx
            .session
            .execute_paged(
                &prep,
                (tenant_str(tenant), source_id, link_type.0.as_str()),
                paging,
            )
            .await
            .map_err(driver_err)?;
        let next_token = encode_paging_state(result.paging_state.clone());
        let mut items = Vec::new();
        for row in result.rows_typed_or_empty::<(Uuid, Option<String>, CqlTimestamp)>() {
            let (target_id, properties, created_at) = row.map_err(driver_err)?;
            items.push(Link {
                tenant: tenant.clone(),
                link_type: link_type.clone(),
                from: from.clone(),
                to: ObjectId(target_id.to_string()),
                payload: Self::payload_from_cql(properties)?,
                created_at_ms: cql_ts_to_ms(created_at),
            });
        }
        Ok(PagedResult { items, next_token })
    }

    async fn list_incoming(
        &self,
        tenant: &TenantId,
        link_type: &LinkTypeId,
        to: &ObjectId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Link>> {
        let target_id = parse_uuid("to", &to.0)?;
        let mut prep = self.stmt_select_incoming().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        prep.set_page_size(clamp_page_size(page.size));
        let paging = decode_paging_state(page.token.as_deref())?;
        let result = self
            .ctx
            .session
            .execute_paged(
                &prep,
                (tenant_str(tenant), target_id, link_type.0.as_str()),
                paging,
            )
            .await
            .map_err(driver_err)?;
        let next_token = encode_paging_state(result.paging_state.clone());
        let mut items = Vec::new();
        for row in result.rows_typed_or_empty::<(Uuid, Option<String>, CqlTimestamp)>() {
            let (source_id, properties, created_at) = row.map_err(driver_err)?;
            items.push(Link {
                tenant: tenant.clone(),
                link_type: link_type.clone(),
                from: ObjectId(source_id.to_string()),
                to: to.clone(),
                payload: Self::payload_from_cql(properties)?,
                created_at_ms: cql_ts_to_ms(created_at),
            });
        }
        Ok(PagedResult { items, next_token })
    }
}

// ---------------------------------------------------------------------------
// SchemaStore (kept on PG until S1.6)
// ---------------------------------------------------------------------------

/// `SchemaStore` placeholder. Schema data stays in PostgreSQL until
/// the schema-cluster migration in S1.6.
pub struct CassandraSchemaStore {
    #[allow(dead_code)]
    ctx: CassandraRepoCtx,
}

impl CassandraSchemaStore {
    /// Build with the standard `ontology_objects` keyspace.
    pub fn new(session: Arc<Session>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, "ontology_objects"),
        }
    }
}

#[async_trait]
impl SchemaStore for CassandraSchemaStore {
    async fn get_latest(
        &self,
        _type_id: &TypeId,
        _consistency: ReadConsistency,
    ) -> RepoResult<Option<Schema>> {
        pending()
    }
    async fn get_version(
        &self,
        _type_id: &TypeId,
        _version: u32,
        _consistency: ReadConsistency,
    ) -> RepoResult<Option<Schema>> {
        pending()
    }
    async fn put(&self, _schema: Schema) -> RepoResult<()> {
        pending()
    }
}

// ---------------------------------------------------------------------------
// SessionStore (kept on PG until S1.6)
// ---------------------------------------------------------------------------

/// `SessionStore` placeholder. Sessions stay in PostgreSQL until S1.6.
pub struct CassandraSessionStore {
    #[allow(dead_code)]
    ctx: CassandraRepoCtx,
}

impl CassandraSessionStore {
    /// Build with the standard `auth_runtime` keyspace.
    pub fn new(session: Arc<Session>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, "auth_runtime"),
        }
    }
}

#[async_trait]
impl SessionStore for CassandraSessionStore {
    async fn get(
        &self,
        _tenant: &TenantId,
        _id: &str,
        _consistency: ReadConsistency,
    ) -> RepoResult<Option<RepoSession>> {
        pending()
    }
    async fn put(&self, _session: RepoSession) -> RepoResult<()> {
        pending()
    }
    async fn revoke(&self, _tenant: &TenantId, _id: &str) -> RepoResult<bool> {
        pending()
    }
}

// ---------------------------------------------------------------------------
// ActionLogStore (S1.4 wiring)
// ---------------------------------------------------------------------------

/// `ActionLogStore` backed by `actions_log.actions_log` and
/// `actions_by_object`.
pub struct CassandraActionLogStore {
    ctx: CassandraRepoCtx,
    prepared: ActionLogPreparedStatements,
}

struct ActionLogPreparedStatements {
    insert_log: OnceCell<PreparedStatement>,
    insert_by_object: OnceCell<PreparedStatement>,
    select_recent: OnceCell<PreparedStatement>,
    select_by_object: OnceCell<PreparedStatement>,
}

#[derive(serde::Deserialize, serde::Serialize)]
struct ActionRecentToken {
    day: u32,
    days_scanned: u8,
    paging: Option<String>,
}

impl ActionLogPreparedStatements {
    fn new() -> Self {
        Self {
            insert_log: OnceCell::new(),
            insert_by_object: OnceCell::new(),
            select_recent: OnceCell::new(),
            select_by_object: OnceCell::new(),
        }
    }
}

impl CassandraActionLogStore {
    /// Build with the standard `actions_log` keyspace.
    pub fn new(session: Arc<Session>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, "actions_log"),
            prepared: ActionLogPreparedStatements::new(),
        }
    }

    /// Build with a custom keyspace.
    pub fn with_keyspace(session: Arc<Session>, keyspace: impl Into<String>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, keyspace),
            prepared: ActionLogPreparedStatements::new(),
        }
    }

    /// Eagerly prepare every statement this store may issue.
    pub async fn warm_up(&self) -> RepoResult<()> {
        self.stmt_insert_log().await?;
        self.stmt_insert_by_object().await?;
        self.stmt_select_recent().await?;
        self.stmt_select_by_object().await?;
        Ok(())
    }

    async fn stmt_insert_log(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.actions_log \
             (tenant, day_bucket, applied_at, action_id, kind, actor_id, subject, \
              target_object_id, target_type_id, payload, status, failure_type, duration_ms) \
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(&self.prepared.insert_log, &self.ctx.session, &cql)
            .await
    }

    async fn stmt_insert_by_object(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.actions_by_object \
             (tenant, target_object_id, applied_at, action_id, kind, actor_id, subject, payload) \
             VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_by_object,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_recent(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT applied_at, action_id, kind, actor_id, subject, target_object_id, payload \
             FROM {ks}.actions_log WHERE tenant = ? AND day_bucket = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_recent,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_by_object(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT applied_at, action_id, kind, actor_id, subject, payload \
             FROM {ks}.actions_by_object WHERE tenant = ? AND target_object_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_by_object,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    fn encode_recent_token(token: ActionRecentToken) -> RepoResult<String> {
        let json = serde_json::to_vec(&token)
            .map_err(|e| RepoError::Backend(format!("recent token encode failed: {e}")))?;
        Ok(B64.encode(json))
    }

    fn decode_recent_token(raw: Option<&str>) -> RepoResult<ActionRecentToken> {
        match raw {
            Some(token) => {
                let json = B64
                    .decode(token.as_bytes())
                    .map_err(|e| invalid(format!("malformed action-log page token: {e}")))?;
                serde_json::from_slice(&json)
                    .map_err(|e| invalid(format!("malformed action-log page token: {e}")))
            }
            None => Ok(ActionRecentToken {
                day: ms_to_day(Utc::now().timestamp_millis()).0,
                days_scanned: 0,
                paging: None,
            }),
        }
    }

    fn subject_from(actor_id: Option<Uuid>, subject: Option<String>) -> String {
        subject
            .filter(|s| !s.trim().is_empty())
            .or_else(|| actor_id.map(|id| id.to_string()))
            .unwrap_or_default()
    }

    fn action_payload_to_cql(payload: &serde_json::Value) -> RepoResult<String> {
        serde_json::to_string(payload)
            .map_err(|e| invalid(format!("action payload is not serialisable: {e}")))
    }

    fn action_payload_from_cql(raw: String) -> RepoResult<serde_json::Value> {
        serde_json::from_str(&raw)
            .map_err(|e| RepoError::Backend(format!("invalid stored action payload JSON: {e}")))
    }
}

#[async_trait]
impl ActionLogStore for CassandraActionLogStore {
    async fn append(&self, entry: ActionLogEntry) -> RepoResult<()> {
        let action_id = parse_uuid("action_id", &entry.action_id)?;
        let actor_id = Uuid::parse_str(&entry.subject).ok();
        let target_object_id = entry
            .object
            .as_ref()
            .map(|object| parse_uuid("object", &object.0))
            .transpose()?;
        let payload = Self::action_payload_to_cql(&entry.payload)?;
        let applied_at = ms_to_ts(entry.recorded_at_ms);
        let day_bucket = ms_to_day(entry.recorded_at_ms);
        let target_type_id: Option<&str> = None;
        let status = entry
            .payload
            .get("status")
            .and_then(serde_json::Value::as_str)
            .unwrap_or("applied");
        let failure_type = entry
            .payload
            .get("failure_type")
            .and_then(serde_json::Value::as_str);
        let duration_ms = entry
            .payload
            .get("duration_ms")
            .and_then(serde_json::Value::as_i64)
            .and_then(|n| i32::try_from(n).ok());

        let insert_log = self.stmt_insert_log().await?.clone();
        self.ctx
            .session
            .execute(
                &insert_log,
                (
                    tenant_str(&entry.tenant),
                    day_bucket,
                    applied_at,
                    action_id,
                    entry.kind.as_str(),
                    actor_id,
                    entry.subject.as_str(),
                    target_object_id,
                    target_type_id,
                    payload.as_str(),
                    Some(status),
                    failure_type,
                    duration_ms,
                ),
            )
            .await
            .map_err(driver_err)?;

        if let Some(object_id) = target_object_id {
            let insert_by_object = self.stmt_insert_by_object().await?.clone();
            self.ctx
                .session
                .execute(
                    &insert_by_object,
                    (
                        tenant_str(&entry.tenant),
                        object_id,
                        applied_at,
                        action_id,
                        entry.kind.as_str(),
                        actor_id,
                        entry.subject.as_str(),
                        payload.as_str(),
                    ),
                )
                .await
                .map_err(driver_err)?;
        }

        Ok(())
    }

    async fn list_recent(
        &self,
        tenant: &TenantId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>> {
        let mut token = Self::decode_recent_token(page.token.as_deref())?;
        let limit = clamp_page_size(page.size) as usize;
        let mut items = Vec::with_capacity(limit.min(128));
        let mut next_token = None;

        while items.len() < limit && token.days_scanned < ACTION_LOG_LOOKBACK_DAYS {
            let mut prep = self.stmt_select_recent().await?.clone();
            prep.set_consistency(cql_consistency(consistency));
            prep.set_page_size((limit - items.len()).clamp(1, 5_000) as i32);
            let paging = match token.paging.take() {
                Some(raw) => decode_paging_state(Some(raw.as_str()))?,
                None => None,
            };

            let result = self
                .ctx
                .session
                .execute_paged(&prep, (tenant_str(tenant), CqlDate(token.day)), paging)
                .await
                .map_err(driver_err)?;
            let page_state = result.paging_state.clone();

            for row in result.rows_typed_or_empty::<(
                CqlTimestamp,
                Uuid,
                String,
                Option<Uuid>,
                Option<String>,
                Option<Uuid>,
                String,
            )>() {
                let (applied_at, action_id, kind, actor_id, subject, object_id, payload) =
                    row.map_err(driver_err)?;
                items.push(ActionLogEntry {
                    tenant: tenant.clone(),
                    action_id: action_id.to_string(),
                    kind,
                    subject: Self::subject_from(actor_id, subject),
                    object: object_id.map(|id| ObjectId(id.to_string())),
                    payload: Self::action_payload_from_cql(payload)?,
                    recorded_at_ms: cql_ts_to_ms(applied_at),
                });
            }

            if page_state.is_some() {
                token.paging = encode_paging_state(page_state);
                next_token = Some(Self::encode_recent_token(token)?);
                break;
            }

            let Some(previous_day) = token.day.checked_sub(1) else {
                break;
            };
            token.day = previous_day;
            token.days_scanned += 1;
            token.paging = None;

            if items.len() >= limit && token.days_scanned < ACTION_LOG_LOOKBACK_DAYS {
                next_token = Some(Self::encode_recent_token(token)?);
                break;
            }
        }

        Ok(PagedResult { items, next_token })
    }

    async fn list_for_object(
        &self,
        tenant: &TenantId,
        object: &ObjectId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>> {
        let object_id = parse_uuid("object", &object.0)?;
        let mut prep = self.stmt_select_by_object().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        prep.set_page_size(clamp_page_size(page.size));
        let paging = decode_paging_state(page.token.as_deref())?;
        let result = self
            .ctx
            .session
            .execute_paged(&prep, (tenant_str(tenant), object_id), paging)
            .await
            .map_err(driver_err)?;
        let next_token = encode_paging_state(result.paging_state.clone());
        let mut items = Vec::new();
        for row in result.rows_typed_or_empty::<(
            CqlTimestamp,
            Uuid,
            String,
            Option<Uuid>,
            Option<String>,
            String,
        )>() {
            let (applied_at, action_id, kind, actor_id, subject, payload) =
                row.map_err(driver_err)?;
            items.push(ActionLogEntry {
                tenant: tenant.clone(),
                action_id: action_id.to_string(),
                kind,
                subject: Self::subject_from(actor_id, subject),
                object: Some(object.clone()),
                payload: Self::action_payload_from_cql(payload)?,
                recorded_at_ms: cql_ts_to_ms(applied_at),
            });
        }
        Ok(PagedResult { items, next_token })
    }
}

// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn paging_state_round_trip() {
        let raw = Bytes::from_static(b"opaque-binary-state");
        let encoded = encode_paging_state(Some(raw.clone())).unwrap();
        let decoded = decode_paging_state(Some(&encoded)).unwrap().unwrap();
        assert_eq!(decoded, raw);
    }

    #[test]
    fn page_size_is_clamped() {
        assert_eq!(clamp_page_size(0), 1);
        assert_eq!(clamp_page_size(10), 10);
        assert_eq!(clamp_page_size(10_000), 5_000);
    }

    #[test]
    fn cql_consistency_strong_maps_to_local_quorum() {
        assert_eq!(
            cql_consistency(ReadConsistency::Strong),
            Consistency::LocalQuorum
        );
        assert_eq!(
            cql_consistency(ReadConsistency::Eventual),
            Consistency::LocalOne
        );
    }

    #[test]
    fn truncate_summary_caps_at_1024() {
        let big = "x".repeat(2_000);
        assert_eq!(truncate_summary(&big).len(), 1024);
        assert_eq!(truncate_summary("tiny"), "tiny");
    }
}
