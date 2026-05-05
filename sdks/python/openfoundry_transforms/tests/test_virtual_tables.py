"""Unit tests for the ``virtual_tables`` Code-Repositories surface.

Covers (mirroring the prompt's list of P6 SDK requirements):

* Compute pushdown engine resolution per provider (BigQuery → Ibis,
  Snowflake → Snowpark, Databricks → PySpark, object stores →
  foundry-compute).
* ``read_virtual_table`` returns engine-native handles that compose
  with the per-engine vocabulary (``.filter``, ``.select``, ``.execute``,
  ``.collect``, ``.to_pandas``).
* ``write_virtual_table`` honours capability flags and the snapshot /
  append-only mode rules from the published Foundry matrix.
* ``@pushdown_to`` + ``@use_external_systems`` decorators set the tags
  the build-time validator inspects, and the validator emits
  ``VIRTUAL_TABLE_USE_EXTERNAL_SYSTEMS_INCOMPAT`` for the doc-blocked
  combination (Foundry doc § Limitations).
"""

from __future__ import annotations

import pytest

from openfoundry_transforms import (
    InMemoryVirtualTableBackend,
    PushdownEngine,
    TransformDescriptor,
    VirtualTableCapabilities,
    VirtualTableInput,
    VirtualTableOutput,
    VirtualTablePushdownNotSupported,
    VirtualTableWriteModeNotSupported,
    get_pushdown_engine,
    pushdown_to,
    read_virtual_table,
    resolve_engine,
    use_external_systems,
    validate_transform,
    write_virtual_table,
)


def _bq_caps() -> VirtualTableCapabilities:
    return VirtualTableCapabilities(
        read=True,
        write=True,
        incremental=True,
        versioning=False,
        append_only_supported=True,
        compute_pushdown="ibis",
    )


def _snowflake_caps() -> VirtualTableCapabilities:
    return VirtualTableCapabilities(
        read=True,
        write=True,
        incremental=True,
        versioning=False,
        append_only_supported=True,
        compute_pushdown="snowpark",
    )


def _databricks_caps() -> VirtualTableCapabilities:
    return VirtualTableCapabilities(
        read=True,
        write=True,
        incremental=True,
        versioning=True,
        append_only_supported=True,
        compute_pushdown="pyspark",
    )


def _s3_caps() -> VirtualTableCapabilities:
    return VirtualTableCapabilities(
        read=True,
        write=True,
        incremental=False,
        versioning=False,
        append_only_supported=False,
        compute_pushdown=None,
    )


# ---------------------------------------------------------------------------
# resolve_engine.
# ---------------------------------------------------------------------------


class TestResolveEngine:
    def test_bigquery_defaults_to_ibis(self) -> None:
        assert (
            resolve_engine(requested=None, provider="BIGQUERY", capabilities=_bq_caps())
            == PushdownEngine.Ibis
        )

    def test_snowflake_defaults_to_snowpark(self) -> None:
        assert (
            resolve_engine(
                requested=None, provider="SNOWFLAKE", capabilities=_snowflake_caps()
            )
            == PushdownEngine.Snowpark
        )

    def test_databricks_defaults_to_pyspark(self) -> None:
        assert (
            resolve_engine(
                requested=None, provider="DATABRICKS", capabilities=_databricks_caps()
            )
            == PushdownEngine.PySpark
        )

    def test_object_store_falls_back_to_foundry_compute(self) -> None:
        assert (
            resolve_engine(requested=None, provider="AMAZON_S3", capabilities=_s3_caps())
            == PushdownEngine.FoundryCompute
        )

    def test_explicit_engine_validated_against_capability(self) -> None:
        with pytest.raises(VirtualTablePushdownNotSupported):
            resolve_engine(
                requested="ibis", provider="SNOWFLAKE", capabilities=_snowflake_caps()
            )

    def test_unknown_engine_string_rejected(self) -> None:
        with pytest.raises(VirtualTablePushdownNotSupported):
            resolve_engine(
                requested="postgres", provider="BIGQUERY", capabilities=_bq_caps()
            )

    def test_foundry_compute_always_allowed(self) -> None:
        # Even on a source whose matrix slot has a native engine, the
        # caller can opt back to local materialisation.
        assert (
            resolve_engine(
                requested="foundry-compute",
                provider="SNOWFLAKE",
                capabilities=_snowflake_caps(),
            )
            == PushdownEngine.FoundryCompute
        )


