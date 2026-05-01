// Lightweight in-memory cache that resolves resource labels by
// (kind, id). Used to replace `${kind} · ${id.slice(0,8)}…` placeholders
// in trash, share, and bulk-action UIs without a dedicated cross-service
// resolver endpoint (which doesn't exist yet on the backend).
//
// For ontology projects/folders we hit the existing endpoints; for any
// other kind we currently fall back to the placeholder. When a real
// resource-resolver service ships, swap the per-kind branches below.

import type { ResourceKind } from '$lib/api/workspace';
import {
  getProject,
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

async function resolveOne(kind: ResourceKind, id: string): Promise<string> {
  if (kind === 'ontology_project') {
    const project: OntologyProject = await getProject(id);
    return project.display_name || project.slug;
  }
  if (kind === 'ontology_folder') {
    // No direct GET /folders/{id} on the client; folder belongs to a
    // project, but we don't know which without an extra endpoint.
    // Skip for now and let callers seed the cache via `setLabel` when
    // they already have the folder loaded (e.g., projects/[projectId]).
    return placeholder(kind, id);
  }
  return placeholder(kind, id);
}

export async function resolveLabel(kind: ResourceKind, id: string): Promise<string> {
  const k = key(kind, id);
  const hit = cache.get(k);
  if (hit) return hit.label;

  const pending = inflight.get(k);
  if (pending) return pending;

  const promise = resolveOne(kind, id)
    .then((label) => {
      cache.set(k, { label, loadedAt: Date.now() });
      inflight.delete(k);
      return label;
    })
    .catch(() => {
      inflight.delete(k);
      return placeholder(kind, id);
    });
  inflight.set(k, promise);
  return promise;
}

export async function resolveLabels(
  targets: Array<{ kind: ResourceKind; id: string }>,
): Promise<Map<string, string>> {
  const results = await Promise.all(
    targets.map(async (t) => [key(t.kind, t.id), await resolveLabel(t.kind, t.id)] as const),
  );
  return new Map(results);
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
