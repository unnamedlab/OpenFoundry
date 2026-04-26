# Function package lifecycle

Function packages already behave like managed semantic artifacts in the ontology backend.

## Repository signals

`ontology-service` exposes:

- function package CRUD
- validation
- simulation

These endpoints are defined in `services/ontology-service/src/main.rs` and implemented through `handlers/functions.rs`.

## Lifecycle

1. create a function package
2. update its metadata and code-linked references
3. validate the package
4. simulate execution
5. attach or consume it from applications, rules, or semantic workflows
