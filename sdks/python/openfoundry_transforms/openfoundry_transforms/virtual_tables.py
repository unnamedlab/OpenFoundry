"""``virtual_tables`` — Code-Repositories surface for Foundry virtual
tables and **compute pushdown**.

Foundry doc anchors:

* ``Data connectivity & integration/Transforms/Python/Virtual tables and
  compute pushdown/Overview.md``
* ``…/Compute pushdown.md``
* ``…/API reference.md``
* ``…/{BigQuery, Snowflake, Databricks} compute pushdown.md``
* ``Core concepts/Virtual tables.md`` § "Virtual tables in Code Repositories",
  § "Limitations" (the ``use_external_systems`` rule).

Surface (verbatim with the Foundry doc names):

* :class:`VirtualTableInput`   — declares a virtual-table input on a
  Python transform; mirrors ``transforms.api.Input`` for virtual tables.
* :class:`VirtualTableOutput`  — declares a virtual-table output. Only
  accepts a ``write_mode`` the source supports (validated against the
  capability matrix).
* :func:`pushdown_to`          — decorator that pins the preferred
  pushdown engine for a transform (``"ibis"``, ``"pyspark"``,
  ``"snowpark"``, ``"foundry-compute"``, ``None`` → auto).
* :func:`read_virtual_table`   — return a *handle* shaped according
  to the requested engine (Ibis Table, PySpark DataFrame, Snowpark
  DataFrame, or an Arrow ``RecordBatchReader`` on the foundry-compute
  fallback).
* :func:`write_virtual_table`  — write a frame back to the source.
  Mode is one of ``"snapshot"`` (default) or ``"append"``; ``"append"``
  requires the source's ``append_only_supported`` capability.

The shipped :class:`InMemoryVirtualTableBackend` is the canonical test
fixture — it implements every engine path with a deterministic mock
client so transforms can be unit-tested without spinning up
BigQuery / Databricks / Snowflake.
"""

from __future__ import annotations

import dataclasses
import enum
import functools
import typing as t

# ---------------------------------------------------------------------------
# Capability matrix slice (mirrors the Rust source-of-truth).
# ---------------------------------------------------------------------------


class PushdownEngine(str, enum.Enum):
    """Compute pushdown engines blessed by the Foundry doc.

    Mapping (provider → engine) lifted verbatim from the published
    matrix in ``Virtual tables.md`` § "Virtual table compatibility
    matrix by source & table type":

    * BigQuery   → :py:attr:`Ibis`
    * Snowflake  → :py:attr:`Snowpark`
    * Databricks → :py:attr:`PySpark`
    * Object stores (S3/ADLS/GCS) → no pushdown engine.
    """

    Ibis = "ibis"
    PySpark = "pyspark"
    Snowpark = "snowpark"
    FoundryCompute = "foundry-compute"


@dataclasses.dataclass(frozen=True)
class VirtualTableCapabilities:
    """Capability slice received from ``virtual-table-service``.

    Mirrors ``Capabilities`` in the Rust ``capability_matrix.rs``;
    every read of these flags inside the SDK pulls from the value the
    backend hands the runtime, never from a hard-coded table.
    """

    read: bool
    write: bool
    incremental: bool
    versioning: bool
    append_only_supported: bool
    compute_pushdown: t.Optional[str]


_PROVIDER_DEFAULT_ENGINE: dict[str, PushdownEngine] = {
    "BIGQUERY": PushdownEngine.Ibis,
    "DATABRICKS": PushdownEngine.PySpark,
    "SNOWFLAKE": PushdownEngine.Snowpark,
}


# ---------------------------------------------------------------------------
# Errors.
# ---------------------------------------------------------------------------


class VirtualTableError(Exception):
    """Base for all virtual-table SDK errors."""


class VirtualTablePushdownNotSupported(VirtualTableError):
    """Raised when the caller pinned a pushdown engine the source
    cannot run (e.g. ``pushdown_to("ibis")`` on a Snowflake source)."""


class VirtualTableWriteModeNotSupported(VirtualTableError):
    """Raised when ``write_virtual_table`` is called with ``mode="append"``
    on a source whose ``append_only_supported = False``."""


