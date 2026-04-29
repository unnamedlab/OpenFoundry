import type { Page, Route } from '@playwright/test';

const demoUser = {
  id: 'user-1',
  email: 'operator@openfoundry.dev',
  name: 'OpenFoundry Operator',
  is_active: true,
  roles: ['admin'],
  groups: ['platform'],
  permissions: ['*'],
  organization_id: 'org-1',
  attributes: {},
  mfa_enabled: false,
  mfa_enforced: false,
  auth_source: 'local',
  created_at: '2026-01-01T00:00:00Z',
};

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

const demoProject = {
  id: 'project-1',
  slug: 'ontology-training',
  display_name: 'Ontology Training',
  description: 'Editable ontology for guided training flows.',
  workspace_slug: 'training',
  owner_id: demoUser.id,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-02T00:00:00Z',
};

const demoDatasetPreview = {
  dataset_id: demoDataset.id,
  columns: [
    { name: 'tail_number', field_type: 'string', nullable: false },
    { name: 'platform_name', field_type: 'string', nullable: false },
    { name: 'status', field_type: 'string', nullable: true },
  ],
  rows: [
    { tail_number: 'AF-101', platform_name: 'Atlas', status: 'ready' },
    { tail_number: 'AF-102', platform_name: 'Comet', status: 'maintenance' },
  ],
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
  const objectTypes = [demoObjectType];
  let projectResources = [{ project_id: demoProject.id, resource_kind: 'object_type', resource_id: demoObjectType.id, bound_by: demoUser.id, created_at: '2026-01-02T00:00:00Z' }];
  const propertiesByType: Record<string, Array<Record<string, unknown>>> = {
    [demoObjectType.id]: [
      {
        id: 'property-1',
        object_type_id: demoObjectType.id,
        name: 'tail_number',
        display_name: 'Tail Number',
        description: '',
        property_type: 'string',
        required: true,
        unique_constraint: true,
        time_dependent: false,
        default_value: null,
        validation_rules: null,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
      },
    ],
  };
  let workingState = {
    project_id: demoProject.id,
    changes: [],
    updated_by: demoUser.id,
    updated_at: '2026-01-02T00:00:00Z',
  };
  let branches = [
    {
      id: 'branch-main',
      project_id: demoProject.id,
      name: 'main',
      description: 'Live ontology branch',
      status: 'main',
      proposal_id: null,
      changes: [],
      conflict_resolutions: {},
      enable_indexing: true,
      created_by: demoUser.id,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-02T00:00:00Z',
      latest_rebased_at: '2026-01-02T00:00:00Z',
    },
  ];
  let proposals: Array<Record<string, unknown>> = [];
  let migrations: Array<Record<string, unknown>> = [];

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

    if (pathname === '/api/v1/datasets/catalog/facets') {
      return json(route, { tags: [{ value: 'operations', count: 1 }] });
    }

    if (pathname === '/api/v1/datasets') {
      return json(route, { data: [demoDataset], page: 1, per_page: 100, total: 1, total_pages: 1 });
    }

    if (pathname === `/api/v1/datasets/${demoDataset.id}/preview`) {
      return json(route, demoDatasetPreview);
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

    if (pathname === '/api/v1/ontology/types' && request.method() === 'GET') {
      return json(route, { data: objectTypes, total: objectTypes.length, page: 1, per_page: 100 });
    }

    if (pathname === '/api/v1/ontology/types' && request.method() === 'POST') {
      const body = JSON.parse(request.postData() ?? '{}');
      const created = {
        id: `object-type-${objectTypes.length + 1}`,
        owner_id: demoUser.id,
        created_at: '2026-01-02T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
        ...body,
      };
      objectTypes.unshift(created);
      propertiesByType[created.id] = [];
      return json(route, created, 201);
    }

    if (pathname === `/api/v1/ontology/types/${demoObjectType.id}`) {
      return json(route, demoObjectType);
    }

    if (pathname.match(/^\/api\/v1\/ontology\/types\/[^/]+\/properties$/) && request.method() === 'GET') {
      const typeId = pathname.split('/')[5];
      return json(route, { data: propertiesByType[typeId] ?? [] });
    }

    if (pathname.match(/^\/api\/v1\/ontology\/types\/[^/]+\/properties$/) && request.method() === 'POST') {
      const typeId = pathname.split('/')[5];
      const body = JSON.parse(request.postData() ?? '{}');
      const created = {
        id: `property-${(propertiesByType[typeId]?.length ?? 0) + 1}`,
        object_type_id: typeId,
        default_value: null,
        validation_rules: null,
        created_at: '2026-01-02T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
        ...body,
      };
      propertiesByType[typeId] = [...(propertiesByType[typeId] ?? []), created];
      return json(route, created, 201);
    }

    if (pathname === '/api/v1/ontology/projects') {
      return json(route, { data: [demoProject], total: 1, page: 1, per_page: 100 });
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/memberships`) {
      return json(route, {
        data: [{ project_id: demoProject.id, user_id: demoUser.id, role: 'owner', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-02T00:00:00Z' }],
      });
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/resources`) {
      if (request.method() === 'GET') return json(route, { data: projectResources });
      if (request.method() === 'POST') {
        const body = JSON.parse(request.postData() ?? '{}');
        const created = { project_id: demoProject.id, bound_by: demoUser.id, created_at: '2026-01-02T00:00:00Z', ...body };
        projectResources = [...projectResources.filter((item) => !(item.resource_kind === created.resource_kind && item.resource_id === created.resource_id)), created];
        return json(route, created, 201);
      }
    }

    if (pathname.match(/^\/api\/v1\/ontology\/projects\/project-1\/resources\/[^/]+\/[^/]+$/) && request.method() === 'DELETE') {
      const [, , , , , resourceKind, resourceId] = pathname.split('/');
      projectResources = projectResources.filter((item) => !(item.resource_kind === resourceKind && item.resource_id === resourceId));
      return json(route, {}, 204);
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/working-state` && request.method() === 'GET') {
      return json(route, workingState);
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/working-state` && request.method() === 'PUT') {
      const body = JSON.parse(request.postData() ?? '{}');
      workingState = { ...workingState, changes: body.changes ?? [], updated_at: '2026-01-02T01:00:00Z' };
      return json(route, workingState);
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/branches` && request.method() === 'GET') {
      return json(route, { data: branches });
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/branches` && request.method() === 'POST') {
      const body = JSON.parse(request.postData() ?? '{}');
      const created = {
        id: `branch-${branches.length + 1}`,
        project_id: demoProject.id,
        status: 'draft',
        proposal_id: null,
        conflict_resolutions: {},
        enable_indexing: Boolean(body.enable_indexing),
        created_by: demoUser.id,
        created_at: '2026-01-02T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
        latest_rebased_at: '2026-01-02T00:00:00Z',
        ...body,
      };
      branches = [created, ...branches];
      return json(route, created, 201);
    }

    if (pathname.match(/^\/api\/v1\/ontology\/projects\/project-1\/branches\/[^/]+$/) && request.method() === 'PATCH') {
      const branchId = pathname.split('/').pop()!;
      const body = JSON.parse(request.postData() ?? '{}');
      const updated = branches.find((branch) => branch.id === branchId);
      if (!updated) return json(route, { error: 'not found' }, 404);
      Object.assign(updated, body, { updated_at: '2026-01-02T01:00:00Z' });
      return json(route, updated);
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/proposals` && request.method() === 'GET') {
      return json(route, { data: proposals });
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/proposals` && request.method() === 'POST') {
      const body = JSON.parse(request.postData() ?? '{}');
      const created = {
        id: `proposal-${proposals.length + 1}`,
        project_id: demoProject.id,
        created_by: demoUser.id,
        created_at: '2026-01-02T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z',
        reviewer_ids: [],
        comments: [],
        status: 'in_review',
        ...body,
      };
      proposals = [created, ...proposals];
      return json(route, created, 201);
    }

    if (pathname.match(/^\/api\/v1\/ontology\/projects\/project-1\/proposals\/[^/]+$/) && request.method() === 'PATCH') {
      const proposalId = pathname.split('/').pop()!;
      const body = JSON.parse(request.postData() ?? '{}');
      const updated = proposals.find((proposal) => proposal.id === proposalId);
      if (!updated) return json(route, { error: 'not found' }, 404);
      Object.assign(updated, body, { updated_at: '2026-01-02T01:00:00Z' });
      return json(route, updated);
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/migrations` && request.method() === 'GET') {
      return json(route, { data: migrations });
    }

    if (pathname === `/api/v1/ontology/projects/${demoProject.id}/migrations` && request.method() === 'POST') {
      const body = JSON.parse(request.postData() ?? '{}');
      const created = {
        id: `migration-${migrations.length + 1}`,
        project_id: demoProject.id,
        submitted_by: demoUser.id,
        submitted_at: '2026-01-02T00:00:00Z',
        status: 'planned',
        ...body,
      };
      migrations = [created, ...migrations];
      return json(route, created, 201);
    }

    if (pathname === '/api/v1/ontology/funnel/sources' && request.method() === 'POST') {
      const body = JSON.parse(request.postData() ?? '{}');
      return json(route, { id: 'funnel-source-1', pipeline_id: null, dataset_branch: 'main', dataset_version: null, default_marking: 'public', status: 'active', trigger_context: {}, owner_id: demoUser.id, last_run_at: null, created_at: '2026-01-02T00:00:00Z', updated_at: '2026-01-02T00:00:00Z', ...body }, 201);
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
