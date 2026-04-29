import type { NexusSpace } from '$lib/api/nexus';

export type SpaceOption = {
  id: string;
  label: string;
  workspaceSlug: string;
  description: string;
};

export const FALLBACK_SPACE_OPTIONS: SpaceOption[] = [
  {
    id: 'operations',
    label: 'Operations',
    workspaceSlug: 'operations',
    description: 'Shared space for operational workflows and secure project containers.',
  },
  {
    id: 'data-platform',
    label: 'Data Platform',
    workspaceSlug: 'data-platform',
    description: 'Central engineering space for data products, pipelines, and platform tools.',
  },
  {
    id: 'research',
    label: 'Research',
    workspaceSlug: 'research',
    description: 'Sandboxed space for experiments, notebooks, and exploratory delivery.',
  },
];

function cleanString(value: unknown) {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

export function buildSpaceOptions(spaces: NexusSpace[]): SpaceOption[] {
  const next = spaces
    .map((space) => ({
      id: space.slug,
      label: cleanString(space.display_name) ?? cleanString(space.slug) ?? 'Workspace',
      workspaceSlug: space.slug,
      description:
        cleanString(space.description) ??
        `${cleanString(space.display_name) ?? cleanString(space.slug) ?? 'Workspace'} space`,
    }))
    .filter((space) => cleanString(space.id) && cleanString(space.workspaceSlug));

  if (next.length === 0) {
    return FALLBACK_SPACE_OPTIONS;
  }

  const deduped = new Map<string, SpaceOption>();
  for (const option of next) {
    deduped.set(option.id, option);
  }

  return [...deduped.values()];
}

export function getPreferredWorkspaceSlug(attributes: Record<string, unknown> | null | undefined) {
  return cleanString(attributes?.workspace) ?? cleanString(attributes?.default_workspace);
}

export function resolveSelectedSpaceId(
  spaceOptions: SpaceOption[],
  currentSpaceId: string,
  preferredWorkspaceSlug?: string | null,
) {
  const preferred = cleanString(preferredWorkspaceSlug);
  if (spaceOptions.some((option) => option.id === currentSpaceId)) {
    return currentSpaceId;
  }
  if (preferred) {
    const match = spaceOptions.find((option) => option.workspaceSlug === preferred);
    if (match) {
      return match.id;
    }
  }
  return spaceOptions[0]?.id ?? '';
}

export function resolveSpaceLabel(spaceOptions: SpaceOption[], spaceId: string) {
  return spaceOptions.find((option) => option.id === spaceId)?.label ?? 'Workspace';
}
