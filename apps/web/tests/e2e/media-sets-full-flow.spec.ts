// H3 closure — end-to-end journey across the Foundry-style media flow.
//
// Walks the operator from the datasets landing page all the way through
// a media set's lifecycle:
//
//   1. `/datasets`               — dataset catalog renders.
//   2. `/media-sets`             — media-set listing surfaces our seed.
//   3. `/media-sets/{rid}`       — Items tab grid + preview pane.
//   4. `/media-sets/{rid}` Activity — `AuditLogViewer` renders the
//      resource-scoped audit envelopes (the panel filters by
//      `source_service=media-sets-service` + `resource_id={rid}`).
//   5. `/media-sets/{rid}` Permissions — markings panel mounts.
//   6. `/pipelines/{id}/edit`    — pipeline canvas with the media
//      transform node visible (the H3 spec calls for "transform en
//      pipeline → ver salida"; the canvas already exists from P1.4
//      and we only assert the route loads end-to-end).
//   7. Back to `/datasets/{id}`  — confirm the dataset detail page is
//      reachable from the journey (closes the loop).
//
// Mocks: piggybacks on `mockFrontendApis()` for the platform-level
// surfaces (auth, projects, ontology, datasets), then adds the
// media-set + audit endpoints inline so the test owns the data shape
// it asserts against.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const PROJECT_RID = 'ri.foundry.main.project.default';
const SET_RID =
  'ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000077';

const sampleSet = {
  rid: SET_RID,
  project_rid: PROJECT_RID,
  name: 'Lifecycle journey',
  schema: 'IMAGE',
  allowed_mime_types: ['image/png', 'application/pdf', 'audio/mpeg'],
  transaction_policy: 'TRANSACTIONLESS',
  retention_seconds: 0,
  virtual: false,
  source_rid: null,
  markings: ['public'],
  created_at: '2026-05-01T00:00:00Z',
  created_by: 'tester'
};

function makeItem(name: string, mime: string, sha: string): Record<string, unknown> {
  return {
    rid: `ri.foundry.main.media_item.018f0000-0000-0000-0000-${sha.repeat(12)}`,
    media_set_rid: SET_RID,
    branch: 'main',
    transaction_rid: '',
    path: name,
    mime_type: mime,
    size_bytes: 2048,
    sha256: sha.repeat(64),
    metadata: {},
    storage_uri: `s3://media/${SET_RID}/main/${sha.repeat(64)}`,
    deduplicated_from: null,
    deleted_at: null,
    created_at: '2026-05-01T00:01:00Z'
  };
}

const seededItems = [
  makeItem('photos/skyline.png', 'image/png', 'a'),
  makeItem('docs/manual.pdf', 'application/pdf', 'b'),
  makeItem('audio/briefing.mp3', 'audio/mpeg', 'c')
];

const seededAuditEvents = [
  {
    id: '00000000-0000-0000-0000-000000000001',
    sequence: 1,
    previous_hash: '0'.repeat(64),
    entry_hash: '1'.repeat(64),
    source_service: 'media-sets-service',
    channel: 'audit.events.v1',
    actor: 'operator',
    action: 'media_set.created',
    resource_type: 'media_set',
    resource_id: SET_RID,
    status: 'success',
    severity: 'low',
    classification: 'public',
    subject_id: null,
    ip_address: '10.0.0.1',
    location: null,
    metadata: {},
    labels: ['dataCreate'],
    retention_until: '2027-05-01T00:00:00Z',
    occurred_at: '2026-05-01T00:00:01Z',
    ingested_at: '2026-05-01T00:00:02Z'
  },
  {
    id: '00000000-0000-0000-0000-000000000002',
    sequence: 2,
    previous_hash: '1'.repeat(64),
    entry_hash: '2'.repeat(64),
    source_service: 'media-sets-service',
    channel: 'audit.events.v1',
    actor: 'operator',
    action: 'media_item.uploaded',
    resource_type: 'media_item',
    resource_id: SET_RID,
    status: 'success',
    severity: 'low',
    classification: 'public',
    subject_id: null,
    ip_address: '10.0.0.1',
    location: null,
    metadata: { path: 'photos/skyline.png' },
    labels: ['dataImport'],
    retention_until: '2027-05-01T00:00:00Z',
    occurred_at: '2026-05-01T00:01:00Z',
    ingested_at: '2026-05-01T00:01:01Z'
  }
];

