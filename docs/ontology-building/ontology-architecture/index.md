# Ontology architecture

The OpenFoundry ontology should act as the operational layer of the platform.

It sits on top of integrated data, generated contracts, application workflows, and model-driven behavior, and connects those technical assets to the real-world entities that matter to operators: assets, incidents, work orders, policies, suppliers, reports, facilities, customers, or cases.

## Functional components and architecture

Inspired by the way mature ontology platforms separate responsibilities, the OpenFoundry backend already suggests six cooperating concerns.

### 1. Ontology metadata service

`services/ontology-service` is the clearest semantic control-plane component in the current repo.

Its role is to define and evolve:

- object types
- link types
- semantic properties
- interfaces and action-adjacent structures

This is the closest analogue to a metadata authority for the ontology.

### 2. Datasource and indexing path

OpenFoundry does not currently expose a single component literally named "funnel", but the write path is already distributed across:

- `services/data-connector`
- `services/dataset-service`
- `services/pipeline-service`
- `services/streaming-service`

Together, these services form the ingestion and transformation path that can feed ontology-backed objects from batch and streaming sources.

### 3. Query and object access layer

Reads against ontology-backed concepts would naturally span:

- `services/query-service`
- `services/ontology-service`
- `services/geospatial-service`
- `services/gateway`

These services together provide search, filtering, querying, and user-facing access patterns over operational entities.

### 4. Mutation and workflow layer

Controlled writes and decisions are distributed across:

- `services/ontology-service`
- `services/workflow-service`
- `services/notification-service`
- `services/audit-service`

This is where actions, approvals, notifications, and change history come together.

### 5. Security and governance layer

Semantic operations are not useful without permissions and traceability. In OpenFoundry, this layer maps primarily to:

- `services/auth-service`
- `services/audit-service`
- gateway middleware

This is the foundation for object-aware access control, reviewability, and compliance.

### 6. Function and intelligence layer

Code-backed and model-backed ontology behavior is implied by:

- `services/ai-service`
- `services/ml-service`
- `services/notebook-service`
- `tools/of-cli`

This is the layer that can make ontology objects programmable, automatable, and AI-aware.

## OpenFoundry ontology backend model

At a high level, the repository already supports a layered ontology architecture:

```text
Sources and events
  -> connector, dataset, pipeline, streaming
  -> ontology definitions and indexed operational entities
  -> query, workflow, geospatial, reporting, and app surfaces
  -> governed actions, audit, and AI-assisted decision flows
```

## Why this matters

The ontology is the part of the platform that can make OpenFoundry feel like a coherent operational system instead of a collection of independent services.

It becomes the shared semantic backbone for:

- applications
- search
- analytics
- workflows
- actions
- AI and model-assisted operations
