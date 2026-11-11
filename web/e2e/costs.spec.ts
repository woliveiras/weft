import { expect, test } from '@playwright/test';

test('administrators price models and set budgets; viewers only read them', async ({ page }) => {
  await page.goto('/#/models');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  const prices = page.getByRole('form', { name: 'Model prices' });
  await expect(prices).toContainText(/No prices saved yet|Version/);
  await prices.getByRole('button', { name: 'Add price', exact: true }).click();
  const row = prices.getByRole('group').last();
  await row.getByLabel('Provider name').fill('e2e');
  await row.getByLabel(/^Input/).fill('2.5');
  await row.getByLabel(/^Output/).fill('10');
  await prices.getByRole('button', { name: 'Save prices', exact: true }).click();
  await expect(prices.getByRole('status')).toContainText(/Prices saved as version \d+\./);

  const budgets = page.getByRole('form', { name: 'Budget settings' });
  await budgets.getByLabel(/^Task run budget/).fill('1.5');
  await budgets.getByRole('button', { name: 'Add monthly budget', exact: true }).click();
  await budgets.getByRole('group').last().getByLabel(/^Monthly limit/).fill('20');
  await budgets.getByRole('button', { name: 'Save budgets', exact: true }).click();
  await expect(budgets.getByRole('status')).toHaveText('Budgets saved.');
  await expect(page.getByRole('group', { name: /^Spend in \d{4}-\d{2}$/ })).toBeVisible();

  // Persisted on the server, not only in the page.
  await page.reload();
  await expect(page.getByRole('form', { name: 'Budget settings' }).getByLabel(/^Task run budget/)).toHaveValue('1.5');
  const saved = page.getByRole('form', { name: 'Model prices' }).getByRole('group').last();
  await expect(saved.getByLabel('Provider name')).toHaveValue('e2e');
  await expect(saved.getByLabel(/^Output/)).toHaveValue('10');

  // New tasks show the budget they are held to.
  await page.goto('/#/tasks/new');
  await page.getByText('Review settings').click();
  await page.getByRole('combobox', { name: 'Workflow', exact: true }).selectOption('git-inspect');
  await expect(page.getByRole('region', { name: 'Budget' })).toContainText('This run may spend up to $1.50');

  // Viewers see the same data with every control disabled.
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.goto('/#/models');
  await page.reload();
  await expect(page.getByRole('form', { name: 'Model prices' }).getByRole('button', { name: 'Save prices' })).toBeDisabled();
  await expect(page.getByRole('form', { name: 'Budget settings' }).getByRole('button', { name: 'Save budgets' })).toBeDisabled();

  // Restore the installation for later journeys.
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  const reset = await page.request.put('/api/v1/cost/policy', { headers: { Authorization: 'Bearer e2e-admin-token' }, data: { taskRunBudgetMicros: 0, warningPercent: 80 } });
  expect(reset.ok()).toBe(true);
  expect((await page.request.put('/api/v1/cost/budgets', { headers: { Authorization: 'Bearer e2e-admin-token' }, data: { budgets: [] } })).ok()).toBe(true);
});
