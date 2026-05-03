// H3 — smoke test for the media-sets Permissions tab.
//
// Walks the operator through:
//   1. Opening the Permissions tab on a media-set detail page.
//   2. Seeing the current direct markings rendered as `MarkingBadge`s.
//   3. Opening "Edit markings", toggling a marking, running the Cedar
//      dry-run preview ("X users will lose access"), and committing
//      the change via PATCH.
//
// The Rust services are mocked end-to-end. We assert the request
// bodies hitting `/markings/preview` + `PATCH /markings` carry the
// expected normalised payload, since that is the integration contract
// between the EditMarkingsModal and `media-sets-service`.
import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const SET_RID =
  'ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000042';

const initialSet = {
  rid: SET_RID,
  project_rid: 'ri.foundry.main.project.default',
  name: 'Permissions fixture set',
  schema: 'IMAGE',
  allowed_mime_types: ['image/png'],
  transaction_policy: 'TRANSACTIONLESS',
  retention_seconds: 0,
  virtual: false,
  source_rid: null,
  markings: ['public'],
  created_at: '2026-05-01T00:00:00Z',
  created_by: 'tester'
};

test.describe('media set detail — permissions tab', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('opens edit markings modal, runs dry-run, and persists the change', async ({
    page
  }) => {
    let currentSet = { ...initialSet };

    // Track the request bodies the modal sent so we can assert them.
    const previewRequests: Array<{ markings: string[] }> = [];
    const patchRequests: Array<{ markings: string[] }> = [];

    // ── Mock GET /api/v1/media-sets/{rid} ─────────────────────────
    await page.route(/\/api\/v1\/media-sets\/[^/]+$/, async (route) => {
      if (route.request().method() !== 'GET') {
        return route.fulfill({ status: 405, body: '{}' });
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(currentSet)
      });
    });

    // ── Mock POST /api/v1/media-sets/{rid}/markings/preview ───────
    await page.route(
      /\/api\/v1\/media-sets\/[^/]+\/markings\/preview$/,
      async (route) => {
        const body = JSON.parse(route.request().postData() ?? '{}') as {
          markings: string[];
        };
        previewRequests.push(body);
        const next = body.markings;
        const current = currentSet.markings;
        const added = next.filter((m) => !current.includes(m));
        const removed = current.filter((m) => !next.includes(m));
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            markings: next,
            current_markings: current,
            added,
            removed,
            // Two synthetic users; the modal only needs > 0 to render
            // the "will lose access" warning in the rose colour.
            users_losing_access: added.length > 0 ? 2 : 0
          })
        });
      }
    );

    // ── Mock PATCH /api/v1/media-sets/{rid}/markings ──────────────
    await page.route(
      /\/api\/v1\/media-sets\/[^/]+\/markings$/,
      async (route) => {
        if (route.request().method() !== 'PATCH') {
          return route.fulfill({ status: 405, body: '{}' });
        }
        const body = JSON.parse(route.request().postData() ?? '{}') as {
          markings: string[];
        };
        patchRequests.push(body);
        currentSet = { ...currentSet, markings: body.markings };
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(currentSet)
        });
      }
    );

    // ── Navigate ──────────────────────────────────────────────────
    await page.goto(`/media-sets/${encodeURIComponent(SET_RID)}`);
    await expect(
      page.getByRole('heading', { name: 'Permissions fixture set' })
    ).toBeVisible();

    // Open the Permissions tab.
    await page.getByTestId('tab-permissions').click();
    const panel = page.getByTestId('media-permissions-panel');
    await expect(panel).toBeVisible();

    // Initial direct markings render the existing PUBLIC badge.
    const directMarkings = page.getByTestId('direct-markings');
    await expect(
      directMarkings.locator('[data-marking-id="public"]')
    ).toBeVisible();

    // ── Open the Edit markings modal ──────────────────────────────
    await page.getByTestId('open-edit-markings').click();
    const modal = page.getByTestId('edit-markings-modal');
    await expect(modal).toBeVisible();

    // Toggle PII on top of the existing PUBLIC selection.
    await page.getByTestId('edit-markings-pii').check();

    // ── Dry-run preview ───────────────────────────────────────────
    // The modal is `fixed inset-0` and the dialog is taller than the
    // headless viewport with the diff card mounted; Playwright's
    // viewport check trips even with `force: true`, so we dispatch
    // the click event directly through the DOM.
    await page.getByTestId('edit-markings-preview').dispatchEvent('click');
    const previewResult = page.getByTestId('edit-markings-preview-result');
    await expect(previewResult).toBeVisible();
    await expect(previewResult).toContainText('Added:');
    await expect(previewResult).toContainText('pii');
    await expect(previewResult).toContainText('2 users will lose access');

    expect(previewRequests).toHaveLength(1);
    // Selection order is insertion order; the modal preserves it on the
    // wire — `media-sets-service` is responsible for dedup + sort.
    expect(previewRequests[0].markings.sort()).toEqual(['pii', 'public']);

    // ── Save markings ─────────────────────────────────────────────
    await page.getByTestId('edit-markings-save').dispatchEvent('click');

    // Modal closes once the PATCH resolves; the panel re-renders with
    // the new direct marking list.
    await expect(modal).toBeHidden();
    await expect(
      directMarkings.locator('[data-marking-id="pii"]')
    ).toBeVisible();
    await expect(
      directMarkings.locator('[data-marking-id="public"]')
    ).toBeVisible();

    expect(patchRequests).toHaveLength(1);
    expect(patchRequests[0].markings.sort()).toEqual(['pii', 'public']);
  });
});
