//! Postgres logical-decoding CDC worker.
//!
//! ## What this does
//!
//! Runs a background loop that consumes changes from a Postgres logical
//! replication slot using the built-in `test_decoding` output plugin and
//! republishes every change as an [`CdcEnvelope`] with the headers the
//! rest of the data plane expects:
//!
//! * `cdc.op`    — `append_only` | `upsert` | `hard_delete` (mapped from
//!   the `incremental_mode` enum modelled by `cdc_metadata`).
//! * `cdc.lsn`   — `X/X` text LSN of the change.
//! * `cdc.tx_id` — Postgres transaction id (xid).
//! * `cdc.table` / `cdc.schema` — convenience for downstream routing.
//!
//! ## Why slot functions and not streaming replication
//!
//! Postgres exposes two equivalent ways to read a logical slot:
//!
//! 1. The streaming `START_REPLICATION` over a replication-mode connection
//!    (low latency, requires the `replication` Cargo feature on
//!    `tokio-postgres` which the upstream crate does not currently ship).
//! 2. The `pg_logical_slot_peek_changes` / `pg_replication_slot_advance`
//!    SQL pair (slightly higher latency due to polling, but works with the
//!    stock client and gives us crash-safe at-least-once semantics out of
//!    the box).
//!
//! We pick (2). Each iteration *peeks* the slot — which leaves the LSN
//! horizon untouched — publishes every change to the sink, persists the
//! new LSN to a local checkpoint manifest, and only then asks Postgres to
//! advance the slot. A crash anywhere before the advance simply means the
//! same batch is replayed on the next start; subscribers can dedupe by
//! `cdc.lsn`.
//!
//! ## Resume on restart
//!
//! On startup the worker:
//!
//! * reads the checkpoint manifest for its `subscription_id`;
//! * if the slot exists and its `confirmed_flush_lsn` is *behind* our
//!   checkpoint, calls `pg_replication_slot_advance` to fast-forward the
//!   slot — this avoids re-emitting changes we have already published;
//! * if the slot does *not* exist, creates it with the `test_decoding`
//!   plugin (built into Postgres, no extension required).
//!
//! The slot itself acts as the durable WAL retainer; the checkpoint manifest
//! mirrors what we have published so we can detect divergence without routing
//! hot-path state back through Postgres.

use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, Utc};
use sqlx::{PgPool, Row};
use tokio::sync::Mutex;
use uuid::Uuid;

/// Static configuration for one CDC subscription.
#[derive(Debug, Clone)]
pub struct PostgresCdcConfig {
    /// Stable id used as the checkpoint manifest key.
    pub subscription_id: Uuid,
    /// libpq connection string for the upstream database.
    pub connection_string: String,
    /// `pg_replication_slots.slot_name`. Created on first run.
    pub slot_name: String,
    /// Publication that scopes which tables the slot streams. Created on
    /// first run if non-empty.
    pub publication_name: String,
    /// Tables to include in `CREATE PUBLICATION` (schema-qualified).
    pub tables: Vec<String>,
    /// Topic to publish CDC envelopes to.
    pub event_topic: String,
    /// How long to sleep between polls when the slot returned no rows.
    pub poll_interval: Duration,
}

impl PostgresCdcConfig {
    pub fn with_defaults(
        subscription_id: Uuid,
        connection_string: impl Into<String>,
        slot_name: impl Into<String>,
        publication_name: impl Into<String>,
        tables: Vec<String>,
        event_topic: impl Into<String>,
    ) -> Self {
        Self {
            subscription_id,
            connection_string: connection_string.into(),
            slot_name: slot_name.into(),
            publication_name: publication_name.into(),
            tables,
            event_topic: event_topic.into(),
            poll_interval: Duration::from_millis(250),
        }
    }
}

