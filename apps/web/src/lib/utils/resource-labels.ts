// Lightweight in-memory cache that resolves resource labels by
// (kind, id). Used to replace `${kind} · ${id.slice(0,8)}…` placeholders
// in trash, share, and bulk-action UIs.
//
// Backed by `POST /workspace/resources/resolve` which can resolve
// ontology projects/folders server-side in a single round-trip.
// Other kinds fall back to the placeholder until the backend grows
// resolvers for them (datasets, pipelines, notebooks, …).

import type { ResourceKind } from '$lib/api/workspace';
import { resolveResourceLabels } from '$lib/api/workspace';
import {
  listProjectFolders,
  type OntologyProject,
} from '$lib/api/ontology';

type LabelEntry = { label: string; loadedAt: number };

const cache = new Map<string, LabelEntry>();
const inflight = new Map<string, Promise<string>>();

function key(kind: ResourceKind, id: string) {
  return `${kind}:${id}`;
}

function placeholder(kind: ResourceKind, id: string) {
  return `${kind} · ${id.slice(0, 8)}…`;
}

export function cachedLabel(kind: ResourceKind, id: string): string {
  return cache.get(key(kind, id))?.label ?? placeholder(kind, id);
}

export function setLabel(kind: ResourceKind, id: string, label: string) {
  cache.set(key(kind, id), { label, loadedAt: Date.now() });
}

async function resolveBatch(
  targets: Array<{ kind: ResourceKind; id: string }>,
): Promise<Map<string, string>> {
  if (targets.length === 0) return new Map();
  try {
    const response = await resolveResourceLabels(
      targets.map((t) => ({ resource_kind: t.kind, resource_id: t.id })),
    );
    const out = new Map<string, string>();
    for (const entry of response.data) {
      const k = key(entry.resource_kind, entry.resource_id);
      const label = entry.resolved && entry.label
        ? entry.label
        : placeholder(entry.resource_kind, entry.resource_id);
      out.set(k, label);
      cache.set(k, { label, loadedAt: Date.now() });
    }
    return out;
  } catch {
    // Backend unreachable — fall back to placeholders so callers don't
    // hang. Retry naturally happens on the next access (we don't seed
    // the cache with placeholders).
    const fallback = new Map<string, string>();
    for (const t of targets) fallback.set(key(t.kind, t.id), placeholder(t.kind, t.id));
    return fallback;
  }
}

export async function resolveLabel(kind: ResourceKind, id: string): Promise<string> {
  const k = key(kind, id);
  const hit = cache.get(k);
  if (hit) return hit.label;

  const pending = inflight.get(k);
  if (pending) return pending;

  const promise = resolveBatch([{ kind, id }])
    .then((map) => map.get(k) ?? placeholder(kind, id))
    .finally(() => {
      inflight.delete(k);
    });
  inflight.set(k, promise);
  return promise;
}

export async function resolveLabels(
  targets: Array<{ kind: ResourceKind; id: string }>,
): Promise<Map<string, string>> {
  // Split cached vs. pending vs. cold so we issue at most one batch
  // request per call and reuse in-flight requests where possible.
  const result = new Map<string, string>();
  const cold: Array<{ kind: ResourceKind; id: string }> = [];
  const awaitingInflight: Array<Promise<void>> = [];

  for (const t of targets) {
    const k = key(t.kind, t.id);
    const hit = cache.get(k);
    if (hit) {
      result.set(k, hit.label);
      continue;
    }
    const pending = inflight.get(k);
    if (pending) {
      awaitingInflight.push(
        pending.then((label) => {
          result.set(k, label);
        }),
      );
      continue;
    }
    cold.push(t);
  }

  if (cold.length > 0) {
    // Register an inflight promise per cold entry so a concurrent
    // resolveLabel for the same id deduplicates onto this batch.
    const batchPromise = resolveBatch(cold);
    for (const t of cold) {
      const k = key(t.kind, t.id);
      const perEntry = batchPromise
        .then((m) => m.get(k) ?? placeholder(t.kind, t.id))
        .finally(() => {
          inflight.delete(k);
        });
      inflight.set(k, perEntry);
      awaitingInflight.push(
        perEntry.then((label) => {
          result.set(k, label);
        }),
      );
    }
  }

  await Promise.all(awaitingInflight);
  return result;
}

// Optional helper for callers that already have a project's folder list
// at hand: seed the cache so subsequent UI lookups are instant.
export function seedFromProject(project: OntologyProject) {
  setLabel('ontology_project', project.id, project.display_name || project.slug);
}

export async function seedFromProjectFolders(projectId: string) {
  try {
    const folders = await listProjectFolders(projectId);
    for (const folder of folders) {
      setLabel('ontology_folder', folder.id, folder.name);
    }
  } catch {
    // best-effort; placeholders remain
  }
}
