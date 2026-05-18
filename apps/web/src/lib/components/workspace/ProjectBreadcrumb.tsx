import { useState } from 'react';
import { Link } from 'react-router-dom';

import { folderStablePath, projectStablePath } from '@/lib/compass/stableResourceUrls';
import { resourceRIDForKind } from '@/lib/compass/resourceTypeRegistry';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';

export interface BreadcrumbItem {
  id: string;
  label: string;
  href?: string;
  rid?: string;
  icon?: GlyphName;
  kind?: 'root' | 'project' | 'folder' | 'resource';
}

interface ProjectBreadcrumbProps {
  items: BreadcrumbItem[];
  onNavigate?: (item: BreadcrumbItem, index: number) => void;
  showCopyActions?: boolean;
}

interface ProjectBreadcrumbProject {
  id: string;
  slug?: string;
  display_name?: string;
  rid?: string | null;
}

interface ProjectBreadcrumbFolder {
  id: string;
  name: string;
  rid?: string | null;
  parent_folder_id: string | null;
}

export function projectRIDFromID(id: string) {
  return resourceRIDForKind('ontology_project', id);
}

export function folderRIDFromID(id: string) {
  return resourceRIDForKind('ontology_folder', id);
}

export function buildProjectFolderBreadcrumbItems(
  project: ProjectBreadcrumbProject,
  folders: ProjectBreadcrumbFolder[] = [],
  folderId?: string | null,
): BreadcrumbItem[] {
  const items: BreadcrumbItem[] = [
    { id: 'projects', label: 'Projects', href: '/projects', kind: 'root', icon: 'folder-open' },
    {
      id: project.id,
      label: project.display_name || project.slug || project.id,
      href: projectStablePath(project),
      rid: project.rid || projectRIDFromID(project.id),
      kind: 'project',
      icon: 'project',
    },
  ];

  for (const folder of buildFolderPath(folders, folderId)) {
    items.push({
      id: folder.id,
      label: folder.name,
      href: folderStablePath(project, folder),
      rid: folder.rid || folderRIDFromID(folder.id),
      kind: 'folder',
      icon: 'folder',
    });
  }

  return items;
}

function buildFolderPath(folders: ProjectBreadcrumbFolder[], folderId?: string | null) {
  const byId = new Map(folders.map((folder) => [folder.id, folder]));
  const path: ProjectBreadcrumbFolder[] = [];
  const seen = new Set<string>();
  let cursor = folderId ?? null;
  while (cursor && !seen.has(cursor)) {
    const folder = byId.get(cursor);
    if (!folder) break;
    seen.add(cursor);
    path.unshift(folder);
    cursor = folder.parent_folder_id;
  }
  return path;
}

export function ProjectBreadcrumb({ items, onNavigate, showCopyActions = true }: ProjectBreadcrumbProps) {
  const [copiedRID, setCopiedRID] = useState<string | null>(null);

  async function copyRID(rid: string) {
    await copyText(rid);
    setCopiedRID(rid);
    window.setTimeout(() => setCopiedRID((current) => (current === rid ? null : current)), 1400);
  }

  return (
    <nav className="of-resource-breadcrumb" aria-label="Resource breadcrumb">
      <ol className="of-resource-breadcrumb__list">
        {items.map((item, index) => {
          const isCurrent = index === items.length - 1;
          return (
            <li key={`${item.kind ?? 'item'}:${item.id}`} className="of-resource-breadcrumb__item">
              {index > 0 && (
                <span aria-hidden="true" className="of-resource-breadcrumb__separator">
                  <Glyph name="chevron-right" size={12} />
                </span>
              )}
              <span className="of-resource-breadcrumb__node">
                {item.icon && (
                  <span className="of-resource-breadcrumb__icon">
                    <Glyph name={item.icon} size={13} />
                  </span>
                )}
                {renderBreadcrumbLabel(item, index, isCurrent, onNavigate)}
                {showCopyActions && item.rid && (
                  <button
                    type="button"
                    className="of-resource-breadcrumb__copy"
                    onClick={() => void copyRID(item.rid!)}
                    aria-label={`Copy RID for ${item.label}`}
                    title={copiedRID === item.rid ? 'Copied' : item.rid}
                  >
                    <Glyph name={copiedRID === item.rid ? 'check' : 'duplicate'} size={12} />
                  </button>
                )}
              </span>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

function renderBreadcrumbLabel(
  item: BreadcrumbItem,
  index: number,
  isCurrent: boolean,
  onNavigate?: (item: BreadcrumbItem, index: number) => void,
) {
  if (isCurrent) {
    return (
      <span className="of-resource-breadcrumb__current" aria-current="page">
        {item.label}
      </span>
    );
  }
  if (item.href) {
    return (
      <Link
        to={item.href}
        className="of-resource-breadcrumb__link"
        onClick={(event) => {
          if (onNavigate) {
            event.preventDefault();
            onNavigate(item, index);
          }
        }}
      >
        {item.label}
      </Link>
    );
  }
  return (
    <button type="button" onClick={() => onNavigate?.(item, index)} className="of-resource-breadcrumb__button">
      {item.label}
    </button>
  );
}

async function copyText(value: string) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const textarea = document.createElement('textarea');
  textarea.value = value;
  textarea.setAttribute('readonly', 'true');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand('copy');
  document.body.removeChild(textarea);
}
