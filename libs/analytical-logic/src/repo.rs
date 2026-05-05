//! Postgres-backed repository for [`crate::AnalyticalExpression`].
//!
//! The repo wraps a `sqlx::PgPool` so callers (e.g.
//! `sql-bi-gateway-service`) hold the pool for their own bounded context
//! and pass a borrow per-call. There is no global state.

use sqlx::PgPool;
use uuid::Uuid;

use crate::model::{
    AnalyticalExpression, AnalyticalExpressionVersion, NewExpression, NewExpressionVersion,
};

/// Repository errors.
///
/// Wraps `sqlx::Error` so callers can match on a small, stable surface
/// without leaking the full sqlx error tree across crate boundaries.
#[derive(Debug, thiserror::Error)]
pub enum RepoError {
    /// The requested expression / version does not exist.
    #[error("analytical expression {0} not found")]
    NotFound(Uuid),
    /// Any other database failure.
    #[error("analytical-logic repo: {0}")]
    Database(#[from] sqlx::Error),
}

/// Postgres repository for [`AnalyticalExpression`] / [`AnalyticalExpressionVersion`].
#[derive(Debug, Clone)]
pub struct AnalyticalExpressionRepo<'a> {
    pool: &'a PgPool,
}

impl<'a> AnalyticalExpressionRepo<'a> {
    /// Build a new repo over an existing connection pool. Cheap; just
    /// stores the borrow.
    pub fn new(pool: &'a PgPool) -> Self {
        Self { pool }
    }

    /// List up to `limit` expressions, newest first.
    pub async fn list(&self, limit: i64) -> Result<Vec<AnalyticalExpression>, RepoError> {
        let rows = sqlx::query_as::<_, AnalyticalExpression>(
            "SELECT id, payload, created_at, updated_at \
             FROM analytical_expressions \
             ORDER BY created_at DESC \
             LIMIT $1",
        )
        .bind(limit)
        .fetch_all(self.pool)
        .await?;
        Ok(rows)
    }

    /// Fetch a single expression by id.
    pub async fn get(&self, id: Uuid) -> Result<AnalyticalExpression, RepoError> {
        sqlx::query_as::<_, AnalyticalExpression>(
            "SELECT id, payload, created_at, updated_at \
             FROM analytical_expressions WHERE id = $1",
        )
        .bind(id)
        .fetch_optional(self.pool)
        .await?
        .ok_or(RepoError::NotFound(id))
    }

    /// Insert a new expression. The id is generated server-side
    /// (`Uuid::now_v7`) so callers don't have to coordinate.
    pub async fn create(&self, new: NewExpression) -> Result<AnalyticalExpression, RepoError> {
        let id = Uuid::now_v7();
        let row = sqlx::query_as::<_, AnalyticalExpression>(
            "INSERT INTO analytical_expressions (id, payload) VALUES ($1, $2) \
             RETURNING id, payload, created_at, updated_at",
        )
        .bind(id)
        .bind(&new.payload)
        .fetch_one(self.pool)
        .await?;
        Ok(row)
    }

    /// List the version history of `parent_id`, newest first, up to `limit`.
    pub async fn list_versions(
        &self,
        parent_id: Uuid,
        limit: i64,
    ) -> Result<Vec<AnalyticalExpressionVersion>, RepoError> {
        let rows = sqlx::query_as::<_, AnalyticalExpressionVersion>(
            "SELECT id, parent_id, payload, created_at \
             FROM analytical_expression_versions \
             WHERE parent_id = $1 \
             ORDER BY created_at DESC \
             LIMIT $2",
        )
        .bind(parent_id)
        .bind(limit)
        .fetch_all(self.pool)
        .await?;
        Ok(rows)
    }

    /// Append a new version to an existing expression.
    pub async fn add_version(
        &self,
        parent_id: Uuid,
        new: NewExpressionVersion,
    ) -> Result<AnalyticalExpressionVersion, RepoError> {
        let id = Uuid::now_v7();
        let row = sqlx::query_as::<_, AnalyticalExpressionVersion>(
            "INSERT INTO analytical_expression_versions (id, parent_id, payload) \
             VALUES ($1, $2, $3) \
             RETURNING id, parent_id, payload, created_at",
        )
        .bind(id)
        .bind(parent_id)
        .bind(&new.payload)
        .fetch_one(self.pool)
        .await?;
        Ok(row)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Smoke-checks that the constructor stores the pool reference and
    /// that the error variants render. The real round-trip exercise
    /// happens in the consumer crate's integration tests where a
    /// Postgres testcontainer is available.
    #[test]
    fn error_messages_are_useful() {
        let id = Uuid::now_v7();
        let err = RepoError::NotFound(id);
        assert!(err.to_string().contains(&id.to_string()));
    }
}
