# Language and runtime comparison

OpenFoundry already hints at a multi-runtime function future.

## Current runtime matrix

| Runtime | Current repository signal | Likely role |
| --- | --- | --- |
| Rust native | service logic across the platform | control-plane and high-performance backend flows |
| Node runtime | `node_runtime_command` in ontology config | package simulation, JS/TS-oriented function execution |
| Python-linked | `pyo3` in `ontology-service` | Python-assisted semantic logic and model-friendly operations |

## OpenFoundry current vs target

| Dimension | Current | Target |
| --- | --- | --- |
| packaging | backend-centric | explicit function package product model |
| language support | implicit | documented runtime contracts |
| permissions | JWT and service-level | fine-grained function execution policies |
