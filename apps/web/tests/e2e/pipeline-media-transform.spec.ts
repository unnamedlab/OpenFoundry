// U21 — Pipeline Builder media transform smoke.
//
// Walks the operator through:
//   1. Open the existing demo pipeline in the editor.
//   2. Add three media nodes from the palette
//      (MediaSetInput → media_transform/extract_text_ocr →
//       MediaSetOutput).
//   3. Verify the canvas reflects all 4 nodes (1 demo + 3 new) and
//      that `/api/v1/pipelines/_validate` was hit (Foundry-style
//      live validation).
//   4. Trigger an explicit "Validate" click as the dry-run signal —
//      the execution provider for media nodes is still stub-backed
//      per H1, so the response only needs to round-trip.
//
// The validate endpoint is mocked here because the support helper
// does not cover it; everything else (auth, /pipelines, /pipelines/{id})
// is wired through `mockFrontendApis`.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const PIPELINE_ID = 'pipeline-1';

type ValidationPayload = {
  nodes: Array<{ id: string; transform_type: string; config?: Record<string, unknown> }>;
};

test.describe('pipeline builder — media transform graph', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('drags MediaSetInput → ExtractTextOCR → MediaSetOutput and validates', async ({ page }) => {
    let validateCalls = 0;
    let lastValidationBody: ValidationPayload | null = null;

    await page.route('**/api/v1/pipelines/_validate', async (route) => {
      validateCalls += 1;
      const body = JSON.parse(route.request().postData() ?? '{}') as ValidationPayload;
      lastValidationBody = body;
      const nodeIds: string[] = (body.nodes ?? []).map((n) => n.id);
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          valid: true,
          errors: [],
          warnings: [],
          next_run_at: null,
          summary: {
            node_count: nodeIds.length,
            edge_count: 0,
            root_node_ids: nodeIds,
            leaf_node_ids: nodeIds
          }
        })
      });
    });

    await page.goto(`/pipelines/${PIPELINE_ID}/edit`);
    await expect(page.getByTestId('node-palette')).toBeVisible();

    // ── 1. Add MediaSetInput ──────────────────────────────────────
    await page.getByTestId('palette-entry-media_set_input').click();
    await expect(page.getByTestId('media-transform-editor')).toBeVisible();
    await page.getByTestId('media-input-rid').fill(
      'ri.foundry.main.media_set.018f-source'
    );

    // ── 2. Add the OCR media transform ────────────────────────────
    await page
      .getByTestId('palette-entry-media_transform-extract_text_ocr')
      .click();
    // The editor switches to the new node; the kind dropdown should be
    // pre-selected to `extract_text_ocr` (no required params).
    await expect(page.getByTestId('media-transform-kind')).toHaveValue(
      'extract_text_ocr'
    );

    // ── 3. Add MediaSetOutput ─────────────────────────────────────
    await page.getByTestId('palette-entry-media_set_output').click();
    await page.getByTestId('media-output-rid').fill(
      'ri.foundry.main.media_set.018f-target'
    );

    // ── 4. Canvas reflects 4 nodes (1 demo + 3 added) ─────────────
    // PipelineCanvas tags each rect with `data-testid="canvas-node-<id>"`
    // — anything matching that prefix counts.
    const nodes = page.locator('[data-testid^="canvas-node-"]');
    await expect(nodes).toHaveCount(4);

    // ── 5. Validate (dry-run) ─────────────────────────────────────
    // The canvas auto-validates after every change; click the explicit
    // button to ensure at least one call lands inside this assertion
    // window.
    await page.getByRole('button', { name: 'Validate' }).click();
    await expect.poll(() => validateCalls).toBeGreaterThan(0);

    // The validation payload includes the new media nodes with their
    // seeded config — confirms the wire format the backend expects.
    // The cast here breaks TS's control-flow narrowing: the assignment
    // to `lastValidationBody` happens inside the async route handler,
    // which TS can't prove runs before this read.
    const captured = lastValidationBody as ValidationPayload | null;
    const sentNodes: ValidationPayload['nodes'] = captured?.nodes ?? [];
    const transformTypes = sentNodes.map((n) => n.transform_type);
    expect(transformTypes).toContain('media_set_input');
    expect(transformTypes).toContain('media_transform');
    expect(transformTypes).toContain('media_set_output');

    const ocrNode = sentNodes.find(
      (n) => n.transform_type === 'media_transform' && (n.config as { kind?: string })?.kind === 'extract_text_ocr'
    );
    expect(ocrNode, 'OCR media transform should land in the validate payload').toBeTruthy();

    // No diagnostics surface as the mock returns valid.
    await expect(
      page.locator('[data-testid^="canvas-node-"][data-node-state="error"]')
    ).toHaveCount(0);
  });
});
