// U21 — Workshop "Media Preview" widget smoke.
//
// Mounts the published-app runtime route with a mocked app whose page
// contains a single `media_preview` widget bound to a media reference
// property. The widget should resolve the underlying media item via
// `/api/v1/items/{rid}` and mount the U14 `MediaPreview` component.

import { expect, test } from '@playwright/test';
import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

const APP_SLUG = 'media-preview-demo';

const MEDIA_SET_RID =
  'ri.foundry.main.media_set.018f0000-0000-0000-0000-000000000001';
const MEDIA_ITEM_RID =
  'ri.foundry.main.media_item.018f0000-0000-0000-0000-000000000010';

const mediaItem = {
  rid: MEDIA_ITEM_RID,
  media_set_rid: MEDIA_SET_RID,
  branch: 'main',
  transaction_rid: '',
  path: 'apron.png',
  mime_type: 'image/png',
  size_bytes: 2048,
  sha256: 'a'.repeat(64),
  metadata: {},
  storage_uri: `s3://media/${MEDIA_SET_RID}/main/aa/${'a'.repeat(64)}`,
  deduplicated_from: null,
  deleted_at: null,
  created_at: '2026-05-01T00:01:00Z'
};

// Foundry-shaped media reference cell value the widget consumes via
// `props.media_reference_property`.
const mediaReferenceJson = JSON.stringify({
  mediaSetRid: MEDIA_SET_RID,
  mediaItemRid: MEDIA_ITEM_RID,
  branch: 'main',
  schema: 'IMAGE'
});

const publishedApp = {
  app: {
    id: 'app-media-1',
    name: 'Media preview demo',
    slug: APP_SLUG,
    description: 'Preview a single image via the U21 Workshop widget.',
    status: 'published',
    pages: [
      {
        id: 'page-1',
        name: 'Overview',
        path: '/',
        description: '',
        layout: { kind: 'grid', columns: 1, gap: '16px', max_width: '1200px' },
        visible: true,
        widgets: [
          {
            id: 'widget-media-preview-1',
            widget_type: 'media_preview',
            title: 'Apron camera',
            description: 'Latest snapshot from the apron camera.',
            position: { x: 0, y: 0, width: 1, height: 1 },
            props: { media_reference_property: mediaReferenceJson },
            binding: null,
            events: [],
            children: []
          }
        ]
      }
    ],
    theme: {
      primary_color: '#0f766e',
      accent_color: '#f97316',
      background_color: '#f8fafc',
      surface_color: '#ffffff',
      text_color: '#0f172a',
      border_radius: 12,
      heading_font: 'Inter',
      body_font: 'Inter',
      logo_url: null
    },
    settings: {
      home_page_id: 'page-1',
      navigation_style: 'tabs',
      max_width: '1200px',
      show_branding: true,
      custom_css: null,
      builder_experience: 'workshop',
      ontology_source_type_id: null,
      object_set_variables: [],
      consumer_mode: {
        enabled: false,
        allow_guest_access: false,
        portal_title: null,
        portal_subtitle: null,
        primary_cta_label: null,
        primary_cta_url: null
      },
      interactive_workshop: {
        enabled: false,
        title: 'Interactive Workshop',
        subtitle: '',
        primary_scenario_widget_id: null,
        primary_agent_widget_id: null,
        briefing_template: '',
        suggested_questions: [],
        scenario_presets: []
      },
      workshop_header: { title: null, icon: 'cube', color: '#3b82f6' },
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
          files: []
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
          selected_group: null
        }
      }
    },
    template_key: null,
    created_by: 'user-1',
    published_version_id: 'version-1',
    created_at: '2026-05-01T00:00:00Z',
    updated_at: '2026-05-01T00:00:00Z'
  },
  embed: { url: '', iframe_html: '' },
  published_version_number: 1,
  published_at: '2026-05-01T00:30:00Z'
};

test.describe('workshop — media preview widget', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('renders MediaPreview for a media reference property', async ({ page }) => {
    await page.route(/\/api\/v1\/apps\/public\/[^/]+$/, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(publishedApp)
      });
    });

    await page.route(/\/api\/v1\/items\/[^/]+$/, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mediaItem)
      });
    });

    await page.route(
      /\/api\/v1\/items\/[^/]+\/download-url(\?.*)?$/,
      async (route) => {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            url: `https://mock-storage.test/${MEDIA_ITEM_RID}/${mediaItem.path}`,
            expires_at: '2026-05-01T01:00:00Z',
            headers: {},
            item: mediaItem
          })
        });
      }
    );

    await page.route('**/mock-storage.test/**', async (route) => {
      const onePixelPng = Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR4nGNgYAAAAAMAASsJTYQAAAAASUVORK5CYII=',
        'base64'
      );
      await route.fulfill({
        status: 200,
        contentType: 'image/png',
        body: onePixelPng
      });
    });

    await page.goto(`/apps/runtime/${APP_SLUG}`);

    // Wait for the published app to land + the widget to mount.
    await expect(page.getByTestId('widget-media-preview')).toBeVisible();
    await expect(
      page.getByTestId('widget-media-preview').locator('strong').first()
    ).toHaveText('Apron camera');

    // The widget should have resolved the item RID through
    // `/api/v1/items/{rid}` and forwarded it to the U14 MediaPreview
    // component, which renders the `image` kind for `image/png`.
    await expect(page.getByTestId('media-preview')).toHaveAttribute(
      'data-kind',
      'image'
    );
    await expect(page.getByTestId('media-preview-image')).toBeVisible();

    // The placeholder paths are NOT taken — confirms the resolution
    // ran end-to-end.
    await expect(
      page.getByTestId('widget-media-preview-empty')
    ).toHaveCount(0);
    await expect(
      page.getByTestId('widget-media-preview-error')
    ).toHaveCount(0);
  });
});
