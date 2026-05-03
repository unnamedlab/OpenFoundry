//! CRUD over `service_principals` — the project-scoped run-as
//! identities a `PROJECT_SCOPED` schedule executes under.
//!
//! Per the Foundry doc § "Project scope":
//!
//!   "Project-scoped mode … is more consistent, since the schedule is
//!    run independently of the user's permissions and only changes if
//!    the set of Projects the schedule is scoped to changes."
//!
//! A service principal is therefore the union of the markings /
//! clearances authorised by every Project in `project_scope_rids`.
//! The actual marking resolution lives in the AuthZ layer (Cedar);
//! here we only persist the principal row.

use chrono::{DateTime, Utc};
use serde::Serialize;
use sqlx::{PgPool, Row, postgres::PgRow};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct ServicePrincipal {
    pub id: Uuid,
    pub rid: String,
    pub display_name: String,
    pub project_scope_rids: Vec<String>,
    pub clearances: Vec<String>,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub revoked_at: Option<DateTime<Utc>>,
}

impl ServicePrincipal {
    pub fn is_active(&self) -> bool {
        self.revoked_at.is_none()
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ServicePrincipalError {
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
    #[error("service principal '{0}' not found")]
    NotFound(String),
}

#[derive(Debug, Clone)]
pub struct CreateServicePrincipal {
    pub display_name: String,
    pub project_scope_rids: Vec<String>,
    pub clearances: Vec<String>,
    pub created_by: String,
}

pub async fn create(
    pool: &PgPool,
    req: CreateServicePrincipal,
) -> Result<ServicePrincipal, ServicePrincipalError> {
    let id = Uuid::now_v7();
    let row = sqlx::query(
        r#"INSERT INTO service_principals
                (id, display_name, project_scope_rids, clearances, created_by)
           VALUES ($1, $2, $3, $4, $5)
           RETURNING id, rid, display_name, project_scope_rids,
                     clearances, created_by, created_at, revoked_at"#,
    )
    .bind(id)
    .bind(&req.display_name)
    .bind(&req.project_scope_rids)
    .bind(&req.clearances)
    .bind(&req.created_by)
    .fetch_one(pool)
    .await?;
    Ok(from_row(&row)?)
}

pub async fn get_by_id(
    pool: &PgPool,
    id: Uuid,
) -> Result<ServicePrincipal, ServicePrincipalError> {
    let row = sqlx::query(
        r#"SELECT id, rid, display_name, project_scope_rids,
                  clearances, created_by, created_at, revoked_at
             FROM service_principals
            WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(r) => Ok(from_row(&r)?),
        None => Err(ServicePrincipalError::NotFound(id.to_string())),
    }
}

pub async fn revoke(pool: &PgPool, id: Uuid) -> Result<(), ServicePrincipalError> {
    sqlx::query("UPDATE service_principals SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL")
        .bind(id)
        .execute(pool)
        .await?;
    Ok(())
}

fn from_row(row: &PgRow) -> Result<ServicePrincipal, sqlx::Error> {
    Ok(ServicePrincipal {
        id: row.try_get("id")?,
        rid: row.try_get("rid")?,
        display_name: row.try_get("display_name")?,
        project_scope_rids: row.try_get("project_scope_rids")?,
        clearances: row.try_get("clearances")?,
        created_by: row.try_get("created_by")?,
        created_at: row.try_get("created_at")?,
        revoked_at: row.try_get("revoked_at")?,
    })
}