/// Filesystem-backed checkpoint manifest for one subscription.
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct IngestionCheckpoint {
    pub subscription_id: Uuid,
    pub slot_name: String,
    pub publication_name: String,
    pub last_lsn: Option<String>,
    pub last_event_at: Option<DateTime<Utc>>,
    pub records_observed: i64,
    pub records_applied: i64,
    pub last_tx_id: Option<i64>,
    pub updated_at: DateTime<Utc>,
}

/// The minimum payload the worker forwards to a downstream sink. The shape
/// mirrors `event_streaming_service::backends::Envelope`.
#[derive(Debug, Clone)]
pub struct CdcEnvelope {
    pub topic: String,
    pub payload: Vec<u8>,
    pub headers: std::collections::BTreeMap<String, String>,
}

/// CDC operation flavours mapped 1:1 to the `incremental_mode` enum
/// modelled by `cdc_metadata`.
///
/// | wal2json `kind` | header value      | `incremental_mode` |
/// |-----------------|-------------------|--------------------|
/// | `insert`        | `append_only`     | `append_only`      |
/// | `update`        | `upsert`          | `upsert`           |
/// | `delete`        | `hard_delete`     | `hard_delete`      |
/// | `truncate`      | `hard_delete`     | `hard_delete`      |
///
/// `soft_delete` and `log_based` are subscriber-side reinterpretations of
/// the same upstream payload, so the worker never produces them directly.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CdcOp {
    Insert,
    Update,
    Delete,
    Truncate,
}

impl CdcOp {
    pub fn as_header(self) -> &'static str {
        match self {
            CdcOp::Insert => "append_only",
            CdcOp::Update => "upsert",
            CdcOp::Delete | CdcOp::Truncate => "hard_delete",
        }
    }

    pub fn from_kind(kind: &str) -> Option<Self> {
        match kind {
            "insert" => Some(Self::Insert),
            "update" => Some(Self::Update),
            "delete" => Some(Self::Delete),
            "truncate" => Some(Self::Truncate),
            _ => None,
        }
    }
}

/// Sink contract: anything that can accept a CDC envelope. The production
/// impl is [`grpc::EventStreamingPublisher`]; tests use
/// [`InMemoryPublisher`].
#[async_trait::async_trait]
pub trait EventPublisher: Send + Sync {
    async fn publish(&self, envelope: CdcEnvelope) -> Result<(), CdcError>;
}

/// Test/fixture sink that just records every envelope.
#[derive(Default, Clone)]
pub struct InMemoryPublisher {
    inner: Arc<Mutex<Vec<CdcEnvelope>>>,
}

impl InMemoryPublisher {
    pub fn new() -> Self {
        Self::default()
    }
    pub async fn drain(&self) -> Vec<CdcEnvelope> {
        std::mem::take(&mut *self.inner.lock().await)
    }
    pub async fn snapshot(&self) -> Vec<CdcEnvelope> {
        self.inner.lock().await.clone()
    }
}

