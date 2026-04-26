# Property categories

Not all properties play the same role. OpenFoundry should distinguish between several categories explicitly.

## Useful categories

| Category | Purpose | Example |
| --- | --- | --- |
| identity | identifies or keys the object | case id, asset id |
| state | describes current operational status | open, approved, delayed |
| metrics | numeric or measurable values | owner count, risk score |
| governance | marking, org scope, review metadata | clearance, org id |
| display | UI-facing presentation hints | label, formatting hints |

## Current repository signals

The P3 smoke scenario already exercises state and metric-like properties such as:

- `status`
- `decision`
- `owner_count`
- `peer_case_count`

## OpenFoundry current vs target

| Topic | Current | Target |
| --- | --- | --- |
| property CRUD | implemented in `ontology-service` | richer UI and metadata vocabulary |
| formatting | implied but not deeply documented | first-class design system for semantic rendering |
| governance fields | present through markings and auth context | explicit property-level governance model |
