# @open-foundry/web

OpenFoundry's React 19 + Vite + TypeScript frontend. See
[`CLAUDE.md`](./CLAUDE.md) for the agent-facing tour of the stack,
layout, conventions, and commands.

## Environment

Vite reads configuration from `.env` / `.env.local` (gitignored) at
build/dev time. Copy `.env.example` to get started:

```sh
cp .env.example .env.local
```

Supported variables:

| Name | Default | Purpose |
|---|---|---|
| `VITE_API_BASE_URL` | `/api/v1` | Base URL the API client prefixes onto every request. Override to point the dev server at a non-default backend (e.g. `https://staging.example.com/api/v1`). |
