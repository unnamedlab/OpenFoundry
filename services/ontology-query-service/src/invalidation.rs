//! NATS subscriber that turns `ontology.write.v1` events into
//! cache invalidations (S1.5.b).
//!
//! The platform's primary control-plane bus is NATS JetStream
//! (see `libs/event-bus-control`). Debezium publishes the canonical
//! `ontology.write.v1` Kafka topic; a JetStream bridge mirrors it
//! into the `ontology.write.v1` subject so every Rust service can
//! consume without pulling in `librdkafka`.
//!
//! ## Event shape
//!
//! Payload is the JSON envelope emitted by `outbox::enqueue`. We only
//! need the `aggregate_id` (object id) and the optional `tenant`
//! header — everything else is ignored. Missing fields are logged at
//! `warn!` level and skipped rather than crashing the consumer.
//!
//! ## Failure handling
//!
//! * Connection errors at startup → returned to the caller so the
//!   binary can decide whether to crash (production) or warn and
//!   keep running with a possibly-stale cache (dev).
//! * Mid-stream errors → logged; the loop reconnects on the next
//!   poll. After 3 consecutive failures we *defensively* call
//!   [`CachingObjectStore::invalidate_all`] so we don't keep serving
//!   stale data while the bus is down.

use std::sync::Arc;

use serde::Deserialize;
use storage_abstraction::repositories::{ObjectId, TenantId};
use tracing::{debug, error, info, warn};

use crate::cache::CachingObjectStore;

/// JetStream subject we subscribe to. The bridge from Kafka uses
/// the same name.
pub const SUBJECT: &str = "ontology.write.v1";

/// Minimal projection of the Debezium outbox envelope. We only
/// extract the object identifier and (if present) the tenant.
#[derive(Debug, Clone, Deserialize)]
struct WriteEvent {
    /// `aggregate_id` from the outbox row.
    #[serde(default)]
    aggregate_id: Option<String>,
    /// Tenant — emitted as a header today, but some producers also
    /// inline it in the payload. We accept both.
    #[serde(default)]
    tenant: Option<String>,
    /// Object id alias. Some upstream emitters use `object_id`
    /// instead of `aggregate_id`; we accept both.
    #[serde(default)]
    object_id: Option<String>,
}

/// Spawn a long-running task that subscribes to [`SUBJECT`] and
/// invalidates `cache` on every received message. Returns
/// immediately; the caller decides whether to await the join handle.
///
/// `nats_url` is the JetStream / NATS URL (e.g. `nats://nats:4222`).
pub async fn spawn(
    nats_url: String,
    cache: Arc<CachingObjectStore>,
) -> Result<tokio::task::JoinHandle<()>, async_nats::Error> {
    let client = async_nats::connect(&nats_url).await?;
    info!(%nats_url, subject = SUBJECT, "subscribed to invalidation bus");
    let mut subscription = client.subscribe(SUBJECT).await?;

    let handle = tokio::spawn(async move {
        use futures::StreamExt;

        let mut consecutive_errors: u32 = 0;
        while let Some(msg) = subscription.next().await {
            match handle_message(&msg.payload, msg.headers.as_ref(), &cache).await {
                Ok(()) => consecutive_errors = 0,
                Err(error) => {
                    consecutive_errors = consecutive_errors.saturating_add(1);
                    warn!(?error, consecutive_errors, "invalidation message ignored");
                    if consecutive_errors >= 3 {
                        warn!("3 consecutive invalidation failures — flushing cache defensively");
                        cache.invalidate_all();
                        consecutive_errors = 0;
                    }
                }
            }
        }
        error!("invalidation subscription ended; cache may serve stale data");
    });

    Ok(handle)
}

async fn handle_message(
    payload: &[u8],
    headers: Option<&async_nats::HeaderMap>,
    cache: &CachingObjectStore,
) -> Result<(), String> {
    let evt: WriteEvent =
        serde_json::from_slice(payload).map_err(|e| format!("invalid envelope JSON: {e}"))?;

    let object_id = evt
        .object_id
        .or(evt.aggregate_id)
        .ok_or_else(|| "envelope missing object_id / aggregate_id".to_string())?;

    let tenant = evt
        .tenant
        .or_else(|| headers.and_then(|h| h.get("tenant")).map(|v| v.to_string()))
        .ok_or_else(|| "envelope missing tenant header".to_string())?;

    debug!(%tenant, %object_id, "invalidating cache entry");
    cache
        .invalidate(&TenantId(tenant), &ObjectId(object_id))
        .await;
    Ok(())
}
