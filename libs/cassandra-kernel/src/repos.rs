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
//! [`CassandraLinkStore`], [`CassandraActionLogStore`],
//! [`CassandraSchemaStore`] and [`CassandraSessionStore`] are wired
//! against the S1.1/S3 Cassandra tables. Declarative ontology
//! definitions still live in `pg-schemas`; `CassandraSchemaStore`
//! only stores the per-object JSON Schema versions referenced by the
//! storage abstraction trait.
//!
//! ## Object ↔ Row mapping
//!
//! The `properties` column is `text` (canonical JSON). Versioned
//! schemas live elsewhere; `CassandraObjectStore` is intentionally
//! schema-free — it rejects only invalid JSON. Markings are mapped
//! to `frozen<set<text>>`. Owner is mapped to `uuid` and is required
//! by the table definition; objects without an `owner` are rejected
//! with [`RepoError::InvalidArgument`].

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use async_trait::async_trait;
use base64::{Engine as _, engine::general_purpose::STANDARD as B64};
use bytes::Bytes;
use chrono::Utc;
use scylla::Session;
use scylla::frame::value::{CqlDate, CqlTimestamp, CqlTimeuuid};
use scylla::prepared_statement::PreparedStatement;
use scylla::statement::{Consistency, SerialConsistency};
use tokio::sync::OnceCell;
use uuid::Uuid;

use storage_abstraction::repositories::{
    ActionLogEntry, ActionLogStore, Link, LinkStore, LinkTypeId, MarkingId, Object, ObjectId,
    ObjectStore, OwnerId, Page, PagedResult, PutOutcome, ReadConsistency, RepoError, RepoResult,
    Schema, SchemaStore, Session as RepoSession, SessionStore, TenantId, TypeId,
};

const ACTION_LOG_LOOKBACK_DAYS: u8 = 90;

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

fn parse_timeuuid(field: &str, raw: &str) -> RepoResult<CqlTimeuuid> {
    parse_uuid(field, raw).map(CqlTimeuuid::from)
}

