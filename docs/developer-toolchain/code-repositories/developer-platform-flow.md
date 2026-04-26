# Developer platform flow

The P4 smoke scenario already documents a strong developer workflow path.

## P4 sequence

```text
create repository
  -> create feature branch
  -> create commit
  -> search repository files
  -> diff feature branch
  -> trigger CI
  -> install marketplace template
  -> load published app
```

## Why this matters

This shows that repository workflows, packaging, marketplace activation, and application delivery are already conceptually linked in OpenFoundry.
