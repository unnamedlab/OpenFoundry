"""In-memory + abstract backends for ``openfoundry_transforms``.

The transforms API is purposely backend-agnostic: production wires a
backend that talks to ``media-sets-service`` over HTTP; tests use the
:class:`InMemoryMediaSetBackend` shipped here.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Iterable, Mapping

from .errors import MediaSetWriteModeError


@dataclass
class MediaItemRow:
    """Single media item — wire-form mirrors the Rust struct
    ``services/media-sets-service::models::MediaItem`` so OpenFoundry's
    Python pipelines and the Rust backend agree byte-for-byte.

    The ``transaction_rid`` is empty for transactionless writes, per
    the H4 schema (the column itself is nullable on the Postgres side
    but we surface ``""`` to preserve ergonomic comparisons).
    """

    rid: str
    media_set_rid: str
    branch: str
    path: str
    mime_type: str
    size_bytes: int
    sha256: str
    storage_uri: str
    transaction_rid: str = ""
    created_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    deleted_at: datetime | None = None
    deduplicated_from: str | None = None
    metadata: Mapping[str, object] = field(default_factory=dict)


class MediaSetBackend:
    """Abstract media-sets-service surface used by the transforms API.

    Mirrors the contract Code Repositories' ``transforms.mediasets``
    expects: list current/previous/added items (incremental modes),
    upload a new item, and replace-on-commit when the output's write
    mode is ``replace``.

    Concrete implementations:
      * :class:`InMemoryMediaSetBackend` — test fixture.
      * (production) HTTP client against ``media-sets-service`` — to
        be wired in the binary that runs the transform; not bundled
        with this SDK to keep dependencies at zero.
    """

    def list_items(
        self,
        media_set_rid: str,
        *,
        branch: str = "main",
        mode: str = "current",
        deduplicate_by_path: bool = True,
    ) -> list[MediaItemRow]:
        raise NotImplementedError

    def upload_item(
        self,
        media_set_rid: str,
        *,
        branch: str = "main",
        path: str,
        mime_type: str,
        size_bytes: int,
        sha256: str,
        bytes_: bytes | None = None,
    ) -> MediaItemRow:
        raise NotImplementedError

    def commit_write(
        self,
        media_set_rid: str,
        *,
        branch: str = "main",
        write_mode: str = "modify",
        items_written: Iterable[str] = (),
    ) -> None:
        """Apply the write-mode contract to the items the transform
        just produced. ``modify`` is a no-op (path-dedup happens at
        upload time). ``replace`` soft-deletes everything on the
        branch that the transform did NOT touch.
        """
        raise NotImplementedError


class InMemoryMediaSetBackend(MediaSetBackend):
    """Mock backend used by tests + the transforms-api unit tests.

    Tracks per-set ``items`` plus a ``previous`` snapshot the
    incremental "previous" listing serves from. Each call to
    ``commit_write`` rolls the current view into ``previous`` so the
    next build's incremental listing has something to compare
    against.
    """

    def __init__(self) -> None:
        # media_set_rid → list of currently-live MediaItemRow
        self._items: dict[str, list[MediaItemRow]] = {}
        # media_set_rid → snapshot of items BEFORE the most recent
        # commit, used to satisfy mode="previous" / mode="added".
        self._previous: dict[str, list[MediaItemRow]] = {}
        # Transaction policy per set; defaults to TRANSACTIONLESS.
        self._policies: dict[str, str] = {}

    # ── Test setup helpers ────────────────────────────────────────
    def register_set(self, media_set_rid: str, *, transaction_policy: str = "TRANSACTIONLESS") -> None:
        if transaction_policy not in {"TRANSACTIONLESS", "TRANSACTIONAL"}:
            raise ValueError(f"unknown transaction policy {transaction_policy!r}")
        self._items.setdefault(media_set_rid, [])
        self._previous.setdefault(media_set_rid, [])
        self._policies[media_set_rid] = transaction_policy

    def seed_item(self, item: MediaItemRow) -> None:
        self._items.setdefault(item.media_set_rid, []).append(item)
        self._policies.setdefault(item.media_set_rid, "TRANSACTIONLESS")

    def snapshot_set(self, media_set_rid: str) -> None:
        """Roll the live view of ``media_set_rid`` into its ``previous``
        snapshot. The runtime calls this on every input the transform
        consumed once a build is about to commit, so the next build's
        ``mode="added"`` listing reflects the post-build state.

        Production backends snapshot at the dataset-versioning layer
        (one transaction per build); this in-memory backend mirrors
        that contract verbatim so the doc-shaped incremental tests
        round-trip without a network harness.
        """
        self._previous[media_set_rid] = list(self._items.get(media_set_rid, []))

    # ── MediaSetBackend impl ──────────────────────────────────────
    def list_items(
        self,
        media_set_rid: str,
        *,
        branch: str = "main",
        mode: str = "current",
        deduplicate_by_path: bool = True,
    ) -> list[MediaItemRow]:
        current = [
            i
            for i in self._items.get(media_set_rid, [])
            if i.branch == branch and i.deleted_at is None
        ]
        previous = [
            i
            for i in self._previous.get(media_set_rid, [])
            if i.branch == branch and i.deleted_at is None
        ]
        if mode == "current":
            rows = current
        elif mode == "previous":
            rows = previous
        elif mode == "added":
            previous_paths = {i.path for i in previous}
            rows = [i for i in current if i.path not in previous_paths]
        else:
            raise ValueError(f"unknown listing mode {mode!r}")
        if deduplicate_by_path:
            seen: set[str] = set()
            keep: list[MediaItemRow] = []
            for row in sorted(rows, key=lambda r: r.created_at, reverse=True):
                if row.path in seen:
                    continue
                seen.add(row.path)
                keep.append(row)
            keep.sort(key=lambda r: r.created_at)
            return keep
        return rows

    def upload_item(
        self,
        media_set_rid: str,
        *,
        branch: str = "main",
        path: str,
        mime_type: str,
        size_bytes: int,
        sha256: str,
        bytes_: bytes | None = None,
    ) -> MediaItemRow:
        # Path-dedup: soft-delete the existing item at this path.
        live = self._items.setdefault(media_set_rid, [])
        now = datetime.now(timezone.utc)
        replaced: str | None = None
        for prior in live:
            if prior.branch == branch and prior.path == path and prior.deleted_at is None:
                prior.deleted_at = now
                replaced = prior.rid
                break
        new_rid = f"ri.foundry.main.media_item.{media_set_rid[-12:]}-{path.replace('/', '_')}-{len(live):04x}"
        item = MediaItemRow(
            rid=new_rid,
            media_set_rid=media_set_rid,
            branch=branch,
            path=path,
            mime_type=mime_type,
            size_bytes=size_bytes,
            sha256=sha256,
            storage_uri=f"s3://media/{media_set_rid}/{branch}/{sha256}",
            created_at=now,
            deduplicated_from=replaced,
        )
        live.append(item)
        return item

    def commit_write(
        self,
        media_set_rid: str,
        *,
        branch: str = "main",
        write_mode: str = "modify",
        items_written: Iterable[str] = (),
    ) -> None:
        if write_mode not in {"modify", "replace"}:
            raise ValueError(f"unknown write_mode {write_mode!r}")

        policy = self._policies.get(media_set_rid, "TRANSACTIONLESS")
        if write_mode == "replace" and policy != "TRANSACTIONAL":
            raise MediaSetWriteModeError(
                f"replace write_mode is not allowed on transactionless media set {media_set_rid!r}",
            )

        # Roll the pre-commit snapshot into `previous` so the next
        # incremental "previous" / "added" listing has a baseline.
        # Take the snapshot BEFORE we apply the replace pass so the
        # baseline reflects the state the transform read from.
        self._previous[media_set_rid] = list(self._items.get(media_set_rid, []))

        if write_mode == "replace":
            now = datetime.now(timezone.utc)
            written = set(items_written)
            for row in self._items.get(media_set_rid, []):
                if row.rid not in written and row.deleted_at is None:
                    row.deleted_at = now
