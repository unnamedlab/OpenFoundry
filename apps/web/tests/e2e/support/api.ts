import type { Page, Route } from '@playwright/test';

const demoUser = {
  id: 'user-1',
  email: 'operator@openfoundry.dev',
  name: 'OpenFoundry Operator',
  is_active: true,
  roles: ['operator'],
  groups: ['platform'],
  permissions: [],
  organization_id: 'org-1',
  attributes: { workspace: 'operations' },
  mfa_enabled: false,
  mfa_enforced: false,
  auth_source: 'local',
  created_at: '2026-01-01T00:00:00Z',
};

const demoSpaces = [
  {
    id: 'space-1',
    slug: 'operations',
    display_name: 'Operations Command',
    description: 'Operational workspaces and secure project containers.',
    space_kind: 'private',
    owner_peer_id: demoUser.organization_id,
    region: 'eu-west-1',
    member_peer_ids: [],
    governance_tags: [],
    status: 'active',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
  },
  {
    id: 'space-2',
    slug: 'research',
    display_name: 'Research Lab',
    description: 'Experimental workspaces for exploratory teams.',
    space_kind: 'private',
    owner_peer_id: 'org-2',
    region: 'eu-west-1',
    member_peer_ids: [demoUser.organization_id],
    governance_tags: [],
    status: 'active',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
  },
  {
    id: 'space-3',
    slug: 'archive',
    display_name: 'Archive Vault',
    description: 'Archived space that should not accept new projects.',
    space_kind: 'private',
    owner_peer_id: 'org-9',
    region: 'eu-west-1',
    member_peer_ids: [],
    governance_tags: [],
    status: 'paused',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
  },
];

const demoProjects = [
  {
    id: 'project-1',
    slug: 'ops-readiness',
    display_name: 'Ops readiness',
    description: 'Operations review workspace',
    workspace_slug: 'operations',
    owner_id: demoUser.id,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
  },
];

const demoDataset = {
  id: 'dataset-1',
  name: 'Aircraft health telemetry',
  description: 'Operational telemetry for the fleet health cockpit.',
  format: 'parquet',
  storage_path: '/datasets/aircraft-health-telemetry',
  size_bytes: 524288,
  row_count: 1280,
  owner_id: demoUser.id,
  tags: ['operations'],
  current_version: 3,
  active_branch: 'main',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-02T00:00:00Z',
};

const demoPipeline = {
  id: 'pipeline-1',
  name: 'Telemetry enrichment',
  description: 'Joins telemetry with maintenance context.',
  owner_id: demoUser.id,
  dag: [
    {
      id: 'node-1',
      label: 'SQL transform',
      transform_type: 'sql',
      config: { sql: 'select * from telemetry' },
      depends_on: [],
      input_dataset_ids: [demoDataset.id],
      output_dataset_id: demoDataset.id,
    },
  ],
  status: 'active',
  schedule_config: { enabled: true, cron: '0 */15 * * * *' },
  retry_policy: { max_attempts: 2, retry_on_failure: true, allow_partial_reexecution: true },
  next_run_at: '2026-01-02T00:15:00Z',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-02T00:00:00Z',
};

const demoPipelineRun = {
  id: 'run-1',
  pipeline_id: demoPipeline.id,
  status: 'completed',
  trigger_type: 'manual',
  started_by: demoUser.id,
  attempt_number: 1,
  started_from_node_id: null,
  retry_of_run_id: null,
  execution_context: {},
  node_results: [
    {
      node_id: 'node-1',
      label: 'SQL transform',
      transform_type: 'sql',
      status: 'completed',
      rows_affected: 1280,
      attempts: 1,
      output: null,
      error: null,
    },
  ],
  error_message: null,
  started_at: '2026-01-02T00:00:00Z',
  finished_at: '2026-01-02T00:01:00Z',
};

const demoObjectType = {
  id: 'object-type-1',
  name: 'aircraft',
  display_name: 'Aircraft',
  description: 'Operational aircraft tracked in the ontology.',
  primary_key_property: 'tail_number',
  icon: 'plane',
  color: '#2458b8',
  owner_id: demoUser.id,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-02T00:00:00Z',
};

