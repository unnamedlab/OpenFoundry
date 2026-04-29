import { expect, test } from '@playwright/test';

import { mockFrontendApis, seedAuthenticatedSession } from './support/api';

test.describe('projects and files flow', () => {
  test.beforeEach(async ({ page }) => {
    await mockFrontendApis(page);
    await seedAuthenticatedSession(page);
  });

  test('opens the Files entry point and creates a project in a live space', async ({ page }) => {
    await page.goto('/');

    await page.getByRole('link', { name: 'Files' }).click();
    await expect(page).toHaveURL('/projects');
    await expect(page.getByRole('button', { name: 'New project' })).toBeVisible();

    await page.getByRole('button', { name: 'New project' }).click();
    await expect(page.getByRole('heading', { name: 'Create a project' })).toBeVisible();
    await expect(page.getByLabel('Organization space')).toHaveValue('operations');
    await expect(page.locator('#project-space option[value="archive"]')).toBeDisabled();
    await expect(
      page.getByText('Spaces without access are disabled.'),
    ).toBeVisible();

    await page.getByLabel('Organization space').selectOption('research');
    await page.getByLabel('Project name').fill('Learning');
    await page.getByRole('button', { name: 'Continue' }).evaluate((element) => {
      (element as HTMLButtonElement).click();
    });
    await page.getByRole('button', { name: 'Create project' }).evaluate((element) => {
      (element as HTMLButtonElement).click();
    });

    await expect(page.getByText('Learning')).toBeVisible();
    await expect(page.getByText('Research Lab / research')).toBeVisible();
  });
});
