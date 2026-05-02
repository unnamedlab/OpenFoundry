//! T3.3 — Marking enforcement.
//!
//! Catalog/preview/export/upload/views handlers must reject requests
//! when the caller's clearances don't cover the dataset's effective
//! markings (direct + inherited). This module provides:
//!
//!   * [`CallerClearances`]: a strongly-typed `HashSet<MarkingId>`
//!     derived from a [`Claims`] token.
//!   * [`MarkingNameResolver`]: a lookup trait `name → MarkingId`
//!     pluggable by services (production wires it to a cached call
//!     into the markings master table; tests use an in-memory map).
//!   * [`enforce_markings`]: the binary "allowed?" check used by
//!     handlers, returning an [`EnforcementError`] with HTTP semantics
//!     (`Forbidden` carries the missing markings for audit/logging).
//!
//! Why the indirection: [`Claims::allowed_markings`] returns string
//! names (legacy ladder `public ⊂ confidential ⊂ pii`), but the
//! source-of-truth `dataset_markings` table stores [`MarkingId`]s.
//! `MarkingNameResolver` bridges the two until the JWT issuer
//! (identity-federation-service) emits typed UUIDs natively.

use std::collections::HashSet;

use core_models::security::MarkingId;

use crate::Claims;

/// Per-request set of marking ids the caller is cleared for.
#[derive(Debug, Default, Clone)]
pub struct CallerClearances {
    ids: HashSet<MarkingId>,
    /// Original lowercase string names — kept so legacy handlers that
    /// haven't migrated to typed ids can still call
    /// `claims.allows_marking(&str)`-style checks via this struct.
    names: HashSet<String>,
    /// Admins bypass marking enforcement entirely (mirrors the
    /// behaviour of [`Claims::allows_marking`]).
    admin: bool,
}

impl CallerClearances {
    /// Build from claims, resolving each marking name into its
    /// [`MarkingId`] via `resolver`. Names the resolver doesn't know
    /// are kept in [`Self::names`] so a legacy string-only enforcement
    /// can still grant access.
    pub fn from_claims<R: MarkingNameResolver + ?Sized>(claims: &Claims, resolver: &R) -> Self {
        let admin = claims.has_role("admin");
        let names: HashSet<String> = claims
            .allowed_markings()
            .into_iter()
            .map(|n| n.to_ascii_lowercase())
            .collect();
        let ids = names
            .iter()
            .filter_map(|name| resolver.resolve(name))
            .collect();
        Self { ids, names, admin }
    }

    /// Convenience for tests / call sites that don't want to plumb a
    /// resolver — keeps the string ladder only.
    pub fn from_claims_names_only(claims: &Claims) -> Self {
        let admin = claims.has_role("admin");
        let names = claims
            .allowed_markings()
            .into_iter()
            .map(|n| n.to_ascii_lowercase())
            .collect();
        Self {
            ids: HashSet::new(),
            names,
            admin,
        }
    }

    pub fn is_admin(&self) -> bool {
        self.admin
    }

    pub fn ids(&self) -> &HashSet<MarkingId> {
        &self.ids
    }

    pub fn names(&self) -> &HashSet<String> {
        &self.names
    }

    /// Whether the caller is cleared for `marking_id`.
    pub fn allows_id(&self, marking_id: MarkingId) -> bool {
        self.admin || self.ids.contains(&marking_id)
    }

    /// Whether the caller is cleared for the marking *name*
    /// (case-insensitive). Useful while the JWT only carries names.
    pub fn allows_name(&self, marking_name: &str) -> bool {
        self.admin || self.names.contains(&marking_name.to_ascii_lowercase())
    }
}

/// Resolves a marking *name* (case-insensitive) into its catalogued
/// [`MarkingId`]. Implementations typically cache this mapping in
/// memory since the markings table is tiny and rarely changes.
pub trait MarkingNameResolver: Send + Sync {
    fn resolve(&self, name: &str) -> Option<MarkingId>;
}

/// Static map implementation, useful for tests and bootstrap.
#[derive(Debug, Default, Clone)]
pub struct StaticMarkingNameResolver {
    map: std::collections::HashMap<String, MarkingId>,
}

