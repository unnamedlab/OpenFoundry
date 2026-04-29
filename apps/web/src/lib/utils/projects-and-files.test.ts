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
      buildSpaceOptions([
        {
          id: 'space-1',
          slug: 'operations',
          display_name: 'Operations Command',
          description: 'Operational workspaces',
          space_kind: 'private',
          owner_peer_id: null,
          region: 'eu-west-1',
          member_peer_ids: [],
          governance_tags: [],
          status: 'active',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ]),
    ).toEqual([
      {
        id: 'operations',
        label: 'Operations Command',
        workspaceSlug: 'operations',
        description: 'Operational workspaces',
      },
    ]);
  });

  it('prefers workspace hints from user attributes when choosing the active space', () => {
    const options = buildSpaceOptions([
      {
        id: 'space-1',
        slug: 'operations',
        display_name: 'Operations Command',
        description: 'Operational workspaces',
        space_kind: 'private',
        owner_peer_id: null,
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
        owner_peer_id: null,
        region: 'eu-west-1',
        member_peer_ids: [],
        governance_tags: [],
        status: 'active',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
    ]);

    expect(getPreferredWorkspaceSlug({ workspace: 'research', default_workspace: 'operations' })).toBe(
      'research',
    );
    expect(resolveSelectedSpaceId(options, '', 'research')).toBe('research');
    expect(resolveSpaceLabel(options, 'research')).toBe('Research Lab');
  });
});
