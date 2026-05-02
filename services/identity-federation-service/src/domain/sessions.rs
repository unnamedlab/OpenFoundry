use auth_middleware::{
    claims::SessionScope,
    jwt::{self, JwtConfig, JwtError},
};
use chrono::{DateTime, Utc};
use sqlx::PgPool;
use uuid::Uuid;

use crate::{
    domain::{access, rbac},
    models::{
        session::{ScopedSession, ScopedSessionKind, ScopedSessionWithToken},
        user::User,
    },
    sessions_cassandra::{ScopedSessionRecord, SessionsAdapter},
};

#[derive(Debug)]
pub enum ScopedSessionError {
    Database(sqlx::Error),
    Cassandra(cassandra_kernel::KernelError),
    Token(JwtError),
    InvalidExpiration,
    Decode(String),
}

impl From<sqlx::Error> for ScopedSessionError {
    fn from(value: sqlx::Error) -> Self {
        Self::Database(value)
    }
}

impl From<JwtError> for ScopedSessionError {
    fn from(value: JwtError) -> Self {
        Self::Token(value)
    }
}

impl From<cassandra_kernel::KernelError> for ScopedSessionError {
    fn from(value: cassandra_kernel::KernelError) -> Self {
        Self::Cassandra(value)
    }
}

pub async fn list_scoped_sessions(
    sessions: &SessionsAdapter,
    user_id: Uuid,
) -> Result<Vec<ScopedSession>, ScopedSessionError> {
    sessions
        .list_scoped_sessions(user_id)
        .await?
        .into_iter()
        .map(scoped_session_from_record)
        .collect::<Result<Vec<_>, _>>()
}

pub async fn issue_scoped_session(
    pool: &PgPool,
    sessions: &SessionsAdapter,
    config: &JwtConfig,
    user: &User,
    label: &str,
    session_kind: ScopedSessionKind,
    requested_permissions: Vec<String>,
    mut scope: SessionScope,
    expires_at: Option<DateTime<Utc>>,
    guest_email: Option<String>,
    guest_name: Option<String>,
) -> Result<ScopedSessionWithToken, ScopedSessionError> {
    let now = Utc::now();
    let expires_at = expires_at.unwrap_or_else(|| {
        now + if session_kind == ScopedSessionKind::Guest {
            chrono::Duration::hours(12)
        } else {
            chrono::Duration::hours(24)
        }
    });
    if expires_at <= now {
        return Err(ScopedSessionError::InvalidExpiration);
    }

    normalize_scope(&mut scope, user.organization_id, session_kind);

    let access_bundle = rbac::get_user_access_bundle(pool, user.id)
        .await
        .unwrap_or_default();
    let granted_permissions = if requested_permissions.is_empty() {
        access_bundle.permissions.clone()
    } else {
        requested_permissions
    };

    let session_id = Uuid::now_v7();
    let expires_in_secs = (expires_at - now).num_seconds().max(60);
    let token = jwt::encode_token(
        config,
        &jwt::build_access_claims_with_scope(
            &config.clone().with_access_ttl(expires_in_secs),
            user.id,
            &user.email,
            &user.name,
            if session_kind == ScopedSessionKind::Guest {
                vec!["guest".to_string()]
            } else {
                access_bundle.roles.clone()
            },
            granted_permissions,
            user.organization_id,
            user.attributes.clone(),
            vec![match session_kind {
                ScopedSessionKind::Scoped => "scoped_session".to_string(),
                ScopedSessionKind::Guest => "guest".to_string(),
            }],
            Some(scope.clone()),
            Some(match session_kind {
                ScopedSessionKind::Scoped => "scoped_session".to_string(),
                ScopedSessionKind::Guest => "guest_session".to_string(),
            }),
        ),
    )?;

    sessions
        .record_scoped_session(ScopedSessionRecord {
            id: session_id,
            user_id: user.id,
            label: label.to_string(),
            session_kind: session_kind.as_str().to_string(),
            scope: serde_json::to_value(&scope).unwrap_or_default(),
            guest_email: guest_email.clone(),
            guest_name: guest_name.clone(),
            expires_at,
            revoked_at: None,
            created_at: now,
        })
        .await?;

    Ok(ScopedSessionWithToken {
        id: session_id,
        label: label.to_string(),
        session_kind,
        scope,
        token,
        expires_at,
        guest_email,
        guest_name,
        created_at: now,
    })
}

pub async fn revoke_scoped_session(
    sessions: &SessionsAdapter,
    session_id: Uuid,
    user_id: Uuid,
    allow_any_user: bool,
) -> Result<bool, ScopedSessionError> {
    Ok(sessions
        .revoke_scoped_session(session_id, user_id, allow_any_user)
        .await?)
}

