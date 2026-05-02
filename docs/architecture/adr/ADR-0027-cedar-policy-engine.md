# ADR-0027: Cedar policy engine, embedded as a Rust library

- **Status:** Accepted
- **Date:** 2026-05-02
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes / supplements:**
  - The implicit "every service rolls its own authorisation" pattern
    documented in
    [docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md).
- **Related ADRs:**
  - [ADR-0011](./ADR-0011-control-vs-data-bus-contract.md) — control
    plane (NATS JetStream) is the channel for the policy hot-reload
    event.
  - [ADR-0024](./ADR-0024-postgres-consolidation.md) — Cedar policy
    documents live in `pg-policy.cedar_policies`.
  - [ADR-0026](./ADR-0026-identity-custom-retained.md) — token claims
    feed the Cedar `Principal` entity.

## Context

OpenFoundry needs an authorisation model that can express:

- **Object-level** access (read, write, branch) on ontology objects,
  datasets, files, models.
- **Marking-based** propagation (markings inherited along the
  ontology graph; access requires holding every marking applied to
  the object).
- **Branch-aware** access (a user with read on `main` may have write
  on a personal branch).
- **Project / organisation** boundaries.
- **Policy authoring by humans** (security and platform admins) with
  a typed schema.

The current state is "every service rolls its own check" against
hard-coded rules and ad-hoc table joins. Outcome: drift across
services, no auditable policy text, no central place to validate
"can user U perform action A on resource R", no tests for negative
cases.

We need to choose an authorisation engine, a deployment model and a
storage model for policies.

## Options considered

### Option A — Cedar embedded as a Rust library (chosen)

- **Cedar** is the open-source policy language and engine published
  by AWS (Apache-2.0), with a first-class Rust crate
  (`cedar-policy = "4"`). The same engine powers Amazon Verified
  Permissions in production.
- The engine is **a library**, not a service: each Rust service
  evaluates Cedar policies in-process against an in-memory
  `PolicySet` and an in-memory entity store assembled from the
  request context.
- Policy text and schema are version-controlled in
  `pg-policy.cedar_policies`; services load them at startup and
  hot-reload on a NATS event
  ([ADR-0011](./ADR-0011-control-vs-data-bus-contract.md)).
- Schema is **derived from `core-models`**: `User`, `Object`,
  `Branch`, `Marking`, `Project`, `Action` — the same domain types
  the platform uses everywhere else. A single schema source feeds
  Cedar's typed policies and the Rust types used by callers.
- Latency: in-process evaluation is sub-millisecond and removes any
  network hop from the authorisation path.

### Option B — OpenFGA (rejected)

- OpenFGA is a relationship-based authorisation service (Zanzibar-
  style), CNCF-incubating, well documented. The model is powerful
  for tuple-based relationships.
- Deal-breakers in our context:
  - **Separate HA service** to operate, with its own datastore (a
    Postgres of its own, or a Cassandra instance of its own — neither
    of which we want).
  - **Network hop on every check**: the platform issues thousands of
    authorisation checks per request (every object touched, every
    link traversed). A network hop per check is incompatible with
    the hot-path SLO.
  - **Tuple model is awkward for marking propagation**: the
    "user has marking M and resource has marking M" check is more
    naturally expressed in Cedar's predicate-on-attribute model
    than in a tuple.
  - **Caching tuples client-side reintroduces** the same coherency
    problem we are trying to solve, with worse failure modes.