#[async_trait::async_trait]
impl EventPublisher for InMemoryPublisher {
    async fn publish(&self, envelope: CdcEnvelope) -> Result<(), CdcError> {
        self.inner.lock().await.push(envelope);
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CdcError {
    #[error("metadata database error: {0}")]
    Db(#[from] sqlx::Error),
    #[error("checkpoint I/O error: {0}")]
    Io(#[from] std::io::Error),
    #[error("checkpoint serialization error: {0}")]
    CheckpointSerde(#[from] serde_json::Error),
    #[error("invalid LSN '{0}'")]
    InvalidLsn(String),
    #[error("publisher error: {0}")]
    Publisher(String),
    #[error("malformed CDC payload: {0}")]
    MalformedPayload(String),
}

/// Read the most recent checkpoint manifest or initialise a fresh one.
pub async fn load_or_init_checkpoint(
    _pool: &PgPool,
    config: &PostgresCdcConfig,
) -> Result<IngestionCheckpoint, CdcError> {
    let path = checkpoint_path(config.subscription_id);
    if path.exists() {
        return read_checkpoint(&path);
    }
    let checkpoint = IngestionCheckpoint {
        subscription_id: config.subscription_id,
        slot_name: config.slot_name.clone(),
        publication_name: config.publication_name.clone(),
        last_lsn: None,
        last_event_at: None,
        records_observed: 0,
        records_applied: 0,
        last_tx_id: None,
        updated_at: Utc::now(),
    };
    write_checkpoint(&path, &checkpoint)?;
    Ok(checkpoint)
}

/// Persist a new LSN/tx_id pair and bump the running counters.
pub async fn record_advance(
    _pool: &PgPool,
    config: &PostgresCdcConfig,
    new_lsn: &str,
    tx_id: Option<i64>,
    rows_in_batch: i64,
    event_at: DateTime<Utc>,
) -> Result<(), CdcError> {
    let path = checkpoint_path(config.subscription_id);
    let mut checkpoint = if path.exists() {
        read_checkpoint(&path)?
    } else {
        load_or_init_checkpoint(_pool, config).await?
    };
    checkpoint.last_lsn = Some(new_lsn.to_string());
    checkpoint.last_tx_id = tx_id.or(checkpoint.last_tx_id);
    checkpoint.last_event_at = Some(event_at);
    checkpoint.records_observed += rows_in_batch;
    checkpoint.records_applied += rows_in_batch;
    checkpoint.updated_at = Utc::now();
    write_checkpoint(&path, &checkpoint)?;
    Ok(())
}

fn checkpoint_path(subscription_id: Uuid) -> PathBuf {
    let mut root = std::env::var_os("OPENFOUNDRY_INGESTION_RUNTIME_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|| std::env::temp_dir().join("openfoundry-ingestion-runtime"));
    root.push("cdc-checkpoints");
    root.push(format!("{subscription_id}.json"));
    root
}

fn read_checkpoint(path: &Path) -> Result<IngestionCheckpoint, CdcError> {
    let bytes = fs::read(path)?;
    Ok(serde_json::from_slice(&bytes)?)
}

fn write_checkpoint(path: &Path, checkpoint: &IngestionCheckpoint) -> Result<(), CdcError> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    let bytes = serde_json::to_vec_pretty(checkpoint)?;
    let tmp = path.with_extension("json.tmp");
    fs::write(&tmp, bytes)?;
    fs::rename(tmp, path)?;
    Ok(())
}

/// Connect to the upstream database with a small dedicated pool. Kept
/// separate from the legacy metadata pool parameter so an upstream outage
/// does not back up control-plane traffic.
pub async fn connect_upstream(connection_string: &str) -> Result<PgPool, CdcError> {
    sqlx::postgres::PgPoolOptions::new()
        .max_connections(2)
        .connect(connection_string)
        .await
        .map_err(CdcError::Db)
}

/// Make sure the publication and the logical-replication slot exist.
/// Idempotent: existing objects are left untouched.
pub async fn ensure_slot_and_publication(
    upstream: &PgPool,
    config: &PostgresCdcConfig,
) -> Result<(), CdcError> {
    if !config.tables.is_empty() {
        let exists: Option<i64> =
            sqlx::query_scalar("SELECT 1::bigint FROM pg_publication WHERE pubname = $1")
                .bind(&config.publication_name)
                .fetch_optional(upstream)
                .await?;
        if exists.is_none() {
            // Identifier values cannot be parameterised; we quote them so a
            // hostile slot/publication name still cannot inject SQL.
            let stmt = format!(
                "CREATE PUBLICATION {} FOR TABLE {}",
                quote_ident(&config.publication_name),
                config.tables.join(", ")
            );
            sqlx::query(&stmt).execute(upstream).await?;
        }
    }

    let exists: Option<i64> =
        sqlx::query_scalar("SELECT 1::bigint FROM pg_replication_slots WHERE slot_name = $1")
            .bind(&config.slot_name)
            .fetch_optional(upstream)
            .await?;
    if exists.is_none() {
        sqlx::query("SELECT pg_create_logical_replication_slot($1, 'test_decoding')")
            .bind(&config.slot_name)
            .execute(upstream)
            .await?;
    }
    Ok(())
}

/// Fast-forward the slot to `target_lsn` (no-op if the slot is already at
/// or past the target).
async fn fast_forward_slot(
    upstream: &PgPool,
    slot: &str,
    target_lsn: &str,
) -> Result<(), CdcError> {
    sqlx::query("SELECT pg_replication_slot_advance($1, $2::pg_lsn)")
        .bind(slot)
        .bind(target_lsn)
        .execute(upstream)
        .await?;
    Ok(())
}

/// Run one polling iteration: peek the slot, publish every change, persist
/// the checkpoint, and advance the slot to the last published LSN.
///
/// Returns the number of envelopes published.
pub async fn poll_once<P: EventPublisher>(
    metadata_pool: &PgPool,
    upstream_pool: &PgPool,
    config: &PostgresCdcConfig,
    publisher: &P,
) -> Result<usize, CdcError> {
    // PEEK rather than GET: leaves the slot horizon untouched until we
    // have actually persisted the checkpoint manifest. The `test_decoding` plugin
    // is built into Postgres and emits one text line per change.
    let rows = sqlx::query(
        r#"SELECT lsn::text AS lsn, xid::text AS xid, data
             FROM pg_logical_slot_peek_changes($1, NULL, NULL,
                'include-xids', 'true',
                'include-timestamp', 'true',
                'skip-empty-xacts', 'true')"#,
    )
    .bind(&config.slot_name)
    .fetch_all(upstream_pool)
    .await?;

    if rows.is_empty() {
        return Ok(0);
    }

    let mut last_seen_lsn: Option<String> = None;
    let mut last_xid: Option<i64> = None;
    let mut produced = 0usize;

    for row in &rows {
        let lsn: String = row.try_get("lsn").map_err(CdcError::Db)?;
        let xid_text: String = row.try_get("xid").map_err(CdcError::Db)?;
        let data: String = row.try_get("data").map_err(CdcError::Db)?;
        let xid: Option<i64> = xid_text.parse::<i64>().ok();

        last_seen_lsn = Some(lsn.clone());
        last_xid = xid.or(last_xid);

        let Some(parsed) = parse_test_decoding_line(&data) else {
            // Non-row payloads (BEGIN/COMMIT, logical messages, ...) are
            // safe to skip.
            tracing::debug!(%lsn, "skipping non-row test_decoding payload: {data}");
            continue;
        };
        // `test_decoding` does not honour publications (unlike `pgoutput`)
        // — every change in the database hits the slot. Filter to the
        // tables this subscription was configured for so we never leak
        // unrelated mutations.
        if !config.tables.is_empty() && !table_matches(&config.tables, &parsed) {
            continue;
        }
        let op = parsed.op;

        let mut headers = std::collections::BTreeMap::new();
        headers.insert("cdc.op".into(), op.as_header().to_string());
        headers.insert("cdc.lsn".into(), lsn.clone());
        if let Some(xid) = xid {
            headers.insert("cdc.tx_id".into(), xid.to_string());
        }
        if let Some(table) = parsed.table.as_ref() {
            headers.insert("cdc.table".into(), table.clone());
        }
        if let Some(schema) = parsed.schema.as_ref() {
            headers.insert("cdc.schema".into(), schema.clone());
        }

        publisher
            .publish(CdcEnvelope {
                topic: config.event_topic.clone(),
                payload: data.into_bytes(),
                headers,
            })
            .await
            .map_err(|e| CdcError::Publisher(e.to_string()))?;

        produced += 1;
    }

    if let Some(lsn) = last_seen_lsn {
        record_advance(
            metadata_pool,
            config,
            &lsn,
            last_xid,
            produced as i64,
            Utc::now(),
        )
        .await?;
        // Only NOW do we let Postgres release the WAL up to this point.
        fast_forward_slot(upstream_pool, &config.slot_name, &lsn).await?;
    }

    Ok(produced)
}

/// Long-running worker. Loops [`poll_once`] forever, sleeping
/// `config.poll_interval` between empty polls. Returns when `shutdown`
/// resolves; any fatal error is returned to the caller.
pub async fn run<P, S>(
    metadata_pool: PgPool,
    upstream_pool: PgPool,
    config: PostgresCdcConfig,
    publisher: P,
    mut shutdown: S,
) -> Result<(), CdcError>
where
    P: EventPublisher,
    S: std::future::Future<Output = ()> + Unpin,
{
    let checkpoint = load_or_init_checkpoint(&metadata_pool, &config).await?;
    ensure_slot_and_publication(&upstream_pool, &config).await?;
    if let Some(lsn) = checkpoint.last_lsn.as_deref() {
        // Best effort — if Postgres rejects the LSN (slot horizon already
        // past it) we fall through to the normal polling loop and let the
        // slot stream resume from wherever it currently sits. The persisted
        // checkpoint will be overwritten on the next successful poll.
        if let Err(error) = fast_forward_slot(&upstream_pool, &config.slot_name, lsn).await {
            tracing::warn!(%lsn, %error, "slot fast-forward failed; proceeding from current slot horizon");
        }
    }

    loop {
        let produced = tokio::select! {
            biased;
            _ = &mut shutdown => return Ok(()),
            res = poll_once(&metadata_pool, &upstream_pool, &config, &publisher) => res?,
        };
        if produced == 0 {
            tokio::select! {
                biased;
                _ = &mut shutdown => return Ok(()),
                _ = tokio::time::sleep(config.poll_interval) => {}
            }
        }
    }
}

#[derive(Debug)]
struct DecodedRow {
    op: CdcOp,
    schema: Option<String>,
    table: Option<String>,
}

/// Parse a single `test_decoding` output line. Returns `None` for
/// transaction envelopes (`BEGIN`/`COMMIT`) and any other line we do not
/// recognise as a row mutation.
///
/// Recognised shapes (see `contrib/test_decoding`):
///
/// * `table public.orders: INSERT: id[bigint]:1 status[text]:'pending'`
/// * `table public.orders: UPDATE: id[bigint]:1 status[text]:'paid'`
/// * `table public.orders: DELETE: id[bigint]:1`
/// * `table public.orders: TRUNCATE: (options: cascade)`
fn parse_test_decoding_line(line: &str) -> Option<DecodedRow> {
    let rest = line.strip_prefix("table ")?;
    let (qualified, rest) = rest.split_once(": ")?;
    let (op_str, _) = rest.split_once(':').unwrap_or((rest, ""));
    let op = match op_str.trim() {
        "INSERT" => CdcOp::Insert,
        "UPDATE" => CdcOp::Update,
        "DELETE" => CdcOp::Delete,
        "TRUNCATE" => CdcOp::Truncate,
        _ => return None,
    };
    let (schema, table) = match qualified.split_once('.') {
        Some((s, t)) => (Some(s.to_string()), Some(t.to_string())),
        None => (None, Some(qualified.to_string())),
    };
    Some(DecodedRow { op, schema, table })
}

fn quote_ident(name: &str) -> String {
    let escaped = name.replace('"', "\"\"");
    format!("\"{escaped}\"")
}

/// Match a parsed row against a list of `schema.table` (or unqualified
/// `table`) entries. Quoted identifiers in the configuration are
/// tolerated by stripping double quotes before comparison.
fn table_matches(allowed: &[String], row: &DecodedRow) -> bool {
    let row_schema = row.schema.as_deref();
    let row_table = row.table.as_deref();
    allowed.iter().any(|entry| {
        let cleaned: String = entry.chars().filter(|c| *c != '"').collect();
        match cleaned.split_once('.') {
            Some((s, t)) => row_schema == Some(s) && row_table == Some(t),
            None => row_table == Some(cleaned.as_str()),
        }
    })
}

/// gRPC publisher that forwards each envelope to `event-streaming-service`.
pub mod grpc {
    use super::*;
    use tonic::transport::Channel;

    #[allow(clippy::all, missing_docs)]
    mod proto_router {
        tonic::include_proto!("openfoundry.streaming.router.v1");
    }

    use proto_router::PublishRequest;
    use proto_router::event_router_client::EventRouterClient;

    /// Forwards CDC envelopes to the EventRouter gRPC service. The client
    /// is cheap to clone (internally a `Channel` wrapping an `Arc`).
    #[derive(Clone)]
    pub struct EventStreamingPublisher {
        client: EventRouterClient<Channel>,
    }

    impl EventStreamingPublisher {
        pub async fn connect(endpoint: String) -> Result<Self, CdcError> {
            let channel = Channel::from_shared(endpoint)
                .map_err(|e| CdcError::Publisher(e.to_string()))?
                .connect()
                .await
                .map_err(|e| CdcError::Publisher(e.to_string()))?;
            Ok(Self {
                client: EventRouterClient::new(channel),
            })
        }
    }

    #[async_trait::async_trait]
    impl EventPublisher for EventStreamingPublisher {
        async fn publish(&self, envelope: CdcEnvelope) -> Result<(), CdcError> {
            let mut client = self.client.clone();
            let request = PublishRequest {
                topic: envelope.topic,
                payload: envelope.payload,
                headers: envelope.headers.into_iter().collect(),
                schema_id: None,
            };
            client
                .publish(request)
                .await
                .map(|_| ())
                .map_err(|status| CdcError::Publisher(status.to_string()))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cdc_op_maps_to_incremental_mode_header() {
        assert_eq!(CdcOp::Insert.as_header(), "append_only");
        assert_eq!(CdcOp::Update.as_header(), "upsert");
        assert_eq!(CdcOp::Delete.as_header(), "hard_delete");
        assert_eq!(CdcOp::Truncate.as_header(), "hard_delete");
    }

    #[test]
    fn cdc_op_round_trips_known_kinds_only() {
        assert_eq!(CdcOp::from_kind("insert"), Some(CdcOp::Insert));
        assert_eq!(CdcOp::from_kind("update"), Some(CdcOp::Update));
        assert_eq!(CdcOp::from_kind("delete"), Some(CdcOp::Delete));
        assert_eq!(CdcOp::from_kind("truncate"), Some(CdcOp::Truncate));
        assert_eq!(CdcOp::from_kind("commit"), None);
    }

    #[test]
    fn parse_test_decoding_recognises_dml_and_skips_envelopes() {
        let insert = parse_test_decoding_line(
            "table public.orders: INSERT: id[bigint]:1 status[text]:'pending'",
        )
        .expect("insert parses");
        assert_eq!(insert.op, CdcOp::Insert);
        assert_eq!(insert.schema.as_deref(), Some("public"));
        assert_eq!(insert.table.as_deref(), Some("orders"));

        let update = parse_test_decoding_line(
            "table public.orders: UPDATE: id[bigint]:1 status[text]:'paid'",
        )
        .expect("update parses");
        assert_eq!(update.op, CdcOp::Update);

        let delete = parse_test_decoding_line("table public.orders: DELETE: id[bigint]:1")
            .expect("delete parses");
        assert_eq!(delete.op, CdcOp::Delete);

        assert!(parse_test_decoding_line("BEGIN 707").is_none());
        assert!(parse_test_decoding_line("COMMIT 707").is_none());
        assert!(parse_test_decoding_line("").is_none());
    }

    #[test]
    fn quote_ident_escapes_embedded_quotes() {
        assert_eq!(quote_ident(r#"a"b"#), r#""a""b""#);
        assert_eq!(quote_ident("orders_pub"), "\"orders_pub\"");
    }

    #[tokio::test]
    async fn in_memory_publisher_records_envelopes() {
        let p = InMemoryPublisher::new();
        p.publish(CdcEnvelope {
            topic: "t".into(),
            payload: vec![1],
            headers: Default::default(),
        })
        .await
        .unwrap();
        assert_eq!(p.snapshot().await.len(), 1);
    }
}
