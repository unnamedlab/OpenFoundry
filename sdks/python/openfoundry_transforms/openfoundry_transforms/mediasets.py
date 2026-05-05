"""``MediaSetInput`` / ``MediaSetOutput`` — Foundry-doc-shaped helpers.

The objects mirror the names in ``transforms.mediasets`` so a
Foundry tutorial copies into an OpenFoundry repo with no diff. They
are intentionally controlled by the runtime: a transform creates an
:class:`MediaSetInput` / :class:`MediaSetOutput` (typically via the
:func:`~openfoundry_transforms.api.transform` decorator), and the
runtime injects a backend at call time.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Iterable, Literal

from .backend import MediaItemRow, MediaSetBackend
from .errors import MediaSetWriteModeError

WriteMode = Literal["modify", "replace"]
ReadMode = Literal["current", "previous", "added"]


@dataclass
class MediaSetInput:
    """Read-only handle to a media set the transform consumes.

    Attributes mirror the Foundry doc's
    ``MediaSetInput('/examples/input_PNGs')`` signature: a path/RID
    and a branch. The runtime resolves the backend at call time so
    the transform stays serialisable.
    """

    media_set_rid: str
    branch: str = "main"
    _backend: MediaSetBackend | None = None

    def bind_backend(self, backend: MediaSetBackend) -> "MediaSetInput":
        """Inject the runtime backend. Called by the runtime once the
        transform is loaded; tests call it directly.
        """
        return MediaSetInput(self.media_set_rid, self.branch, backend)

    def list_items(
        self,
        *,
        mode: ReadMode = "current",
        deduplicate_by_path: bool = True,
    ) -> list[MediaItemRow]:
        if self._backend is None:
            raise RuntimeError("MediaSetInput is not bound to a backend yet")
        return self._backend.list_items(
            self.media_set_rid,
            branch=self.branch,
            mode=mode,
            deduplicate_by_path=deduplicate_by_path,
        )


@dataclass
class MediaSetOutput:
    """Write-side handle. Carries the ``write_mode`` the runtime
    enforces at commit time. Calling :meth:`set_write_mode` mid-
    transform is supported per the Foundry doc.
    """

    media_set_rid: str
    branch: str = "main"
    write_mode: WriteMode = "modify"
    _backend: MediaSetBackend | None = None
    _written_rids: list[str] = None  # type: ignore[assignment]

    def __post_init__(self) -> None:  # noqa: D401
        if self._written_rids is None:
            self._written_rids = []

    def bind_backend(self, backend: MediaSetBackend) -> "MediaSetOutput":
        bound = MediaSetOutput(
            self.media_set_rid,
            self.branch,
            self.write_mode,
            backend,
            list(self._written_rids),
        )
        return bound

    def set_write_mode(self, mode: WriteMode) -> None:
        if mode not in ("modify", "replace"):
            raise ValueError(f"unknown write_mode {mode!r}")
        self.write_mode = mode

    def upload(
        self,
        *,
        path: str,
        mime_type: str,
        size_bytes: int,
        sha256: str,
        bytes_: bytes | None = None,
    ) -> MediaItemRow:
        if self._backend is None:
            raise RuntimeError("MediaSetOutput is not bound to a backend yet")
        item = self._backend.upload_item(
            self.media_set_rid,
            branch=self.branch,
            path=path,
            mime_type=mime_type,
            size_bytes=size_bytes,
            sha256=sha256,
            bytes_=bytes_,
        )
        self._written_rids.append(item.rid)
        return item

    def commit(self) -> None:
        """Apply the write-mode contract and seal the batch.

        ``modify`` is a no-op (path-dedup happens at upload). ``replace``
        soft-deletes everything on the branch the transform did NOT
        touch — and is rejected at the backend boundary on
        transactionless media sets via :class:`MediaSetWriteModeError`.
        """
        if self._backend is None:
            raise RuntimeError("MediaSetOutput is not bound to a backend yet")
        self._backend.commit_write(
            self.media_set_rid,
            branch=self.branch,
            write_mode=self.write_mode,
            items_written=tuple(self._written_rids),
        )

    @property
    def written_rids(self) -> Iterable[str]:
        return tuple(self._written_rids)


__all__ = ["MediaSetInput", "MediaSetOutput", "WriteMode", "ReadMode"]
