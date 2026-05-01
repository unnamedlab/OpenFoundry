//! Real credential vending for Iceberg REST clients.
//!
//! Foundry's "Authenticating Iceberg clients" doc describes a pattern where
//! the catalog mediates access to lake storage and hands clients short-lived,
//! table-scoped credentials under spec keys (`s3.access-key-id`,
//! `adls.sas-token`, …). This module turns the connection's stored
//! configuration into those credentials by calling the real cloud APIs:
//!
//! * **AWS S3**: [`assume_role`] invokes `sts:AssumeRole` against the IAM
//!   role declared in `connection.config.assume_role_arn`. The session
//!   credentials returned by STS are short-lived (TTL controlled by the
//!   caller) and never include the catalog's own keys.
//! * **Azure ADLS / Blob**: [`generate_account_sas`] builds an account-level
//!   SAS token using HMAC-SHA256 over the canonical
//!   `account-key`-signed string described in the
//!   [Azure Storage SAS docs ↗](https://learn.microsoft.com/azure/storage/common/storage-sas-overview).
//!
//! When the source has no IAM role / account key configured the vendor
//! falls back to **passthrough**: any static `access_key_id` /
//! `secret_access_key` / `sas_token` already in the connection is returned
//! verbatim. This preserves the previous behaviour for sources operating
//! against MinIO, fakes or pre-issued tokens.

use base64::Engine;
use base64::engine::general_purpose::STANDARD as B64;
use hmac::{Hmac, Mac};
use serde_json::{Value, json};
use sha2::Sha256;
use std::collections::HashMap;
use std::sync::OnceLock;
use tokio::sync::Mutex;
use uuid::Uuid;

use crate::models::connection::Connection;

type HmacSha256 = Hmac<Sha256>;

