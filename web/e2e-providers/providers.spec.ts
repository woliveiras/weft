import { expect, test, type Page } from '@playwright/test';

const key = 'sk-artificial-provider-journey';
const wrongKey = 'sk-artificial-wrong-key';
const headers = { Authorization: 'Bearer e2e-provider-admin-token' };

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-provider-admin-token'));
});

async function read(page: Page, path: string) {
  const response = await page.request.get(path, { headers });
  expect(response.ok()).toBeTruthy();
  const text = await response.text();
  expect(text).not.toContain(key);
  expect(text).not.toContain(wrongKey);
  return JSON.parse(text);
}

async function configure(page: Page, name: string, model: string) {
  const fixture = await read(page, '/fixture');
  await page.goto('/#/models');
  const openaiPlugin=page.getByRole('listitem',{name:'Inference plugin OpenAI'});await expect(openaiPlugin).toBeVisible();
  if(await openaiPlugin.getByRole('button',{name:'Enable',exact:true}).isVisible())await openaiPlugin.getByRole('button',{name:'Enable',exact:true}).click();
  await expect(openaiPlugin.getByText('Enabled',{exact:true})).toBeVisible();
  await page.getByRole('button', { name: 'Add provider', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/^Name/).fill(name);
  await dialog.getByRole('combobox', { name: /^Type/ }).selectOption('openai');
  await dialog.getByLabel(/^Base URL/).fill(fixture.modelURL);
  await dialog.getByLabel(/^Default model/).fill(model);
  await dialog.getByLabel(/^API key/).fill(key);
  await dialog.getByText('Limits', { exact: true }).click();
  await dialog.getByLabel('Retries', { exact: true }).fill('-1');
  return dialog;
}

async function selectAndRun(page: Page, id: string, provider: string, model?: string) {
  await page.goto(`/#/g/${id}/canvas`);
  await page.locator('.react-flow__node').filter({ hasText: 'writer' }).click();
  const editor = page.getByRole('region', { name: 'Agent configuration' });
  await editor.getByLabel(/^Provider/).selectOption(provider);
  if (model) await editor.getByLabel(/^Model/).fill(model);
  await page.getByRole('button', { name: 'Save graph', exact: true }).click();
  await expect.poll(async () => (await read(page, `/api/v1/graphs/${id}`)).graph.spec.nodes[0].model.provider).toBe(provider);
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
}

test('configure, test, select and run the real LangGraph adapter without returning the key', async ({ page }) => {
  const dialog = await configure(page, 'browser-selected', 'journey-ok');
  await dialog.getByRole('button', { name: 'Test settings', exact: true }).click();
  await expect(dialog.getByRole('status')).toContainText('Inference works with journey-ok');
  await expect(dialog.getByRole('status')).toContainText('Tool calling works.');
  await dialog.getByRole('button', { name: 'Add provider', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const card = page.getByRole('article', { name: 'Provider browser-selected', exact: true });
  await card.getByRole('button', { name: 'Test connection', exact: true }).click();
  await expect(card).toContainText('Inference works with journey-ok');
  await card.getByRole('button', { name: 'Edit', exact: true }).click();
  await expect(page.getByRole('dialog').getByLabel(/^API key/)).toHaveValue('');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect((await read(page, '/api/v1/provider-settings')).default).toBe('unselected-default');
  await selectAndRun(page, 'provider-success', 'browser-selected');
  await expect.poll(async () => (await read(page, '/api/v1/graphs/provider-success/executions')).some(
    (e: { status: string; summary: string }) => e.status === 'succeeded' && e.summary === 'LANGGRAPH_GATEWAY_OK',
  )).toBe(true);
  await expect(page.getByText('LANGGRAPH_GATEWAY_OK', { exact: true }).first()).toBeVisible();
  for (const path of ['providers', 'providers/browser-selected', 'provider-settings', 'graphs/provider-success/events']) {
    await read(page, `/api/v1/${path}`);
  }
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain(key);
  expect(await page.locator('body').innerText()).not.toContain(key);
});

test('invalid configuration and provider errors remain failures with redacted credentials', async ({ page }) => {
  const dialog = await configure(page, 'browser-failing', 'journey-ok');
  await dialog.getByLabel(/^Base URL/).fill('invalid-url');
  await dialog.getByRole('button', { name: 'Test settings', exact: true }).click();
  await expect(dialog).toContainText('Use an http:// or https:// URL.');
  const { modelURL } = await read(page, '/fixture');
  await dialog.getByLabel(/^Base URL/).fill(modelURL);
  await dialog.getByLabel(/^API key/).fill(wrongKey);
  await dialog.getByRole('button', { name: 'Test settings', exact: true }).click();
  await expect(dialog.getByRole('status')).toContainText('Inference failed');
  expect(await dialog.getByRole('status').innerText()).not.toContain(wrongKey);
  await dialog.getByLabel(/^API key/).fill(key);
  await dialog.getByLabel(/^Default model/).fill('journey-fail');
  await dialog.getByRole('button', { name: 'Test settings', exact: true }).click();
  await expect(dialog.getByRole('status')).toContainText('deterministic provider failure');
  expect(await dialog.getByRole('status').innerText()).not.toContain(key);
  await dialog.getByRole('button', { name: 'Add provider', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await selectAndRun(page, 'provider-failure', 'browser-failing', 'journey-fail');
  await expect.poll(async () => (await read(page, '/api/v1/graphs/provider-failure/executions')).some(
    (e: { status: string }) => e.status === 'failed',
  )).toBe(true);
  await read(page, '/api/v1/graphs/provider-failure/events');
  expect(await page.locator('body').innerText()).not.toContain(key);
  await page.getByRole('button', { name: 'Stop all', exact: true }).click();
});

test('missing provider blocks execution before reaching the runtime', async ({ page }) => {
  await page.goto('/#/g/provider-missing/canvas');
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'not-configured' }).first()).toBeVisible();
  expect(await read(page, '/api/v1/graphs/provider-missing/executions')).toHaveLength(0);
});
