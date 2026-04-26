# Object type metadata reference

This page summarizes the metadata that the current backend already persists for object types.

## Stored fields

| Field | Meaning |
| --- | --- |
| `id` | object type identifier |
| `name` | stable internal semantic key |
| `display_name` | human-facing label |
| `description` | semantic description |
| `primary_key_property` | optional key field reference |
| `icon` | optional UI icon |
| `color` | optional UI color |
| `owner_id` | creating user or owner identity |
| `created_at` | creation timestamp |
| `updated_at` | latest update timestamp |

## Why this matters

Even this relatively small metadata model already shows an important design choice: object types in OpenFoundry are not only schema containers, they are also intended to drive UX and ownership-aware governance.
