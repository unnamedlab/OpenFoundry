use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::{
    AppState,
    models::{
        IngestPointsRequest, QueryTimeSeriesRequest, RegisterTimeSeriesRequest, TimeSeries,
        TimeSeriesPoint, TimeSeriesStoragePartition,
    },
};

fn db_error(label: &str, error: sqlx::Error) -> axum::response::Response {
    tracing::error!("time-series-data-service {label} failed: {error}");
    StatusCode::INTERNAL_SERVER_ERROR.into_response()
}

pub async fn list_series(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, TimeSeries>(
        "SELECT id, slug, display_name, value_kind, unit, granularity_seconds, retention_days,
                source_kind, source_ref, tags, created_at, updated_at
         FROM time_series
         ORDER BY slug",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(json!({ "data": rows })).into_response(),
        Err(error) => db_error("list_series", error),
    }
}

pub async fn register_series(
    State(state): State<AppState>,
    Json(body): Json<RegisterTimeSeriesRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let tags = if body.tags.is_null() {
        json!({})
    } else {
        body.tags
    };

    match sqlx::query_as::<_, TimeSeries>(
        "INSERT INTO time_series (id, slug, display_name, value_kind, unit, granularity_seconds,
                                  retention_days, source_kind, source_ref, tags)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
         ON CONFLICT (slug) DO UPDATE
         SET display_name = EXCLUDED.display_name,
             value_kind = EXCLUDED.value_kind,
             unit = EXCLUDED.unit,
             granularity_seconds = EXCLUDED.granularity_seconds,
             retention_days = EXCLUDED.retention_days,
             source_kind = EXCLUDED.source_kind,
             source_ref = EXCLUDED.source_ref,
             tags = EXCLUDED.tags,
             updated_at = NOW()
         RETURNING id, slug, display_name, value_kind, unit, granularity_seconds, retention_days,
                   source_kind, source_ref, tags, created_at, updated_at",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(&body.display_name)
    .bind(&body.value_kind)
    .bind(body.unit.as_deref())
    .bind(body.granularity_seconds)
    .bind(body.retention_days)
    .bind(body.source_kind.as_deref())
    .bind(body.source_ref.as_deref())
    .bind(tags)
    .fetch_one(&state.db)
    .await
    {
        Ok(series) => (StatusCode::CREATED, Json(series)).into_response(),
        Err(error) => db_error("register_series", error),
    }
}

pub async fn get_series(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, TimeSeries>(
        "SELECT id, slug, display_name, value_kind, unit, granularity_seconds, retention_days,
                source_kind, source_ref, tags, created_at, updated_at
         FROM time_series WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(series)) => Json(series).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => db_error("get_series", error),
    }
}

pub async fn list_points(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, TimeSeriesPoint>(
        "SELECT series_id, recorded_at, value_numeric, value_text, attributes
         FROM time_series_points
         WHERE series_id = $1
         ORDER BY recorded_at DESC
         LIMIT 1000",
    )
    .bind(id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(json!({ "data": rows })).into_response(),
        Err(error) => db_error("list_points", error),
    }
}

pub async fn ingest_points(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<IngestPointsRequest>,
) -> impl IntoResponse {
    let mut inserted: i64 = 0;
    for point in body.points {
        let attrs = if point.attributes.is_null() {
            json!({})
        } else {
            point.attributes
        };
        match sqlx::query(
            "INSERT INTO time_series_points (series_id, recorded_at, value_numeric, value_text, attributes)
             VALUES ($1, $2, $3, $4, $5::jsonb)
             ON CONFLICT (series_id, recorded_at) DO UPDATE
             SET value_numeric = EXCLUDED.value_numeric,
                 value_text = EXCLUDED.value_text,
                 attributes = EXCLUDED.attributes",
        )
        .bind(id)
        .bind(point.recorded_at)
        .bind(point.value_numeric)
        .bind(point.value_text.as_deref())
        .bind(attrs)
        .execute(&state.db)
        .await
        {
            Ok(_) => inserted += 1,
            Err(error) => return db_error("ingest_points", error),
        }
    }
    (StatusCode::ACCEPTED, Json(json!({ "ingested": inserted }))).into_response()
}

pub async fn query_series(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<QueryTimeSeriesRequest>,
) -> impl IntoResponse {
    if body.aggregation == "raw" {
        let limit = body.limit.unwrap_or(1000).clamp(1, 100_000);
        return match sqlx::query_as::<_, TimeSeriesPoint>(
            "SELECT series_id, recorded_at, value_numeric, value_text, attributes
             FROM time_series_points
             WHERE series_id = $1 AND recorded_at >= $2 AND recorded_at < $3
             ORDER BY recorded_at ASC
             LIMIT $4",
        )
        .bind(id)
        .bind(body.from)
        .bind(body.to)
        .bind(limit)
        .fetch_all(&state.db)
        .await
        {
            Ok(rows) => Json(json!({ "aggregation": "raw", "data": rows })).into_response(),
            Err(error) => db_error("query_series_raw", error),
        };
    }

    let bucket = body.bucket_seconds.unwrap_or(60).max(1);
    let agg_sql = match body.aggregation.as_str() {
        "mean" => "AVG(value_numeric)",
        "min" => "MIN(value_numeric)",
        "max" => "MAX(value_numeric)",
        "sum" => "SUM(value_numeric)",
        "count" => "COUNT(value_numeric)::DOUBLE PRECISION",
        _ => return StatusCode::BAD_REQUEST.into_response(),
    };

    let sql = format!(
        "SELECT to_timestamp(floor(extract(epoch from recorded_at) / $4) * $4) AS bucket,
                {agg_sql} AS value
         FROM time_series_points
         WHERE series_id = $1 AND recorded_at >= $2 AND recorded_at < $3
         GROUP BY bucket
         ORDER BY bucket ASC"
    );

    match sqlx::query_as::<_, (chrono::DateTime<chrono::Utc>, Option<f64>)>(&sql)
        .bind(id)
        .bind(body.from)
        .bind(body.to)
        .bind(bucket)
        .fetch_all(&state.db)
        .await
    {
        Ok(rows) => {
            let buckets: Vec<_> = rows
                .into_iter()
                .map(|(bucket, value)| json!({ "bucket": bucket, "value": value }))
                .collect();
            Json(json!({
                "aggregation": body.aggregation,
                "bucket_seconds": bucket,
                "data": buckets,
            }))
            .into_response()
        }
        Err(error) => db_error("query_series_aggregated", error),
    }
}

pub async fn list_storage_partitions(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, TimeSeriesStoragePartition>(
        "SELECT id, series_id, tier, partition_start, partition_end, storage_uri, byte_size,
                point_count, created_at
         FROM time_series_storage_partitions
         ORDER BY partition_start DESC
         LIMIT 200",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(json!({ "data": rows })).into_response(),
        Err(error) => db_error("list_storage_partitions", error),
    }
}
