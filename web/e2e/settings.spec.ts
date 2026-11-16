import { expect, test } from '@playwright/test';

test('opens settings by section, preserves the graph draft in history and links a validation error', async ({ page }) => {
  const headers = { Authorization: 'Bearer e2e-admin-token' };
  const created = await page.request.post('/api/v1/graphs', { headers, data: {
    metadata: { name: 'Settings navigation' },
    spec: { nodes: [{ id: 'echo', runtime: { kind: 'echo' } }] },
  } });
  expect(created.ok()).toBeTruthy();
  const { graph } = await created.json();
  const base = `/#/g/${graph.metadata.id}`;
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto(`${base}/settings/workspace`);
  const nav = page.getByRole('navigation', { name: 'Graph settings sections' });
  const workspace = page.getByRole('region', { name: 'Workspace' });
  const repository = workspace.getByLabel('Repository URL');
  await expect(page.getByRole('tab', { name: 'Settings' })).toHaveAttribute('aria-selected', 'true');
  await expect(workspace.getByRole('heading')).toBeFocused();
  await expect(page.locator('.react-flow')).toHaveCount(0);
  await expect(nav.getByRole('link')).toHaveText([
    'General', 'Workspace · local', 'Environment · not built', 'Network', 'Triggers · 0', 'Parameters · 0', 'Safety',
  ]);
  await repository.fill('/var/lib/weft/other');
  await nav.getByRole('link', { name: 'Network', exact: true }).press('Enter');
  await expect(page.getByRole('heading', { name: 'Network', exact: true })).toBeFocused();
  await expect(page.getByRole('link', { name: 'Administration → Network policies' })).toHaveAttribute('href', '#/administration/network');
  await page.goBack();
  await expect(repository).toHaveValue('/var/lib/weft/other');
  await expect(page.getByText('unsaved graph changes', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Save graph', exact: true }).click();
  const localIssues = workspace.getByRole('list', { name: 'Workspace validation' });
  await expect(localIssues).toContainText('spec.workspace.repo');
  await expect(nav.getByRole('link', { name: /^Workspace.*Error$/ })).toBeVisible();
  await page.getByRole('tab', { name: 'Graph', exact: true }).click();
  await page.getByRole('list', { name: 'Graph validation' }).getByRole('link', { name: 'Graph settings' }).click();
  await expect(page).toHaveURL(new RegExp(`${base.slice(1)}/settings/workspace$`));
  await expect(localIssues).toBeVisible();
  await page.getByRole('button', { name: 'Discard', exact: true }).click();
  await expect(repository).toHaveValue('');
  await expect(localIssues).toHaveCount(0);

  await page.setViewportSize({ width: 390, height: 844 });
  await nav.getByRole('link', { name: 'General' }).click();
  const maxHops = page.getByLabel('Max hops');
  await maxHops.scrollIntoViewIfNeeded();
  await expect(maxHops).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
});
