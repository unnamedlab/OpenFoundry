# Implement an interface

Interfaces become useful only when they are attached to real object types.

## Current implementation flow

OpenFoundry already supports binding interfaces to object types through the dedicated type-interface attachment route.

## Sequence

```text
create interface
  -> add interface properties
  -> create object type
  -> attach interface to object type
  -> create objects that now inherit that semantic contract
```

## Why this matters

This attachment model is what turns interfaces from abstract documentation into reusable ontology structure.
