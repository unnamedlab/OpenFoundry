# of-sdk-gen

Thin orchestrator that drives a language-specific OpenAPI client
generator as a subprocess. Inputs are the `internal/openapi/openapi.yaml`
specs committed per service; outputs are the generated client trees
that `services/sdk-generation-service` zips up and serves over
`POST /api/v1/sdk/generate`.

The Go binary itself emits zero source code — the heavy lifting is
delegated to:

- TypeScript: `npx --yes openapi-typescript-codegen --input <spec> --output <out> --client fetch`
- Python: `openapi-python-client generate --path <spec> --output-path <out> --overwrite`

Both tools must be present on `PATH`. We keep them out of `go.mod`
intentionally: Node and Python toolchains live in CI as system tools,
not as Go modules.

## Usage

```sh
go build -o bin/of-sdk-gen ./tools/of-sdk-gen
bin/of-sdk-gen --service audit-compliance-service --lang ts  --out /tmp/audit-ts-sdk
bin/of-sdk-gen --service notification-alerting-service --lang py --out /tmp/notif-py-sdk
```

Flags:

| Flag           | Required | Default | Purpose                                                     |
| -------------- | :------: | ------- | ----------------------------------------------------------- |
| `--service`    | ✱        |         | service dir under `services/` (looks up the YAML spec)      |
| `--spec`       | ✱        |         | explicit path to the OpenAPI YAML (overrides `--service`)   |
| `--lang`       | ✅       |         | `ts` or `py`                                                |
| `--out`        | ✅       |         | output directory (created if missing)                       |
| `--repo-root`  |          | walk up | repository root (used to resolve `--service`)               |
| `--client`     |          | `fetch` | TypeScript client kind                                      |

✱ one of `--service` or `--spec` is required.

## CI requirement

Both generators must be installed before running
`go test -tags=integration ./services/sdk-generation-service/...`:

```sh
node --version          # 18+
npx --yes openapi-typescript-codegen --version
pip install openapi-python-client
openapi-python-client --version
```