fn scoped_session_from_record(
    record: ScopedSessionRecord,
) -> Result<ScopedSession, ScopedSessionError> {
    let session_kind = match record.session_kind.as_str() {
        "scoped" => ScopedSessionKind::Scoped,
        "guest" => ScopedSessionKind::Guest,
        other => {
            return Err(ScopedSessionError::Decode(format!(
                "unsupported scoped session kind: {other}"
            )));
        }
    };
    let scope = serde_json::from_value(record.scope)
        .map_err(|error| ScopedSessionError::Decode(error.to_string()))?;

    Ok(ScopedSession {
        id: record.id,
        user_id: record.user_id,
        label: record.label,
        session_kind,
        scope,
        guest_email: record.guest_email,
        guest_name: record.guest_name,
        expires_at: record.expires_at,
        revoked_at: record.revoked_at,
        created_at: record.created_at,
    })
}

fn normalize_scope(
    scope: &mut SessionScope,
    user_org_id: Option<Uuid>,
    session_kind: ScopedSessionKind,
) {
    scope.allowed_methods = normalize_methods(&scope.allowed_methods);
    scope.allowed_path_prefixes = normalize_paths(&scope.allowed_path_prefixes);
    scope.allowed_subject_ids.sort();
    scope.allowed_subject_ids.dedup();
    scope.allowed_org_ids.sort();
    scope.allowed_org_ids.dedup();
    scope.allowed_markings =
        access::normalize_markings(&scope.allowed_markings).unwrap_or_default();
    scope.restricted_view_ids.sort();
    scope.restricted_view_ids.dedup();

    if scope.allowed_org_ids.is_empty() {
        if let Some(org_id) = user_org_id {
            scope.allowed_org_ids.push(org_id);
        }
    }

    if scope.classification_clearance.is_none() {
        scope.classification_clearance = access::max_marking(&scope.allowed_markings);
    }
    if scope.allowed_markings.is_empty() {
        scope.allowed_markings =
            access::markings_for_clearance(scope.classification_clearance.as_deref());
    }

    if session_kind == ScopedSessionKind::Guest {
        if scope.allowed_methods.is_empty() {
            scope.allowed_methods = vec!["GET".to_string()];
        }
        if scope.allowed_path_prefixes.is_empty() {
            scope.allowed_path_prefixes = vec![
                "/api/v1/datasets".to_string(),
                "/api/v1/lineage".to_string(),
                "/api/v1/ontology".to_string(),
                "/api/v1/reports".to_string(),
                "/api/v1/audit".to_string(),
            ];
        }
        if scope.classification_clearance.is_none() {
            scope.classification_clearance = Some("public".to_string());
        }
        if scope.allowed_markings.is_empty() {
            scope.allowed_markings = vec!["public".to_string()];
        }
        scope.consumer_mode = true;
    }
}

fn normalize_methods(values: &[String]) -> Vec<String> {
    let mut methods = values
        .iter()
        .map(|value| value.trim().to_ascii_uppercase())
        .filter(|value| !value.is_empty())
        .collect::<Vec<_>>();
    methods.sort();
    methods.dedup();
    methods
}

fn normalize_paths(values: &[String]) -> Vec<String> {
    let mut paths = values
        .iter()
        .map(|value| value.trim())
        .filter(|value| !value.is_empty())
        .map(|value| {
            if value.starts_with('/') {
                value.to_string()
            } else {
                format!("/{value}")
            }
        })
        .collect::<Vec<_>>();
    paths.sort();
    paths.dedup();
    paths
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_guest_scope_sets_safe_defaults() {
        let mut scope = SessionScope::default();
        normalize_scope(&mut scope, Some(Uuid::nil()), ScopedSessionKind::Guest);
        assert_eq!(scope.allowed_methods, vec!["GET".to_string()]);
        assert!(
            scope
                .allowed_path_prefixes
                .iter()
                .any(|prefix| prefix == "/api/v1/datasets")
        );
        assert_eq!(scope.allowed_org_ids, vec![Uuid::nil()]);
        assert_eq!(scope.classification_clearance.as_deref(), Some("public"));
        assert_eq!(scope.allowed_markings, vec!["public".to_string()]);
        assert!(scope.consumer_mode);
    }

    #[test]
    fn normalize_paths_and_methods_deduplicate() {
        assert_eq!(
            normalize_methods(&["get".into(), "GET".into(), "post".into()]),
            vec!["GET".to_string(), "POST".to_string()]
        );
        assert_eq!(
            normalize_paths(&["api/v1/datasets".into(), "/api/v1/datasets".into()]),
            vec!["/api/v1/datasets".to_string()]
        );
    }
}
