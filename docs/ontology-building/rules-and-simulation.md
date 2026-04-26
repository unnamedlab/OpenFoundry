# Rules and simulation

Rules and simulation are where the ontology starts behaving like an operational decision system rather than a passive metadata layer.

## Repository signals

The current `ontology-service` already exposes a surprisingly rich surface here:

- `/api/v1/ontology/rules`
- `/api/v1/ontology/rules/{id}/simulate`
- `/api/v1/ontology/rules/{id}/apply`
- `/api/v1/ontology/types/{type_id}/rules`
- `/api/v1/ontology/objects/{obj_id}/rule-runs`
- machinery queue and insights endpoints

The route wiring lives in `services/ontology-service/src/main.rs`, and the domain/handler split is visible in:

- `services/ontology-service/src/domain/rules.rs`
- `services/ontology-service/src/handlers/rules.rs`

## Why this matters

Rules sit at the boundary between:

- semantic state
- operational policy
- workflow automation
- human decisions

Simulation is what makes those rules safe to evolve. It lets teams test the effect of a rule before applying it to live operational objects.

## Design implications

This area is a natural convergence point for:

- ontology modeling
- workflow orchestration
- notifications
- auditability
- AI-assisted recommendations

## Section map

- [Rule lifecycle](/ontology-building/rules/rule-lifecycle)
- [Simulation and what-if analysis](/ontology-building/rules/simulation-and-what-if-analysis)
- [P3 governance flow](/ontology-building/rules/p3-governance-flow)
