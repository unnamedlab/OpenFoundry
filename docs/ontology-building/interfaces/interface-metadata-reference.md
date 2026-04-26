# Interface metadata reference

The current interface model is intentionally simple but already useful.

## Interface fields

| Field | Meaning |
| --- | --- |
| `name` | stable interface key |
| `display_name` | human-facing label |
| `description` | semantic description |
| `owner_id` | owning identity |
| `created_at` | creation timestamp |
| `updated_at` | update timestamp |

## Interface property fields

Interface properties currently mirror many object-property concepts:

- `name`
- `display_name`
- `description`
- `property_type`
- `required`
- `unique_constraint`
- `time_dependent`
- `default_value`
- `validation_rules`

## Why this matters

That symmetry is a good sign: it means interfaces are not second-class annotations, but reusable semantic envelopes.
