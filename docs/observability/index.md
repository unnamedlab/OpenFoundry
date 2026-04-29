# Observability

This section covers runtime visibility, health, auditability, and operational diagnosis.

## OpenFoundry mapping

- `/health` conventions across services
- `services/audit-service`
- tracing dependencies in the Rust workspace
- smoke and benchmark suites
- runbooks under `infra/runbooks`

## Key concerns

- health and readiness
- logs, traces, and metrics
- runtime smoke validation
- incident diagnosis and recovery support

## Monitoring stack status

The `infra/docker-compose.monitoring.yml` stub previously referenced from
ADR-0012 was empty and has been removed to avoid giving a false signal of an
existing monitoring stack. The real Prometheus / Grafana / Loki / Tempo stack
described in ADR-0012 will be reintroduced as part of the formal
observability work (T17).
