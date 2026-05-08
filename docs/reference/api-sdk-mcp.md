# API, SDK, and MCP

OpenFoundry treats contracts and client surfaces as generated platform assets,
not hand-maintained side projects. The current generator is the Go CLI under
[`tools/of-cli`](../../tools/of-cli); older `cargo run -p of-cli` examples are
migration-era references and do not match the active source tree.

## Source of Truth

The repository uses `go run ./tools/of-cli` to generate and validate these
outputs:

- OpenAPI
- TypeScript SDK
- Python SDK
- Java SDK
- Terraform provider schema metadata (checked-in JSON artifact; validated by infra CI)

## Generated Artifact Locations

| Artifact | Location |
| --- | --- |
| OpenAPI contract | `apps/web/public/generated/openapi/openfoundry.json` |
| TypeScript SDK | `sdks/typescript/openfoundry-sdk` |
| Python SDK | `sdks/python/openfoundry-sdk` |
| Java SDK | `sdks/java/openfoundry-sdk` |
| Terraform provider schema | `infra/terraform/providers/openfoundry/provider.schema.json` |
| Frontend Terraform schema | `apps/web/public/generated/terraform/openfoundry-provider.json` |

## Core Recipes

Run these commands from the repository root. They mirror the active Go CLI
argument parser in `tools/of-cli/main.go`.

```bash
go run ./tools/of-cli docs generate-openapi \
  --proto-dir proto \
  --output apps/web/public/generated/openapi/openfoundry.json

go run ./tools/of-cli docs validate-openapi \
  --proto-dir proto \
  --expected apps/web/public/generated/openapi/openfoundry.json

go run ./tools/of-cli docs generate-sdk-typescript \
  --input apps/web/public/generated/openapi/openfoundry.json \
  --output sdks/typescript/openfoundry-sdk

go run ./tools/of-cli docs validate-sdk-typescript \
  --input apps/web/public/generated/openapi/openfoundry.json \
  --output sdks/typescript/openfoundry-sdk

go run ./tools/of-cli docs generate-sdk-python \
  --input apps/web/public/generated/openapi/openfoundry.json \
  --output sdks/python/openfoundry-sdk

go run ./tools/of-cli docs validate-sdk-python \
  --input apps/web/public/generated/openapi/openfoundry.json \
  --output sdks/python/openfoundry-sdk

go run ./tools/of-cli docs generate-sdk-java \
  --input apps/web/public/generated/openapi/openfoundry.json \
  --output sdks/java/openfoundry-sdk

go run ./tools/of-cli docs validate-sdk-java \
  --input apps/web/public/generated/openapi/openfoundry.json \
  --output sdks/java/openfoundry-sdk
```

Type-check or compile generated SDKs with the native toolchains after
regeneration:

```bash
pnpm --dir apps/web exec tsc \
  -p ../../sdks/typescript/openfoundry-sdk/tsconfig.json \
  --noEmit

python3 -m compileall sdks/python/openfoundry-sdk

find sdks/java/openfoundry-sdk/src/main/java -name '*.java' -print0 \
  | xargs -0 javac --release 17
```

Terraform provider schemas are checked-in JSON artifacts today rather than an
active `tools/of-cli` generation subcommand. Validate their syntax with the same
shape used by the Terraform workflow:

```bash
jq empty infra/terraform/providers/openfoundry/provider.schema.json
jq empty apps/web/public/generated/terraform/openfoundry-provider.json
```

## MCP Surface

The SDK layer includes MCP-oriented surfaces in both generated client stacks:

- `sdks/typescript/openfoundry-sdk/src/mcp.ts`
- `sdks/python/openfoundry-sdk/openfoundry_sdk/mcp.py`

These surfaces let the repository expose a more agent-friendly operation model
on top of the generated contract set.

## Why This Matters

Keeping generated contracts in-repo gives OpenFoundry several advantages:

- frontend and backend evolve in lockstep
- SDK changes are visible in pull requests
- CI can validate that checked-in outputs still match the generator
- external integration surfaces become part of normal platform review
