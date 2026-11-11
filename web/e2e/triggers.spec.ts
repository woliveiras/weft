import { expect, test } from '@playwright/test';

test('administrators add an issue trigger to a workflow, preview matching issues and control it', async ({ page }) => {
  await page.goto('/#/g/git-inspect/canvas');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
  const section = page.getByRole('group', { name: 'Issue triggers' });
  await expect(section).toBeVisible();
  await section.getByRole('button', { name: 'Add issue trigger', exact: true }).click();
  const form = section.getByRole('form', { name: 'Add issue trigger' });
  await form.getByLabel('Name').fill('Ready issues');
  await form.getByLabel('MCP connection').fill('e2e-tracker');
  await form.getByLabel('Repository').fill('acme/app');
  await expect(form).toContainText('Saving records your consent: tasks start as you, with Ask authorization');
  await form.getByRole('button', { name: 'Save trigger', exact: true }).click();
  const card = section.getByRole('article', { name: 'Trigger Ready issues' });
  await expect(card.getByText('Active', { exact: true })).toBeVisible();
  await expect(card).toContainText('Runs as e2e-admin for workflow version');

  await card.getByRole('button', { name: 'Preview matching issues' }).click();
  const matches = card.getByRole('group', { name: 'Matching issues' });
  await expect(matches).toContainText('Next: acme/app#11 — Add CSV export');
  await expect(matches).toContainText('acme/app#12: is blocked');

  await card.getByRole('button', { name: 'Pause', exact: true }).click();
  await expect(card.getByText('Paused', { exact: true })).toBeVisible();
  await expect(card.getByRole('status')).toContainText('Paused by e2e-admin.');
  await card.getByRole('button', { name: 'Resume and record consent' }).click();
  await expect(card.getByText('Active', { exact: true })).toBeVisible();

  page.on('dialog', (d) => void d.accept());
  await card.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(section.getByRole('article', { name: 'Trigger Ready issues' })).toHaveCount(0);
});