# ---------------------------------------------------------------------------
# read_virtual_table.
# ---------------------------------------------------------------------------


class TestReadVirtualTable:
    def test_ibis_handle_supports_filter_and_execute(self) -> None:
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.1": [{"id": 1}, {"id": 2}, {"id": 3}]},
            providers_by_rid={"ri.vt.1": "BIGQUERY"},
            capabilities_by_rid={"ri.vt.1": _bq_caps()},
        )
        handle = read_virtual_table("ri.vt.1", backend=backend)
        assert handle.engine == PushdownEngine.Ibis
        # `.filter` composes; `.execute()` materialises.
        filtered = handle.handle.filter(lambda r: r["id"] > 1).execute()
        assert filtered == [{"id": 2}, {"id": 3}]

    def test_pyspark_handle_supports_select(self) -> None:
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.dbx": [{"id": 1, "payload": "a"}]},
            providers_by_rid={"ri.vt.dbx": "DATABRICKS"},
            capabilities_by_rid={"ri.vt.dbx": _databricks_caps()},
        )
        handle = read_virtual_table("ri.vt.dbx", backend=backend)
        assert handle.engine == PushdownEngine.PySpark
        selected = handle.handle.select("id").collect()
        assert selected == [{"id": 1}]

    def test_snowpark_handle_supports_filter_and_to_pandas(self) -> None:
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.sf": [{"id": 1}, {"id": 2}]},
            providers_by_rid={"ri.vt.sf": "SNOWFLAKE"},
            capabilities_by_rid={"ri.vt.sf": _snowflake_caps()},
        )
        handle = read_virtual_table("ri.vt.sf", backend=backend)
        assert handle.engine == PushdownEngine.Snowpark
        result = handle.handle.filter(lambda r: r["id"] == 2).to_pandas()
        assert result == [{"id": 2}]

    def test_object_store_falls_back_to_arrow_handle(self) -> None:
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.s3": [{"row": "x"}]},
            providers_by_rid={"ri.vt.s3": "AMAZON_S3"},
            capabilities_by_rid={"ri.vt.s3": _s3_caps()},
        )
        handle = read_virtual_table("ri.vt.s3", backend=backend)
        assert handle.engine == PushdownEngine.FoundryCompute
        assert handle.handle.to_list() == [{"row": "x"}]

    def test_pinned_engine_overrides_default(self) -> None:
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.bq": [{"id": 1}]},
            providers_by_rid={"ri.vt.bq": "BIGQUERY"},
            capabilities_by_rid={"ri.vt.bq": _bq_caps()},
        )
        handle = read_virtual_table(
            "ri.vt.bq", pushdown_engine="foundry-compute", backend=backend
        )
        assert handle.engine == PushdownEngine.FoundryCompute


# ---------------------------------------------------------------------------
# write_virtual_table.
# ---------------------------------------------------------------------------


