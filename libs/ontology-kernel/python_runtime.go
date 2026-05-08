// Python sidecar interface (decoupled from the concrete python-sidecar
// implementation so the kernel can be unit-tested without spawning a
// subprocess).
//
// libs/python-sidecar provides the canonical implementation backed by
// the openfoundry-pyruntime gRPC binary. Tests inject a fake.

package ontologykernel

import "context"

// PythonInlineRuntime is the slice of the python sidecar contract the
// ontology-kernel needs. The full client surface lives in
// libs/python-sidecar; this interface keeps the kernel free of a
// subprocess dependency at compile time.
type PythonInlineRuntime interface {
	// ExecuteInline runs the user's Python source against the JSON
	// envelope. Implementations must honour ``timeoutSeconds`` (0 = no
	// timeout). The returned JSON is the full enriched payload the
	// kernel re-emits to its caller.
	ExecuteInline(ctx context.Context, source string, inputJSON []byte, timeoutSeconds uint32) (*InlineRuntimeResult, error)
}

// InlineRuntimeResult is what the sidecar returns. Mirrors the Rust
// path's PyResult<Value>: a JSON payload plus captured stdout/stderr.
type InlineRuntimeResult struct {
	ResultJSON []byte
	Stdout     string
	Stderr     string
}
