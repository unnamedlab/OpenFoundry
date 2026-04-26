# Property metadata reference

The current property model in OpenFoundry stores more semantic detail than a bare column definition.

## Stored fields

| Field | Meaning |
| --- | --- |
| `name` | stable property key |
| `display_name` | human-readable label |
| `description` | semantic description |
| `property_type` | type of value |
| `required` | mandatory flag |
| `unique_constraint` | uniqueness expectation |
| `time_dependent` | whether the value evolves over time |
| `default_value` | default payload |
| `validation_rules` | validation metadata |

## Why this matters

This metadata set is already rich enough to support:

- application validation
- semantic governance
- time-aware operational logic
- UI and workflow interpretation
