# syntax=docker/dockerfile:1.7
FROM node:24.15.0-alpine3.23 AS base
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
RUN corepack enable && pnpm config set store-dir /pnpm/store

FROM base AS deps
WORKDIR /workspace

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN --mount=type=cache,id=openfoundry-pnpm-store,target=/pnpm/store,sharing=locked \
	pnpm install --frozen-lockfile

FROM deps AS build
COPY . .
RUN --mount=type=cache,id=openfoundry-pnpm-store,target=/pnpm/store,sharing=locked \
	pnpm --filter @open-foundry/web build
RUN --mount=type=cache,id=openfoundry-pnpm-store,target=/pnpm/store,sharing=locked \
	pnpm deploy --legacy --filter @open-foundry/web --prod /prod/web

FROM node:24.15.0-alpine3.23 AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV HOST=0.0.0.0
ENV PORT=3000

COPY --from=build /prod/web/package.json ./package.json
COPY --from=build /prod/web/node_modules ./node_modules
COPY --from=build /workspace/apps/web/build ./build

EXPOSE 3000
CMD ["node", "build"]