impl StaticMarkingNameResolver {
    pub fn new<I: IntoIterator<Item = (String, MarkingId)>>(entries: I) -> Self {
        let map = entries
            .into_iter()
            .map(|(k, v)| (k.to_ascii_lowercase(), v))
            .collect();
        Self { map }
    }
}

impl MarkingNameResolver for StaticMarkingNameResolver {
    fn resolve(&self, name: &str) -> Option<MarkingId> {
        self.map.get(&name.to_ascii_lowercase()).copied()
    }
}

/// Outcome of [`enforce_markings`].
#[derive(Debug, thiserror::Error)]
pub enum EnforcementError {
    /// The caller is missing one or more markings required by the
    /// dataset. The vec contains the offending [`MarkingId`]s — the
    /// handler should map this to HTTP 403 and log the set for audit.
    #[error("forbidden: missing markings {0:?}")]
    Forbidden(Vec<MarkingId>),
}

/// Reject when `effective_markings - clearances ≠ ∅`.
///
/// Returns `Ok(())` when the caller is cleared for *every* marking on
/// the dataset (direct + inherited). The check is intentionally strict:
/// inheriting `RESTRICTED` from any upstream gates the entire dataset.
pub fn enforce_markings(
    effective_markings: impl IntoIterator<Item = MarkingId>,
    clearances: &CallerClearances,
) -> Result<(), EnforcementError> {
    if clearances.is_admin() {
        return Ok(());
    }
    let missing: Vec<MarkingId> = effective_markings
        .into_iter()
        .filter(|id| !clearances.ids.contains(id))
        .collect();
    if missing.is_empty() {
        Ok(())
    } else {
        Err(EnforcementError::Forbidden(missing))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Utc;
    use serde_json::Value;
    use uuid::Uuid;

    fn claims_with(roles: Vec<&str>, allowed: Vec<&str>) -> Claims {
        Claims {
            sub: Uuid::now_v7(),
            iat: Utc::now().timestamp(),
            exp: Utc::now().timestamp() + 3600,
            iss: None,
            aud: None,
            jti: Uuid::now_v7(),
            email: "u@example.test".into(),
            name: "u".into(),
            roles: roles.into_iter().map(String::from).collect(),
            permissions: vec![],
            org_id: None,
            attributes: Value::Null,
            auth_methods: vec![],
            token_use: None,
            api_key_id: None,
            session_kind: None,
            session_scope: Some(crate::claims::SessionScope {
                allowed_markings: allowed.into_iter().map(String::from).collect(),
                ..Default::default()
            }),
        }
    }

    #[test]
    fn admin_bypasses_enforcement() {
        let claims = claims_with(vec!["admin"], vec![]);
        let clearances = CallerClearances::from_claims_names_only(&claims);
        assert!(clearances.is_admin());
        let restricted = MarkingId::new();
        assert!(enforce_markings([restricted], &clearances).is_ok());
    }

    #[test]
    fn non_admin_with_full_coverage_passes() {
        let public = MarkingId::new();
        let restricted = MarkingId::new();
        let resolver = StaticMarkingNameResolver::new(vec![
            ("public".into(), public),
            ("restricted".into(), restricted),
        ]);
        // Note: claims_with bypasses the ladder — we set both names.
        let claims = claims_with(vec![], vec!["public", "restricted"]);
        let clearances = CallerClearances::from_claims(&claims, &resolver);
        assert!(enforce_markings([public, restricted], &clearances).is_ok());
    }

    #[test]
    fn non_admin_missing_marking_is_forbidden() {
        let public = MarkingId::new();
        let restricted = MarkingId::new();
        let resolver = StaticMarkingNameResolver::new(vec![
            ("public".into(), public),
            ("restricted".into(), restricted),
        ]);
        let claims = claims_with(vec![], vec!["public"]);
        let clearances = CallerClearances::from_claims(&claims, &resolver);
        let err = enforce_markings([public, restricted], &clearances).unwrap_err();
        let EnforcementError::Forbidden(missing) = err;
        assert_eq!(missing, vec![restricted]);
    }

    #[test]
    fn allows_name_handles_case_insensitive_lookup() {
        let claims = claims_with(vec![], vec!["PII"]);
        let clearances = CallerClearances::from_claims_names_only(&claims);
        assert!(clearances.allows_name("pii"));
        assert!(clearances.allows_name("PiI"));
        assert!(!clearances.allows_name("restricted"));
    }
}