const demoTemplate = {
  id: 'template-1',
  key: 'ops-cockpit',
  name: 'Operations cockpit',
  description: 'Starter template for operational dashboards and workflows.',
  category: 'operations',
  preview_image_url: null,
  definition: {
    pages: [],
    theme: {
      name: 'Signal',
      primary_color: '#0f766e',
      accent_color: '#f97316',
      heading_font: 'Space Grotesk',
      body_font: 'Manrope',
    },
    settings: {
      home_page_id: null,
      navigation_style: 'tabs',
      max_width: '1280px',
      show_branding: true,
      custom_css: null,
      builder_experience: 'workshop',
      consumer_mode: {
        enabled: false,
        allow_guest_access: false,
        portal_title: null,
        portal_subtitle: null,
        primary_cta_label: null,
        primary_cta_url: null,
      },
      interactive_workshop: {
        enabled: false,
        title: 'Interactive Workshop',
        subtitle: 'Coordinate scenario presets, decision briefs, and copilots from one runtime surface.',
        primary_scenario_widget_id: null,
        primary_agent_widget_id: null,
        briefing_template: '',
        suggested_questions: [],
        scenario_presets: [],
      },
      slate: {
        enabled: false,
        framework: 'react',
        package_name: '@open-foundry/slate-app',
        entry_file: 'src/App.tsx',
        sdk_import: '@open-foundry/sdk/react',
        workspace: {
          enabled: false,
          repository_id: null,
          layout: 'split',
          runtime: 'typescript-react',
          dev_command: 'pnpm dev',
          preview_command: 'pnpm build',
          files: [],
        },
        quiver_embed: {
          enabled: false,
          primary_type_id: null,
          secondary_type_id: null,
          join_field: null,
          secondary_join_field: null,
          date_field: null,
          metric_field: null,
          group_field: null,
          selected_group: null,
        },
      },
    },
  },
  created_at: '2026-01-01T00:00:00Z',
};

const demoWidgetCatalog = [
  {
    widget_type: 'chart.line',
    label: 'Line chart',
    description: 'Trend metrics over time.',
    category: 'analytics',
    default_props: { metric: 'value' },
    default_size: { width: 6, height: 4 },
    supported_bindings: ['query'],
    supports_children: false,
  },
];

const demoApp = {
  id: 'app-1',
  name: 'Ops workspace',
  slug: 'ops-workspace',
  description: 'Mission control for the operations team.',
  status: 'draft',
  page_count: 1,
  widget_count: 1,
  template_key: demoTemplate.key,
  published_version_id: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-02T00:00:00Z',
};

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  });
}

export async function seedAuthenticatedSession(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('of_access_token', 'test-access-token');
    window.localStorage.setItem('of_refresh_token', 'test-refresh-token');
  });
}

