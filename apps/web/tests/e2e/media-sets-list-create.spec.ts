// U-Media: smoke test for `/media-sets`.
//
// Asserts the list + create + upload + delete loop wired up by
// `apps/web/src/lib/api/mediaSets.ts` against a fully-mocked
// media-sets-service. The Rust side is exercised by the testcontainer
// suite under `services/media-sets-service/tests/`.
//
// Coverage:
//   1. Empty state renders with the "Upload your first file" CTA.
//   2. The create drawer round-trips a new media set.
//   3. The uploader drawer accepts two image files and the items list
//      reflects the upload via the freshly-mocked `listItems` response.
//   4. Delete clears the row + restores the empty state.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const PROJECT_RID = 'ri.foundry.main.project.default';

const SET_RID =
  'ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000001';

const sampleSet = {
  rid: SET_RID,
  project_rid: PROJECT_RID,
  name: 'Aircraft photos',
  schema: 'IMAGE',
  allowed_mime_types: ['image/png', 'image/jpeg'],
  transaction_policy: 'TRANSACTIONLESS',
  retention_seconds: 0,
  virtual: false,
  source_rid: null,
  markings: [],
  created_at: '2026-05-01T00:00:00Z',
  created_by: 'tester'
};

function makeItem(name: string, sha256Char: string): Record<string, unknown> {
  return {
    rid: `ri.foundry.main.media_item.018f0000-0000-0000-0000-${sha256Char.repeat(12)}`,
    media_set_rid: SET_RID,
    branch: 'main',
    transaction_rid: '',
    path: name,
    mime_type: 'image/png',
    size_bytes: 1024,
    sha256: sha256Char.repeat(64),
    metadata: {},
    storage_uri: `s3://media/media-sets/${SET_RID}/main/${sha256Char}${sha256Char}/${sha256Char.repeat(64)}`,
    deduplicated_from: null,
    deleted_at: null,
    created_at: '2026-05-01T00:01:00Z'
  };
}

