import { expect, test, type Page } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

function captureErrors(page: Page) {
  const consoleErrors: string[] = [];
  const pageErrors: string[] = [];

  page.on('console', (message) => {
    if (message.type() === 'error') {
      consoleErrors.push(message.text());
    }
  });

  page.on('pageerror', (error) => {
    pageErrors.push(error.message);
  });

  return { consoleErrors, pageErrors };
}

test.describe('frontend verification smoke flows', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
  });

  test('supports login and renders the authenticated home surface', async ({ page }) => {
    const { consoleErrors, pageErrors } = captureErrors(page);

    await page.goto('/auth/login');

    await expect(page.getByRole('heading', { name: 'Sign in to OpenFoundry' })).toBeVisible();
    await page.getByLabel('Email').fill('operator@openfoundry.dev');
    await page.getByLabel('Password').fill('password123');
    await page.getByRole('button', { name: 'Sign In' }).click();

    await expect(page).toHaveURL('/');
    await expect(page.getByRole('heading', { name: 'Welcome to OpenFoundry' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Apps' })).toBeVisible();
    expect(consoleErrors).toEqual([]);
    expect(pageErrors).toEqual([]);
  });

  test('loads the critical routes used for smoke validation', async ({ page }) => {
    const { consoleErrors, pageErrors } = captureErrors(page);

    await seedAuthenticatedSession(page);
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Welcome to OpenFoundry' })).toBeVisible();

    await page.getByLabel('Datasets').click();
    await expect(page).toHaveURL('/datasets');
    await expect(page.getByRole('heading', { name: 'Data Catalog' })).toBeVisible();
    await expect(page.getByText('Aircraft health telemetry')).toBeVisible();
    await expect(page.getByPlaceholder('Full-text search by dataset name or description')).toBeVisible();

    await page.getByLabel('Pipelines').click();
    await expect(page).toHaveURL('/pipelines');
    await expect(page.getByRole('heading', { name: 'Pipeline Enhancements' })).toBeVisible();
    await expect(page.getByLabel('Pipeline name')).toBeVisible();

    await page.getByLabel('Ontology').click();
    await expect(page).toHaveURL('/ontology');
    await expect(page.getByRole('heading', { name: /Build the operational ontology/i })).toBeVisible();
    await expect(page.getByText('Ontology building overview')).toBeVisible();

    await page.goto('/apps');
    await expect(page.getByRole('heading', { name: 'Workshop App Builder' })).toBeVisible();
    await expect(page.getByPlaceholder('Search apps...')).toBeVisible();

    await page.goto('/dashboards');
    await expect(page.getByRole('heading', { name: /Quiver dashboards with real widgets/i })).toBeVisible();
    await expect(page.getByRole('button', { name: 'New Dashboard' })).toBeVisible();

    expect(consoleErrors).toEqual([]);
    expect(pageErrors).toEqual([]);
  });
});
