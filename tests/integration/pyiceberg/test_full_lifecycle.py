"""Full PyIceberg lifecycle against `iceberg-catalog-service` (P3).

Covers the spec sequence from D1.1.8 P3 § 4:

  a) Boot stack (handled by docker-compose; conftest skips if absent).
  b) Mint OAuth2 access_token (conftest fixture).
  c) Configure PyIceberg via REST.
  d) Create namespace + table with `{id:int, name:string}`.
  e) Append a batch via PyArrow → PyIceberg.
  f) Read back and assert.
  g) Append a second batch (overwrite snapshot).
  h) ListSnapshots returns 2 entries.
  i) Drop table + namespace.
"""

from __future__ import annotations

import pytest


pytestmark = pytest.mark.integration


def test_full_lifecycle(pyiceberg_catalog, unique_namespace):
    pa = pytest.importorskip("pyarrow")
    pyiceberg_schema = pytest.importorskip("pyiceberg.schema")
    pyiceberg_types = pytest.importorskip("pyiceberg.types")

    # --- (d) Create namespace + table ----------------------------------
    pyiceberg_catalog.create_namespace(unique_namespace)
    schema = pyiceberg_schema.Schema(
        pyiceberg_types.NestedField(1, "id", pyiceberg_types.IntegerType(), required=True),
        pyiceberg_types.NestedField(2, "name", pyiceberg_types.StringType(), required=False),
    )
    table = pyiceberg_catalog.create_table(
        identifier=(unique_namespace, "users"),
        schema=schema,
    )

    # --- (e) Append batch via PyArrow ----------------------------------
    batch_a = pa.Table.from_pydict(
        {"id": [1, 2, 3], "name": ["alice", "bob", "carol"]},
        schema=table.schema().as_arrow(),
    )
    table.append(batch_a)

    # --- (f) Read back -------------------------------------------------
    scanned = table.scan().to_arrow()
    assert scanned.num_rows == 3
    names = scanned.column("name").to_pylist()
    assert sorted(names) == ["alice", "bob", "carol"]

    # --- (g) Overwrite -------------------------------------------------
    batch_b = pa.Table.from_pydict(
        {"id": [10, 11], "name": ["dave", "eve"]},
        schema=table.schema().as_arrow(),
    )
    table.overwrite(batch_b)

    # --- (h) Snapshot history -----------------------------------------
    snapshots = list(table.snapshots())
    assert len(snapshots) >= 2, f"expected ≥2 snapshots, got {snapshots}"
    operations = {s.summary["operation"] for s in snapshots}
    assert "append" in operations
    assert "overwrite" in operations

    # --- (i) Drop ------------------------------------------------------
    pyiceberg_catalog.drop_table((unique_namespace, "users"))
    pyiceberg_catalog.drop_namespace(unique_namespace)
