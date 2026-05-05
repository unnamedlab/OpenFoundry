# `openfoundry_transforms`

Code-Repositories-style transforms API for OpenFoundry, mirroring
Foundry's `transforms.api` + `transforms.mediasets` +
`transforms.mediasets.virtual_tables` so a Python transform written
against an upstream Foundry tutorial drops into an OpenFoundry repo
with minimal changes.

## Surface

```python
from openfoundry_transforms import (
    # Media sets (D1.1.5).
    MediaSetInput, MediaSetOutput, transform, incremental,
    # Virtual tables (D1.1.9 P6).
    VirtualTableInput, VirtualTableOutput,
    pushdown_to, use_external_systems,
    read_virtual_table, write_virtual_table,
    PushdownEngine, VirtualTableCapabilities,
)
```

## Virtual tables

### Compute pushdown (Foundry doc § "Compute pushdown")

Pin the engine that runs the query natively on the source. Resolution
defaults follow the published Foundry matrix:

| Provider     | Default engine               |
| ------------ | ---------------------------- |
| BigQuery     | `pushdown_to('ibis')`        |
| Databricks   | `pushdown_to('pyspark')`     |
| Snowflake    | `pushdown_to('snowpark')`    |
| S3 / GCS / ADLS (raw files) | `pushdown_to('foundry-compute')` (no native engine) |

```python
from openfoundry_transforms import (
    VirtualTableInput, pushdown_to, read_virtual_table, transform,
)

@transform(orders=VirtualTableInput("ri.foundry.main.virtual-table.bq.orders"))
@pushdown_to("ibis")  # BigQuery → Ibis Table; .filter / .select compose into one query
def gold_orders(ctx, orders):
    handle = read_virtual_table(orders.rid, pushdown_engine="ibis", backend=ctx.virtual_tables)
    rows = handle.handle.filter(lambda r: r["status"] == "PAID").execute()
    return rows
```

### Snowflake (Snowpark)

```python
@transform(events=VirtualTableInput("ri.foundry.main.virtual-table.sf.events"))
@pushdown_to("snowpark")
def filtered_events(ctx, events):
    handle = read_virtual_table(events.rid, pushdown_engine="snowpark", backend=ctx.virtual_tables)
    df = handle.handle.filter(lambda r: r["region"] == "EU")
    return df.to_pandas()
```

### Databricks (PySpark)

```python
@transform(items=VirtualTableInput("ri.foundry.main.virtual-table.dbx.items"))
@pushdown_to("pyspark")
def items_view(ctx, items):
    handle = read_virtual_table(items.rid, pushdown_engine="pyspark", backend=ctx.virtual_tables)
    return handle.handle.select("id", "sku").collect()
```

### Auto-resolution (no `pushdown_to`)

If you don't pin an engine, the SDK reads the source's
`capabilities.compute_pushdown` and picks the matching default. Object
stores fall back to `foundry-compute` (Arrow stream, local
materialisation).

### Writing back (only when capability matrix allows)

```python
from openfoundry_transforms import VirtualTableOutput, write_virtual_table

@transform(out=VirtualTableOutput(
    source_rid="ri.foundry.main.source.bq",
    locator={"kind": "tabular", "database": "warehouse", "schema": "public", "table": "gold"},
    write_mode="snapshot",   # or "append" — requires append_only_supported
))
def materialise(ctx, out):
    rows = [{"id": 1}, {"id": 2}]
    write_virtual_table(out.rid_or_locator, rows, mode="snapshot", backend=ctx.virtual_tables)
```

`mode="append"` raises `VirtualTableWriteModeNotSupported` when the
target's capability slot does not advertise `append_only_supported`
(Foundry doc § "Compute for queries on virtual tables").

## Build-time validation

The SDK exposes the same rules `services/virtual-table-service`
enforces server-side, so `code-repository-review-service` (or any CI
that loads the transform module) can refuse builds before they hit
the runtime:

```python
from openfoundry_transforms import (
    TransformDescriptor, VirtualTableInput, use_external_systems, validate_transform,
)

@use_external_systems
def bad_transform(ctx, vt):
    ...

issues = validate_transform(TransformDescriptor(
    fn=bad_transform,
    inputs=[VirtualTableInput(rid="ri.foundry.main.virtual-table.x")],
))
# → [ValidationIssue(code="VIRTUAL_TABLE_USE_EXTERNAL_SYSTEMS_INCOMPAT", ...)]
```

The doc § "Limitations" rule — "Transforms that use the
`use_external_systems` decorator are currently not compatible with
Virtual Tables" — is enforced uniformly across the Python author
surface and the Rust validator in
`services/virtual-table-service/src/domain/code_imports.rs`.

## Tests

```bash
cd sdks/python/openfoundry_transforms
python3 -m pytest tests/test_virtual_tables.py -v
```

Ships with 26 unit tests covering: per-engine resolution + handle
shape, snapshot vs append-only writes, capability gating, and the
build-time validator's `use_external_systems` rule.
