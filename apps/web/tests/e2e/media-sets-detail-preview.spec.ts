// U14 — smoke test for the media-sets detail page.
//
// Asserts the right-panel `MediaPreview` mounts the appropriate
// child element per MIME type by selecting an image, then a PDF,
// then an audio item from the items grid. The download / delete /
// copy-media-reference actions are exercised at the page level.
//
// The Rust services are mocked end-to-end (media-sets-service +
// presigned URL response). The fake storage URL returns 200 so the
// `<img|audio|video>` element can complete its load handler.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const SET_RID =
  'ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000001';

const sampleSet = {
  rid: SET_RID,
  project_rid: 'ri.foundry.main.project.default',
  name: 'Mixed assets',
  schema: 'IMAGE',
  allowed_mime_types: ['image/png', 'application/pdf', 'audio/mpeg'],
  transaction_policy: 'TRANSACTIONLESS',
  retention_seconds: 0,
  virtual: false,
  source_rid: null,
  markings: [],
  created_at: '2026-05-01T00:00:00Z',
  created_by: 'tester'
};

function makeItem(overrides: Record<string, unknown>) {
  return {
    rid: 'ri.foundry.main.media_item.unset',
    media_set_rid: SET_RID,
    branch: 'main',
    transaction_rid: '',
    path: 'unset',
    mime_type: 'application/octet-stream',
    size_bytes: 1024,
    sha256: 'a'.repeat(64),
    metadata: { foundry: { uploaded_via: 'e2e-test' } },
    storage_uri: `s3://media/${SET_RID}/unset`,
    deduplicated_from: null,
    deleted_at: null,
    created_at: '2026-05-01T00:01:00Z',
    ...overrides
  };
}

const seededItems = [
  makeItem({
    rid: 'ri.foundry.main.media_item.018f0000-0000-0000-0000-000000000010',
    path: 'apron.png',
    mime_type: 'image/png',
    size_bytes: 2048
  }),
  makeItem({
    rid: 'ri.foundry.main.media_item.018f0000-0000-0000-0000-000000000011',
    path: 'manual.pdf',
    mime_type: 'application/pdf',
    size_bytes: 4096
  }),
  makeItem({
    rid: 'ri.foundry.main.media_item.018f0000-0000-0000-0000-000000000012',
    path: 'briefing.mp3',
    mime_type: 'audio/mpeg',
    size_bytes: 8192
  })
];

