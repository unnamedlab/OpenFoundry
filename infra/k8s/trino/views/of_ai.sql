-- Trino views over Iceberg `of_ai.*` for model-evaluation queries.
-- Substrate per S5.3.c; applied when the Trino chart lands in S5.6.
--
-- Naming convention: `of_ai.v_<purpose>` — every view is read-only,
-- backed by Iceberg, and partition-pruned on `at`.
--
-- These views are intentionally narrow: each one targets a single
-- evaluation question so dashboards do not push joins through Trino's
-- coordinator.

------------------------------------------------------------
-- v_responses_by_run
--   One row per (run_id, response event) — the canonical join axis
--   for "show me what the agent emitted for run X".
------------------------------------------------------------
CREATE OR REPLACE VIEW of_ai.v_responses_by_run AS
SELECT
    run_id,
    event_id,
    at,
    producer,
    schema_version,
    payload
FROM iceberg.of_ai.responses
WHERE run_id IS NOT NULL;

------------------------------------------------------------
-- v_eval_scores_daily
--   Daily aggregate of evaluation scores per (producer, kind of
--   eval). `payload->>'score'` is the universal numeric score field
--   (schema_version=1 contract).
------------------------------------------------------------
CREATE OR REPLACE VIEW of_ai.v_eval_scores_daily AS
SELECT
    DATE(from_unixtime(at / 1000000)) AS day,
    producer,
    json_extract_scalar(payload, '$.metric') AS metric,
    AVG(CAST(json_extract_scalar(payload, '$.score') AS DOUBLE)) AS avg_score,
    COUNT(*) AS n_samples
FROM iceberg.of_ai.evaluations
WHERE json_extract_scalar(payload, '$.score') IS NOT NULL
GROUP BY 1, 2, 3;

------------------------------------------------------------
-- v_prompt_response_pairs
--   Trace-correlated prompt → response pairs, useful for offline
--   evaluation pipelines (prompt-response-judge).
------------------------------------------------------------
CREATE OR REPLACE VIEW of_ai.v_prompt_response_pairs AS
SELECT
    p.run_id,
    p.trace_id,
    p.event_id          AS prompt_event_id,
    p.at                AS prompt_at,
    p.payload           AS prompt_payload,
    r.event_id          AS response_event_id,
    r.at                AS response_at,
    r.payload           AS response_payload,
    (r.at - p.at) / 1000.0 AS latency_ms
FROM iceberg.of_ai.prompts p
JOIN iceberg.of_ai.responses r
  ON r.trace_id = p.trace_id
 AND r.run_id   = p.run_id
WHERE p.trace_id IS NOT NULL;

------------------------------------------------------------
-- v_traces_with_outcome
--   Trace span × terminal evaluation — one row per finished trace.
------------------------------------------------------------
CREATE OR REPLACE VIEW of_ai.v_traces_with_outcome AS
SELECT
    t.trace_id,
    t.run_id,
    MIN(t.at)                                                      AS trace_started_at,
    MAX(t.at)                                                      AS trace_ended_at,
    MAX(json_extract_scalar(e.payload, '$.outcome'))               AS outcome,
    AVG(CAST(json_extract_scalar(e.payload, '$.score') AS DOUBLE)) AS final_score
FROM iceberg.of_ai.traces t
LEFT JOIN iceberg.of_ai.evaluations e
  ON e.run_id = t.run_id
GROUP BY t.trace_id, t.run_id;
