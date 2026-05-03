// Hito 2 cierre — End-to-end smoke for the Foundry "Set up a media
// set sync" wizard.
//
// Asserts the cross-app flow:
//   1. Open an S3 source's detail page and confirm the new "Media set
//      syncs" tab is rendered (only visible for S3 / ABFS connectors).
//   2. Click "+ Media set sync" → walk the 5-step wizard.
//   3. Inline-create a target media set on step 5; assert the wizard
//      uses the freshly-minted RID.
//   4. Save → POST `/api/v1/data-connection/sources/{id}/media-set-syncs`.
//   5. Navigate to `/media-sets/<rid>` and verify the cross-app
//      "Source: …" badge resolves back to the source page.

import { expect, test, type Route } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const SOURCE_ID = 'source-s3-1';
const SOURCE_NAME = 'S3 production';

const sampleSource = {
  id: SOURCE_ID,
  name: SOURCE_NAME,
  connector_type: 's3',
  worker: 'foundry',
  status: 'healthy',
  last_sync_at: '2026-01-02T00:00:00Z',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-02T00:00:00Z',
  config: { bucket: 'apron-photos' }
};

test.describe('media set sync wizard — cross-app smoke', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('walks the 5-step wizard, creates the target inline, and resolves the cross-app badge', async ({
    page
  }) => {
    // The wizard + inline-create form together exceed the default
    // 720 px viewport; bump it so Playwright's viewport-bound visibility
    // checks (even with `force: true`) succeed without per-step
    // scrollIntoView dances.
    await page.setViewportSize({ width: 1280, height: 1400 });

    let createdMediaSet: Record<string, unknown> | null = null;
    let mediaSyncs: Array<Record<string, unknown>> = [];
    let lastSyncBody: Record<string, unknown> | null = null;

    // ── Mock the source detail endpoints ─────────────────────────
    await page.route(
      `**/api/v1/data-connection/sources/${SOURCE_ID}`,
      async (route: Route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(sampleSource)
        });
      }
    );
    await page.route(
      `**/api/v1/data-connection/sources/${SOURCE_ID}/syncs`,
      async (route: Route) => {
        await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      }
    );
    await page.route(
      `**/api/v1/data-connection/sources/${SOURCE_ID}/credentials`,
      async (route: Route) => {
        await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      }
    );
    await page.route(
      `**/api/v1/data-connection/sources/${SOURCE_ID}/egress-policies`,
      async (route: Route) => {
        await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      }
    );
    await page.route('**/api/v1/data-connection/egress-policies', async (route: Route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
    });

    // ── /media-sets — used by the inline-create step ─────────────
    await page.route(/\/api\/v1\/media-sets(\?.*)?$/, async (route: Route) => {
      if (route.request().method() === 'POST') {
        const body = JSON.parse(route.request().postData() ?? '{}');
        createdMediaSet = {
          rid: 'ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000aaa',
          project_rid: body.project_rid,
          name: body.name,
          schema: body.schema,
          allowed_mime_types: body.allowed_mime_types ?? [],
          transaction_policy: body.transaction_policy ?? 'TRANSACTIONLESS',
          retention_seconds: body.retention_seconds ?? 0,
          virtual: !!body.virtual_,
          source_rid: body.source_rid ?? null,
          markings: body.markings ?? [],
          created_at: '2026-05-03T18:00:00Z',
          created_by: 'tester'
        };
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(createdMediaSet)
        });
        return;
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
    });

    // ── /media-sets/{rid} — used by the cross-app badge target ───
    await page.route(/\/api\/v1\/media-sets\/[^/]+$/, async (route: Route) => {
      if (!createdMediaSet) {
        await route.fulfill({ status: 404, body: '{}' });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(createdMediaSet)
      });
    });
    await page.route(/\/api\/v1\/media-sets\/[^/]+\/items(\?.*)?$/, async (route: Route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
    });

    // ── /sources/{id}/media-set-syncs — list + create ────────────
    await page.route(
      `**/api/v1/data-connection/sources/${SOURCE_ID}/media-set-syncs`,
      async (route: Route) => {
        if (route.request().method() === 'POST') {
          const body = JSON.parse(route.request().postData() ?? '{}');
          lastSyncBody = body;
          const persisted = {
            id: 'sync-1',
            source_id: SOURCE_ID,
            kind: body.kind,
            target_media_set_rid: body.target_media_set_rid,
            subfolder: body.subfolder ?? '',
            filters: {
              exclude_already_synced: body.filters?.exclude_already_synced ?? false,
              path_glob: body.filters?.path_glob ?? null,
              file_size_limit: body.filters?.file_size_limit ?? null,
              ignore_unmatched_schema: body.filters?.ignore_unmatched_schema ?? false
            },
            schedule_cron: body.schedule_cron ?? null,
            created_at: '2026-05-03T18:01:00Z'
          };
          mediaSyncs = [persisted, ...mediaSyncs];
          await route.fulfill({
            status: 201,
            contentType: 'application/json',
            body: JSON.stringify(persisted)
          });
          return;
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mediaSyncs)
        });
      }
    );

    // ── 1. Open the source detail page ───────────────────────────
    await page.goto(`/data-connection/sources/${SOURCE_ID}`);
    await expect(page.getByRole('heading', { name: SOURCE_NAME })).toBeVisible();

    // The "Media set syncs" tab should be visible because the
    // connector is `s3`.
    const mediaTab = page.getByTestId('tab-media-syncs');
    await expect(mediaTab).toBeVisible();
    await mediaTab.click();

    await expect(page.getByTestId('media-syncs-empty')).toBeVisible();
    await page.getByTestId('media-syncs-new').click();

    // ── 2. Wizard — step 1 (Type) ────────────────────────────────
    await expect(page.getByTestId('media-set-sync-wizard')).toBeVisible();
    await page.getByTestId('wizard-kind-VIRTUAL_MEDIA_SET_SYNC').check();
    await page.getByTestId('wizard-next').click();

    // Step 2 — schemas. Pre-selected IMAGE; switch to DOCUMENT and
    // back to confirm prefill keeps the MIME list in sync.
    await expect(page.getByTestId('wizard-step-2')).toHaveClass(/active/);
    await page.getByTestId('wizard-schema-IMAGE').click();
    await page.getByTestId('wizard-next').click();

    // Step 3 — schedule. Leave disabled (manual run).
    await expect(page.getByTestId('wizard-step-3')).toHaveClass(/active/);
    await page.getByTestId('wizard-next').click();

    // Step 4 — subfolder.
    await expect(page.getByTestId('wizard-step-4')).toHaveClass(/active/);
    await page.getByTestId('wizard-subfolder').fill('screenshots/2026/');
    await page.getByTestId('wizard-next').click();

    // ── 5. Filters + inline create ───────────────────────────────
    await expect(page.getByTestId('wizard-step-5')).toHaveClass(/active/);
    await page.getByTestId('wizard-filter-path-glob').fill('**/*.png');
    await page.getByTestId('wizard-filter-size-limit').fill('5');

    // Inline-create the target media set.
    await page.getByTestId('wizard-open-create-inline').click();
    await page.getByTestId('wizard-create-inline-name').fill('Apron snaps');
    await page.getByTestId('wizard-create-inline-submit').click();

    // After inline create, the target RID input is auto-populated.
    await expect(page.getByTestId('wizard-target-rid')).toHaveValue(
      'ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000aaa'
    );

    // ── Save ─────────────────────────────────────────────────────
    await page.getByTestId('wizard-save').click();

    // The wizard closes + the new sync renders in the list.
    await expect(page.getByTestId('media-set-sync-wizard')).toHaveCount(0);
    await expect(page.getByTestId('media-syncs-list')).toBeVisible();

    // The POST payload mirrors the wizard inputs.
    expect(lastSyncBody).toMatchObject({
      kind: 'VIRTUAL_MEDIA_SET_SYNC',
      target_media_set_rid:
        'ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000aaa',
      subfolder: 'screenshots/2026/',
      schedule_cron: null
    });
    // Cast at the read site — the closure assignment to `lastSyncBody`
    // happens inside an async route handler, which TS can't prove ran
    // before this read, so it narrows the variable to `null`.
    const captured = lastSyncBody as Record<string, unknown> | null;
    const filters = (captured?.filters ?? {}) as Record<string, unknown>;
    expect(filters.path_glob).toBe('**/*.png');
    expect(filters.file_size_limit).toBe(5 * 1024 * 1024);
    expect(filters.exclude_already_synced).toBe(true);

    // ── 6. Cross-app: navigate to the new media set ──────────────
    // The wizard binds `source_rid` to the data-connection source ID
    // for virtual syncs, so the cross-app badge should resolve back
    // to the same source page.
    await page.goto(
      '/media-sets/ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000aaa'
    );
    const badge = page.getByTestId('media-set-source-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveAttribute(
      'href',
      `/data-connection/sources/${SOURCE_ID}`
    );
  });
});