class TestWriteVirtualTable:
    def test_snapshot_replaces_contents(self) -> None:
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.bq": [{"id": 1}, {"id": 2}]},
            providers_by_rid={"ri.vt.bq": "BIGQUERY"},
            capabilities_by_rid={"ri.vt.bq": _bq_caps()},
        )
        written = write_virtual_table(
            "ri.vt.bq", [{"id": 99}], mode="snapshot", backend=backend
        )
        assert written == 1
        assert backend.rows_by_rid["ri.vt.bq"] == [{"id": 99}]

    def test_append_extends_contents(self) -> None:
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.bq": [{"id": 1}]},
            providers_by_rid={"ri.vt.bq": "BIGQUERY"},
            capabilities_by_rid={"ri.vt.bq": _bq_caps()},
        )
        written = write_virtual_table(
            "ri.vt.bq", [{"id": 2}, {"id": 3}], mode="append", backend=backend
        )
        assert written == 2
        assert backend.rows_by_rid["ri.vt.bq"] == [{"id": 1}, {"id": 2}, {"id": 3}]

    def test_append_rejected_when_capability_missing(self) -> None:
        # S3 row in the matrix advertises `append_only_supported = False`.
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.s3": []},
            providers_by_rid={"ri.vt.s3": "AMAZON_S3"},
            capabilities_by_rid={"ri.vt.s3": _s3_caps()},
        )
        with pytest.raises(VirtualTableWriteModeNotSupported):
            write_virtual_table("ri.vt.s3", [], mode="append", backend=backend)

    def test_write_rejected_when_read_only(self) -> None:
        backend = InMemoryVirtualTableBackend(
            rows_by_rid={"ri.vt.ro": []},
            providers_by_rid={"ri.vt.ro": "DATABRICKS"},
            capabilities_by_rid={
                "ri.vt.ro": VirtualTableCapabilities(
                    read=True,
                    write=False,
                    incremental=False,
                    versioning=False,
                    append_only_supported=False,
                    compute_pushdown="pyspark",
                )
            },
        )
        with pytest.raises(VirtualTableWriteModeNotSupported):
            write_virtual_table("ri.vt.ro", [], mode="snapshot", backend=backend)

    def test_invalid_mode_string_rejected(self) -> None:
        backend = InMemoryVirtualTableBackend()
        with pytest.raises(ValueError):
            write_virtual_table("ri.vt", [], mode="upsert", backend=backend)


# ---------------------------------------------------------------------------
# pushdown_to + use_external_systems decorators.
# ---------------------------------------------------------------------------


class TestDecorators:
    def test_pushdown_to_tags_function(self) -> None:
        @pushdown_to("snowpark")
        def transform(_: object) -> object:
            return None

        assert get_pushdown_engine(transform) == PushdownEngine.Snowpark

    def test_pushdown_to_none_means_auto(self) -> None:
        @pushdown_to(None)
        def transform(_: object) -> object:
            return None

        assert get_pushdown_engine(transform) is None

    def test_pushdown_to_rejects_unknown_engine(self) -> None:
        with pytest.raises(ValueError):
            @pushdown_to("postgres")  # type: ignore[arg-type]
            def transform(_: object) -> object:
                return None

    def test_validate_transform_rejects_use_external_systems_with_virtual_table(
        self,
    ) -> None:
        @use_external_systems
        def transform_fn(_: object) -> object:
            return None

        descriptor = TransformDescriptor(
            fn=transform_fn,
            inputs=[VirtualTableInput(rid="ri.foundry.main.virtual-table.x")],
        )
        issues = validate_transform(descriptor)
        codes = [issue.code for issue in issues]
        assert "VIRTUAL_TABLE_USE_EXTERNAL_SYSTEMS_INCOMPAT" in codes

    def test_validate_transform_passes_when_no_virtual_table_input(self) -> None:
        @use_external_systems
        def transform_fn(_: object) -> object:
            return None

        descriptor = TransformDescriptor(fn=transform_fn, inputs=[])
        assert validate_transform(descriptor) == []

    def test_validate_transform_passes_when_no_use_external_systems(self) -> None:
        def transform_fn(_: object) -> object:
            return None

        descriptor = TransformDescriptor(
            fn=transform_fn,
            inputs=[VirtualTableInput(rid="ri.foundry.main.virtual-table.x")],
        )
        assert validate_transform(descriptor) == []


# ---------------------------------------------------------------------------
# Input / output dataclasses.
# ---------------------------------------------------------------------------


class TestInputOutputDataclasses:
    def test_virtual_table_input_rejects_unknown_incremental_mode(self) -> None:
        with pytest.raises(ValueError):
            VirtualTableInput(rid="ri.vt", incremental_mode="upsert")

    def test_virtual_table_output_rejects_unknown_write_mode(self) -> None:
        with pytest.raises(ValueError):
            VirtualTableOutput(
                source_rid="ri.source",
                locator={"kind": "tabular"},
                write_mode="merge",
            )

    def test_virtual_table_output_accepts_snapshot_and_append(self) -> None:
        VirtualTableOutput(
            source_rid="ri.source",
            locator={"kind": "tabular"},
            write_mode="snapshot",
        )
        VirtualTableOutput(
            source_rid="ri.source",
            locator={"kind": "tabular"},
            write_mode="append",
        )
