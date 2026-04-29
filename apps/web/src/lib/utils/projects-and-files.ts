import type { UserProfile } from '$lib/api/auth';
import type { NexusSpace } from '$lib/api/nexus';

export type SpaceOption = {
  id: string;
  label: string;
  workspaceSlug: string;
  description: string;
  ownerPeerId: string | null;
  memberPeerIds: string[];
  status: string;
  source: 'live' | 'fallback';
  canCreateProject: boolean;
  createPermissionReason: string | null;
};

export const FALLBACK_SPACE_OPTIONS: SpaceOption[] = [
  {
    id: 'operations',
    label: 'Operations',
    workspaceSlug: 'operations',
    description: 'Shared space for operational workflows and secure project containers.',
    ownerPeerId: null,
    memberPeerIds: [],
    status: 'unknown',
    source: 'fallback',
    canCreateProject: false,
    createPermissionReason: 'Live space permissions are unavailable right now.',
  },
  {
    id: 'data-platform',
    label: 'Data Platform',
    workspaceSlug: 'data-platform',
    description: 'Central engineering space for data products, pipelines, and platform tools.',
    ownerPeerId: null,
    memberPeerIds: [],
    status: 'unknown',
    source: 'fallback',
    canCreateProject: false,
    createPermissionReason: 'Live space permissions are unavailable right now.',
  },
  {
    id: 'research',
    label: 'Research',
    workspaceSlug: 'research',
    description: 'Sandboxed space for experiments, notebooks, and exploratory delivery.',
    ownerPeerId: null,
    memberPeerIds: [],
    status: 'unknown',
    source: 'fallback',
    canCreateProject: false,
    createPermissionReason: 'Live space permissions are unavailable right now.',
  },
];

type ProjectCreationUser = Pick<UserProfile, 'roles' | 'permissions' | 'organization_id' | 'attributes'>;

const GENERIC_PROJECT_CREATE_PERMISSIONS = [
  'projects:create',
  'projects:write',
  'ontology.projects:create',
  'ontology.projects:write',
  'spaces.projects:create',
];

function cleanString(value: unknown) {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

function hasPermission(user: ProjectCreationUser | null | undefined, permission: string) {
  return user?.permissions.includes('*') || user?.permissions.includes(permission) || false;
}

function hasScopedProjectCreatePermission(
  user: ProjectCreationUser | null | undefined,
  space: Pick<SpaceOption, 'id' | 'workspaceSlug'>,
) {
  if (!user) return false;

  return [
    `spaces:${space.id}:projects:create`,
    `spaces:${space.workspaceSlug}:projects:create`,
    `ontology.projects:create:${space.workspaceSlug}`,
  ].some((permission) => hasPermission(user, permission));
}

function canCreateProjectInSpace(space: SpaceOption, user: ProjectCreationUser | null | undefined) {
  if (space.source !== 'live') {
    return {
      allowed: false,
      reason: 'Live space permissions are unavailable right now.',
    };
  }

  if (space.status === 'paused') {
    return {
      allowed: false,
      reason: 'This space is paused and cannot accept new projects.',
    };
  }

  if (!user) {
    return {
      allowed: false,
      reason: 'Sign in to verify whether you can create projects in this space.',
    };
  }

  if (
    user.roles.includes('admin') ||
    GENERIC_PROJECT_CREATE_PERMISSIONS.some((permission) => hasPermission(user, permission))
  ) {
    return { allowed: true, reason: null };
  }

  if (hasScopedProjectCreatePermission(user, space)) {
    return { allowed: true, reason: null };
  }

  const organizationId = cleanString(user.organization_id);
  if (
    organizationId &&
    (space.ownerPeerId === organizationId || space.memberPeerIds.includes(organizationId))
  ) {
    return { allowed: true, reason: null };
  }

  return {
    allowed: false,
    reason: 'Your organization is not assigned to create projects in this space.',
  };
}

export function buildSpaceOptions(
  spaces: NexusSpace[],
  user?: ProjectCreationUser | null,
): SpaceOption[] {
  const next = spaces
    .map((space) => ({
      id: space.slug,
      label: cleanString(space.display_name) ?? cleanString(space.slug) ?? 'Workspace',
      workspaceSlug: space.slug,
      description:
        cleanString(space.description) ??
        `${cleanString(space.display_name) ?? cleanString(space.slug) ?? 'Workspace'} space`,
      ownerPeerId: space.owner_peer_id,
      memberPeerIds: space.member_peer_ids ?? [],
      status: cleanString(space.status) ?? 'active',
      source: 'live' as const,
      canCreateProject: false,
      createPermissionReason: null,
    }))
    .filter((space) => cleanString(space.id) && cleanString(space.workspaceSlug));

  if (next.length === 0) {
    return FALLBACK_SPACE_OPTIONS;
  }

  const deduped = new Map<string, SpaceOption>();
  for (const option of next) {
    const permission = canCreateProjectInSpace(option, user);
    deduped.set(option.id, {
      ...option,
      canCreateProject: permission.allowed,
      createPermissionReason: permission.reason,
    });
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
  if (spaceOptions.some((option) => option.id === currentSpaceId && option.canCreateProject)) {
    return currentSpaceId;
  }
  if (preferred) {
    const match = spaceOptions.find(
      (option) => option.workspaceSlug === preferred && option.canCreateProject,
    );
    if (match) {
      return match.id;
    }
  }
  const firstCreatable = spaceOptions.find((option) => option.canCreateProject);
  if (firstCreatable) {
    return firstCreatable.id;
  }
  return spaceOptions[0]?.id ?? '';
}

export function resolveSpaceLabel(spaceOptions: SpaceOption[], spaceId: string) {
  return spaceOptions.find((option) => option.id === spaceId)?.label ?? 'Workspace';
}
