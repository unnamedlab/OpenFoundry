# Object type anti-patterns

Several common mistakes can make an ontology brittle very quickly.

## Anti-patterns

| Anti-pattern | Why it is harmful | Better alternative |
| --- | --- | --- |
| one object type per source table | mirrors ingestion layout, not business meaning | consolidate around operational entities |
| unstable names | breaks long-term reuse and contracts | treat `name` as a durable semantic key |
| missing ownership semantics | no one governs evolution | use clear owner and team stewardship |
| UI-first modeling | objects optimized for screens, not semantics | model domain first, then adapt presentation |
| embedding relationships in strings | blocks graph and link-aware behavior | use link types or supporting objects |

## OpenFoundry current vs target

| Topic | Current risk | Target posture |
| --- | --- | --- |
| naming discipline | depends on author behavior | explicit naming conventions and linting |
| lifecycle governance | partially manual | proposal and review-backed changes |
| semantic reuse | early-stage | strong interface and shared-property patterns |
