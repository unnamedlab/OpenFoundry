// Shared types + context key for the project workspace shell.
// Both the layout and the child pages (`+page.svelte`,
// `[folderId]/+page.svelte`) read from the same context so the breadcrumb
// and folder tree stay consistent without re-fetching per route.

import { getContext, setContext } from 'svelte';
import type {
  OntologyProject,
  OntologyProjectFolder,
  OntologyProjectResourceBinding,
} from '$lib/api/ontology';
import type { ResourceKind } from '$lib/api/workspace';

export interface DragTarget {
  kind: ResourceKind;
  id: string;
  /** For folder targets, the parent we'd traverse for cycle detection. */
  parentFolderId?: string | null;
  /** Optional human label (only used in notifications). */
  label?: string;
}

export interface DragSource {
  targets: DragTarget[];
}

export interface ProjectWorkspaceContext {
  project: OntologyProject | null;
  folders: OntologyProjectFolder[];
  resources: OntologyProjectResourceBinding[];
  loading: boolean;
  error: string;
  reload(): Promise<void>;
  // Drag bus (Phase 6 — DnD across detail page ↔ FolderTree).
  dragSource: DragSource | null;
  beginDrag(source: DragSource): void;
  endDrag(): void;
  /**
   * Attempt to drop the active source onto the given folder (or the
   * project root if `null`). Returns `true` when something was actually
   * moved. Cycle/no-op cases are handled internally.
   */
  tryDrop(targetFolderId: string | null): Promise<boolean>;
  /**
   * Open the cross-project move dialog pre-loaded with the active drag
   * source. The dialog reuses MoveDialog so users still pick the target
   * project + folder explicitly. Folder sources are rejected with a
   * notification because the backend forbids cross-project folder moves
   * (Phase 1 — would require deep clone of nested resources).
   */
  openCrossProjectMove(): void;
}

const KEY = Symbol('project-workspace');

export function setProjectWorkspaceContext(value: () => ProjectWorkspaceContext) {
  setContext(KEY, value);
}

export function getProjectWorkspaceContext(): () => ProjectWorkspaceContext {
  const value = getContext<() => ProjectWorkspaceContext>(KEY);
  if (!value) {
    throw new Error('Project workspace context is not initialized');
  }
  return value;
}
