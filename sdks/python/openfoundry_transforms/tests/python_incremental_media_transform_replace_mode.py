"""H7 — incremental Python transform contract.

Exercises the Foundry-doc contract of `Incremental media sets.md`
end-to-end against the in-memory backend:

  1. Seed an input set with one path.
  2. Run an incremental transform that copies the input into an
     output set with `write_mode = "replace"`. Build #1 sees one
     "added" item.
  3. Add a second input path.
  4. Run again. The "added" listing now surfaces only the new path
     (the doc's contract: incremental + replace). After the commit,
     `replace` soft-deletes everything in the output the transform
     did NOT write — so the output's live view is exactly the items
     written in build #2.
  5. Sister rejection: ``replace`` is illegal on transactionless
     sets — the SDK raises `MediaSetWriteModeError` at commit time.

Invoked via:
    pytest sdks/python/openfoundry_transforms/tests/

(Run only manually; the H7 closure documents this as the canonical
Python-side gate.)
"""

from __future__ import annotations

try:
    import pytest
except ImportError:  # pragma: no cover - vendored fallback for hermetic CI
    # Tiny pytest shim so this file can run on an interpreter that
    # does not have pytest installed (the H7 closure run on dev
    # macOS where the homebrew Python is bare). Real CI always has
    # pytest; this fallback keeps `python tests/...py` working.
    class _RaisesContext:
        def __init__(self, expected: type[BaseException]):
            self.expected = expected
            self.value: BaseException | None = None

        def __enter__(self) -> "_RaisesContext":
            return self

        def __exit__(self, exc_type, exc, tb) -> bool:
            if exc_type is None:
                raise AssertionError(f"expected {self.expected.__name__}")
            self.value = exc
            return issubclass(exc_type, self.expected)

    class _PytestShim:
        @staticmethod
        def raises(expected: type[BaseException]) -> _RaisesContext:
            return _RaisesContext(expected)

    pytest = _PytestShim()  # type: ignore[assignment]

from openfoundry_transforms import (
    InMemoryMediaSetBackend,
    MediaItemRow,
    MediaSetInput,
    MediaSetOutput,
    MediaSetWriteModeError,
    incremental,
    transform,
)


SOURCE_RID = "ri.foundry.main.media_set.source"
SINK_RID = "ri.foundry.main.media_set.sink"


def _make_backend(*, sink_policy: str = "TRANSACTIONAL") -> InMemoryMediaSetBackend:
    backend = InMemoryMediaSetBackend()
    backend.register_set(SOURCE_RID, transaction_policy="TRANSACTIONLESS")
    backend.register_set(SINK_RID, transaction_policy=sink_policy)
    return backend


def _seed(backend: InMemoryMediaSetBackend, *, set_rid: str, path: str, sha: str) -> MediaItemRow:
    item = MediaItemRow(
        rid=f"ri.foundry.main.media_item.seed-{path}",
        media_set_rid=set_rid,
        branch="main",
        path=path,
        mime_type="image/png",
        size_bytes=2048,
        sha256=sha,
        storage_uri=f"s3://media/{set_rid}/main/{sha}",
    )
    backend.seed_item(item)
    return item


def test_incremental_replace_mode_round_trips_added_inputs_to_a_replaced_output() -> None:
    backend = _make_backend()
    _seed(backend, set_rid=SOURCE_RID, path="raw/a.png", sha="a" * 64)

    @incremental()
    @transform(
        images=MediaSetInput(SOURCE_RID),
        output_images=MediaSetOutput(SINK_RID, write_mode="replace"),
    )
    def copy_added(images: MediaSetInput, output_images: MediaSetOutput) -> None:
        # The doc-canonical incremental shape: read added items, write
        # them to the output. `replace` makes the output a snapshot of
        # whatever the transform produced this build.
        for item in images.list_items(mode="added", deduplicate_by_path=False):
            output_images.upload(
                path=item.path,
                mime_type=item.mime_type,
                size_bytes=item.size_bytes,
                sha256=item.sha256,
            )

    # Build #1 — first run, "added" returns the seed item.
    copy_added.run(backend)  # type: ignore[attr-defined]
    sink = backend.list_items(SINK_RID)
    assert [r.path for r in sink] == ["raw/a.png"]

    # Add a second input path; build #2 surfaces only the NEW one
    # via the "added" listing because the previous snapshot now
    # contains "raw/a.png".
    _seed(backend, set_rid=SOURCE_RID, path="raw/b.png", sha="b" * 64)
    copy_added.run(backend)  # type: ignore[attr-defined]

    # Output's live view is `replace`-d — only the items written in
    # build #2 are live. `raw/a.png` from build #1 is soft-deleted.
    live_sink = backend.list_items(SINK_RID)
    assert [r.path for r in live_sink] == ["raw/b.png"], (
        "replace must soft-delete previously-written items the transform "
        "did not touch in this build"
    )


def test_replace_on_transactionless_sink_raises_write_mode_error() -> None:
    backend = _make_backend(sink_policy="TRANSACTIONLESS")
    _seed(backend, set_rid=SOURCE_RID, path="raw/a.png", sha="a" * 64)

    @transform(
        images=MediaSetInput(SOURCE_RID),
        output_images=MediaSetOutput(SINK_RID, write_mode="replace"),
    )
    def copy(images: MediaSetInput, output_images: MediaSetOutput) -> None:
        for item in images.list_items(mode="current", deduplicate_by_path=False):
            output_images.upload(
                path=item.path,
                mime_type=item.mime_type,
                size_bytes=item.size_bytes,
                sha256=item.sha256,
            )

    with pytest.raises(MediaSetWriteModeError):
        copy.run(backend)  # type: ignore[attr-defined]


def test_set_write_mode_can_flip_to_modify_at_runtime() -> None:
    """The doc shows operators flipping write_mode mid-transform when
    they detect that the build cannot run incrementally. Pinning the
    contract here so the runtime + the doc stay in lockstep."""
    backend = _make_backend(sink_policy="TRANSACTIONLESS")
    _seed(backend, set_rid=SOURCE_RID, path="raw/a.png", sha="a" * 64)

    @transform(
        images=MediaSetInput(SOURCE_RID),
        output_images=MediaSetOutput(SINK_RID, write_mode="replace"),
    )
    def copy(images: MediaSetInput, output_images: MediaSetOutput) -> None:
        # A real transform would inspect the input via
        # `mode="previous"` to decide; here we hard-code the flip.
        output_images.set_write_mode("modify")
        for item in images.list_items(mode="current", deduplicate_by_path=False):
            output_images.upload(
                path=item.path,
                mime_type=item.mime_type,
                size_bytes=item.size_bytes,
                sha256=item.sha256,
            )

    copy.run(backend)  # type: ignore[attr-defined]
    sink = backend.list_items(SINK_RID)
    assert [r.path for r in sink] == ["raw/a.png"]


if __name__ == "__main__":  # pragma: no cover - hermetic shim entrypoint
    # Plain `python tests/...py` run path: pytest collection is bypassed,
    # so we hand-call each test_* function and surface failures with a
    # non-zero exit code. CI uses pytest proper.
    import sys
    import traceback

    failures: list[tuple[str, BaseException]] = []
    for _name, _fn in sorted(globals().items()):
        if _name.startswith("test_") and callable(_fn):
            try:
                _fn()
                print(f"PASS {_name}")
            except BaseException as exc:  # noqa: BLE001
                traceback.print_exc()
                failures.append((_name, exc))
    if failures:
        print(f"\n{len(failures)} failure(s):")
        for name, exc in failures:
            print(f"  - {name}: {exc!r}")
        sys.exit(1)
