import { expect, test } from '@playwright/test';

const admin = { Authorization: 'Bearer e2e-admin-token' };

test('Bitbucket Cloud API-token and OAuth choices survive the console and API boundary without exposing secrets', async ({ page }, testInfo) => {
  await page.goto('/#/connections/source-control/new');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.reload();
  const form = page.getByRole('form', { name: 'Add source control connection' });
  await form.getByLabel('Kind', { exact: true }).selectOption('bitbucket-cloud');
  await expect(form.getByLabel('Authentication', { exact: true })).toHaveValue('api-token');
  await form.getByText('Create a Bitbucket API token', { exact: true }).click();
  // Capture the setup guidance before entering any account or credential.
  await page.setViewportSize({ width: 1280, height: 720 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1280);
  await page.screenshot({ path: testInfo.outputPath('bitbucket-api-token-desktop.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  await page.screenshot({ path: testInfo.outputPath('bitbucket-api-token-mobile.png'), fullPage: true });
  await page.setViewportSize({ width: 1280, height: 720 });
  await form.getByLabel('Name', { exact: true }).fill('e2e-bitbucket-api-token');
  await form.getByLabel('Repository', { exact: true }).fill('fixture-workspace/rehearsal');
  await form.getByLabel('API token', { exact: true }).fill('synthetic-bitbucket-api-token');
  await form.getByRole('button', { name: 'Save connection', exact: true }).click();
  await expect(page).toHaveURL(/#\/connections\/source-control\/new$/);
  await expect(form.getByLabel('Atlassian account email', { exact: true })).toBeFocused();
  await form.getByLabel('Atlassian account email', { exact: true }).fill('fixture@example.com');
  await expect(form.getByRole('link', { name: 'Atlassian account API tokens' })).toHaveAttribute('href', 'https://id.atlassian.com/manage-profile/security/api-tokens');
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  const saveRequest = page.waitForRequest(request => request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/scm/connections');
  await form.getByRole('button', { name: 'Save connection', exact: true }).click();
  expect((await saveRequest).postDataJSON()).toMatchObject({ kind: 'bitbucket-cloud', authType: 'api-token', username: 'fixture@example.com', token: 'synthetic-bitbucket-api-token' });
  await expect(page).toHaveURL(/#\/connections$/);
  await expect(page.getByRole('row', { name: /e2e-bitbucket-api-token/ })).toContainText('fixture-workspace/rehearsal');
  const listResponse = await page.request.get('/api/v1/scm/connections', { headers: admin });
  expect(listResponse.ok()).toBeTruthy();
  const listed = await listResponse.json();
  expect(listed.find((connection: { name: string }) => connection.name === 'e2e-bitbucket-api-token')).toMatchObject({ authType: 'api-token', username: 'fixture@example.com', tokenConfigured: true });
  expect(JSON.stringify(listed)).not.toContain('synthetic-bitbucket-api-token');
  await page.reload();
  await expect(page.getByRole('row', { name: /e2e-bitbucket-api-token/ })).toBeVisible();
  await expect(page.locator('body')).not.toContainText('synthetic-bitbucket-api-token');

  await page.goto('/#/connections/source-control/new');
  await form.getByLabel('Kind', { exact: true }).selectOption('bitbucket-cloud');
  await form.getByLabel('Atlassian account email', { exact: true }).fill('cleared@example.com');
  await form.getByLabel('API token', { exact: true }).fill('discarded-synthetic-api-token');
  await form.getByLabel('Authentication', { exact: true }).selectOption('bearer');
  await expect(form.getByLabel('Atlassian account email', { exact: true })).toHaveCount(0);
  await expect(form.getByLabel('Token', { exact: true })).toHaveValue('');
  await form.getByLabel('Name', { exact: true }).fill('e2e-bitbucket-bearer');
  await form.getByLabel('Repository', { exact: true }).fill('fixture-workspace/rehearsal');
  await form.getByLabel('Token', { exact: true }).fill('synthetic-bitbucket-bearer');
  await form.getByRole('button', { name: 'Save connection', exact: true }).click();
  await expect(page).toHaveURL(/#\/connections$/);
  const afterOAuth = await (await page.request.get('/api/v1/scm/connections', { headers: admin })).json();
  const bearer = afterOAuth.find((connection: { name: string }) => connection.name === 'e2e-bitbucket-bearer');
  expect(bearer).toMatchObject({ authType: 'bearer', tokenConfigured: true });
  expect(bearer.username).toBeFalsy();
  expect(JSON.stringify(afterOAuth)).not.toContain('synthetic-bitbucket-bearer');
  expect(JSON.stringify(afterOAuth)).not.toContain('discarded-synthetic-api-token');

  // Only disposable fixture connections were created; leave other journeys unchanged.
  for (const name of ['e2e-bitbucket-api-token', 'e2e-bitbucket-bearer']) {
    expect((await page.request.delete(`/api/v1/scm/connections/${name}`, { headers: admin })).ok()).toBeTruthy();
  }
});

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