test.describe('media set detail — preview switching', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('selecting items mounts the right preview component for image / pdf / audio', async ({
    page
  }) => {
    let items = [...seededItems];

    // ── Mock /api/v1/media-sets/{rid} ─────────────────────────────
    await page.route(/\/api\/v1\/media-sets\/[^/]+$/, async (route) => {
      const decoded = decodeURIComponent(new URL(route.request().url()).pathname);
      const rid = decoded.split('/').pop()!;
      if (route.request().method() === 'DELETE') {
        return route.fulfill({ status: 204, body: '' });
      }
      if (rid === SET_RID) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(sampleSet)
        });
      }
      return route.fulfill({ status: 404, body: '{}' });
    });

    // ── Mock /api/v1/media-sets/{rid}/items ───────────────────────
    await page.route(/\/api\/v1\/media-sets\/[^/]+\/items(\?.*)?$/, async (route) => {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(items)
      });
    });

    // ── Mock /api/v1/items/{rid} (GET + DELETE) ──────────────────
    await page.route(/\/api\/v1\/items\/[^/]+$/, async (route) => {
      const decoded = decodeURIComponent(new URL(route.request().url()).pathname);
      const rid = decoded.split('/').pop()!;
      const method = route.request().method();
      if (method === 'DELETE') {
        items = items.filter((item) => item.rid !== rid);
        return route.fulfill({ status: 204, body: '' });
      }
      const found = items.find((item) => item.rid === rid);
      if (!found) return route.fulfill({ status: 404, body: '{}' });
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(found)
      });
    });

    // ── Mock /api/v1/items/{rid}/download-url ────────────────────
    await page.route(
      /\/api\/v1\/items\/[^/]+\/download-url(\?.*)?$/,
      async (route) => {
        const decoded = decodeURIComponent(new URL(route.request().url()).pathname);
        const rid = decoded.split('/').slice(-2)[0];
        const found = items.find((item) => item.rid === rid);
        const synthetic = `https://mock-storage.test/${rid}/${
          found?.path ?? 'file'
        }`;
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            url: synthetic,
            expires_at: '2026-05-01T01:00:00Z',
            headers: {},
            item: found ?? null
          })
        });
      }
    );

    // ── Mock storage origin so <img|audio|video> can resolve. ────
    await page.route('**/mock-storage.test/**', async (route) => {
      // Tiny PNG bytes are sufficient for image elements; audio/video
      // elements only need a 200 to start metadata fetch in headless.
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

    // ── Navigate ──────────────────────────────────────────────────
    await page.goto(`/media-sets/${encodeURIComponent(SET_RID)}`);
    await expect(page.getByRole('heading', { name: 'Mixed assets' })).toBeVisible();

    // Switch to the Items tab — the first item is auto-selected.
    await page.getByTestId('tab-items').click();
    await expect(page.getByTestId('items-grid')).toBeVisible();
    await expect(page.getByTestId('item-card')).toHaveCount(3);

    // ── 1. Image preview ──────────────────────────────────────────
    await page
      .getByTestId('item-card')
      .filter({ hasText: 'apron.png' })
      .click();
    await expect(page.getByTestId('preview-path')).toHaveText('apron.png');
    const imagePreview = page.getByTestId('media-preview');
    await expect(imagePreview).toHaveAttribute('data-kind', 'image');
    await expect(page.getByTestId('media-preview-image')).toBeVisible();
    await expect(page.getByTestId('media-preview-rotate')).toBeVisible();

    // ── 2. PDF preview ────────────────────────────────────────────
    await page
      .getByTestId('item-card')
      .filter({ hasText: 'manual.pdf' })
      .click();
    await expect(page.getByTestId('preview-path')).toHaveText('manual.pdf');
    await expect(imagePreview).toHaveAttribute('data-kind', 'pdf');
    await expect(page.getByTestId('media-preview-pdf-placeholder')).toBeVisible();
    await expect(page.getByTestId('media-preview-pdf-open')).toBeVisible();

    // ── 3. Audio preview ──────────────────────────────────────────
    await page
      .getByTestId('item-card')
      .filter({ hasText: 'briefing.mp3' })
      .click();
    await expect(page.getByTestId('preview-path')).toHaveText('briefing.mp3');
    await expect(imagePreview).toHaveAttribute('data-kind', 'audio');
    await expect(page.getByTestId('media-preview-audio')).toBeVisible();
    // The waveform placeholder is the audio kind's signature DOM node.
    // Bare presence is the contract — `toBeAttached` avoids flakiness
    // around CSS gradients that headless engines sometimes report as
    // "hidden" even when on-screen.
    await expect(page.getByTestId('media-preview-waveform-placeholder')).toBeAttached();

    // ── Metadata panel renders the metadata tree ─────────────────
    await expect(page.getByTestId('preview-metadata')).toContainText('SHA-256');
    await expect(
      page.getByTestId('preview-metadata').locator('[data-testid="tree"]').first()
    ).toBeVisible();

    // ── 4. Copy-media-reference action ────────────────────────────
    // Browsers expose `navigator.clipboard.writeText` only over HTTPS or
    // localhost in headless contexts; we patch it to a stub so the
    // success toast fires deterministically.
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: {
          writeText: (text: string) => {
            (window as unknown as { __copied?: string }).__copied = text;
            return Promise.resolve();
          }
        }
      });
    });
    await page.getByTestId('action-copy-reference').click();
    const copied = await page.evaluate(
      () => (window as unknown as { __copied?: string }).__copied ?? ''
    );
    const parsed = JSON.parse(copied);
    expect(parsed).toEqual({
      mediaSetRid: SET_RID,
      mediaItemRid: seededItems[2].rid,
      branch: 'main',
      schema: 'IMAGE'
    });
  });
});
