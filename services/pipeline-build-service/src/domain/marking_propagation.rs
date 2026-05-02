//! T3.4 — Propagation of markings from pipeline inputs to outputs.
//!
//! When a pipeline node finishes, every marking present on any of its
//! input datasets must be inherited by the output dataset, recorded as
//! `source = 'inherited_from_upstream'` with `inherited_from = <input_rid>`.
//! The Datasets / Lineage docs require this so downstream consumers
//! cannot escape an upstream classification (e.g. `RESTRICTED`) just
//! by re-deriving the data through a transform.
//!
//! ## Design
//!
//! The actual pipeline executor lives in `engine::dag_executor` and
//! `engine::runtime`; both files are large and currently have no
//! single explicit "commit output" hook. To stay surgical, this module
//! exposes a *pure* helper [`propagate_markings_to_output`] that the
//! executor (or a future `commit_output` extraction) can call once it
//! knows `(output_rid, &[input_rids])`. Wiring it into the actual
//! commit path is a follow-up to T3.4 and tracked separately so this
//! step can land as a tested, idempotent SQL utility today.
//!
//! Idempotency is delegated to the `dataset_markings` unique index
//! (`(dataset_rid, marking_id, COALESCE(inherited_from, ''))`):
//! re-running a build on the same inputs is a no-op.

use sqlx::PgPool;

/// Insert `inherited_from_upstream` rows into `dataset_markings` for
/// `output_rid` covering every marking that exists on any of the
/// `input_rids`. Returns the number of rows actually inserted (rows
/// already present are silently skipped via `ON CONFLICT DO NOTHING`).
///
/// The query reads the *direct* markings of each input plus the
/// closure they themselves inherited; the closure is captured by
/// taking every row whose `dataset_rid IN (input_rids)` regardless of
/// its `source`, which is correct because:
///
///   * direct rows on an input represent labels the input carries;
///   * inherited rows on an input represent labels its own upstream
///     contributed and that the input is currently bound to.
///
/// Both must propagate downstream — otherwise a 3-hop lineage chain
/// (A → B → C) would silently drop A's `RESTRICTED` at C.
pub async fn propagate_markings_to_output(
    db: &PgPool,
    output_rid: &str,
    input_rids: &[String],
) -> Result<u64, sqlx::Error> {
    if input_rids.is_empty() {
        return Ok(0);
    }

    // For each input we insert a separate set of rows so
    // `inherited_from` correctly identifies the immediate hop.
    let mut total = 0u64;
    let mut tx = db.begin().await?;
    for input_rid in input_rids {
        let res = sqlx::query(
            r#"INSERT INTO dataset_markings
                   (dataset_rid, marking_id, source, inherited_from)
               SELECT DISTINCT $1, marking_id, 'inherited_from_upstream', $2
                 FROM dataset_markings
                WHERE dataset_rid = $2
               ON CONFLICT DO NOTHING"#,
        )
        .bind(output_rid)
        .bind(input_rid)
        .execute(&mut *tx)
        .await?;
        total += res.rows_affected();
    }
    tx.commit().await?;
    Ok(total)
}

#[cfg(test)]
mod tests {
    //! No DB-touching tests live here: integration coverage that
    //! actually exercises the SQL belongs in
    //! `services/pipeline-build-service/tests/marking_propagation.rs`
    //! (gated by `sqlx::test`). The module is included so this file's
    //! visibility surface matches the rest of the `domain` tree.

    #[test]
    fn module_compiles() {
        // Sentinel: keeps the cfg(test) section warning-free without
        // forcing a DB-bound test in the unit suite.
    }
}
