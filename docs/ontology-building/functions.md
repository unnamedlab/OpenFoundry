# Functions

Functions are the programmable extension surface of the ontology.

## What belongs here

This area typically includes:

- code-backed operations on objects and interfaces
- query and aggregation helpers
- side effects and API integrations
- runtime permissions
- monitoring and versioning
- language-specific authoring experiences

## OpenFoundry mapping

The current repo suggests a multi-service function story across:

- `services/ai-service`
- `services/ml-service`
- `services/query-service`
- `services/workflow-service`
- `services/notebook-service`
- `tools/of-cli`

## Cross-cutting concerns

- generated SDKs
- API gateway exposure
- auditability
- performance and runtime limits