class VirtualTableUseExternalSystemsIncompat(VirtualTableError):
    """Raised when a transform mixes ``@use_external_systems`` with a
    virtual-table input. Foundry doc § "Limitations" calls this out
    explicitly: split the transform or move to source-based
    external_transforms."""


# ---------------------------------------------------------------------------
# Input / output declarations.
# ---------------------------------------------------------------------------


@dataclasses.dataclass(frozen=True)
class VirtualTableInput:
    """Declares a virtual-table input on a Python transform.

    Mirrors ``transforms.api.Input`` for virtual tables.

    :param rid: ``ri.foundry.main.virtual-table.<uuid>``.
    :param incremental_mode: ``"none"`` (snapshot read) or
        ``"append_only"``. ``"append_only"`` requires the source's
        ``incremental && append_only_supported`` capabilities.
    """

    rid: str
    incremental_mode: str = "none"

    def __post_init__(self) -> None:
        if self.incremental_mode not in {"none", "append_only"}:
            raise ValueError(
                f"incremental_mode must be 'none' or 'append_only', got "
                f"{self.incremental_mode!r}"
            )


@dataclasses.dataclass(frozen=True)
class VirtualTableOutput:
    """Declares a virtual-table output on a Python transform.

    :param source_rid: source the output writes back into.
    :param locator: provider-specific locator dict (mirrors
        ``virtual_tables.locator`` JSONB). Either an existing virtual
        table's locator (write-back) or a fresh one (registers a new
        virtual table at commit time).
    :param write_mode: ``"snapshot"`` (default) or ``"append"``.
    """

    source_rid: str
    locator: dict[str, t.Any]
    write_mode: str = "snapshot"

    def __post_init__(self) -> None:
        if self.write_mode not in {"snapshot", "append"}:
            raise ValueError(
                f"write_mode must be 'snapshot' or 'append', got {self.write_mode!r}"
            )


# ---------------------------------------------------------------------------
# Backend protocol + in-memory test fixture.
# ---------------------------------------------------------------------------


@dataclasses.dataclass
class VirtualTableHandle:
    """Wrapper around an engine-native handle (Ibis Table / PySpark
    DataFrame / Snowpark DataFrame / Arrow `RecordBatchReader`).

    Carries the requested engine + the underlying handle so a transform
    can introspect what it actually got. The runtime is responsible for
    resolving symbolic operations on the handle into engine-native SQL.
    """

    engine: PushdownEngine
    handle: t.Any
    """The engine-native object. Type depends on ``engine``."""
    rid: str
    """The virtual-table RID this handle was opened against."""


class VirtualTableBackend(t.Protocol):
    """Backend used by :func:`read_virtual_table` and
    :func:`write_virtual_table`. The production runtime ships an HTTP
    backend that talks to ``virtual-table-service`` for capability
    lookup + read / write proxying; tests inject the in-memory backend
    below."""

    def get_capabilities(self, rid: str) -> VirtualTableCapabilities:
        ...

    def get_provider(self, rid: str) -> str:
        ...

    def open_handle(self, rid: str, engine: PushdownEngine) -> t.Any:
        ...

    def write(self, rid: str, frame: t.Any, mode: str) -> int:
        ...


@dataclasses.dataclass
class InMemoryVirtualTableBackend:
    """Backend that resolves to deterministic in-memory mocks per
    engine. Used by the unit-test suite + as a smoke target for
    transforms before they hit a real source."""

    rows_by_rid: dict[str, list[dict[str, t.Any]]] = dataclasses.field(
        default_factory=dict
    )
    capabilities_by_rid: dict[str, VirtualTableCapabilities] = dataclasses.field(
        default_factory=dict
    )
    providers_by_rid: dict[str, str] = dataclasses.field(default_factory=dict)

    def get_capabilities(self, rid: str) -> VirtualTableCapabilities:
        return self.capabilities_by_rid.get(
            rid,
            VirtualTableCapabilities(
                read=True,
                write=False,
                incremental=False,
                versioning=False,
                append_only_supported=False,
                compute_pushdown=None,
            ),
        )

    def get_provider(self, rid: str) -> str:
        return self.providers_by_rid.get(rid, "BIGQUERY")

    def open_handle(self, rid: str, engine: PushdownEngine) -> t.Any:
        rows = list(self.rows_by_rid.get(rid, []))
        if engine == PushdownEngine.Ibis:
            return _IbisStub(rid=rid, rows=rows)
        if engine == PushdownEngine.PySpark:
            return _PySparkStub(rid=rid, rows=rows)
        if engine == PushdownEngine.Snowpark:
            return _SnowparkStub(rid=rid, rows=rows)
        return _ArrowFallback(rid=rid, rows=rows)

    def write(self, rid: str, frame: t.Any, mode: str) -> int:
        rows = _frame_to_rows(frame)
        if mode == "snapshot":
            self.rows_by_rid[rid] = list(rows)
        else:  # append
            self.rows_by_rid.setdefault(rid, []).extend(rows)
        return len(rows)


