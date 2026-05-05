"""Exception types for ``openfoundry_transforms``.

Kept in their own module so the public ``__init__`` can re-export
without circular imports between :mod:`backend` and :mod:`mediasets`.
"""

from __future__ import annotations


class TransformError(Exception):
    """Base exception for transform-runtime errors."""


class TransformNotIncrementalError(TransformError):
    """Raised when the runtime asks for an ``"added"`` / ``"previous"``
    listing on an output media set that has not run incrementally yet
    (for example, the first build, or after the input set was
    replaced). Mirrors the Foundry doc's
    ``IncrementalCannotRunError`` shape — Python transforms catch this
    to opt into a snapshot fallback.
    """


class MediaSetWriteModeError(TransformError):
    """Raised when a ``replace`` write_mode is set on a transactionless
    media set. Mirrors the Foundry contract:
    ``Transactionless media sets … cannot use the replace write
    mode``. The runtime checks this **before** any items are
    materialised so the error is fast and deterministic.
    """