export async function mockFrontendApis(page: Page) {
  const projects = [...demoProjects];
  const projectFolders = new Map<string, Array<{
    id: string;
    project_id: string;
    parent_folder_id: string | null;
    name: string;
    slug: string;
    description: string;
    created_by: string;
    created_at: string;
    updated_at: string;
  }>>([
    [
      'project-1',
      [
        {
          id: 'folder-1',
          project_id: 'project-1',
          parent_folder_id: null,
          name: 'Planning',
          slug: 'planning',
          description: 'Starter folder inside Ops readiness.',
          created_by: demoUser.id,
          created_at: '2026-01-02T00:00:00Z',
          updated_at: '2026-01-02T00:00:00Z',
        },
      ],
    ],
  ]);

  await page.route('**/api/v1/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

    if (pathname === '/api/v1/auth/sso/providers/public') {
      return json(route, []);
    }

    if (pathname === '/api/v1/auth/login' && request.method() === 'POST') {
      return json(route, {
        status: 'authenticated',
        access_token: 'test-access-token',
        refresh_token: 'test-refresh-token',
        token_type: 'Bearer',
        expires_in: 3600,
      });
    }

    if (pathname === '/api/v1/users/me') {
      return json(route, demoUser);
    }

    if (pathname === '/api/v1/users') {
      return json(route, [demoUser]);
    }

    if (pathname === '/api/v1/nexus/spaces') {
      return json(route, { items: demoSpaces });
    }

    if (pathname === '/api/v1/ontology/projects' && request.method() === 'GET') {
      return json(route, { data: projects, total: projects.length, page: 1, per_page: 100 });
    }

    if (pathname === '/api/v1/ontology/projects' && request.method() === 'POST') {
      const body = request.postDataJSON() as {
        slug: string;
        display_name?: string;
        description?: string;
        workspace_slug?: string;
        folders?: Array<{
          name: string;
          description?: string;
          parent_folder_id?: string | null;
        }>;
      };
      const created = {
        id: `project-${projects.length + 1}`,
        slug: body.slug,
        display_name: body.display_name ?? body.slug,
        description: body.description ?? '',
        workspace_slug: body.workspace_slug ?? null,
        owner_id: demoUser.id,
        created_at: '2026-01-03T00:00:00Z',
        updated_at: '2026-01-03T00:00:00Z',
      };
      projects.unshift(created);
      projectFolders.set(
        created.id,
        (body.folders ?? []).map((folder, index) => ({
          id: `folder-${created.id}-${index + 1}`,
          project_id: created.id,
          parent_folder_id: folder.parent_folder_id ?? null,
          name: folder.name,
          slug: folder.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, ''),
          description: folder.description ?? '',
          created_by: demoUser.id,
          created_at: '2026-01-03T00:00:00Z',
          updated_at: '2026-01-03T00:00:00Z',
        })),
      );
      return json(route, created, 201);
    }

    const projectFolderMatch = pathname.match(/^\/api\/v1\/ontology\/projects\/([^/]+)\/folders$/);
    if (projectFolderMatch && request.method() === 'GET') {
      return json(route, { data: projectFolders.get(projectFolderMatch[1]) ?? [] });
    }

    if (pathname === '/api/v1/datasets/catalog/facets') {
      return json(route, { tags: [{ value: 'operations', count: 1 }] });
    }

    if (pathname === '/api/v1/datasets') {
      return json(route, { data: [demoDataset], page: 1, per_page: 100, total: 1, total_pages: 1 });
    }

    if (pathname === `/api/v1/datasets/${demoDataset.id}/quality`) {
      return json(route, {
        score: 96,
        alerts: [],
      });
    }

    if (pathname === '/api/v1/pipelines') {
      return json(route, { data: [demoPipeline], total: 1, page: 1, per_page: 50 });
    }

    if (pathname === `/api/v1/pipelines/${demoPipeline.id}`) {
      return json(route, demoPipeline);
    }

    if (pathname === `/api/v1/pipelines/${demoPipeline.id}/runs`) {
      return json(route, { data: [demoPipelineRun] });
    }

    if (pathname === `/api/v1/lineage/datasets/${demoDataset.id}/columns`) {
      return json(route, []);
    }

    if (pathname === '/api/v1/ai/providers') {
      return json(route, { data: [], total: 0 });
    }

    if (pathname === '/api/v1/ai/knowledge-bases') {
      return json(route, { data: [], total: 0 });
    }

    if (pathname === '/api/v1/streaming/topologies') {
      return json(route, { data: [], total: 0 });
    }

    if (pathname === '/api/v1/ontology/types') {
      return json(route, { data: [demoObjectType], total: 1, page: 1, per_page: 100 });
    }

    if (pathname === '/api/v1/ontology/actions') {
      return json(route, { data: [], total: 0, page: 1, per_page: 100 });
    }

    if (pathname === '/api/v1/ontology/functions') {
      return json(route, { data: [], total: 0, page: 1, per_page: 100 });
    }

    if (pathname === '/api/v1/ontology/shared-property-types') {
      return json(route, { data: [], total: 0, page: 1, per_page: 100 });
    }

    if (pathname === '/api/v1/ontology/links') {
      return json(route, { data: [], total: 0 });
    }

    if (pathname === '/api/v1/apps') {
      return json(route, { data: [demoApp], total: 1 });
    }

    if (pathname === '/api/v1/apps/templates') {
      return json(route, { data: [demoTemplate] });
    }

    if (pathname === '/api/v1/widgets/catalog') {
      return json(route, demoWidgetCatalog);
    }

    if (pathname === '/api/v1/ai/agents') {
      return json(route, { data: [], total: 0 });
    }

    if (pathname === '/api/v1/code-repos/repositories') {
      return json(route, { items: [] });
    }

    return json(route, { error: `Unhandled mock for ${pathname}` }, 500);
  });
}
