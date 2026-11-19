import { expect, test } from '@playwright/test';

const admin = { Authorization: 'Bearer e2e-admin-token' };

test('administrators add a source control connection and tasks choose it as their hand-off', async ({ page }) => {
  await page.goto('/#/connections');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  const add = page.getByRole('region', { name: 'Source control', exact: true }).getByRole('link', { name: 'Add connection', exact: true });
  await add.click();
  await expect(page).toHaveURL(/#\/connections\/source-control\/new$/);
  await expect(page.getByRole('table', { name: 'Source control connections' })).toHaveCount(0);
  await page.getByRole('link', { name: 'Cancel', exact: true }).click();
  await expect(page).toHaveURL(/#\/connections$/);
  await add.click();
  const form = page.getByRole('form', { name: 'Add source control connection' });
  await form.getByLabel('Name', { exact: true }).fill('e2e-github');
  await form.getByLabel('Repository', { exact: true }).fill('acme/app');
  await form.getByLabel('API URL', { exact: true }).fill(new URL('/api/v1/scm-fixture-unavailable', page.url()).href);
  await form.getByLabel('Token', { exact: true }).fill('synthetic-scm-token');
  await form.getByRole('button', { name: 'Save connection', exact: true }).click();
  await expect(page).toHaveURL(/#\/connections$/);
  const card = page.getByRole('row', { name: /e2e-github/ });
  await expect(card).toContainText('acme/app');
  await expect(card).toContainText('weft/*');
  await expect(card.getByRole('cell', { name: 'Required', exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('region', { name: 'Source control connection list' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  await page.setViewportSize({ width: 1280, height: 720 });
  await expect(card.getByText('Not tested', { exact: true })).toBeVisible();
  let releaseTest!: () => void;
  const waitForPending = new Promise<void>(resolve => { releaseTest = resolve; });
  await page.route('**/api/v1/scm/connections/e2e-github/test', async route => {
    await waitForPending;
    await route.continue();
  });
  await card.getByRole('button', { name: 'Test connection' }).click();
  try {
    await expect(card.getByRole('status')).toContainText('Checking connection');
    await expect(card.getByRole('button', { name: 'Testing…' })).toBeDisabled();
  } finally {
    releaseTest();
  }
  await expect(card.getByRole('status')).toContainText('Connection failed');
  await expect(card.getByRole('button', { name: 'Test connection' })).toBeEnabled();
  const scm = page.getByRole('region', { name: 'Source control', exact: true });
  const mcp = page.getByRole('region', { name: 'MCP connections', exact: true });
  for (const panel of [mcp, scm]) {
    const controls = panel.locator('.connection-filter-fields');
    const search = await controls.getByRole('searchbox').boundingBox();
    const status = await controls.getByRole('combobox').boundingBox();
    expect(search).not.toBeNull();
    expect(status).not.toBeNull();
    expect(Math.abs(search!.y - status!.y)).toBeLessThan(2);
    expect(status!.x).toBeGreaterThan(search!.x + search!.width);
  }
  await scm.getByRole('searchbox', { name: 'Search connections' }).fill('ACME/APP');
  await scm.getByRole('combobox', { name: 'Test status' }).selectOption('failed');
  await expect(card).toBeVisible();
  await scm.getByRole('combobox', { name: 'Test status' }).selectOption('connected');
  await expect(scm.getByText('No connections match these filters.')).toBeVisible();
  await scm.getByRole('button', { name: 'Clear filters' }).click();
  await expect(card).toBeVisible();
  await expect(scm.getByRole('searchbox')).toBeFocused();
  await mcp.getByRole('searchbox').fill('unmatched-mcp-connection');
  await expect(card).toBeVisible();
  await expect(mcp.getByText('No connections match these filters.')).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  await page.setViewportSize({ width: 1280, height: 720 });
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
  await page.getByRole('row', { name: /e2e-github/ }).getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByRole('row', { name: /e2e-github/ })).toHaveCount(0);
});
