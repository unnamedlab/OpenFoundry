use uuid::Uuid;

use crate::{AppState, models::transaction::DatasetTransaction};

use super::runtime::DatasetSourceError;

const DATASET_TRANSACTIONS_PROJECTION_SQL: &str = r#"
    SELECT * FROM dataset_transactions
    WHERE dataset_id = $1
    ORDER BY created_at DESC
"#;

pub async fn list_dataset_transactions(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Vec<DatasetTransaction>, DatasetSourceError> {
    sqlx::query_as::<_, DatasetTransaction>(DATASET_TRANSACTIONS_PROJECTION_SQL)
        .bind(dataset_id)
        .fetch_all(&state.db)
        .await
        .map_err(|error| DatasetSourceError::Database(error.to_string()))
}

#[cfg(test)]
mod tests {
    use super::DATASET_TRANSACTIONS_PROJECTION_SQL;

    #[test]
    fn transaction_projection_query_is_read_only() {
        let upper = DATASET_TRANSACTIONS_PROJECTION_SQL.to_ascii_uppercase();

        assert!(upper.contains("SELECT * FROM DATASET_TRANSACTIONS"));
        assert!(!upper.contains(" INSERT "));
        assert!(!upper.contains(" UPDATE "));
        assert!(!upper.contains(" DELETE "));
    }
}