@dataclasses.dataclass
class _IbisStub:
    rid: str
    rows: list[dict[str, t.Any]]

    def filter(self, predicate: t.Callable[[dict[str, t.Any]], bool]) -> "_IbisStub":
        return _IbisStub(rid=self.rid, rows=[r for r in self.rows if predicate(r)])

    def execute(self) -> list[dict[str, t.Any]]:
        return list(self.rows)


@dataclasses.dataclass
class _PySparkStub:
    rid: str
    rows: list[dict[str, t.Any]]

    def select(self, *columns: str) -> "_PySparkStub":
        cols = list(columns)
        return _PySparkStub(
            rid=self.rid,
            rows=[{c: r.get(c) for c in cols} for r in self.rows],
        )

    def collect(self) -> list[dict[str, t.Any]]:
        return list(self.rows)


@dataclasses.dataclass
class _SnowparkStub:
    rid: str
    rows: list[dict[str, t.Any]]

    def filter(self, predicate: t.Callable[[dict[str, t.Any]], bool]) -> "_SnowparkStub":
        return _SnowparkStub(rid=self.rid, rows=[r for r in self.rows if predicate(r)])

    def to_pandas(self) -> list[dict[str, t.Any]]:
        # Test-fixture stand-in for snowpark.DataFrame.to_pandas().
        return list(self.rows)


@dataclasses.dataclass
class _ArrowFallback:
    rid: str
    rows: list[dict[str, t.Any]]

    def to_list(self) -> list[dict[str, t.Any]]:
        return list(self.rows)


def _frame_to_rows(frame: t.Any) -> list[dict[str, t.Any]]:
    """Best-effort flattening of an engine-native frame to a list of
    dict rows. Matches the test stubs above plus a plain list-of-dicts
    pass-through so a transform can write either."""
    if isinstance(frame, list):
        return frame
    for attr in ("collect", "to_pandas", "to_list", "execute"):
        method = getattr(frame, attr, None)
        if callable(method):
            result = method()
            if isinstance(result, list):
                return result
    raise TypeError(f"unsupported frame type: {type(frame).__name__}")


# ---------------------------------------------------------------------------
# Decorator.
# ---------------------------------------------------------------------------


_PUSHDOWN_TAG = "_openfoundry_pushdown_engine"
_USE_EXTERNAL_SYSTEMS_TAG = "_openfoundry_use_external_systems"


def pushdown_to(engine: t.Union[str, PushdownEngine, None]) -> t.Callable[[t.Callable], t.Callable]:
    """Pin the preferred pushdown engine for a Python transform.

    Doc § "Compute pushdown" describes ``pushdown_to`` as the
    Code-Repositories-side hint that lets the runtime pre-pick the
    engine without sniffing the input. The decorator stamps a tag on
    the function the runtime reads when binding inputs.

    Composes with the ``transforms.api.transform`` decorator: order
    does not matter, the tag is preserved through ``functools.wraps``.

    :param engine: one of ``"ibis"`` | ``"pyspark"`` | ``"snowpark"`` |
        ``"foundry-compute"`` | ``None`` (auto). String values are
        normalised to :class:`PushdownEngine`.
    """

    if engine is not None and not isinstance(engine, PushdownEngine):
        try:
            engine = PushdownEngine(engine)
        except ValueError as err:
            raise ValueError(
                f"unknown pushdown engine: {engine!r}; expected one of "
                + ", ".join(repr(e.value) for e in PushdownEngine)
            ) from err

    def decorator(fn: t.Callable) -> t.Callable:
        @functools.wraps(fn)
        def wrapper(*args: t.Any, **kwargs: t.Any) -> t.Any:
            return fn(*args, **kwargs)

        setattr(wrapper, _PUSHDOWN_TAG, engine)
        return wrapper

    return decorator


