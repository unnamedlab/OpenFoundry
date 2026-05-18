import { resourceRIDForKind } from './resourceTypeRegistry';

interface ProjectURLResource {
  id: string;
  rid?: string | null;
  slug?: string | null;
  display_name?: string | null;
}

interface FolderURLResource {
  id: string;
  rid?: string | null;
  slug?: string | null;
  name?: string | null;
}

export function stableResourceSlug(label: string | null | undefined): string {
  return (label ?? '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 80);
}

export function stableRIDSegment(rid: string, label?: string | null): string {
  const slug = stableResourceSlug(label);
  return encodeURIComponent(slug ? `${rid}--${slug}` : rid);
}

export function resourceIDFromStableSegment(value: string | null | undefined): string {
  const decoded = safeDecodeURIComponent(value ?? '').trim();
  if (!decoded.startsWith('ri.')) return decoded;
  const slugMarker = decoded.indexOf('--');
  return slugMarker >= 0 ? decoded.slice(0, slugMarker) : decoded;
}

function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export function resourceLocatorFromStableSegment(value: string | null | undefined): string {
  const resourceID = resourceIDFromStableSegment(value);
  if (!resourceID.startsWith('ri.')) return resourceID;
  const parts = resourceID.split('.');
  return parts.length >= 5 ? parts.slice(4).join('.') : resourceID;
}

export function projectStableRID(project: ProjectURLResource): string {
  return project.rid || resourceRIDForKind('ontology_project', project.id);
}

export function folderStableRID(folder: FolderURLResource): string {
  return folder.rid || resourceRIDForKind('ontology_folder', folder.id);
}

export function projectStablePath(project: ProjectURLResource): string {
  return `/projects/${stableRIDSegment(projectStableRID(project), project.display_name || project.slug)}`;
}

export function folderStablePath(project: ProjectURLResource, folder: FolderURLResource): string {
  return `${projectStablePath(project)}/folders/${stableRIDSegment(folderStableRID(folder), folder.name || folder.slug)}`;
}

export function workspaceResourceStablePath(
  kind: string,
  idOrRID: string,
  label?: string | null,
): string {
  const rid = idOrRID.startsWith('ri.') ? idOrRID : resourceRIDForKind(kind, idOrRID);
  if (kind === 'ontology_project' || kind === 'project') {
    return `/projects/${stableRIDSegment(rid, label)}`;
  }
  if (kind === 'ontology_folder' || kind === 'folder') {
    return `/projects?folder_rid=${encodeURIComponent(resourceIDFromStableSegment(rid))}`;
  }
  if (kind === 'dataset') return `/datasets/${stableRIDSegment(rid, label)}`;
  if (kind === 'pipeline') return `/pipelines/${stableRIDSegment(rid, label)}`;
  if (kind === 'query') return `/queries/${stableRIDSegment(rid, label)}`;
  if (kind === 'notebook') return `/notebooks/${stableRIDSegment(rid, label)}`;
  if (kind === 'app') return `/apps/${stableRIDSegment(rid, label)}`;
  if (kind === 'dashboard') return `/dashboards/${stableRIDSegment(rid, label)}`;
  if (kind === 'report') return `/reports/${stableRIDSegment(rid, label)}`;
  if (kind === 'model') return `/ml?model=${encodeURIComponent(rid)}`;
  if (kind === 'workflow') return `/workflows/${stableRIDSegment(rid, label)}`;
  return `/search?q=${encodeURIComponent(rid)}`;
}
