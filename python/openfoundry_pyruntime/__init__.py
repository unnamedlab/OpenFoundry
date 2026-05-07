"""openfoundry_pyruntime — Python sidecar that replaces Rust's pyo3.

Three Go services own the lifecycle of one of these processes and drive
it via gRPC over a Unix domain socket (or loopback TCP). The sidecar
binary is a single entrypoint that exposes:

    rpc ExecuteInlineFunction      — ontology inline functions
    rpc ExecutePipelineTransform   — pipeline node Python transforms
    rpc ExecuteNotebookCell        — notebook cell execution (stateful)
    rpc EnsureSession / DropSession
    grpc.health.v1.Health (standard)

See proto/runtime/python_runtime.proto for the wire contract.
"""

__all__ = ("__version__",)
__version__ = "0.1.0"
