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
  const repository = workspace.getByRole('textbox', { name: 'Repository URL', exact: true });
  await expect(page.getByRole('tab', { name: 'Settings' })).toHaveAttribute('aria-selected', 'true');
  await expect(workspace.getByRole('heading')).toBeFocused();
  await expect(page.locator('.react-flow')).toHaveCount(0);
  await expect(nav.getByRole('link')).toHaveText([
    'General', 'Task defaults', 'Workspace · local', 'Environment · not built', 'Network', 'Triggers · 0', 'Parameters · 0', 'Safety',
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
  const maxHops = page.getByRole('spinbutton', { name: 'Max hops', exact: true });
  await maxHops.scrollIntoViewIfNeeded();
  await expect(maxHops).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
});

test('fits the workflow toolbar and aligns Definition and Settings on wide and narrow screens', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  const created = await page.request.post('/api/v1/graphs', {
    headers: { Authorization: 'Bearer e2e-admin-token' },
    data: { metadata: { name: 'Workflow layout' }, spec: { nodes: [{ id: 'echo', runtime: { kind: 'echo' } }] } },
  });
  expect(created.ok()).toBe(true);
  const { graph } = await created.json();
  await page.goto(`/#/g/${graph.metadata.id}/settings/general`);

  const settings = page.getByRole('region', { name: 'General' });
  const title = await settings.getByRole('heading').boundingBox();
  const name = await settings.getByRole('textbox', { name: 'Name' }).boundingBox();
  expect(name?.x).toBe(title?.x);
  await expect(page.getByRole('button', { name: 'Kill', exact: true })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1280);

  await page.getByRole('tab', { name: 'Definition', exact: true }).click();
  const yaml = page.getByRole('textbox', { name: 'Graph YAML' });
  const versions = page.getByRole('heading', { name: 'Versions', exact: true });
  await expect(page.getByRole('button', { name: 'Export saved' })).toBeInViewport();
  expect((await versions.boundingBox())!.x).toBeGreaterThan((await yaml.boundingBox())!.x);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1280);

  await page.setViewportSize({ width: 390, height: 844 });
  const editor = (await yaml.boundingBox())!;
  const history = (await versions.boundingBox())!;
  expect(history.y).toBeGreaterThan(editor.y + editor.height);
  expect(history.x).toBe(editor.x);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
});
