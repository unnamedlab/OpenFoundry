# internal/executor — Function runtime executors

This package owns "given a published FunctionVersion + JSON input,
return a Result". Two stubs ship today; both are intentionally minimal
and gated for replacement in v1.

## v0 — what's here

| Runtime | Implementation | Isolation |
|---|---|---|
| `ts` | `TSStubExecutor` launches `node <script>` directly | env stripped to `PATH`/`LANG`/`OF_*`; per-run timeout via `context.WithTimeout`; on linux a hint env (`NODE_OPTIONS=--max-old-space-size`) plus `Setpgid` + `Pdeathsig` for reap-on-parent-death |
| `python` | `PythonStubExecutor` launches `python3 <script>` directly | same env / timeout discipline; no Python-side memory hint yet |

The shared driver `runScript` is in [executor.go](./executor.go); the
build-tag-gated rlimit hook is in [rlimit_linux.go](./rlimit_linux.go)
(other OSes get a no-op via [rlimit_other.go](./rlimit_other.go)).

When the runtime binary is not on `$PATH`, both executors return
`executor.ErrNotImplemented` so the HTTP layer can map cleanly to
`501 Not Implemented` instead of conflating with a user-side failure.

## Source URI resolution

`materialise(sourceURI)` accepts:

- `file:///abs/path` — used in place; no copy, no cleanup.
- `/abs/path` or `./relative` — used in place.
- `inline:<body>` — written to an `os.CreateTemp` file; deleted on
  return. Tests use this; production callers should never see it once
  v1 wires code-repository-service.

Anything else (`http://`, `https://`, `code-repo://`) returns
`ErrNotImplemented` — v1 will fetch the blob from
`code-repository-service` instead of expecting a pre-resolved path.

## What needs to change for v1

This is the explicit TODO list. None of the bullets below are
blockers for v0 wiring; they are the reasons we keep `ErrNotImplemented`
prominently in scope.

1. **Real TS isolation.** `node` shares the host filesystem, network,
   and process table. Replacements (in preference order):
   - `deno run --allow-none` with explicit allow-lists for env/net/read.
   - `isolated-vm` worker pool (long-running node host, untrusted code
     in a separate v8 isolate).
   - `v8go` embedded directly in the Go process (eliminates the
     subprocess hop entirely).
2. **Real Python isolation.** `python3 -c '…'` is even thinner. Path
   forward:
   - `subinterpreter`-per-call (PEP 684) when 3.13+ is acceptable.
   - WASM (`wasmtime` + `py-wasi` / `cpython-wasm`) for hard isolation.
3. **Real memory limits.** The current linux hook only sets a hint env
   var; it does **not** call `prlimit(2)` / `setrlimit(RLIMIT_AS,…)`
   on the child. Wiring that needs a small `pre_exec` shim (a wrapper
   binary or a `forkAndExec` patch) since Go's `syscall.SysProcAttr`
   does not expose rlimit setters directly.
4. **Source fetch via code-repository-service.** `materialise` should
   accept a `code-repo://<rid>@<version>` URI and call the upstream
   blob endpoint with the service-account JWT.
5. **Sandboxed filesystem.** Today the script can read any path the
   service user can. v1 must scope it to a per-run `tmpfs` mount.
6. **Resource-budget reporting.** `Result` should carry the actual
   peak RSS / CPU-seconds so the run row stores something more
   useful than wall-clock duration.

Until those land, the executor is fit for trusted-developer demos and
the CI loop only.
