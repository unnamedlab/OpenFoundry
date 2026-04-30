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

export interface ProjectWorkspaceContext {
  project: OntologyProject | null;
  folders: OntologyProjectFolder[];
  resources: OntologyProjectResourceBinding[];
  loading: boolean;
  error: string;
  reload(): Promise<void>;
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
