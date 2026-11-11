import { expect, test } from '@playwright/test';

const admin = { Authorization: 'Bearer e2e-admin-token' };

test('administrators add a source control connection and tasks choose it as their hand-off', async ({ page }) => {
  await page.goto('/#/connections');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  await page.getByRole('button', { name: 'Add source control connection', exact: true }).click();
  const form = page.getByRole('form', { name: 'Add source control connection' });
  await form.getByLabel('Name').fill('e2e-github');
  await form.getByLabel('Repository').fill('acme/app');
  await form.getByLabel('Token').fill('synthetic-scm-token');
  await form.getByRole('button', { name: 'Save connection', exact: true }).click();
  const card = page.getByRole('listitem', { name: 'Source control e2e-github' });
  await expect(card).toContainText('acme/app · branches weft/* · pull requests need approval');
  // The token never comes back to the browser.
  const listed = await (await page.request.get('/api/v1/scm/connections', { headers: admin })).text();
  expect(listed).not.toContain('synthetic-scm-token');
  await expect(page.locator('body')).not.toContainText('synthetic-scm-token');

  await page.goto('/#/tasks/new');
  await page.getByText('Review settings').click();
  await page.getByRole('textbox', { name: 'Title', exact: true }).fill('Hand-off task');
  await page.getByRole('textbox', { name: 'Request', exact: true }).fill('Change the parser and open a pull request.');
  await page.getByRole('combobox', { name: 'Workflow', exact: true }).selectOption('git-inspect');
  await page.getByLabel(/^Source control hand-off/).selectOption('e2e-github');
  await page.getByRole('button', { name: 'Save draft', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Draft saved' })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel(/^Source control hand-off/)).toHaveValue('e2e-github');

  // Clean up for later journeys.
  page.on('dialog', (d) => void d.accept());
  await page.goto('/#/connections');
  await page.getByRole('listitem', { name: 'Source control e2e-github' }).getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByRole('listitem', { name: 'Source control e2e-github' })).toHaveCount(0);
});
