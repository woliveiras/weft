import { readFileSync } from "node:fs";
import { parse } from "yaml";
import type { Execution, MessageRecord } from "../src/api/types";
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
  await page.goto('/#/models/plugins');
  const openaiPlugin=page.getByRole('listitem',{name:'Inference plugin OpenAI'});await expect(openaiPlugin).toBeVisible();
  if(await openaiPlugin.getByRole('button',{name:'Enable',exact:true}).isVisible())await openaiPlugin.getByRole('button',{name:'Enable',exact:true}).click();
  await expect(openaiPlugin.getByText('Enabled',{exact:true})).toBeVisible();
  await page.goto('/#/models/providers');
  await page.getByRole('button', { name: 'Add provider', exact: true }).click();
  const dialog = page.getByRole('form', { name: 'Provider settings' });
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
  await page.getByRole('button', { name: 'Configure agent', exact: true }).click();
  const editor = page.getByRole('region', { name: 'Agent configuration' });
  await editor.getByLabel(/^Provider/).selectOption(provider);
  if (model) await editor.getByLabel(/^Model/).fill(model);
  await page.getByRole('button', { name: 'Review & save', exact: true }).click();
  await page.getByRole('button', { name: 'Save graph draft', exact: true }).click();
  await expect.poll(async () => (await read(page, `/api/v1/graphs/${id}`)).graph.spec.nodes[0].model.provider).toBe(provider);
  await page.goto(`/#/g/${id}/canvas`);
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
}

test('configure, test, select and run the real Eino adapter without returning the key', async ({ page }) => {
  const originalDefault = (await read(page, '/api/v1/provider-settings')).default;
  expect(originalDefault).not.toBe('browser-selected');
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
  await expect(page.getByRole('form', { name: 'Provider settings' }).getByLabel(/^API key/)).toHaveValue('');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect((await read(page, '/api/v1/provider-settings')).default).toBe(originalDefault);
  await selectAndRun(page, 'provider-success', 'browser-selected');
  await expect.poll(async () => (await read(page, '/api/v1/graphs/provider-success/executions')).some(
    (e: { status: string; summary: string }) => e.status === 'succeeded' && e.summary === 'EINO_GATEWAY_OK',
  )).toBe(true);
  const runs = await read(page, '/api/v1/graphs/provider-success/executions');
  const completed = runs.find((e: { status: string }) => e.status === 'succeeded');
  await page.goto(`/#/g/provider-success/runs?execution=${encodeURIComponent(completed.id)}&section=result`);
  await expect(page.getByText('EINO_GATEWAY_OK', { exact: true }).first()).toBeVisible();
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
  const events = await read(page, '/api/v1/graphs/provider-failure/events');
  expect(events.some((e: { executionId?: string; data?: { operation?: string; providerStatus?: number } }) =>
    e.executionId && e.data?.operation === 'llm.chat' && e.data?.providerStatus === 503,
  )).toBe(true);
  expect(await page.locator('body').innerText()).not.toContain(key);
  await page.getByRole('button', { name: 'Stop all', exact: true }).click();
});

test('missing provider blocks execution before reaching the runtime', async ({ page }) => {
  await page.goto('/#/g/provider-missing/canvas');
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'not-configured' }).first()).toBeVisible();
  expect(await read(page, '/api/v1/graphs/provider-missing/executions')).toHaveLength(0);
});

test('save, export and reopen the summary pilot and settle its two proposals', async ({ page }) => {
  const pilot = parse(readFileSync('../examples/graphs/eino-summary.yaml', 'utf8'));
  const id = pilot.metadata.id;
  const graph = `/api/v1/graphs/${id}`;
  const { runtimeURL } = await read(page, '/fixture');
  expect((await page.request.post('/api/v1/graphs', { headers, data: pilot })).status()).toBe(201);
  const dialog = await configure(page, 'pilot-model', 'journey-ok');
  await dialog.getByRole('button', { name: 'Add provider', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const openEditor = async () => {
    await page.goto(`/#/g/${id}/canvas`);
    await page.locator('.react-flow__node').filter({ hasText: 'summarize' }).click();
    await page.getByRole('button', { name: 'Configure agent', exact: true }).click();
    return page.getByRole('region', { name: 'Agent configuration' });
  };
  const editor = await openEditor();
  await editor.getByLabel(/^Endpoint/).fill(runtimeURL);
  await editor.getByLabel(/^Provider/).selectOption('pilot-model');
  await page.getByRole('button', { name: 'Review & save', exact: true }).click();
  await page.getByRole('button', { name: 'Save graph draft', exact: true }).click();
  await expect.poll(async () => (await read(page, graph)).record.version).toBe(2);
  await page.goto(`/#/g/${id}/settings/general`);
  await page.getByRole('tab', { name: 'Definition', exact: true }).click();
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Export saved' }).click();
  const exported = parse(readFileSync((await (await download).path())!, 'utf8'));
  expect(exported.spec.nodes[0]).toMatchObject({
    runtime: { kind: 'external', framework: 'eino', agent: 'summarize', endpoint: runtimeURL },
    model: { provider: 'pilot-model' }, tools: [{ name: 'filesystem.read', paths: ['docs/**'] }],
  });
  expect(exported.spec.channels.find((c: { id: string }) => c.id === 'summaries')).toMatchObject({ approval: true, to: ['accepted'] });
  await page.reload();
  const reopened = await openEditor();
  await expect(reopened.getByLabel(/^Endpoint/)).toHaveValue(runtimeURL);
  await expect(reopened.getByLabel(/^Framework/)).toHaveValue('eino');
  await expect(reopened.getByLabel(/^Provider/)).toHaveValue('pilot-model');
  await page.goto(`/#/g/${id}/canvas`);
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
  for (const decision of ['Reject', 'Approve']) {
    const submitted = await page.request.post(`${graph}/channels/requests/messages`, {
      headers, data: { type: 'summary.requested', payload: { path: 'docs/input.md' } },
    });
    expect(submitted.status()).toBe(201);
    const input = await submitted.json();
    const pending = `/api/v1/approvals?graph=${id}&status=pending`;
    await expect.poll(async () => (await read(page, pending)).length).toBe(1);
    const [approval] = await read(page, pending);
    expect(approval.detail.message.payload).toEqual({ sourceMessageId: input.id, summary: 'Seven green apples.' });
    await expect.poll(async () => (await read(page, `${graph}/executions`)).find((x: Execution) => x.id === approval.executionId)).toMatchObject({
      status: 'succeeded', toolCalls: 1, usage: { inputTokens: 12, outputTokens: 1 },
    });
    expect((await read(page, `${graph}/executions`)).filter((x: Execution) => x.nodeId === 'accepted')).toHaveLength(0);
    await page.goto('/#/approvals');
    await page.getByRole('table', { name: 'Approval history' }).locator('.approval-request-link').click();
    await page.getByRole('dialog').getByRole('button', { name: decision, exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect.poll(async () => (await read(page, `${graph}/messages?channel=summaries`)).some((r: MessageRecord) =>
      r.message.id === approval.detail.message.id && r.status === (decision === 'Reject' ? 'rejected' : 'acked'),
    )).toBe(true);
  }
});
