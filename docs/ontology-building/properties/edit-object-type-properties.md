# Edit object type properties

Editing properties is one of the highest-risk semantic changes because applications and workflows often assume property stability.

## Current editable fields

The backend currently allows updates to:

- `display_name`
- `description`
- `required`
- `unique_constraint`
- `time_dependent`
- `default_value`
- `validation_rules`

The `name` and `property_type` are notably not part of the update contract, which is a strong signal that OpenFoundry is already protecting property identity and typing from casual mutation.

## OpenFoundry current vs target

| Dimension | Current | Target |
| --- | --- | --- |
| edit flow | direct API mutation | guarded semantic migration workflow |
| validation | value validation exists | impact analysis across apps and workflows |
| compatibility | implicit | explicit compatibility checks and release notes |
