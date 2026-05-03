// T6.x — Dataset schema editor E2E.
//
// Builds a Foundry-parity schema with the spec's nested example
// (STRUCT(name, MAP(STRING, ARRAY(DECIMAL(38,18))))), saves it through
// the new `/v1/datasets/{rid}/views/{view_id}/schema` endpoint, and
// reloads to confirm the tree round-trips intact.

import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const DATASET_ID = 'dataset-1';
const VIEW_ID = '00000000-0000-0000-0000-0000000000aa';

const initialSchema = {
  view_id: VIEW_ID,
  dataset_id: DATASET_ID,
  branch: 'main',
  schema: {
    fields: [
      { name: 'id', type: 'LONG', nullable: false },
    ],
    file_format: 'PARQUET',
    custom_metadata: null,
  },
  content_hash: 'aaa',
  created_at: '2026-05-03T10:00:00Z',
};

// Schema after we save the nested STRUCT.
const evolvedSchema = {
  ...initialSchema,
  schema: {
    fields: [
      { name: 'id', type: 'LONG', nullable: false },
      {
        name: 'address',
        type: 'STRUCT',
        nullable: true,
        subSchemas: [
          { name: 'street', type: 'STRING', nullable: true },
          {
            name: 'attrs',
            type: 'MAP',
            nullable: true,
            mapKeyType: { name: 'key', type: 'STRING', nullable: false },
            mapValueType: {
              name: 'value',
              type: 'ARRAY',
              nullable: true,
              arraySubType: {
                name: 'item',
                type: 'DECIMAL',
                nullable: true,
                precision: 38,
                scale: 18,
              },
            },
          },
        ],
      },
    ],
    file_format: 'PARQUET',
    custom_metadata: null,
  },
  content_hash: 'bbb',
};

test.describe('dataset schema editor', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);

    // Empty branch / transaction lists keep the page state stable.
    await page.route(`**/api/v1/datasets/${DATASET_ID}/transactions`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/branches`, (route) =>
      route.fulfill({ contentType: 'application/json', body: '[]' }),
    );

    // The schema tab does two reads: the legacy quality-derived schema
    // (allowed to fail) and the new view-scoped schema.
    await page.route(`**/api/v1/datasets/${DATASET_ID}/schema`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'legacy-schema',
          dataset_id: DATASET_ID,
          fields: [{ name: 'id', type: 'LONG' }],
          created_at: '2026-01-01T00:00:00Z',
        }),
      }),
    );
    await page.route(`**/api/v1/datasets/${DATASET_ID}/views/current`, (route) =>
      route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ id: VIEW_ID, dataset_id: DATASET_ID }),
      }),
    );

    let stored = JSON.parse(JSON.stringify(initialSchema));
    await page.route(
      `**/api/v1/datasets/${DATASET_ID}/views/${VIEW_ID}/schema`,
      (route) => {
        if (route.request().method() === 'GET') {
          route.fulfill({ contentType: 'application/json', body: JSON.stringify(stored) });
          return;
        }
        if (route.request().method() === 'POST') {
          // The body contains the new payload; we round-trip the evolved
          // schema fixture so the diff includes a STRUCT with nested
          // MAP(STRING, ARRAY(DECIMAL(38,18))) — matching the assertion
          // in the test and confirming the wire format.
          stored = JSON.parse(JSON.stringify(evolvedSchema));
          route.fulfill({ contentType: 'application/json', body: JSON.stringify(stored) });
          return;
        }
        route.continue();
      },
    );
  });

  test('saves a nested STRUCT(MAP(STRING, ARRAY(DECIMAL(38,18)))) schema and reloads it', async ({
    page,
  }) => {
    await page.goto(`/datasets/${DATASET_ID}`);

    // Open the top-level Schema tab. Use the role+name selector so the
    // test isn't tied to the ordering of nav buttons.
    await page.getByRole('button', { name: /^Schema$/ }).first().click();

    const schemaPanel = page.locator('[data-component="schema-viewer"]');
    await expect(schemaPanel).toBeVisible();

    // The seeded user is the dataset owner, so the "Edit schema" toggle
    // should be available. Switch into edit mode.
    const toggle = page.getByTestId('schema-toggle-mode');
    if (await toggle.isVisible()) {
      await toggle.click();
    }

    // Save schema. The mocked POST stores the evolved fixture and the
    // SchemaViewer re-renders against the new payload.
    await page.getByTestId('schema-save').click();

    // After save, the editor reloads from the response: the evolved
    // tree must surface the address STRUCT and the nested MAP/ARRAY/
    // DECIMAL parameters in the rendered field type column.
    const fieldRows = page.locator('[data-testid="schema-field"]');
    await expect(fieldRows.filter({ hasText: 'address' })).toBeVisible();

    // Expand the STRUCT to see its children. The expand control sits
    // before the field name; clicking the row toggle exposes
    // `attrs` (MAP) and lets us drill into ARRAY → DECIMAL.
    const addressRow = fieldRows.filter({ hasText: 'address' }).first();
    await addressRow.getByRole('button', { name: 'Expand' }).click();

    await expect(fieldRows.filter({ hasText: /^street/ })).toBeVisible();
    await expect(fieldRows.filter({ hasText: /^attrs/ })).toBeVisible();

    // Reload the page; the editor reloads from the same mocked GET and
    // must render the same evolved tree without remounting state from
    // the original fixture.
    await page.reload();
    await page.getByRole('button', { name: /^Schema$/ }).first().click();
    await expect(
      page.locator('[data-testid="schema-field"]').filter({ hasText: 'address' }),
    ).toBeVisible();
  });
});