test.describe('media sets — list + create + upload + delete', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('round-trips a media set through create, two uploads, and delete', async ({ page }) => {
    let mediaSets: Array<typeof sampleSet> = [];
    let items: Array<Record<string, unknown>> = [];

    // ── /api/v1/media-sets — list + create ─────────────────────────
    await page.route('**/api/v1/media-sets**', async (route) => {
      const request = route.request();
      const method = request.method();
      const url = new URL(request.url());
      // Intercept only the bare `/media-sets` path here; per-RID paths
      // are caught by the regex routes below so they take priority.
      if (!/\/api\/v1\/media-sets\/?$/.test(url.pathname) && !url.pathname.endsWith('/media-sets')) {
        return route.fallback();
      }
      if (method === 'GET') {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mediaSets)
        });
      }
      if (method === 'POST') {
        const body = JSON.parse(request.postData() ?? '{}');
        const created = {
          ...sampleSet,
          name: body.name ?? sampleSet.name,
          project_rid: body.project_rid ?? PROJECT_RID,
          schema: body.schema ?? 'IMAGE',
          allowed_mime_types: body.allowed_mime_types ?? sampleSet.allowed_mime_types,
          transaction_policy: body.transaction_policy ?? 'TRANSACTIONLESS',
          retention_seconds: body.retention_seconds ?? 0,
          virtual: body.virtual_ ?? false,
          source_rid: body.source_rid ?? null,
          markings: body.markings ?? []
        };
        mediaSets = [created, ...mediaSets];
        return route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(created)
        });
      }
      return route.fallback();
    });

    // ── /api/v1/media-sets/{rid} — GET / DELETE ───────────────────
    await page.route(/\/api\/v1\/media-sets\/[^/]+$/, async (route) => {
      const request = route.request();
      const method = request.method();
      const decoded = decodeURIComponent(new URL(request.url()).pathname);
      const rid = decoded.split('/').pop()!;
      if (method === 'GET') {
        const found = mediaSets.find((set) => set.rid === rid);
        if (!found) {
          return route.fulfill({ status: 404, body: '{}' });
        }
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(found)
        });
      }
      if (method === 'DELETE') {
        mediaSets = mediaSets.filter((set) => set.rid !== rid);
        items = items.filter((item) => item.media_set_rid !== rid);
        return route.fulfill({ status: 204, body: '' });
      }
      return route.fallback();
    });

    // ── /api/v1/media-sets/{rid}/items — GET ──────────────────────
    await page.route(/\/api\/v1\/media-sets\/[^/]+\/items(\?.*)?$/, async (route) => {
      const decoded = decodeURIComponent(new URL(route.request().url()).pathname);
      const rid = decoded.split('/').slice(-2)[0];
      const filtered = items.filter((item) => item.media_set_rid === rid);
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(filtered)
      });
    });

    // ── /api/v1/media-sets/{rid}/items/upload-url — POST ──────────
    await page.route(/\/api\/v1\/media-sets\/[^/]+\/items\/upload-url$/, async (route) => {
      const request = route.request();
      const decoded = decodeURIComponent(new URL(request.url()).pathname);
      const rid = decoded.split('/').slice(-3)[0];
      const body = JSON.parse(request.postData() ?? '{}');
      // Mimic the backend's path-dedup soft-delete on the previous item.
      let dedupFrom: string | null = null;
      const previous = items.find(
        (item) =>
          item.media_set_rid === rid &&
          item.branch === (body.branch ?? 'main') &&
          item.path === body.path &&
          item.deleted_at === null
      );
      if (previous) {
        dedupFrom = previous.rid as string;
        previous.deleted_at = '2026-05-01T00:02:00Z';
      }
      const item = {
        ...makeItem(body.path ?? 'unknown', body.sha256?.[0] ?? 'a'),
        media_set_rid: rid,
        rid: `ri.foundry.main.media_item.018f0000-0000-0000-0000-${String(items.length + 1).padStart(12, '0')}`,
        deduplicated_from: dedupFrom
      };
      items = [...items, item];
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          url: `https://mock-storage.test/${rid}/${body.path}`,
          expires_at: '2026-05-01T01:00:00Z',
          headers: {},
          item
        })
      });
    });

    // ── /api/v1/items/{rid} — GET ─────────────────────────────────
    await page.route(/\/api\/v1\/items\/[^/]+$/, async (route) => {
      const decoded = decodeURIComponent(new URL(route.request().url()).pathname);
      const rid = decoded.split('/').pop()!;
      const found = items.find((item) => item.rid === rid);
      if (!found) return route.fulfill({ status: 404, body: '{}' });
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(found)
      });
    });

    // ── Mock the storage PUT — Foundry's presigned URL leaves the
    //    gateway, but Playwright's interception is global so we
    //    catch any host. ────────────────────────────────────────────
    await page.route('**/mock-storage.test/**', async (route) => {
      await route.fulfill({ status: 200, body: '' });
    });

    // ── 1. Empty state ────────────────────────────────────────────
    await page.goto('/media-sets');
    await expect(page.getByRole('heading', { name: 'Media sets', exact: true })).toBeVisible();
    await expect(page.getByTestId('media-sets-empty-state')).toBeVisible();
    await expect(page.getByTestId('media-sets-empty-cta')).toBeVisible();

    // ── 2. Create flow ────────────────────────────────────────────
    await page.getByTestId('media-sets-empty-cta').click();
    await page.getByTestId('media-set-create-name').fill('Aircraft photos');
    // Project RID has a default value already; replace it.
    await page.getByTestId('media-set-create-project').fill(PROJECT_RID);
    await page.getByTestId('media-set-schema-IMAGE').click();
    await page.getByTestId('media-set-create-submit').click();

    await expect(page.getByTestId('media-set-row')).toHaveCount(1);
    await expect(page.getByText('Aircraft photos').first()).toBeVisible();

    // After create the upload drawer pops open automatically.
    await expect(
      page.getByRole('dialog', { name: /Upload to Aircraft photos/ })
    ).toBeVisible();

    // ── 3. Upload two images ──────────────────────────────────────
    const uploadInput = page.getByTestId('media-set-uploader-input');
    await uploadInput.setInputFiles([
      {
        name: 'apron.png',
        mimeType: 'image/png',
        buffer: Buffer.from('fake-png-bytes-1')
      },
      {
        name: 'taxiway.png',
        mimeType: 'image/png',
        buffer: Buffer.from('fake-png-bytes-2')
      }
    ]);

    // Two rows in the uploader, both reaching "Uploaded".
    await expect(page.getByTestId('media-set-uploader-rows').locator('li')).toHaveCount(2);
    await expect(page.getByText('Uploaded', { exact: true }).first()).toBeVisible();

    // Close the drawer (Escape) and verify the table reflects the
    // new item count once the page reloads its counts.
    await page.keyboard.press('Escape');
    // The page reloads on uploaderClosed → itemCounts refresh.
    await expect(page.getByText('2', { exact: true }).first()).toBeVisible();

    // ── 4. Delete ─────────────────────────────────────────────────
    page.once('dialog', (dialog) => dialog.accept());
    await page.getByTestId('media-set-delete').click();
    await expect(page.getByTestId('media-sets-empty-state')).toBeVisible();
  });
});
