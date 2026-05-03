# S5 — Lakehouse Operational Evidence

> Owner: data platform maintainers + SRE on-call.
> Gate: this evidence pack must be executed and signed off before
> Stream S5 closes. Passing `G-S5` only proves that the checked runtime
> tree has no explicit stubs; it does not prove that Kafka, Iceberg,
> Trino, Spark and WORM controls work under real traffic.

## Objectives

1. **Append path.** Prove that `audit-sink`, `lineage-service` and
   `ai-sink` consume real Kafka events and append them to Iceberg
   tables.
2. **Query path.** Prove that `sql-bi-gateway-service` routes at least
   one analytical query to Trino and returns rows from Iceberg.
3. **WORM guardrail.** Prove that no maintenance job can rewrite or
   expire snapshots for `of_audit` / `of.audit`.
4. **Lag and recovery.** Prove that sinks recover after a restart
   without duplicate-authoritative state and keep lag within the SLO.

## Required Evidence Pack

Create one directory per run:

```text
docs/architecture/lakehouse-evidence/<YYYY-MM-DD>/
```

It must contain:

- `summary.md` with operator, environment, git SHA, start/end time and
  PASS/FAIL.
- `kafka-offsets.txt` from all sink consumer groups before and after
  the run.
- `iceberg-counts.sql.txt` with Trino queries and row counts for
  `of_audit.events`, `of_lineage.events`, `of_ai.responses` and
  `of_metrics_long.service_metrics_daily` when present.
- `worm-negative-test.txt` showing that `expire_snapshots` or rewrite
  against `of_audit` is denied or rejected by policy.
- `grafana-snapshots.md` with links or exported panel images for sink
  lag, records consumed, commits and errors.
- `restart-drill.txt` showing one restart of each sink/indexer and
  successful catch-up.

## Procedure

1. Record the git SHA and deployed image digests for all S5 services.
2. Capture Kafka offsets for `audit-sink`, `lineage-service`,
   `ai-sink`, `ontology-indexer` and `reindex`.
3. Produce synthetic-but-real events through the same ingress used by
   the platform, not by directly writing Iceberg files.
4. Wait until sink lag returns to steady state.
5. Query Iceberg through Trino and save row counts plus a sample event
   id for each table.
6. Restart each sink/indexer deployment once. Confirm offsets advance
   and no table receives duplicate primary event ids.
7. Run the WORM negative test against audit:
   - Attempted Spark maintenance allowlist must reject `of_audit`.
   - Any direct `expire_snapshots` attempt against `of_audit.events`
     must fail authorization or be blocked by policy.
8. Attach Grafana snapshots for lag and commit counters.

## Pass Criteria

- Every checked consumer group advances offsets during the test.
- Trino returns rows for the generated audit, lineage and AI events.
- Sink/indexer lag returns below the documented SLO after restart.
- `of_audit` WORM negative test fails closed.
- No sink uses Postgres as a runtime fallback for event progress,
  retries or checkpoint authority.
- Two maintainers sign off the run.

## Sign-off

- [ ] Data platform maintainer: ___
- [ ] SRE on-call: ___
- [ ] Date: ___
- [ ] Environment: ___
- [ ] Outcome: PASS / FAIL — ___

> Failures block S5 closure. Re-run after remediation; even on PASS,
> S5 remains open until the final checklist in
> `migration-plan-cassandra-foundry-parity.md` is green.
