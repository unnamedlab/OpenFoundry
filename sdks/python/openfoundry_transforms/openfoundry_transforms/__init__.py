"""``openfoundry_transforms`` — Code-Repositories-style transforms API
for OpenFoundry media sets.

Mirrors Foundry's ``transforms.api`` + ``transforms.mediasets`` so a
Python transform written against an upstream Foundry tutorial drops
into an OpenFoundry repo with minimal changes.

Surface (verbatim with the Foundry doc names):

* :class:`MediaSetInput`  — read media items from a set, optionally
  scoped by ``mode`` (``current`` / ``previous`` / ``added`` for
  incremental transforms).
* :class:`MediaSetOutput` — write media items to a set with a
  ``write_mode`` of ``"modify"`` (default) or ``"replace"``.
* :func:`transform`       — decorator that wires inputs + outputs to
  a function the runtime drives.
* :func:`incremental`     — flag the transform as incremental.

The package ships **without** a network backend; consumers (tests,
the eventual ``code-repository-review-service`` runtime) inject an
:class:`MediaSetBackend` that talks to ``media-sets-service`` over
HTTP. The shipped :class:`InMemoryMediaSetBackend` is the canonical
test fixture — it mirrors the wire contract closely enough that an
incremental Python transform reads + writes against it without
touching the network.
"""

from .api import (
    incremental,
    transform,
)
from .backend import InMemoryMediaSetBackend, MediaItemRow, MediaSetBackend
from .errors import (
    MediaSetWriteModeError,
    TransformError,
    TransformNotIncrementalError,
)
from .mediasets import MediaSetInput, MediaSetOutput, WriteMode
from .virtual_tables import (
    InMemoryVirtualTableBackend,
    PushdownEngine,
    TransformDescriptor,
    ValidationIssue,
    VirtualTableBackend,
    VirtualTableCapabilities,
    VirtualTableError,
    VirtualTableHandle,
    VirtualTableInput,
    VirtualTableOutput,
    VirtualTablePushdownNotSupported,
    VirtualTableUseExternalSystemsIncompat,
    VirtualTableWriteModeNotSupported,
    get_pushdown_engine,
    has_use_external_systems,
    pushdown_to,
    read_virtual_table,
    resolve_engine,
    use_external_systems,
    validate_transform,
    write_virtual_table,
)

__all__ = [
    "MediaSetInput",
    "MediaSetOutput",
    "WriteMode",
    "MediaItemRow",
    "MediaSetBackend",
    "InMemoryMediaSetBackend",
    "TransformError",
    "TransformNotIncrementalError",
    "MediaSetWriteModeError",
    "incremental",
    "transform",
    # D1.1.9 P6 — virtual tables + compute pushdown.
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
