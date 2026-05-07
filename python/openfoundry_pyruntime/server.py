"""gRPC server entrypoint for the Python sidecar binary."""

from __future__ import annotations

import argparse
import logging
import os
import signal
import sys
import threading
from concurrent import futures
from uuid import UUID

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc

from . import _pb  # noqa: F401 — sets sys.path for stub imports
from ._pb.runtime import python_runtime_pb2 as pb
from ._pb.runtime import python_runtime_pb2_grpc as pb_grpc
from .executors import (
    execute_inline_function,
    execute_notebook_cell,
    execute_pipeline_transform,
)
from .sessions import SessionRegistry

logger = logging.getLogger("openfoundry.pyruntime")


def _to_uuid(raw: bytes) -> UUID | None:
    if not raw:
        return None
    if len(raw) == 16:
        return UUID(bytes=raw)
    return UUID(raw.decode("utf-8"))


class PythonRuntimeServicer(pb_grpc.PythonRuntimeServiceServicer):
    def __init__(self, sessions: SessionRegistry) -> None:
        self._sessions = sessions

    def ExecuteInlineFunction(self, request, context):  # noqa: N802 — gRPC naming
        out = execute_inline_function(request.source, request.input_json)
        return pb.ExecuteInlineFunctionResponse(
            result_json=out["result_json"],
            stdout=out["stdout"],
            stderr=out["stderr"],
            error=out["error"],
        )

    def ExecutePipelineTransform(self, request, context):  # noqa: N802
        out = execute_pipeline_transform(
            request.source,
            request.config_json,
            request.prepared_inputs_json,
            list(request.input_dataset_ids),
            request.output_dataset_id,
        )
        return pb.ExecutePipelineTransformResponse(
            rows_affected=out["rows_affected"],
            rows_affected_set=out["rows_affected_set"],
            output_json=out["output_json"],
            result_rows_json=out["result_rows_json"],
            stdout=out["stdout"],
            error=out["error"],
        )

    def ExecuteNotebookCell(self, request, context):  # noqa: N802
        out = execute_notebook_cell(
            self._sessions,
            _to_uuid(request.session_id),
            request.source,
            request.workspace_dir,
            _to_uuid(request.notebook_id),
        )
        return pb.ExecuteNotebookCellResponse(
            output_type=out["output_type"],
            content_json=out["content_json"],
            stdout=out["stdout"],
            error=out["error"],
        )

    def EnsureSession(self, request, context):  # noqa: N802
        sid = _to_uuid(request.session_id)
        if sid is not None:
            self._sessions.ensure(sid)
        return pb.EnsureSessionResponse()

    def DropSession(self, request, context):  # noqa: N802
        sid = _to_uuid(request.session_id)
        if sid is not None:
            self._sessions.drop(sid)
        return pb.DropSessionResponse()


def _serve(bind: str, max_workers: int, idle_seconds: float | None) -> None:
    sessions = SessionRegistry(max_idle_seconds=idle_seconds)
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=max_workers))
    pb_grpc.add_PythonRuntimeServiceServicer_to_server(PythonRuntimeServicer(sessions), server)
    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    health_servicer.set("", health_pb2.HealthCheckResponse.SERVING)
    health_servicer.set(pb_grpc.PythonRuntimeService.__name__, health_pb2.HealthCheckResponse.SERVING)
    server.add_insecure_port(bind)
    server.start()
    logger.info("openfoundry-pyruntime listening on %s", bind)

    stop_event = threading.Event()

    def _shutdown(*_args: object) -> None:
        logger.info("openfoundry-pyruntime shutting down")
        stop_event.set()

    signal.signal(signal.SIGTERM, _shutdown)
    signal.signal(signal.SIGINT, _shutdown)
    try:
        stop_event.wait()
    finally:
        server.stop(grace=5).wait()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="openfoundry-pyruntime")
    parser.add_argument(
        "--bind",
        help="gRPC bind target. Use unix:/path/to/sock for UDS or host:port for TCP.",
        required=True,
    )
    parser.add_argument("--max-workers", type=int, default=8)
    parser.add_argument(
        "--session-idle-seconds",
        type=float,
        default=None,
        help="Evict notebook sessions idle longer than this. Default: never.",
    )
    parser.add_argument("--log-level", default=os.environ.get("PYRUNTIME_LOG", "INFO"))
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=args.log_level.upper(),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    _serve(args.bind, args.max_workers, args.session_idle_seconds)
    return 0


if __name__ == "__main__":
    sys.exit(main())