- The right time for OpenFGA is when the dominant query is
  "list all resources U can access" across millions of resources
  (Zanzibar's strong suit). Our dominant query is the inverse:
  "can U do A on R?" — Cedar's strong suit.

### Option C — OPA / Rego (rejected)

- OPA is the de-facto standard for policy as code in CNCF land. The
  Rego language is expressive and has a large ecosystem.
- Embedding OPA in Rust requires either:
  - Calling out to a sidecar (network hop per decision), or
  - Embedding OPA-WASM (works, adds a WASM runtime to every service,
    Rego compilation cycles to manage, and a different debugging
    surface than the rest of our Rust code).
- Rego is **untyped**; policy authors get errors at evaluation, not
  authoring. Cedar is typed against a schema and rejects ill-typed
  policies at upload.
- Rego is harder to reason about formally than Cedar; Cedar has a
  formal semantics and a published validator.

### Option D — In-house authorisation per service (status quo, rejected)

- Documented above.

## Decision

We adopt **Option A**: **Cedar (the AWS open-source policy language
and engine) is embedded as a Rust library** (`cedar-policy = "4"`)
in every service that needs to make an authorisation decision.

Policy documents and the Cedar schema live in
**`pg-policy.cedar_policies`** ([ADR-0024](./ADR-0024-postgres-consolidation.md)).
A central `policy-decision-service` is the **writer** (validates,
versions, publishes). Every other service is a **reader** that loads
the policy set at startup and hot-reloads on the NATS event
`authz.policy.changed`.

OpenFGA (Option B), OPA (Option C) and per-service in-house code
(Option D) are explicitly rejected for the reasons above.

## Schema (derived from `core-models`)

```
entity Principal in [Group] {
    department: String,
    organization: String,
    markings: Set<Marking>,
};

entity Group in [Group] {};

entity Project {
    organization: String,
};

entity Branch in [Project] {
    name: String,
    owner: Principal,
};

entity Marking {
    classification: String,
};

entity ObjectType {
    name: String,
};

entity Object in [Branch, Project] {
    object_type: ObjectType,
    markings: Set<Marking>,
    owner: Principal,
};

entity Dataset in [Project] {
    markings: Set<Marking>,
};

entity Action in [] {};

action Read appliesTo {
    principal: Principal,
    resource: [Object, Dataset, Branch],
    context: { request_id?: String }
};

action Write appliesTo {
    principal: Principal,
    resource: [Object, Dataset, Branch],
    context: { request_id?: String }
};

action BranchCreate appliesTo {
    principal: Principal,
    resource: Project,
};
```

The Rust types in `libs/core-models` carry `From` / `Into`
implementations for each Cedar entity, so services build the entity
store from their already-loaded domain objects without any manual
mapping.

## Storage and distribution

### Storage

`pg-policy.cedar_policies`:

```sql
CREATE TABLE cedar_policies (
    policy_id      text        PRIMARY KEY,
    version        bigint      NOT NULL,
    description    text        NOT NULL,
    effect         text        NOT NULL CHECK (effect IN ('permit','forbid')),
    source         text        NOT NULL,           -- raw Cedar text
    authored_by    text        NOT NULL,
    authored_at    timestamptz NOT NULL DEFAULT now(),
    active         boolean     NOT NULL DEFAULT true
);

CREATE TABLE cedar_schema (
    version        bigint      PRIMARY KEY,
    source         text        NOT NULL,           -- raw Cedar schema
    authored_by    text        NOT NULL,
    authored_at    timestamptz NOT NULL DEFAULT now()
);
```

- The **writer** is `policy-decision-service`. Writes go through
  Cedar's validator: ill-typed policies are rejected before insert.
- Each successful write enqueues an outbox event
  `authz.policy.changed` ([ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md))
  carrying `(version, changed_policy_ids)`.

### Distribution

- Debezium publishes `authz.policy.changed` to Kafka.
- A bridge in `libs/auth-middleware` re-publishes the event onto
  NATS JetStream control plane (per the control-vs-data
  separation in [ADR-0011](./ADR-0011-control-vs-data-bus-contract.md))
  so that every service can subscribe via the cheap control bus
  and avoid Kafka client overhead in every binary.
- Each service holds:
  - `Arc<RwLock<PolicySet>>` — the active policy set.
  - `Arc<RwLock<Schema>>` — the active Cedar schema.
- Hot-reload handler:
  1. Receives `authz.policy.changed` on NATS.
  2. Reads the new `PolicySet` from `pg-policy.cedar_policies`
     `WHERE active = true`.
  3. Validates against the current schema.
  4. Atomically swaps under the `RwLock`.
- A startup retry loop keeps the service usable if Postgres is briefly
  unavailable: it boots with the **last known-good cached policy set**
  on a local PVC and warns until it can reach Postgres.

### Evaluation

A request handler asks `auth_middleware::authorize(request)`:

```rust
let decision = engine.is_authorized(
    &authz_request,
    policy_set.read().await.deref(),
    entities.read().await.deref(),
);
match decision.decision() {
    Decision::Allow => Ok(()),
    Decision::Deny  => Err(Forbidden { reasons: decision.diagnostics().reason().collect() }),
}
```

Every decision emits a structured audit log line with:

- `principal_id`, `action`, `resource_id`, `decision`, `policy_ids`
  that contributed, `request_id`, `latency_us`.
- Sampled to the audit pipeline (every Deny + 1% of Allows by
  default; configurable per service).

## Operational consequences

- New workspace crate `libs/authz-cedar` providing:
  - `PolicyStore` (loader + hot-reload subscriber).
  - `EntitiesBuilder` (composes Cedar entities from `core-models`).
  - `authorize` helper.
- New service `policy-decision-service` whose only writes are to
  `pg-policy.cedar_policies` and whose only reads are from the same
  table; it does not act as a runtime decision point (every service
  is its own decision point).
- New runbook `infra/runbooks/cedar-policy.md` covering:
  - Authoring a new policy and pushing it through validation.
  - Emergency rollback to a previous version.
  - Hot-reload failure (a service falls back to the last cached
    policy set; alarm triggers; recovery is "fix the policy or
    fix Postgres").
- New CI check: every Cedar policy in `pg-policy.cedar_policies`
  validates against the current `cedar_schema` row in CI; any change
  to the schema runs the validator against every active policy.
- New test pattern: each service that authorizes ships
  authorization unit tests using Cedar's `cedar-policy-validator`
  with fixture entities, covering both Allow and Deny paths.

## Consequences

### Positive

- **Sub-millisecond authorisation** with no network hop.
- **Typed policy language** with a formal semantics; ill-typed
  policies are caught at upload.
- **Single source of truth** for authorisation logic across every
  service.
- **No new HA service** to operate. Cedar is a library; the only
  service involved is `policy-decision-service` for write paths,
  and its unavailability does not block any reader (readers run on
  cached policies).
- **Direct alignment with `core-models`** keeps the entity model and
  the domain model in lock-step.

### Negative

- Every reader holds the full active policy set in memory. At our
  expected policy volume (hundreds, not millions) this is
  negligible; if it ever becomes large, Cedar supports policy
  templates that compress repetitive structure.
- Hot-reload requires the NATS bridge from Kafka. A failure of
  either bus delays propagation, but does not affect availability:
  readers serve on the previously loaded set.

### Neutral

- Policy authoring is a security-team workflow, not a developer
  workflow. The `policy-decision-service` admin UI is the authoring
  surface; PRs against `core-models` keep the entity model in sync.

## Follow-ups

- Implement migration plan tasks **S0.7** (Cedar integration) and
  **S0.1.h** ADR (this document).
- Author `libs/authz-cedar`.
- Stand up `policy-decision-service` (writer + admin UI).
- Translate the existing per-service authorisation rules into Cedar
  policies, one service at a time, gated by a passing test suite
  per service.
- Add the CI check that validates every active policy against the
  current schema.
- Author `infra/runbooks/cedar-policy.md`.
