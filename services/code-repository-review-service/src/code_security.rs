//! Code-security scans / findings — domain absorbed from
//! `code-security-scanning-service` per ADR-0030 (S8 merge).
//!
//! Tables (`code_security_scans`, `code_security_findings`) live in
//! the consolidated migration set under `migrations/`. A scanner
//! integration is not yet wired in this binary; future work will
//! emit findings to [`FINDINGS_TOPIC`].

/// Canonical Kafka topic the scanner emits to. Preserved from the
/// retired `code-security-scanning-service`; downstream consumers
/// continue to subscribe with the same name.
pub const FINDINGS_TOPIC: &str = "code.security.findings";
