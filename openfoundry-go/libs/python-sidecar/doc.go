// Package pythonsidecar manages a Python subprocess that exposes the
// runtime gRPC service defined in proto/runtime/python_runtime.proto.
//
// Three Go services rely on this lib to replace the Rust pyo3 embedded
// interpreter without losing parity:
//
//   - libs/ontology-kernel    — inline Python ontology functions
//   - pipeline-build-service  — Python pipeline transforms
//   - notebook-runtime-service — notebook cell execution (stateful)
//
// Lifecycle: callers construct a [Manager] with [New], call Start to
// spawn the sidecar (the manager picks a Unix socket under TempDir and
// blocks until the gRPC health check reports SERVING), pass Client() to
// downstream code, and call Stop on shutdown.
//
// The manager owns the gRPC connection. It restarts the sidecar with
// exponential backoff after three consecutive failed health probes.
package pythonsidecar