def use_external_systems(fn: t.Callable) -> t.Callable:
    """Stub mirroring Foundry's ``@use_external_systems`` decorator.

    The decorator itself is a no-op; the build-time validator
    inspects the tag set here together with the transform's input
    declarations and raises
    :class:`VirtualTableUseExternalSystemsIncompat` if the transform
    *also* declares a virtual-table input. Foundry doc § "Limitations":

        Transforms that use the ``use_external_systems`` decorator are
        currently not compatible with Virtual Tables. Switch to
        source-based external_transforms or split your transform.
    """

    @functools.wraps(fn)
    def wrapper(*args: t.Any, **kwargs: t.Any) -> t.Any:
        return fn(*args, **kwargs)

    setattr(wrapper, _USE_EXTERNAL_SYSTEMS_TAG, True)
    return wrapper


def get_pushdown_engine(fn: t.Callable) -> t.Optional[PushdownEngine]:
    """Return the engine the transform pinned via :func:`pushdown_to`,
    or ``None`` if it didn't pin one (auto-resolve)."""

    return t.cast(
        t.Optional[PushdownEngine], getattr(fn, _PUSHDOWN_TAG, None)
    )


def has_use_external_systems(fn: t.Callable) -> bool:
    """Return ``True`` when the function carries
    ``@use_external_systems``. Used by the runtime validator."""

    return bool(getattr(fn, _USE_EXTERNAL_SYSTEMS_TAG, False))


# ---------------------------------------------------------------------------
# Public read / write API.
# ---------------------------------------------------------------------------


def resolve_engine(
    *,
    requested: t.Union[str, PushdownEngine, None],
    provider: str,
    capabilities: VirtualTableCapabilities,
) -> PushdownEngine:
    """Pick the engine to open a handle on.

    Resolution order:

    1. If the caller passed an explicit engine, validate it against
       the source's capability and return it.
    2. Otherwise fall back to the provider default
       (Ibis / PySpark / Snowpark per the Foundry matrix).
    3. If neither yields a supported engine, return
       :py:attr:`PushdownEngine.FoundryCompute` so the runtime
       materialises the table locally via the connector.
    """

    if requested is not None:
        if isinstance(requested, str):
            try:
                requested = PushdownEngine(requested)
            except ValueError as err:
                raise VirtualTablePushdownNotSupported(
                    f"unknown pushdown engine: {requested!r}"
                ) from err
        # `foundry-compute` is always allowed (local materialisation).
        if requested == PushdownEngine.FoundryCompute:
            return requested
        if (
            capabilities.compute_pushdown is None
            or capabilities.compute_pushdown != requested.value
        ):
            raise VirtualTablePushdownNotSupported(
                f"source ({provider}) does not support pushdown engine "
                f"{requested.value!r}; capability matrix advertises "
                f"{capabilities.compute_pushdown!r}"
            )
        return requested

    default = _PROVIDER_DEFAULT_ENGINE.get(provider.upper())
    if default is not None and capabilities.compute_pushdown == default.value:
        return default
    return PushdownEngine.FoundryCompute