/// Outcome of credential vending.
pub struct VendedCredentials {
    /// Spec-compliant Iceberg REST config entries.
    pub entries: Vec<(&'static str, Value)>,
    /// Effective expiry as unix-millis. Always set; defaults to `now + ttl`.
    pub expires_at_ms: i64,
}

/// Vends storage credentials for `connection`. Honours the per-source
/// `assume_role_arn` (S3) and `account_key` (Azure) when present, otherwise
/// returns whatever static values the source already carries.
pub async fn vend(connection: &Connection, ttl_secs: i64) -> VendedCredentials {
    let now_ms = chrono::Utc::now().timestamp_millis();
    let expires_at_ms = now_ms + ttl_secs * 1000;
    let mut entries: Vec<(&'static str, Value)> = Vec::new();
    entries.push(("expires-at-ms", json!(expires_at_ms.to_string())));

    let cfg = &connection.config;
    match connection.connector_type.as_str() {
        "s3" => {
            if let Some(region) = cfg.get("region").and_then(Value::as_str) {
                entries.push(("s3.region", json!(region)));
                entries.push(("client.region", json!(region)));
            }
            if let Some(endpoint) = cfg.get("endpoint").and_then(Value::as_str) {
                entries.push(("s3.endpoint", json!(endpoint)));
            }
            if cfg
                .get("path_style")
                .and_then(Value::as_bool)
                .unwrap_or(false)
            {
                entries.push(("s3.path-style-access", json!("true")));
            }

            // Real STS AssumeRole when an IAM role is declared on the source.
            if let Some(arn) = cfg.get("assume_role_arn").and_then(Value::as_str) {
                let session = cfg
                    .get("assume_role_session_name")
                    .and_then(Value::as_str)
                    .unwrap_or("openfoundry-vended");
                let external_id = cfg
                    .get("assume_role_external_id")
                    .and_then(Value::as_str);
                let region = cfg.get("region").and_then(Value::as_str);
                match assume_role_cached(
                    connection.id,
                    arn,
                    session,
                    external_id,
                    region,
                    ttl_secs,
                )
                .await
                {
                    Ok(creds) => {
                        entries.push(("s3.access-key-id", json!(creds.access_key_id)));
                        entries.push(("s3.secret-access-key", json!(creds.secret_access_key)));
                        entries.push(("s3.session-token", json!(creds.session_token)));
                        return VendedCredentials {
                            entries,
                            expires_at_ms: creds.expires_at_ms.unwrap_or(expires_at_ms),
                        };
                    }
                    Err(error) => {
                        tracing::warn!(
                            connection_id = %connection.id,
                            "STS AssumeRole failed, falling back to static credentials: {error}"
                        );
                    }
                }
            }

            // Static / passthrough credentials.
            if let Some(key) = cfg.get("access_key_id").and_then(Value::as_str) {
                entries.push(("s3.access-key-id", json!(key)));
            }
            if let Some(secret) = cfg.get("secret_access_key").and_then(Value::as_str) {
                entries.push(("s3.secret-access-key", json!(secret)));
            }
            if let Some(token) = cfg.get("session_token").and_then(Value::as_str) {
                entries.push(("s3.session-token", json!(token)));
            }
        }
        "azure_blob" | "adls" | "onelake" => {
            if let Some(account) = cfg.get("account_name").and_then(Value::as_str) {
                entries.push(("adls.account-name", json!(account)));

                // Generate a fresh SAS when the storage account key is
                // available; otherwise honour any pre-issued token.
                //
                // When `container_name` is set we issue a *service SAS*
                // scoped to that single container — strictly narrower than
                // an account SAS and the recommended option for catalog
                // vending. Without it we fall back to an account SAS
                // covering blob+file services.
                if let Some(key) = cfg.get("account_key").and_then(Value::as_str) {
                    let perms = cfg
                        .get("sas_permissions")
                        .and_then(Value::as_str)
                        .unwrap_or("rl");
                    let container = cfg.get("container_name").and_then(Value::as_str);
                    let sas_result = match container {
                        Some(c) => generate_service_sas_container(account, key, c, perms, expires_at_ms),
                        None => generate_account_sas(account, key, perms, expires_at_ms),
                    };
                    match sas_result {
                        Ok(sas) => {
                            entries.push(("adls.sas-token", json!(sas)));
                            if let Some(c) = container {
                                entries.push(("adls.container", json!(c)));
                            }
                            return VendedCredentials {
                                entries,
                                expires_at_ms,
                            };
                        }
                        Err(error) => {
                            tracing::warn!(
                                connection_id = %connection.id,
                                "Azure SAS generation failed, falling back to static token: {error}"
                            );
                        }
                    }
                }

                if let Some(sas) = cfg.get("sas_token").and_then(Value::as_str) {
                    entries.push(("adls.sas-token", json!(sas)));
                }
            }
        }
        "gcs" | "google_cloud_storage" => {
            if let Some(token) = cfg.get("access_token").and_then(Value::as_str) {
                entries.push(("gcs.oauth2.token", json!(token)));
            }
            if let Some(project) = cfg.get("project_id").and_then(Value::as_str) {
                entries.push(("gcs.project-id", json!(project)));
            }
        }
        _ => {}
    }

    VendedCredentials {
        entries,
        expires_at_ms,
    }
}

// ────────────────────────── AWS STS ──────────────────────────

#[derive(Clone)]
struct AssumedRoleCredentials {
    access_key_id: String,
    secret_access_key: String,
    session_token: String,
    expires_at_ms: Option<i64>,
}

/// Process-wide cache of `sts:AssumeRole` results keyed by the
/// `(connection_id, role_arn)` pair. The Iceberg REST `loadTable` endpoint
/// is hit by every PyIceberg / Spark / Trino client refresh tick — without a
/// cache we would re-issue an STS call per request and quickly hit
/// AWS's throttling limits.
///
/// Entries are reused while the assumed-role credentials are still
/// considered "fresh" (more than 20 % of their TTL remaining). The 80 %
/// reuse window matches what PyIceberg, Trino's `IcebergSessionCatalog` and
/// AWS's own SDK credential providers do internally.
static STS_CACHE: OnceLock<Mutex<HashMap<(Uuid, String), AssumedRoleCredentials>>> =
    OnceLock::new();

fn sts_cache() -> &'static Mutex<HashMap<(Uuid, String), AssumedRoleCredentials>> {
    STS_CACHE.get_or_init(|| Mutex::new(HashMap::new()))
}

/// Wraps [`assume_role`] with a fresh-credentials cache. Returns the cached
/// session if it has more than 20 % of its declared lifetime remaining;
/// otherwise calls STS, stores the new session and returns it.
async fn assume_role_cached(
    connection_id: Uuid,
    role_arn: &str,
    session_name: &str,
    external_id: Option<&str>,
    region: Option<&str>,
    ttl_secs: i64,
) -> Result<AssumedRoleCredentials, String> {
    let key = (connection_id, role_arn.to_string());
    let now_ms = chrono::Utc::now().timestamp_millis();
    let refresh_threshold_ms = (ttl_secs * 1000) / 5; // refresh at <20% remaining

    {
        let cache = sts_cache().lock().await;
        if let Some(entry) = cache.get(&key) {
            if let Some(expires) = entry.expires_at_ms {
                if expires - now_ms > refresh_threshold_ms {
                    tracing::debug!(
                        connection_id = %connection_id,
                        remaining_ms = expires - now_ms,
                        "STS cache hit"
                    );
                    return Ok(entry.clone());
                }
            }
        }
    }

    let fresh = assume_role(role_arn, session_name, external_id, region, ttl_secs).await?;
    let mut cache = sts_cache().lock().await;
    cache.insert(key, fresh.clone());
    Ok(fresh)
}

async fn assume_role(
    role_arn: &str,
    session_name: &str,
    external_id: Option<&str>,
    region: Option<&str>,
    ttl_secs: i64,
) -> Result<AssumedRoleCredentials, String> {
    use aws_config::BehaviorVersion;

    let mut loader = aws_config::defaults(BehaviorVersion::latest());
    if let Some(r) = region {
        loader = loader.region(aws_config::Region::new(r.to_string()));
    }
    let shared = loader.load().await;
    let client = aws_sdk_sts::Client::new(&shared);

    // STS AssumeRole accepts 900..=43200 seconds; clamp into the valid range.
    let duration = ttl_secs.clamp(900, 43_200) as i32;

    let mut req = client
        .assume_role()
        .role_arn(role_arn)
        .role_session_name(session_name)
        .duration_seconds(duration);
    if let Some(ext) = external_id {
        req = req.external_id(ext);
    }
    let resp = req
        .send()
        .await
        .map_err(|e| format!("sts:AssumeRole failed: {e}"))?;

    let creds = resp
        .credentials
        .ok_or_else(|| "sts:AssumeRole returned no credentials".to_string())?;
    Ok(AssumedRoleCredentials {
        access_key_id: creds.access_key_id,
        secret_access_key: creds.secret_access_key,
        session_token: creds.session_token,
        expires_at_ms: Some(creds.expiration.to_millis().unwrap_or(0)),
    })
}

// ─────────────────────── Azure account SAS ───────────────────────

/// Builds an account SAS query string per
/// <https://learn.microsoft.com/rest/api/storageservices/create-account-sas>.
///
/// Defaults: services=blob+file, resource types=container+object,
/// protocol=https, signed-version=2022-11-02. Permissions are caller-supplied.
fn generate_account_sas(
    account: &str,
    account_key_b64: &str,
    permissions: &str,
    expires_at_ms: i64,
) -> Result<String, String> {
    use chrono::{TimeZone, Utc};

    let signed_version = "2022-11-02";
    let signed_services = "bf"; // blob + file
    let signed_resource_types = "co"; // container + object
    let signed_protocol = "https";

    let expiry = Utc
        .timestamp_millis_opt(expires_at_ms)
        .single()
        .ok_or_else(|| format!("invalid expiry timestamp: {expires_at_ms}"))?;
    let signed_expiry = expiry.format("%Y-%m-%dT%H:%M:%SZ").to_string();
    let signed_start = String::new(); // omitted — token is valid immediately

    // StringToSign for account SAS (see Azure docs link above).
    let string_to_sign = format!(
        "{account}\n{permissions}\n{services}\n{resource_types}\n{start}\n{expiry}\n\n{protocol}\n{version}\n\n\n\n\n",
        account = account,
        permissions = permissions,
        services = signed_services,
        resource_types = signed_resource_types,
        start = signed_start,
        expiry = signed_expiry,
        protocol = signed_protocol,
        version = signed_version,
    );

    let key_bytes = B64
        .decode(account_key_b64.as_bytes())
        .map_err(|e| format!("account_key is not base64: {e}"))?;
    let mut mac = HmacSha256::new_from_slice(&key_bytes)
        .map_err(|e| format!("hmac key error: {e}"))?;
    mac.update(string_to_sign.as_bytes());
    let signature = B64.encode(mac.finalize().into_bytes());

    // Build the SAS query string. Values are URL-encoded the way the
    // Azure SDK does it (RFC 3986 reserved set).
    let sas = format!(
        "sv={ver}&ss={ss}&srt={srt}&sp={sp}&se={se}&spr={spr}&sig={sig}",
        ver = signed_version,
        ss = signed_services,
        srt = signed_resource_types,
        sp = url_encode(permissions),
        se = url_encode(&signed_expiry),
        spr = signed_protocol,
        sig = url_encode(&signature),
    );
    Ok(sas)
}

/// Builds a **service SAS** scoped to a single blob container, per
/// <https://learn.microsoft.com/rest/api/storageservices/create-service-sas>.
///
/// Strictly narrower than an account SAS: the token can only address blobs
/// inside the named container, and only for the supplied permission set.
/// Recommended whenever the caller knows which container backs the table
/// (which is always true for catalog-vended Iceberg / Delta tables).
fn generate_service_sas_container(
    account: &str,
    account_key_b64: &str,
    container: &str,
    permissions: &str,
    expires_at_ms: i64,
) -> Result<String, String> {
    use chrono::{TimeZone, Utc};

    let signed_version = "2022-11-02";
    let signed_resource = "c"; // container
    let signed_protocol = "https";

    let expiry = Utc
        .timestamp_millis_opt(expires_at_ms)
        .single()
        .ok_or_else(|| format!("invalid expiry timestamp: {expires_at_ms}"))?;
    let signed_expiry = expiry.format("%Y-%m-%dT%H:%M:%SZ").to_string();
    let signed_start = String::new();
    let canonicalized_resource = format!("/blob/{account}/{container}");

    // StringToSign for service SAS on a container resource (see Azure docs
    // link above). Empty positional fields preserve the canonical layout
    // expected by the storage service.
    let string_to_sign = format!(
        "{permissions}\n{start}\n{expiry}\n{canonical}\n\n\n\n{protocol}\n{version}\n{resource}\n\n\n\n\n\n",
        permissions = permissions,
        start = signed_start,
        expiry = signed_expiry,
        canonical = canonicalized_resource,
        protocol = signed_protocol,
        version = signed_version,
        resource = signed_resource,
    );

    let key_bytes = B64
        .decode(account_key_b64.as_bytes())
        .map_err(|e| format!("account_key is not base64: {e}"))?;
    let mut mac = HmacSha256::new_from_slice(&key_bytes)
        .map_err(|e| format!("hmac key error: {e}"))?;
    mac.update(string_to_sign.as_bytes());
    let signature = B64.encode(mac.finalize().into_bytes());

    let sas = format!(
        "sv={ver}&sr={sr}&sp={sp}&se={se}&spr={spr}&sig={sig}",
        ver = signed_version,
        sr = signed_resource,
        sp = url_encode(permissions),
        se = url_encode(&signed_expiry),
        spr = signed_protocol,
        sig = url_encode(&signature),
    );
    Ok(sas)
}

fn url_encode(input: &str) -> String {
    // Conservative percent-encoding matching the Azure SDK behaviour.
    const RESERVED: &[u8] = b"!#$&'()*+,/:;=?@[]";
    let mut out = String::with_capacity(input.len());
    for byte in input.as_bytes() {
        let b = *byte;
        let unreserved = b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_' | b'.' | b'~');
        if unreserved && !RESERVED.contains(&b) {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{:02X}", b));
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn account_sas_includes_signature_and_expiry() {
        // Key is base64 of the bytes "supersecretkey1234567890123456" (30 bytes).
        let key = B64.encode(b"supersecretkey1234567890123456");
        let sas = generate_account_sas("acct", &key, "rl", 1_900_000_000_000).unwrap();
        assert!(sas.contains("sv=2022-11-02"));
        assert!(sas.contains("ss=bf"));
        assert!(sas.contains("srt=co"));
        assert!(sas.contains("sp=rl"));
        assert!(sas.contains("se=2030-03-17T17%3A46%3A40Z"));
        assert!(sas.contains("spr=https"));
        assert!(sas.contains("sig="));
    }

    #[test]
    fn service_sas_is_container_scoped() {
        let key = B64.encode(b"supersecretkey1234567890123456");
        let sas =
            generate_service_sas_container("acct", &key, "warehouse", "rl", 1_900_000_000_000)
                .unwrap();
        // Container resource and no service/resource-type fields.
        assert!(sas.contains("sr=c"));
        assert!(sas.contains("sp=rl"));
        assert!(sas.contains("se=2030-03-17T17%3A46%3A40Z"));
        assert!(sas.contains("sig="));
        assert!(!sas.contains("ss="));
        assert!(!sas.contains("srt="));
    }
}
