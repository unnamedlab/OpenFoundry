//! Structured audit events for every SQL statement executed through the
//! gateway. Emitted as `tracing` events at INFO level so they land in
//! whatever sink is configured (stdout JSON in production, the OTLP
//! exporter when wired by the platform).
//!
//! Recorded fields are intentionally narrow: we never log the full SQL
//! payload (it can contain PII / secrets) — we log a SHA-256 prefix that
//! is enough to correlate against client-side logs without leaking
//! contents.

use std::time::Duration;

use crate::routing::Backend;

/// A single SQL execution audit record.
pub struct SqlAuditEvent<'a> {
    pub tenant_id: Option<&'a str>,
    pub tenant_tier: &'a str,
    pub user_email: Option<&'a str>,
    pub backend: Backend,
    pub remote: bool,
    pub sql_hash: &'a str,
    pub row_count: usize,
    pub duration: Duration,
    pub outcome: AuditOutcome,
}

#[derive(Debug, Clone, Copy)]
pub enum AuditOutcome {
    Ok,
    Error,
}

impl AuditOutcome {
    fn as_str(self) -> &'static str {
        match self {
            AuditOutcome::Ok => "ok",
            AuditOutcome::Error => "error",
        }
    }
}

impl SqlAuditEvent<'_> {
    pub fn emit(&self) {
        tracing::info!(
            target: "sql_bi_gateway.audit",
            tenant_id = self.tenant_id.unwrap_or("-"),
            tenant_tier = self.tenant_tier,
            user_email = self.user_email.unwrap_or("-"),
            backend = self.backend.as_str(),
            remote = self.remote,
            sql_hash = self.sql_hash,
            row_count = self.row_count,
            duration_ms = self.duration.as_millis() as u64,
            outcome = self.outcome.as_str(),
            "sql_bi_gateway statement executed",
        );
    }
}

/// Compute a stable, short, non-reversible identifier for a SQL string
/// suitable for log correlation. Uses the first 16 hex chars of the
/// FNV-1a 64-bit hash of the trimmed, case-folded statement — small,
/// deterministic, and avoids pulling in a cryptographic hash crate just
/// for log fingerprints.
pub fn sql_fingerprint(sql: &str) -> String {
    let normalized: String = sql
        .trim()
        .chars()
        .map(|c| c.to_ascii_lowercase())
        .collect();
    let mut hash: u64 = 0xcbf29ce484222325;
    for byte in normalized.as_bytes() {
        hash ^= u64::from(*byte);
        hash = hash.wrapping_mul(0x100000001b3);
    }
    format!("{hash:016x}")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fingerprint_is_stable_and_case_insensitive() {
        let a = sql_fingerprint("SELECT 1");
        let b = sql_fingerprint("select 1");
        let c = sql_fingerprint("  select   1  ");
        assert_eq!(a.len(), 16);
        assert_eq!(a, b);
        // whitespace inside the body is preserved (different statements),
        // only leading/trailing whitespace is trimmed.
        assert_ne!(a, c);
    }

    #[test]
    fn different_statements_produce_different_fingerprints() {
        assert_ne!(sql_fingerprint("SELECT 1"), sql_fingerprint("SELECT 2"));
    }
}
