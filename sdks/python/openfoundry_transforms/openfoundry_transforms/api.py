"""``transforms.api``-shaped decorators.

Mirrors the Foundry doc's ``@transform`` and ``@incremental`` so
existing Code Repositories tutorials port over with no diff. The
decorators are intentionally minimal — the runtime drives binding +
execution; we only attach metadata the runtime reads.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Callable, Iterable

from .mediasets import MediaSetInput, MediaSetOutput


@dataclass
class TransformSpec:
    """Metadata the runtime reads off a decorated transform."""

    func: Callable[..., Any]
    inputs: dict[str, MediaSetInput]
    outputs: dict[str, MediaSetOutput]
    incremental: bool = False
    incremental_v2_semantics: bool = False


def transform(**bindings: Any) -> Callable[[Callable[..., Any]], TransformSpec]:
    """Decorator that records the inputs/outputs the function expects.

    ```python
    @transform(
        images=MediaSetInput('/examples/input_PNGs'),
        output_images=MediaSetOutput('/examples/output_PNGs'),
    )
    def translate(images, output_images):
        for item in images.list_items(mode="added"):
            ...
    ```

    The wrapped function ends up wearing a :class:`TransformSpec`
    attribute (``_openfoundry_spec``) the runtime reads to wire
    backends. Calling the spec is delegated to the original function
    so unit tests can drive it directly.
    """

    def decorate(func: Callable[..., Any]) -> TransformSpec:
        inputs = {k: v for k, v in bindings.items() if isinstance(v, MediaSetInput)}
        outputs = {k: v for k, v in bindings.items() if isinstance(v, MediaSetOutput)}
        spec = TransformSpec(func=func, inputs=inputs, outputs=outputs)
        spec.run = _make_run(spec)  # type: ignore[attr-defined]
        return spec

    return decorate


def incremental(*, v2_semantics: bool = False) -> Callable[[TransformSpec], TransformSpec]:
    """Mark a transform as incremental. Mirrors
    ``@incremental(v2_semantics=True)`` in the Foundry doc.
    """

    def decorate(spec: TransformSpec) -> TransformSpec:
        spec.incremental = True
        spec.incremental_v2_semantics = v2_semantics
        return spec

    return decorate


def _make_run(spec: TransformSpec) -> Callable[..., Any]:
    """Builds the canonical "bind then call" entry-point. Tests can
    invoke ``spec.run(backend)`` to drive the transform end-to-end
    against a backend without a runtime harness.
    """

    def run(backend: Any) -> Any:
        bound_inputs = {k: v.bind_backend(backend) for k, v in spec.inputs.items()}
        bound_outputs = {k: v.bind_backend(backend) for k, v in spec.outputs.items()}
        kwargs = {**bound_inputs, **bound_outputs}
        result = spec.func(**kwargs)
        # Auto-commit any output the transform did not commit
        # itself. Mirrors the Foundry contract: the runtime closes
        # every output transaction at the end of a successful run.
        for output in bound_outputs.values():
            output.commit()
        # Snapshot every consumed input so the next build's
        # ``mode="added"`` listing reflects the post-build baseline
        # (matches Foundry's "previous" snapshot semantics for
        # incremental transforms — inputs roll forward at commit time).
        snapshot = getattr(backend, "snapshot_set", None)
        if callable(snapshot):
            for inp in bound_inputs.values():
                snapshot(inp.media_set_rid)
        return result

    return run


def collect_outputs(spec: TransformSpec) -> Iterable[MediaSetOutput]:
    """Helper for tests / runtimes that want to inspect the output
    declarations without running the transform."""
    return tuple(spec.outputs.values())


__all__ = [
    "TransformSpec",
    "transform",
    "incremental",
    "collect_outputs",
]
