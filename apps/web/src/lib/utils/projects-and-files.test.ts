import { describe, expect, it } from 'vitest';

import {
  FALLBACK_SPACE_OPTIONS,
  buildSpaceOptions,
  getPreferredWorkspaceSlug,
  resolveSelectedSpaceId,
  resolveSpaceLabel,
} from './projects-and-files';

describe('projects and files utilities', () => {
  it('returns fallback spaces when the backend response is empty', () => {
    expect(buildSpaceOptions([])).toEqual(FALLBACK_SPACE_OPTIONS);
  });

  it('maps backend spaces into project space options keyed by slug', () => {
    expect(
      buildSpaceOptions(
        [
          {
            id: 'space-1',
            slug: 'operations',
            display_name: 'Operations Command',
            description: 'Operational workspaces',
            space_kind: 'private',
            owner_peer_id: 'org-1',
            region: 'eu-west-1',
            member_peer_ids: [],
            governance_tags: [],
            status: 'active',
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
          },
        ],
        {
          roles: [],
          permissions: [],
          organization_id: 'org-1',
          attributes: {},
        },
      ),
    ).toEqual([
      {
        id: 'operations',
        label: 'Operations Command',
        workspaceSlug: 'operations',
        description: 'Operational workspaces',
        ownerPeerId: 'org-1',
        memberPeerIds: [],
        status: 'active',
        source: 'live',
        canCreateProject: true,
        createPermissionReason: null,
      },
    ]);
  });

  it('prefers workspace hints from user attributes when choosing the active creatable space', () => {
    const options = buildSpaceOptions(
      [
        {
          id: 'space-1',
          slug: 'operations',
          display_name: 'Operations Command',
          description: 'Operational workspaces',
          space_kind: 'private',
          owner_peer_id: 'org-1',
          region: 'eu-west-1',
          member_peer_ids: [],
          governance_tags: [],
          status: 'active',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
        {
          id: 'space-2',
          slug: 'research',
          display_name: 'Research Lab',
          description: 'Research workspaces',
          space_kind: 'private',
          owner_peer_id: 'org-2',
          region: 'eu-west-1',
          member_peer_ids: ['org-1'],
          governance_tags: [],
          status: 'active',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
      {
        roles: [],
        permissions: [],
        organization_id: 'org-1',
        attributes: {},
      },
    );

    expect(getPreferredWorkspaceSlug({ workspace: 'research', default_workspace: 'operations' })).toBe(
      'research',
    );
    expect(resolveSelectedSpaceId(options, '', 'research')).toBe('research');
    expect(resolveSpaceLabel(options, 'research')).toBe('Research Lab');
  });

  it('blocks spaces that are paused or outside the organization assignment', () => {
    const options = buildSpaceOptions(
      [
        {
          id: 'space-1',
          slug: 'operations',
          display_name: 'Operations Command',
          description: 'Operational workspaces',
          space_kind: 'private',
          owner_peer_id: 'org-1',
          region: 'eu-west-1',
          member_peer_ids: [],
          governance_tags: [],
          status: 'paused',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
        {
          id: 'space-2',
          slug: 'research',
          display_name: 'Research Lab',
          description: 'Research workspaces',
          space_kind: 'private',
          owner_peer_id: 'org-2',
          region: 'eu-west-1',
          member_peer_ids: [],
          governance_tags: [],
          status: 'active',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ],
      {
        roles: [],
        permissions: [],
        organization_id: 'org-1',
        attributes: {},
      },
    );

    expect(options[0]).toMatchObject({
      canCreateProject: false,
      createPermissionReason: 'This space is paused and cannot accept new projects.',
    });
    expect(options[1]).toMatchObject({
      canCreateProject: false,
      createPermissionReason: 'Your organization is not assigned to create projects in this space.',
    });
    expect(resolveSelectedSpaceId(options, 'research', null)).toBe('operations');
  });
});