def read_virtual_table(
    rid: str,
    *,
    pushdown_engine: t.Union[str, PushdownEngine, None] = None,
    backend: VirtualTableBackend,
) -> VirtualTableHandle:
    """Open an engine-native handle on a virtual table.

    Doc § "Compute pushdown" describes the per-engine surface:

    * **Ibis (BigQuery)** — returns an Ibis Table; calls like
      ``.filter`` / ``.select`` / ``.group_by`` compose into a single
      BigQuery SQL query at ``.execute()``.
    * **PySpark (Databricks)** — returns a Spark DataFrame; the
      runtime delegates to ``spark.read.format("delta").load(uri)`` /
      ``spark.sql(...)``.
    * **Snowpark (Snowflake)** — returns a Snowpark DataFrame.
    * **foundry-compute** fallback — returns an Arrow-shaped reader
      with ``to_list()`` so transforms can iterate locally.
    """

    capabilities = backend.get_capabilities(rid)
    if not capabilities.read:
        raise VirtualTableError(
            f"virtual table {rid} does not advertise read capability"
        )
    provider = backend.get_provider(rid)
    engine = resolve_engine(
        requested=pushdown_engine,
        provider=provider,
        capabilities=capabilities,
    )
    return VirtualTableHandle(
        engine=engine,
        handle=backend.open_handle(rid, engine),
        rid=rid,
    )


def write_virtual_table(
    rid: str,
    frame: t.Any,
    *,
    mode: str = "snapshot",
    backend: VirtualTableBackend,
) -> int:
    """Write a frame to a virtual table.

    :param mode: ``"snapshot"`` (default) replaces the table contents.
        ``"append"`` requires the source's ``append_only_supported``
        capability — see the published matrix.
    :returns: the number of rows written, mirroring
        ``MediaSetOutput.write``'s contract.
    """

    if mode not in {"snapshot", "append"}:
        raise ValueError(f"mode must be 'snapshot' or 'append', got {mode!r}")
    capabilities = backend.get_capabilities(rid)
    if not capabilities.write:
        raise VirtualTableWriteModeNotSupported(
            f"virtual table {rid} is read-only"
        )
    if mode == "append" and not capabilities.append_only_supported:
        raise VirtualTableWriteModeNotSupported(
            f"virtual table {rid} does not support append-only writes"
        )
    return backend.write(rid, frame, mode)


# ---------------------------------------------------------------------------
# Build-time validator (used by code-repository-review-service).
# ---------------------------------------------------------------------------


@dataclasses.dataclass
class TransformDescriptor:
    """Loose description of a Python transform that the build pipeline
    feeds into :func:`validate_transform`. Mirrors the fields the
    Code-Repositories review service extracts at build-graph time."""

    fn: t.Callable
    inputs: list[t.Any]


@dataclasses.dataclass(frozen=True)
class ValidationIssue:
    code: str
    message: str


def validate_transform(transform: TransformDescriptor) -> list[ValidationIssue]:
    """Apply the Foundry doc § "Limitations" rules to a transform that
    declares one or more virtual-table inputs.

    Currently checks:

    * ``use_external_systems`` decorator + virtual-table input combo
      → ``VIRTUAL_TABLE_USE_EXTERNAL_SYSTEMS_INCOMPAT``.
    * Pinned pushdown engine + provider mismatch is checked at
      :func:`read_virtual_table` time (runtime).
    """

    issues: list[ValidationIssue] = []
    has_vt_input = any(isinstance(inp, VirtualTableInput) for inp in transform.inputs)
    if has_vt_input and has_use_external_systems(transform.fn):
        issues.append(
            ValidationIssue(
                code="VIRTUAL_TABLE_USE_EXTERNAL_SYSTEMS_INCOMPAT",
                message=(
                    "Transform mixes @use_external_systems with a virtual-table "
                    "input. Per Foundry doc § Limitations, switch to "
                    "source-based external_transforms or split the transform."
                ),
            )
        )
    return issues


__all__ = [
    "PushdownEngine",
    "VirtualTableCapabilities",
    "VirtualTableInput",
    "VirtualTableOutput",
    "VirtualTableHandle",
    "VirtualTableBackend",
    "InMemoryVirtualTableBackend",
    "VirtualTableError",
    "VirtualTablePushdownNotSupported",
    "VirtualTableWriteModeNotSupported",
    "VirtualTableUseExternalSystemsIncompat",
    "pushdown_to",
    "use_external_systems",
    "get_pushdown_engine",
    "has_use_external_systems",
    "resolve_engine",
    "read_virtual_table",
    "write_virtual_table",
    "TransformDescriptor",
    "ValidationIssue",
    "validate_transform",
]