fn timeuuid_to_string(value: CqlTimeuuid) -> String {
    Uuid::from(value).to_string()
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

fn organization_id_from_tenant(t: &TenantId) -> Option<String> {
    Uuid::parse_str(t.0.as_str()).ok().map(|id| id.to_string())
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
             (tenant, object_id, type_id, owner_id, properties, marking, organization_id, revision_number, created_at, updated_at, deleted) \
             VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, false) IF NOT EXISTS",
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
             SET type_id = ?, owner_id = ?, properties = ?, marking = ?, organization_id = ?, \
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
            "SELECT type_id, owner_id, properties, marking, organization_id, revision_number, created_at, updated_at, deleted \
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

    fn organization_uuid(o: &Object) -> RepoResult<Option<Uuid>> {
        o.organization_id
            .as_deref()
            .map(|raw| parse_uuid("organization_id", raw))
            .transpose()
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
            Option<Uuid>,
            i64,
            Option<CqlTimestamp>,
            CqlTimestamp,
            Option<bool>,
        )>();
        let row = match iter.next() {
            Some(r) => r.map_err(driver_err)?,
            None => return Ok(None),
        };

        let (
            type_id,
            owner_uuid,
            properties,
            marking,
            organization_id,
            revision,
            created_at,
            updated_at,
            deleted,
        ) = row;
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
            organization_id: organization_id.map(|id| id.to_string()),
            created_at_ms: created_at.map(cql_ts_to_ms),
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
        let organization_id = Self::organization_uuid(&obj)?;
        let created_at = ms_to_ts(obj.created_at_ms.unwrap_or(obj.updated_at_ms));
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
                            organization_id,
                            created_at,
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
                            organization_id,
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
                organization_id: organization_id_from_tenant(tenant),
                created_at_ms: Some(cql_ts_to_ms(updated_at)),
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
                organization_id: organization_id_from_tenant(tenant),
                created_at_ms: Some(cql_ts_to_ms(updated_at)),
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
                organization_id: organization_id_from_tenant(tenant),
                created_at_ms: Some(cql_ts_to_ms(updated_at)),
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

/// `LinkStore` backed by `ontology_indexes.links_outgoing` and
/// `links_incoming`.
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
             VALUES (?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS",
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
             VALUES (?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS",
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
        let source_id = parse_timeuuid("from", &link.from.0)?;
        let target_id = parse_timeuuid("to", &link.to.0)?;
        let created_at = ms_to_ts(link.created_at_ms);
        let payload = Self::payload_to_cql(link.payload)?;
        let mut outgoing = self.stmt_insert_outgoing().await?.clone();
        outgoing.set_consistency(Consistency::LocalQuorum);
        outgoing.set_serial_consistency(Some(SerialConsistency::LocalSerial));
        let mut incoming = self.stmt_insert_incoming().await?.clone();
        incoming.set_consistency(Consistency::LocalQuorum);
        incoming.set_serial_consistency(Some(SerialConsistency::LocalSerial));

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
        let source_id = parse_timeuuid("from", &from.0)?;
        let target_id = parse_timeuuid("to", &to.0)?;
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

        let mut outgoing = self.stmt_delete_outgoing().await?.clone();
        outgoing.set_consistency(Consistency::LocalQuorum);
        let mut incoming = self.stmt_delete_incoming().await?.clone();
        incoming.set_consistency(Consistency::LocalQuorum);
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
        let source_id = parse_timeuuid("from", &from.0)?;
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
        for row in result.rows_typed_or_empty::<(CqlTimeuuid, Option<String>, CqlTimestamp)>() {
            let (target_id, properties, created_at) = row.map_err(driver_err)?;
            items.push(Link {
                tenant: tenant.clone(),
                link_type: link_type.clone(),
                from: from.clone(),
                to: ObjectId(timeuuid_to_string(target_id)),
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
        let target_id = parse_timeuuid("to", &to.0)?;
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
        for row in result.rows_typed_or_empty::<(CqlTimeuuid, Option<String>, CqlTimestamp)>() {
            let (source_id, properties, created_at) = row.map_err(driver_err)?;
            items.push(Link {
                tenant: tenant.clone(),
                link_type: link_type.clone(),
                from: ObjectId(timeuuid_to_string(source_id)),
                to: to.clone(),
                payload: Self::payload_from_cql(properties)?,
                created_at_ms: cql_ts_to_ms(created_at),
            });
        }
        Ok(PagedResult { items, next_token })
    }
}

// ---------------------------------------------------------------------------
// SchemaStore
// ---------------------------------------------------------------------------

/// `SchemaStore` backed by `ontology_objects.schemas_by_type` and
/// `schemas_latest`.
///
/// This is deliberately narrower than the declarative ontology catalog:
/// object type/link/action definitions remain in `pg-schemas.ontology_schema`.
/// The Cassandra table stores only versioned JSON Schema payloads used by
/// runtime object validation.
pub struct CassandraSchemaStore {
    ctx: CassandraRepoCtx,
    prepared: SchemaPreparedStatements,
}

struct SchemaPreparedStatements {
    insert_version: OnceCell<PreparedStatement>,
    delete_version: OnceCell<PreparedStatement>,
    insert_latest: OnceCell<PreparedStatement>,
    update_latest_if_version: OnceCell<PreparedStatement>,
    select_latest: OnceCell<PreparedStatement>,
    select_version: OnceCell<PreparedStatement>,
}

impl SchemaPreparedStatements {
    fn new() -> Self {
        Self {
            insert_version: OnceCell::new(),
            delete_version: OnceCell::new(),
            insert_latest: OnceCell::new(),
            update_latest_if_version: OnceCell::new(),
            select_latest: OnceCell::new(),
            select_version: OnceCell::new(),
        }
    }
}

impl CassandraSchemaStore {
    /// Build with the standard `ontology_objects` keyspace.
    pub fn new(session: Arc<Session>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, "ontology_objects"),
            prepared: SchemaPreparedStatements::new(),
        }
    }

    /// Build with a custom keyspace.
    pub fn with_keyspace(session: Arc<Session>, keyspace: impl Into<String>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, keyspace),
            prepared: SchemaPreparedStatements::new(),
        }
    }

    /// Eagerly prepare every statement this store may issue.
    pub async fn warm_up(&self) -> RepoResult<()> {
        self.stmt_insert_version().await?;
        self.stmt_delete_version().await?;
        self.stmt_insert_latest().await?;
        self.stmt_update_latest_if_version().await?;
        self.stmt_select_latest().await?;
        self.stmt_select_version().await?;
        Ok(())
    }

    async fn stmt_insert_version(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.schemas_by_type \
             (type_id, version, json_schema, created_at) VALUES (?, ?, ?, ?) IF NOT EXISTS",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_version,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_delete_version(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "DELETE FROM {ks}.schemas_by_type WHERE type_id = ? AND version = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.delete_version,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_insert_latest(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.schemas_latest \
             (type_id, version, json_schema, created_at) VALUES (?, ?, ?, ?) IF NOT EXISTS",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_latest,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_update_latest_if_version(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "UPDATE {ks}.schemas_latest SET version = ?, json_schema = ?, created_at = ? \
             WHERE type_id = ? IF version = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.update_latest_if_version,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_latest(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT version, json_schema, created_at FROM {ks}.schemas_latest WHERE type_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_latest,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_version(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT json_schema, created_at FROM {ks}.schemas_by_type \
             WHERE type_id = ? AND version = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_version,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    fn schema_version_to_cql(version: u32) -> RepoResult<i32> {
        if version == 0 {
            return Err(invalid("schema version must be greater than zero"));
        }
        i32::try_from(version).map_err(|_| invalid("schema version exceeds CQL int range"))
    }

    fn schema_version_from_cql(version: i32) -> RepoResult<u32> {
        u32::try_from(version).map_err(|_| {
            RepoError::Backend(format!("stored schema version is negative: {version}"))
        })
    }

    fn schema_json_to_cql(schema: &serde_json::Value) -> RepoResult<String> {
        serde_json::to_string(schema)
            .map_err(|e| invalid(format!("schema JSON is not serialisable: {e}")))
    }

    fn schema_json_from_cql(raw: String) -> RepoResult<serde_json::Value> {
        serde_json::from_str(&raw)
            .map_err(|e| RepoError::Backend(format!("invalid stored schema JSON: {e}")))
    }

    async fn select_latest_raw(
        &self,
        type_id: &TypeId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<(i32, String, CqlTimestamp)>> {
        let mut prep = self.stmt_select_latest().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        let result = self
            .ctx
            .session
            .execute(&prep, (type_id.0.as_str(),))
            .await
            .map_err(driver_err)?;
        let mut rows = result.rows_typed_or_empty::<(i32, String, CqlTimestamp)>();
        match rows.next() {
            Some(row) => row.map(Some).map_err(driver_err),
            None => Ok(None),
        }
    }

    async fn delete_version_best_effort(&self, type_id: &TypeId, version: i32) {
        if let Ok(prep) = self.stmt_delete_version().await {
            let _ = self
                .ctx
                .session
                .execute(prep, (type_id.0.as_str(), version))
                .await;
        }
    }

    async fn promote_latest(
        &self,
        type_id: &TypeId,
        version: i32,
        json_schema: &str,
        created_at: CqlTimestamp,
    ) -> RepoResult<()> {
        let mut insert_latest = self.stmt_insert_latest().await?.clone();
        insert_latest.set_consistency(Consistency::LocalQuorum);
        insert_latest.set_serial_consistency(Some(SerialConsistency::LocalSerial));
        let res = self
            .ctx
            .session
            .execute(
                &insert_latest,
                (type_id.0.as_str(), version, json_schema, created_at),
            )
            .await
            .map_err(driver_err)?;
        if lwt_applied(&res) {
            return Ok(());
        }

        for _ in 0..8 {
            let Some((latest_version, _, _)) = self
                .select_latest_raw(type_id, ReadConsistency::Strong)
                .await?
            else {
                continue;
            };
            if version <= latest_version {
                return Err(invalid(format!(
                    "schema version {} not greater than latest {}",
                    version, latest_version
                )));
            }

            let mut update_latest = self.stmt_update_latest_if_version().await?.clone();
            update_latest.set_consistency(Consistency::LocalQuorum);
            update_latest.set_serial_consistency(Some(SerialConsistency::LocalSerial));
            let res = self
                .ctx
                .session
                .execute(
                    &update_latest,
                    (
                        version,
                        json_schema,
                        created_at,
                        type_id.0.as_str(),
                        latest_version,
                    ),
                )
                .await
                .map_err(driver_err)?;
            if lwt_applied(&res) {
                return Ok(());
            }
        }

        Err(RepoError::Backend(
            "schema latest CAS did not converge after retries".to_string(),
        ))
    }
}

#[async_trait]
impl SchemaStore for CassandraSchemaStore {
    async fn get_latest(
        &self,
        type_id: &TypeId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<Schema>> {
        let Some((version, raw_schema, created_at)) =
            self.select_latest_raw(type_id, consistency).await?
        else {
            return Ok(None);
        };
        Ok(Some(Schema {
            type_id: type_id.clone(),
            version: Self::schema_version_from_cql(version)?,
            json_schema: Self::schema_json_from_cql(raw_schema)?,
            created_at_ms: cql_ts_to_ms(created_at),
        }))
    }

    async fn get_version(
        &self,
        type_id: &TypeId,
        version: u32,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<Schema>> {
        let version_cql = Self::schema_version_to_cql(version)?;
        let mut prep = self.stmt_select_version().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        let result = self
            .ctx
            .session
            .execute(&prep, (type_id.0.as_str(), version_cql))
            .await
            .map_err(driver_err)?;
        let mut rows = result.rows_typed_or_empty::<(String, CqlTimestamp)>();
        let Some(row) = rows.next() else {
            return Ok(None);
        };
        let (raw_schema, created_at) = row.map_err(driver_err)?;
        Ok(Some(Schema {
            type_id: type_id.clone(),
            version,
            json_schema: Self::schema_json_from_cql(raw_schema)?,
            created_at_ms: cql_ts_to_ms(created_at),
        }))
    }

    async fn put(&self, schema: Schema) -> RepoResult<()> {
        let version = Self::schema_version_to_cql(schema.version)?;
        if let Some((latest, _, _)) = self
            .select_latest_raw(&schema.type_id, ReadConsistency::Strong)
            .await?
        {
            if version <= latest {
                return Err(invalid(format!(
                    "schema version {} not greater than latest {}",
                    version, latest
                )));
            }
        }

        let raw_schema = Self::schema_json_to_cql(&schema.json_schema)?;
        let created_at = ms_to_ts(schema.created_at_ms);
        let mut insert_version = self.stmt_insert_version().await?.clone();
        insert_version.set_consistency(Consistency::LocalQuorum);
        insert_version.set_serial_consistency(Some(SerialConsistency::LocalSerial));
        let res = self
            .ctx
            .session
            .execute(
                &insert_version,
                (
                    schema.type_id.0.as_str(),
                    version,
                    raw_schema.as_str(),
                    created_at,
                ),
            )
            .await
            .map_err(driver_err)?;
        if !lwt_applied(&res) {
            return Err(invalid(format!(
                "schema version {} already exists for type {}",
                version, schema.type_id.0
            )));
        }

        if let Err(err) = self
            .promote_latest(&schema.type_id, version, raw_schema.as_str(), created_at)
            .await
        {
            self.delete_version_best_effort(&schema.type_id, version)
                .await;
            return Err(err);
        }
        Ok(())
    }
}

// ---------------------------------------------------------------------------
// SessionStore
// ---------------------------------------------------------------------------

/// `SessionStore` backed by `auth_runtime.sessions_by_id`.
///
/// This table is a point-lookup repository surface for the
/// `storage-abstraction` trait. Identity-specific refresh-token,
/// OAuth-state and revocation tables remain owned by the identity/session
/// services in the same `auth_runtime` keyspace.
pub struct CassandraSessionStore {
    ctx: CassandraRepoCtx,
    prepared: SessionPreparedStatements,
}

struct SessionPreparedStatements {
    insert_session: OnceCell<PreparedStatement>,
    select_session: OnceCell<PreparedStatement>,
    delete_session: OnceCell<PreparedStatement>,
}

impl SessionPreparedStatements {
    fn new() -> Self {
        Self {
            insert_session: OnceCell::new(),
            select_session: OnceCell::new(),
            delete_session: OnceCell::new(),
        }
    }
}

impl CassandraSessionStore {
    /// Build with the standard `auth_runtime` keyspace.
    pub fn new(session: Arc<Session>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, "auth_runtime"),
            prepared: SessionPreparedStatements::new(),
        }
    }

    /// Build with a custom keyspace.
    pub fn with_keyspace(session: Arc<Session>, keyspace: impl Into<String>) -> Self {
        Self {
            ctx: CassandraRepoCtx::new(session, keyspace),
            prepared: SessionPreparedStatements::new(),
        }
    }

    /// Eagerly prepare every statement this store may issue.
    pub async fn warm_up(&self) -> RepoResult<()> {
        self.stmt_insert_session().await?;
        self.stmt_select_session().await?;
        self.stmt_delete_session().await?;
        Ok(())
    }

    async fn stmt_insert_session(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.sessions_by_id \
             (tenant, session_id, subject, attributes, issued_at, expires_at) \
             VALUES (?, ?, ?, ?, ?, ?) USING TTL ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_session,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_session(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT subject, attributes, issued_at, expires_at \
             FROM {ks}.sessions_by_id WHERE tenant = ? AND session_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_session,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_delete_session(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "DELETE FROM {ks}.sessions_by_id WHERE tenant = ? AND session_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.delete_session,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    fn ttl_seconds_until(expires_at_ms: i64, now_ms: i64) -> Option<i32> {
        let ttl_ms = expires_at_ms.saturating_sub(now_ms);
        if ttl_ms <= 0 {
            return None;
        }
        let ttl_secs = ((ttl_ms as i128) + 999) / 1_000;
        Some(ttl_secs.min(i32::MAX as i128) as i32)
    }
}

#[async_trait]
impl SessionStore for CassandraSessionStore {
    async fn get(
        &self,
        tenant: &TenantId,
        id: &str,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<RepoSession>> {
        let mut prep = self.stmt_select_session().await?.clone();
        prep.set_consistency(cql_consistency(consistency));
        let result = self
            .ctx
            .session
            .execute(&prep, (tenant_str(tenant), id))
            .await
            .map_err(driver_err)?;
        let mut rows = result.rows_typed_or_empty::<(
            String,
            Option<HashMap<String, String>>,
            CqlTimestamp,
            CqlTimestamp,
        )>();
        let Some(row) = rows.next() else {
            return Ok(None);
        };
        let (subject, attributes, issued_at, expires_at) = row.map_err(driver_err)?;
        let expires_at_ms = cql_ts_to_ms(expires_at);
        if expires_at_ms <= Utc::now().timestamp_millis() {
            return Ok(None);
        }
        Ok(Some(RepoSession {
            tenant: tenant.clone(),
            id: id.to_string(),
            subject,
            attributes: attributes.unwrap_or_default(),
            issued_at_ms: cql_ts_to_ms(issued_at),
            expires_at_ms,
        }))
    }

    async fn put(&self, session: RepoSession) -> RepoResult<()> {
        if session.id.trim().is_empty() {
            return Err(invalid("session id must not be empty"));
        }
        let Some(ttl_secs) =
            Self::ttl_seconds_until(session.expires_at_ms, Utc::now().timestamp_millis())
        else {
            let prep = self.stmt_delete_session().await?.clone();
            self.ctx
                .session
                .execute(&prep, (tenant_str(&session.tenant), session.id.as_str()))
                .await
                .map_err(driver_err)?;
            return Ok(());
        };

        let mut prep = self.stmt_insert_session().await?.clone();
        prep.set_consistency(Consistency::LocalQuorum);
        self.ctx
            .session
            .execute(
                &prep,
                (
                    tenant_str(&session.tenant),
                    session.id.as_str(),
                    session.subject.as_str(),
                    session.attributes,
                    ms_to_ts(session.issued_at_ms),
                    ms_to_ts(session.expires_at_ms),
                    ttl_secs,
                ),
            )
            .await
            .map_err(driver_err)?;
        Ok(())
    }

    async fn revoke(&self, tenant: &TenantId, id: &str) -> RepoResult<bool> {
        let mut select = self.stmt_select_session().await?.clone();
        select.set_consistency(Consistency::LocalQuorum);
        let existed = self
            .ctx
            .session
            .execute(&select, (tenant_str(tenant), id))
            .await
            .map_err(driver_err)?
            .rows
            .as_ref()
            .map(|rows| !rows.is_empty())
            .unwrap_or(false);

        let mut delete = self.stmt_delete_session().await?.clone();
        delete.set_consistency(Consistency::LocalQuorum);
        self.ctx
            .session
            .execute(&delete, (tenant_str(tenant), id))
            .await
            .map_err(driver_err)?;
        Ok(existed)
    }
}

// ---------------------------------------------------------------------------
// ActionLogStore (S1.4 wiring)
// ---------------------------------------------------------------------------

/// `ActionLogStore` backed by `actions_log.actions_log`, the object/action
/// read indexes and a deterministic event-id dedupe table.
pub struct CassandraActionLogStore {
    ctx: CassandraRepoCtx,
    prepared: ActionLogPreparedStatements,
}

struct ActionLogPreparedStatements {
    insert_event: OnceCell<PreparedStatement>,
    insert_log: OnceCell<PreparedStatement>,
    insert_by_object: OnceCell<PreparedStatement>,
    insert_by_action: OnceCell<PreparedStatement>,
    select_event: OnceCell<PreparedStatement>,
    select_recent: OnceCell<PreparedStatement>,
    select_by_object: OnceCell<PreparedStatement>,
    select_by_action: OnceCell<PreparedStatement>,
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
            insert_event: OnceCell::new(),
            insert_log: OnceCell::new(),
            insert_by_object: OnceCell::new(),
            insert_by_action: OnceCell::new(),
            select_event: OnceCell::new(),
            select_recent: OnceCell::new(),
            select_by_object: OnceCell::new(),
            select_by_action: OnceCell::new(),
        }
    }
}

#[derive(Clone)]
struct ActionLogRow {
    tenant: TenantId,
    event_id: String,
    action_id: CqlTimeuuid,
    kind: String,
    actor_id: Option<Uuid>,
    subject: String,
    target_object_id: Option<CqlTimeuuid>,
    payload: String,
    applied_at: CqlTimestamp,
    day_bucket: CqlDate,
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
        self.stmt_insert_event().await?;
        self.stmt_insert_log().await?;
        self.stmt_insert_by_object().await?;
        self.stmt_insert_by_action().await?;
        self.stmt_select_event().await?;
        self.stmt_select_recent().await?;
        self.stmt_select_by_object().await?;
        self.stmt_select_by_action().await?;
        Ok(())
    }

    async fn stmt_insert_event(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.actions_by_event \
             (tenant, event_id, action_id, kind, actor_id, subject, target_object_id, payload, applied_at, day_bucket) \
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) IF NOT EXISTS",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_event,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_insert_log(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.actions_log \
             (tenant, day_bucket, applied_at, action_id, kind, actor_id, subject, \
              target_object_id, target_type_id, payload, status, failure_type, duration_ms, event_id) \
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(&self.prepared.insert_log, &self.ctx.session, &cql)
            .await
    }

    async fn stmt_insert_by_object(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.actions_by_object \
             (tenant, target_object_id, applied_at, action_id, kind, actor_id, subject, payload, event_id) \
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_by_object,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_insert_by_action(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "INSERT INTO {ks}.actions_by_action \
             (tenant, action_id, day_bucket, applied_at, event_id, kind, actor_id, subject, target_object_id, payload) \
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.insert_by_action,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_event(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT action_id, kind, actor_id, subject, target_object_id, payload, applied_at, day_bucket \
             FROM {ks}.actions_by_event WHERE tenant = ? AND event_id = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_event,
            &self.ctx.session,
            &cql,
        )
        .await
    }

    async fn stmt_select_recent(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT applied_at, action_id, kind, actor_id, subject, target_object_id, payload, event_id \
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
            "SELECT applied_at, action_id, kind, actor_id, subject, payload, event_id \
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

    async fn stmt_select_by_action(&self) -> RepoResult<&PreparedStatement> {
        let cql = format!(
            "SELECT applied_at, event_id, kind, actor_id, subject, target_object_id, payload \
             FROM {ks}.actions_by_action WHERE tenant = ? AND action_id = ? AND day_bucket = ?",
            ks = self.ctx.keyspace
        );
        ObjectPreparedStatements::get_or_prepare(
            &self.prepared.select_by_action,
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

    fn event_id_from_payload(kind: &str, payload: &serde_json::Value) -> Option<String> {
        for key in [
            "event_id",
            "idempotency_key",
            "idempotencyKey",
            "execution_id",
            "executionId",
            "run_id",
            "runId",
        ] {
            if let Some(value) = payload.get(key).and_then(serde_json::Value::as_str) {
                let trimmed = value.trim();
                if !trimmed.is_empty() {
                    return Some(format!("{kind}:{trimmed}"));
                }
            }
        }
        None
    }

    fn stable_payload_projection(payload: &serde_json::Value) -> Option<serde_json::Value> {
        if payload.get("side_effect_type").is_some() && payload.get("webhook_id").is_some() {
            return Some(serde_json::json!({
                "action_type_id": payload.get("action_type_id").cloned().unwrap_or(serde_json::Value::Null),
                "side_effect_type": payload.get("side_effect_type").cloned().unwrap_or(serde_json::Value::Null),
                "webhook_id": payload.get("webhook_id").cloned().unwrap_or(serde_json::Value::Null),
                "status": payload.get("status").cloned().unwrap_or(serde_json::Value::Null),
            }));
        }
        if payload.get("action_type_id").is_some() && payload.get("parameters").is_some() {
            return Some(serde_json::json!({
                "action_type_id": payload.get("action_type_id").cloned().unwrap_or(serde_json::Value::Null),
                "target_object_id": payload.get("target_object_id").cloned().unwrap_or(serde_json::Value::Null),
                "parameters": payload.get("parameters").cloned().unwrap_or(serde_json::Value::Null),
                "status": payload.get("status").cloned().unwrap_or(serde_json::Value::Null),
                "failure_type": payload.get("failure_type").cloned().unwrap_or(serde_json::Value::Null),
            }));
        }
        None
    }

    fn derive_event_id(entry: &ActionLogEntry, payload: &str) -> String {
        if let Some(event_id) = entry.event_id.as_deref().map(str::trim) {
            if !event_id.is_empty() {
                return event_id.to_string();
            }
        }
        if let Some(event_id) = Self::event_id_from_payload(&entry.kind, &entry.payload) {
            return event_id;
        }

        let object = entry.object.as_ref().map(|id| id.0.as_str()).unwrap_or("");
        let payload_material = Self::stable_payload_projection(&entry.payload)
            .and_then(|value| serde_json::to_string(&value).ok())
            .unwrap_or_else(|| payload.to_string());
        let material = format!(
            "of-action-log-v1\0{}\0{}\0{}\0{}\0{}",
            entry.tenant.0, entry.kind, entry.subject, object, payload_material
        );
        Uuid::new_v5(
            &Uuid::from_u128(0x6d1d_30aa_4d0c_5da9_9d8d_e8280a5a1c3f),
            material.as_bytes(),
        )
        .to_string()
    }

    fn action_payload_to_cql(payload: &serde_json::Value) -> RepoResult<String> {
        serde_json::to_string(payload)
            .map_err(|e| invalid(format!("action payload is not serialisable: {e}")))
    }

    fn action_payload_from_cql(raw: String) -> RepoResult<serde_json::Value> {
        serde_json::from_str(&raw)
            .map_err(|e| RepoError::Backend(format!("invalid stored action payload JSON: {e}")))
    }

    fn row_from_entry(entry: &ActionLogEntry) -> RepoResult<ActionLogRow> {
        let payload = Self::action_payload_to_cql(&entry.payload)?;
        let event_id = Self::derive_event_id(entry, &payload);
        let recorded_at_ms = entry.recorded_at_ms;
        Ok(ActionLogRow {
            tenant: entry.tenant.clone(),
            event_id,
            action_id: parse_timeuuid("action_id", &entry.action_id)?,
            kind: entry.kind.clone(),
            actor_id: Uuid::parse_str(&entry.subject).ok(),
            subject: entry.subject.clone(),
            target_object_id: entry
                .object
                .as_ref()
                .map(|object| parse_timeuuid("object", &object.0))
                .transpose()?,
            payload,
            applied_at: ms_to_ts(recorded_at_ms),
            day_bucket: ms_to_day(recorded_at_ms),
        })
    }

    async fn read_event_row(&self, tenant: &TenantId, event_id: &str) -> RepoResult<ActionLogRow> {
        let mut prep = self.stmt_select_event().await?.clone();
        prep.set_consistency(Consistency::LocalQuorum);
        let result = self
            .ctx
            .session
            .execute(&prep, (tenant_str(tenant), event_id))
            .await
            .map_err(driver_err)?;
        let mut rows = result.rows_typed_or_empty::<(
            CqlTimeuuid,
            String,
            Option<Uuid>,
            Option<String>,
            Option<CqlTimeuuid>,
            String,
            CqlTimestamp,
            CqlDate,
        )>();
        let Some(row) = rows.next() else {
            return Err(RepoError::Backend(format!(
                "action event {event_id} was not readable after idempotent insert"
            )));
        };
        let (action_id, kind, actor_id, subject, target_object_id, payload, applied_at, day_bucket) =
            row.map_err(driver_err)?;
        Ok(ActionLogRow {
            tenant: tenant.clone(),
            event_id: event_id.to_string(),
            action_id,
            kind,
            actor_id,
            subject: Self::subject_from(actor_id, subject),
            target_object_id,
            payload,
            applied_at,
            day_bucket,
        })
    }

    async fn put_event_row(&self, row: &ActionLogRow) -> RepoResult<bool> {
        let mut insert_event = self.stmt_insert_event().await?.clone();
        insert_event.set_consistency(Consistency::LocalQuorum);
        insert_event.set_serial_consistency(Some(SerialConsistency::LocalSerial));
        let res = self
            .ctx
            .session
            .execute(
                &insert_event,
                (
                    tenant_str(&row.tenant),
                    row.event_id.as_str(),
                    row.action_id,
                    row.kind.as_str(),
                    row.actor_id,
                    row.subject.as_str(),
                    row.target_object_id,
                    row.payload.as_str(),
                    row.applied_at,
                    row.day_bucket,
                ),
            )
            .await
            .map_err(driver_err)?;
        Ok(lwt_applied(&res))
    }

    async fn write_action_fanout(&self, row: &ActionLogRow) -> RepoResult<()> {
        let status = Self::action_payload_from_cql(row.payload.clone())?
            .get("status")
            .and_then(serde_json::Value::as_str)
            .unwrap_or("applied")
            .to_string();
        let parsed_payload = Self::action_payload_from_cql(row.payload.clone())?;
        let failure_type = parsed_payload
            .get("failure_type")
            .and_then(serde_json::Value::as_str);
        let duration_ms = parsed_payload
            .get("duration_ms")
            .and_then(serde_json::Value::as_i64)
            .and_then(|n| i32::try_from(n).ok());
        let target_type_id: Option<&str> = None;

        let mut insert_log = self.stmt_insert_log().await?.clone();
        insert_log.set_consistency(Consistency::LocalQuorum);
        self.ctx
            .session
            .execute(
                &insert_log,
                (
                    tenant_str(&row.tenant),
                    row.day_bucket,
                    row.applied_at,
                    row.action_id,
                    row.kind.as_str(),
                    row.actor_id,
                    row.subject.as_str(),
                    row.target_object_id,
                    target_type_id,
                    row.payload.as_str(),
                    Some(status.as_str()),
                    failure_type,
                    duration_ms,
                    row.event_id.as_str(),
                ),
            )
            .await
            .map_err(driver_err)?;

        let mut insert_by_action = self.stmt_insert_by_action().await?.clone();
        insert_by_action.set_consistency(Consistency::LocalQuorum);
        self.ctx
            .session
            .execute(
                &insert_by_action,
                (
                    tenant_str(&row.tenant),
                    row.action_id,
                    row.day_bucket,
                    row.applied_at,
                    row.event_id.as_str(),
                    row.kind.as_str(),
                    row.actor_id,
                    row.subject.as_str(),
                    row.target_object_id,
                    row.payload.as_str(),
                ),
            )
            .await
            .map_err(driver_err)?;

        if let Some(object_id) = row.target_object_id {
            let mut insert_by_object = self.stmt_insert_by_object().await?.clone();
            insert_by_object.set_consistency(Consistency::LocalQuorum);
            self.ctx
                .session
                .execute(
                    &insert_by_object,
                    (
                        tenant_str(&row.tenant),
                        object_id,
                        row.applied_at,
                        row.action_id,
                        row.kind.as_str(),
                        row.actor_id,
                        row.subject.as_str(),
                        row.payload.as_str(),
                        row.event_id.as_str(),
                    ),
                )
                .await
                .map_err(driver_err)?;
        }

        Ok(())
    }

    fn entry_from_row(
        tenant: TenantId,
        event_id: Option<String>,
        action_id: CqlTimeuuid,
        kind: String,
        actor_id: Option<Uuid>,
        subject: Option<String>,
        object_id: Option<CqlTimeuuid>,
        payload: String,
        applied_at: CqlTimestamp,
    ) -> RepoResult<ActionLogEntry> {
        Ok(ActionLogEntry {
            tenant,
            event_id,
            action_id: timeuuid_to_string(action_id),
            kind,
            subject: Self::subject_from(actor_id, subject),
            object: object_id.map(|id| ObjectId(timeuuid_to_string(id))),
            payload: Self::action_payload_from_cql(payload)?,
            recorded_at_ms: cql_ts_to_ms(applied_at),
        })
    }
}

#[async_trait]
impl ActionLogStore for CassandraActionLogStore {
    async fn append(&self, entry: ActionLogEntry) -> RepoResult<()> {
        let row = Self::row_from_entry(&entry)?;
        let row = if self.put_event_row(&row).await? {
            row
        } else {
            self.read_event_row(&entry.tenant, &row.event_id).await?
        };
        self.write_action_fanout(&row).await?;
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
                CqlTimeuuid,
                String,
                Option<Uuid>,
                Option<String>,
                Option<CqlTimeuuid>,
                String,
                Option<String>,
            )>() {
                let (applied_at, action_id, kind, actor_id, subject, object_id, payload, event_id) =
                    row.map_err(driver_err)?;
                items.push(Self::entry_from_row(
                    tenant.clone(),
                    event_id,
                    action_id,
                    kind,
                    actor_id,
                    subject,
                    object_id,
                    payload,
                    applied_at,
                )?);
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
        let object_id = parse_timeuuid("object", &object.0)?;
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
            CqlTimeuuid,
            String,
            Option<Uuid>,
            Option<String>,
            String,
            Option<String>,
        )>() {
            let (applied_at, action_id, kind, actor_id, subject, payload, event_id) =
                row.map_err(driver_err)?;
            items.push(Self::entry_from_row(
                tenant.clone(),
                event_id,
                action_id,
                kind,
                actor_id,
                subject,
                Some(object_id),
                payload,
                applied_at,
            )?);
        }
        Ok(PagedResult { items, next_token })
    }

    async fn list_for_action(
        &self,
        tenant: &TenantId,
        action_id: &str,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>> {
        let action_id = parse_timeuuid("action_id", action_id)?;
        let mut token = Self::decode_recent_token(page.token.as_deref())?;
        let limit = clamp_page_size(page.size) as usize;
        let mut items = Vec::with_capacity(limit.min(128));
        let mut next_token = None;

        while items.len() < limit && token.days_scanned < ACTION_LOG_LOOKBACK_DAYS {
            let mut prep = self.stmt_select_by_action().await?.clone();
            prep.set_consistency(cql_consistency(consistency));
            prep.set_page_size((limit - items.len()).clamp(1, 5_000) as i32);
            let paging = match token.paging.take() {
                Some(raw) => decode_paging_state(Some(raw.as_str()))?,
                None => None,
            };

            let result = self
                .ctx
                .session
                .execute_paged(
                    &prep,
                    (tenant_str(tenant), action_id, CqlDate(token.day)),
                    paging,
                )
                .await
                .map_err(driver_err)?;
            let page_state = result.paging_state.clone();

            for row in result.rows_typed_or_empty::<(
                CqlTimestamp,
                String,
                String,
                Option<Uuid>,
                Option<String>,
                Option<CqlTimeuuid>,
                String,
            )>() {
                let (applied_at, event_id, kind, actor_id, subject, object_id, payload) =
                    row.map_err(driver_err)?;
                items.push(Self::entry_from_row(
                    tenant.clone(),
                    Some(event_id),
                    action_id,
                    kind,
                    actor_id,
                    subject,
                    object_id,
                    payload,
                    applied_at,
                )?);
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

    #[test]
    fn link_payload_round_trips_through_canonical_json() {
        let payload = serde_json::json!({
            "confidence": 0.98,
            "evidence": ["sensor-a", "sensor-b"],
        });
        let encoded = CassandraLinkStore::payload_to_cql(Some(payload.clone()))
            .unwrap()
            .unwrap();
        let decoded = CassandraLinkStore::payload_from_cql(Some(encoded))
            .unwrap()
            .unwrap();
        assert_eq!(decoded, payload);
        assert_eq!(CassandraLinkStore::payload_to_cql(None).unwrap(), None);
    }

    #[test]
    fn link_timeuuid_conversion_preserves_object_id_string() {
        let id = Uuid::now_v7().to_string();
        let cql = parse_timeuuid("object_id", &id).unwrap();
        assert_eq!(timeuuid_to_string(cql), id);
    }

    #[test]
    fn link_timeuuid_conversion_rejects_malformed_ids() {
        let err = parse_timeuuid("source_id", "not-a-uuid").unwrap_err();
        assert!(matches!(err, RepoError::InvalidArgument(_)));
    }

    #[test]
    fn schema_version_mapping_rejects_zero_and_out_of_range() {
        assert!(matches!(
            CassandraSchemaStore::schema_version_to_cql(0).unwrap_err(),
            RepoError::InvalidArgument(_)
        ));
        assert_eq!(CassandraSchemaStore::schema_version_to_cql(7).unwrap(), 7);
        assert!(matches!(
            CassandraSchemaStore::schema_version_to_cql(i32::MAX as u32 + 1).unwrap_err(),
            RepoError::InvalidArgument(_)
        ));
    }

    #[test]
    fn schema_payload_round_trips_through_canonical_json() {
        let schema = serde_json::json!({
            "type": "object",
            "required": ["name"],
            "properties": {
                "name": { "type": "string" }
            }
        });
        let encoded = CassandraSchemaStore::schema_json_to_cql(&schema).unwrap();
        let decoded = CassandraSchemaStore::schema_json_from_cql(encoded).unwrap();
        assert_eq!(decoded, schema);
    }

    #[test]
    fn session_ttl_rounds_up_and_treats_expired_as_absent() {
        assert_eq!(
            CassandraSessionStore::ttl_seconds_until(1_001, 1_000),
            Some(1)
        );
        assert_eq!(
            CassandraSessionStore::ttl_seconds_until(2_001, 1_000),
            Some(2)
        );
        assert_eq!(CassandraSessionStore::ttl_seconds_until(999, 1_000), None);
    }

    #[test]
    fn action_event_id_prefers_explicit_value() {
        let entry = ActionLogEntry {
            tenant: TenantId("tenant-a".into()),
            event_id: Some("explicit-event".into()),
            action_id: Uuid::now_v7().to_string(),
            kind: "action_attempt".into(),
            subject: Uuid::now_v7().to_string(),
            object: Some(ObjectId(Uuid::now_v7().to_string())),
            payload: serde_json::json!({ "status": "applied" }),
            recorded_at_ms: 1,
        };
        let payload = CassandraActionLogStore::action_payload_to_cql(&entry.payload).unwrap();
        assert_eq!(
            CassandraActionLogStore::derive_event_id(&entry, &payload),
            "explicit-event"
        );
    }

    #[test]
    fn action_event_id_uses_payload_idempotency_key() {
        let entry = ActionLogEntry {
            tenant: TenantId("tenant-a".into()),
            event_id: None,
            action_id: Uuid::now_v7().to_string(),
            kind: "side_effect".into(),
            subject: Uuid::now_v7().to_string(),
            object: Some(ObjectId(Uuid::now_v7().to_string())),
            payload: serde_json::json!({ "idempotency_key": "webhook-123" }),
            recorded_at_ms: 1,
        };
        let payload = CassandraActionLogStore::action_payload_to_cql(&entry.payload).unwrap();
        assert_eq!(
            CassandraActionLogStore::derive_event_id(&entry, &payload),
            "side_effect:webhook-123"
        );
    }

    #[test]
    fn action_event_id_is_deterministic_without_timestamp_or_action_id() {
        let tenant = TenantId("tenant-a".into());
        let subject = Uuid::now_v7().to_string();
        let object = ObjectId(Uuid::now_v7().to_string());
        let payload = serde_json::json!({ "status": "applied", "value": 42 });
        let left = ActionLogEntry {
            tenant: tenant.clone(),
            event_id: None,
            action_id: Uuid::now_v7().to_string(),
            kind: "action_attempt".into(),
            subject: subject.clone(),
            object: Some(object.clone()),
            payload: payload.clone(),
            recorded_at_ms: 1,
        };
        let right = ActionLogEntry {
            tenant,
            event_id: None,
            action_id: Uuid::now_v7().to_string(),
            kind: "action_attempt".into(),
            subject,
            object: Some(object),
            payload,
            recorded_at_ms: 999,
        };
        let left_payload = CassandraActionLogStore::action_payload_to_cql(&left.payload).unwrap();
        let right_payload = CassandraActionLogStore::action_payload_to_cql(&right.payload).unwrap();

        assert_eq!(
            CassandraActionLogStore::derive_event_id(&left, &left_payload),
            CassandraActionLogStore::derive_event_id(&right, &right_payload)
        );
    }

    #[test]
    fn action_event_id_ignores_attempt_duration_noise() {
        let tenant = TenantId("tenant-a".into());
        let subject = Uuid::now_v7().to_string();
        let object = ObjectId(Uuid::now_v7().to_string());
        let action_type_id = Uuid::now_v7().to_string();
        let mk = |duration_ms| ActionLogEntry {
            tenant: tenant.clone(),
            event_id: None,
            action_id: Uuid::now_v7().to_string(),
            kind: "action_attempt".into(),
            subject: subject.clone(),
            object: Some(object.clone()),
            payload: serde_json::json!({
                "action_type_id": action_type_id.clone(),
                "target_object_id": object.0.clone(),
                "parameters": { "next_status": "grounded" },
                "status": "applied",
                "duration_ms": duration_ms,
            }),
            recorded_at_ms: duration_ms,
        };
        let left = mk(10);
        let right = mk(90);
        let left_payload = CassandraActionLogStore::action_payload_to_cql(&left.payload).unwrap();
        let right_payload = CassandraActionLogStore::action_payload_to_cql(&right.payload).unwrap();

        assert_eq!(
            CassandraActionLogStore::derive_event_id(&left, &left_payload),
            CassandraActionLogStore::derive_event_id(&right, &right_payload)
        );
    }
}
