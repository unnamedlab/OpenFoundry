# CLAUDE.md — apps/web

OpenFoundry's frontend. **React 19 + Vite + TypeScript.** Despite what
`ARCHITECTURE.md` says, this is **not** SvelteKit.

## Stack

- React 19 with `react-router-dom` v7 (lazy-loaded routes).
- TanStack Query for server state (`@tanstack/react-query`).
- Tailwind CSS 4 (`@tailwindcss/postcss`).
- Vite 8 with `@vitejs/plugin-react-swc`.
- Vitest (unit) + Playwright (e2e).
- Heavy components: Monaco editor, Cytoscape (graphs), MapLibre, ECharts.

## Layout

```
apps/web/src/
  main.tsx           # entry + QueryClient + RouterProvider
  router.tsx         # createBrowserRouter — every route is `lazy`
  routes/<area>/     # one folder per route area (~30 areas, 112 .tsx files)
  lib/
    api/             # one .ts per backend (calls fetch / handles auth)
    auth/            # auth helpers (token, refresh, session)
    components/      # shared UI (AppShell, AuthLayout, drawers, …)
    stores/          # client state (auth store, etc.)
    i18n/            # locale store + restoreLocale()
    utils/           # cross-cutting helpers
  styles/app.css
  types/             # shared TS types (mirror Go wire shapes)
```

## Where to look first

| Task | Open this |
|---|---|
| Add or change a route | `router.tsx` + new `routes/<area>/<Page>.tsx` |
| Call a backend endpoint | extend the matching `lib/api/<area>.ts` |
| Add a shared widget | `lib/components/` |
| Auth-gated UI | `lib/auth/` + use the `auth` store from `lib/stores/auth` |

## Files to handle with care

| File | Lines | Why |
|---|---:|---|
| `routes/apps/WorkshopEditorPage.tsx` | 4699 | Workshop builder; navigate by `grep -n` |
| `routes/action-types/ActionTypesPage.tsx` | 2428 | Action-type editor |
| `routes/projects/ProjectDetailPage.tsx` | 2425 | Project detail |
| `lib/api/ontology.ts` | 2360 | Monolithic ontology client; one of the biggest `lib/api/*` files |
| `routes/lineage/LineagePage.tsx` | 2252 | Lineage graph |
| `lib/api/datasets.ts` | 1347 | Datasets client |

When you change something in a giant page, prefer extracting a hook or
sub-component **only if** the task already touches the relevant
section. Don't open a refactor PR alongside a feature PR.

## Conventions

- **Routes are lazy-loaded.** Every route definition uses
  `lazy: async () => ({ Component: (await import('...')).XxxPage })`.
  Match this pattern when adding new routes — otherwise initial bundle
  size regresses.
- **Server state lives in TanStack Query.** Don't mirror it in
  `useState`/Zustand stores. Use `useQuery`/`useMutation` and rely on
  the cache.
- **Path aliases**: `@components/...` is wired in `tsconfig`/Vite.
  Keep using aliases for shared paths.
- **API clients** in `lib/api/<area>.ts` return typed responses.
  Mirror Go wire shapes from `services/<svc>` exactly — there are
  Playwright tests that pin a few of them.
- **Locale:** call `restoreLocale()` early, then use the i18n store —
  don't read `localStorage` in components.

## Commands

```sh
pnpm --filter @open-foundry/web dev     # vite dev server
pnpm --filter @open-foundry/web build   # tsc -b && vite build
pnpm --filter @open-foundry/web check   # tsc -b --noEmit (typecheck only)
pnpm --filter @open-foundry/web lint    # eslint .
pnpm --filter @open-foundry/web test    # vitest run (unit)
pnpm --filter @open-foundry/web test:e2e  # playwright (needs the backend running)
```

For UI changes, **start the dev server and exercise the feature in a
browser** before reporting done — typecheck and unit tests are not
enough to catch UX regressions in a 4699-line component.

## Don't

- Don't add Redux / new global state libs. We have TanStack Query +
  small Zustand-style stores in `lib/stores/`.
- Don't introduce a new CSS framework. Tailwind is the only one.
- Don't fetch from `useEffect` for new code; use TanStack Query.
- Don't bypass `lib/api/<area>.ts` — calling `fetch` directly skips
  auth-token refresh and error normalization.
