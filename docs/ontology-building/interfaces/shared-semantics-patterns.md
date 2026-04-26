# Shared semantics patterns

Interfaces are most useful when they encode recurring operational patterns.

## Candidate interface patterns

- reviewable
- geo-locatable
- approvable
- schedulable
- reportable
- lineage-aware

## Why this matters

These patterns reduce duplication across domains while keeping applications aligned on the same semantic expectations.

## Repository hint

The P3 smoke scenario already uses a `reviewable`-style interface pattern for cases, which is a strong sign that interface-centered design is viable in OpenFoundry.