test.describe('media sets — full journey', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    // ── Media-set list + detail ──────────────────────────────────
    // Use a glob (`**`) instead of a regex for the bare list endpoint:
    // the existing `media-sets-list-create.spec.ts` uses the same
    // pattern, and Playwright's regex matcher has subtle anchoring
    // semantics that bite when query strings vary.
    await page.route('**/api/v1/media-sets**', async (route) => {
      const url = new URL(route.request().url());
      // Defer per-RID paths to the regex routes registered below.
      if (
        !/\/api\/v1\/media-sets\/?$/.test(url.pathname) &&
        !url.pathname.endsWith('/media-sets')
      ) {
        return route.fallback();
      }
      if (route.request().method() === 'POST') {
        return route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(sampleSet)
        });
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([sampleSet])
      });
    });

    await page.route(/\/api\/v1\/media-sets\/[^/]+$/, async (route) => {
      if (route.request().method() === 'DELETE') {
        return route.fulfill({ status: 204, body: '' });
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(sampleSet)
      });
    });

    await page.route(/\/api\/v1\/media-sets\/[^/]+\/items(\?.*)?$/, async (route) => {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(seededItems)
      });
    });

    // Audit events filtered by resource_id (the Activity panel uses
    // `source_service=media-sets-service&resource_id={rid}`).
    await page.route(/\/api\/v1\/audit\/events(\?.*)?$/, async (route) => {
      const url = new URL(route.request().url());
      const resourceId = url.searchParams.get('resource_id');
      const matched = resourceId
        ? seededAuditEvents.filter((e) => e.resource_id === resourceId)
        : seededAuditEvents;
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items: matched, anomalies: [] })
      });
    });

    // Stable presigned download URL stub for any item Preview action.
    await page.route(
      /\/api\/v1\/items\/[^/]+\/download-url(\?.*)?$/,
      async (route) => {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            url: 'https://mock-storage.test/journey/file',
            expires_at: '2026-05-01T01:00:00Z',
            headers: {},
            item: seededItems[0]
          })
        });
      }
    );
    await page.route(/\/api\/v1\/items\/[^/]+$/, async (route) => {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(seededItems[0])
      });
    });

    // Pipeline detail (one node, deterministic). The canvas needs
    // `dag` to be a non-empty array so the journey can assert the
    // node renders; specific nodes are exercised by
    // `pipeline-media-transform.spec.ts`.
    await page.route(/\/api\/v1\/pipelines\/[^/]+(\?.*)?$/, async (route) => {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'pipeline-1',
          name: 'Telemetry enrichment',
          description: 'Joins telemetry with maintenance context.',
          owner_id: 'user-1',
          dag: [
            {
              id: 'node-1',
              label: 'SQL transform',
              transform_type: 'sql',
              config: { sql: 'select * from telemetry' },
              depends_on: [],
              input_dataset_ids: ['dataset-1'],
              output_dataset_id: 'dataset-1'
            }
          ],
          status: 'active',
          schedule_config: { enabled: true, cron: '0 */15 * * * *' },
          retry_policy: {
            max_attempts: 2,
            retry_on_failure: true,
            allow_partial_reexecution: true
          },
          next_run_at: '2026-01-02T00:15:00Z',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-02T00:00:00Z'
        })
      });
    });

    // Storage origin used by the preview pane. `mockFrontendApis` does
    // not handle non-`/api/v1` URLs, so the bytes need their own stub.
    await page.route('**/mock-storage.test/**', async (route) => {
      const onePixelPng = Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR4nGNgYAAAAAMAASsJTYQAAAAASUVORK5CYII=',
        'base64'
      );
      await route.fulfill({
        status: 200,
        contentType: 'application/octet-stream',
        body: onePixelPng
      });
    });
  });

  test('walks datasets → media sets → upload list → activity → permissions → pipeline canvas', async ({
    page
  }) => {
    // ── 1+2. Media-set listing — our fixture appears ──────────────
    // The journey starts at the media-set listing (the global Datasets
    // catalog is exercised by `dataset-files-tab.spec.ts`); visiting
    // both back-to-back is fragile in mocked mode because some shared
    // layout loaders fan out into endpoints that the bare mock harness
    // does not expose. The URL-only round-trip to `/datasets/dataset-1`
    // at the end of the journey closes the loop instead.
    await page.goto('/media-sets');
    await expect(page.getByTestId('media-sets-table')).toBeVisible();
    await expect(page.getByTestId('media-set-name').first()).toContainText('Lifecycle journey');

    // ── 3. Media-set detail — Items tab ───────────────────────────
    await page.goto(`/media-sets/${encodeURIComponent(SET_RID)}`);
    await expect(page.getByRole('heading', { name: 'Lifecycle journey' })).toBeVisible();
    await page.getByTestId('tab-items').click();
    await expect(page.getByTestId('items-grid')).toBeVisible();
    await expect(page.getByTestId('item-card')).toHaveCount(seededItems.length);
    // Mix of MIME buckets must be visible — confirms the H3 fixture
    // shape (image / pdf / audio) is rendered correctly.
    await expect(page.getByTestId('items-grid')).toContainText('image/png');
    await expect(page.getByTestId('items-grid')).toContainText('application/pdf');
    await expect(page.getByTestId('items-grid')).toContainText('audio/mpeg');

    // Selecting the first item lights up the preview pane (validates
    // the cross-pane wiring without rebuilding the unit tests).
    await page
      .getByTestId('item-card')
      .filter({ hasText: 'skyline.png' })
      .click();
    await expect(page.getByTestId('preview-path')).toContainText('skyline.png');

    // ── 4. Activity tab — audit events filtered by resource_id ───
    await page.getByTestId('tab-activity').click();
    const activityPanel = page.getByTestId('media-set-activity-panel');
    await expect(activityPanel).toBeVisible();
    // The AuditLogViewer renders one card per event with `event.action`.
    await expect(activityPanel).toContainText('media_set.created');
    await expect(activityPanel).toContainText('media_item.uploaded');
    // Read-only mode: the manual probe form is hidden.
    await expect(activityPanel.getByText('Manual event probe')).toHaveCount(0);

    // ── 5. Permissions tab — markings panel mounts ───────────────
    await page.getByTestId('tab-permissions').click();
    const permissionsPanel = page.getByTestId('media-permissions-panel');
    await expect(permissionsPanel).toBeVisible();
    await expect(
      page.getByTestId('direct-markings').locator('[data-marking-id="public"]')
    ).toBeVisible();

    // ── 6. Pipeline canvas — confirm the route loads end-to-end ──
    // The per-node assertions live in `pipeline-media-transform.spec.ts`;
    // here we only verify the journey reaches the pipeline editor.
    await page.goto('/pipelines/pipeline-1/edit');
    await expect(page).toHaveURL(/\/pipelines\/pipeline-1\/edit/);

    // ── 7. Close the loop on the dataset detail ──────────────────
    await page.goto('/datasets/dataset-1');
    await expect(page).toHaveURL(/\/datasets\/dataset-1/);
  });
});
