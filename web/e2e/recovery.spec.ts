import { expect, test, type Page } from '@playwright/test';

const headers = { Authorization: 'Bearer e2e-admin-token' };
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
});
async function read(page: Page, id: string, suffix: string) {
  const response = await page.request.get(`/api/v1/graphs/${id}/${suffix}`, { headers });
  expect(response.ok()).toBeTruthy();
  return response.json();
}
async function open(page: Page, id: string) {
  await page.goto(`/#/g/${id}/canvas`);
}
async function start(page: Page, id: string) {
  await open(page, id);
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
}
async function stop(page: Page, id: string) {
  await open(page, id);
  await page.getByRole('button', { name: 'Stop all', exact: true }).click();
  await expect.poll(async () => (await read(page, id, 'status')).state).toBe('stopped');
}
async function executed(page: Page, id: string) {
  await expect.poll(async () => (await read(page, id, 'executions')).some((e: { status: string }) => e.status === 'succeeded')).toBe(true);
}
async function agent(page: Page) {
  await page.locator('.react-flow__node').filter({ hasText: 'worker' }).click();
}
for (const resource of ['cpu', 'memory']) {
  test(`${resource} exhaustion waits; cancellation releases the request and retry executes`, async ({ page }) => {
    const holder = `${resource}-holder`, waiter = `${resource}-waiter`;
    try {
      await start(page, holder);
      await executed(page, holder);
      await start(page, waiter);
      await agent(page);
      await expect(page.getByRole('region', { name: 'Sandbox admission' })).toContainText('Waiting for capacity');
      expect(await read(page, waiter, 'executions')).toHaveLength(0);
      await stop(page, waiter);
      await expect.poll(async () => (await read(page, waiter, 'events')).some((e: { type: string; data?: { state: string } }) => e.type === 'sandbox.capacity' && e.data?.state === 'cancelled')).toBe(true);
      expect(await read(page, waiter, 'executions')).toHaveLength(0);
      // The holder still owns its reservation: cancelling a waiter cannot free it.
      expect((await read(page, holder, 'status')).state).toBe('running');
      await start(page, waiter);
      await agent(page);
      await expect(page.getByRole('region', { name: 'Sandbox admission' })).toContainText('Waiting for capacity');
      await stop(page, holder);
      await executed(page, waiter);
      await open(page, waiter);
      await agent(page);
      await expect(page.getByRole('region', { name: 'Sandbox admission' })).not.toContainText('Waiting for capacity');
      await stop(page, waiter);
    } finally {
      for (const id of [waiter, holder]) {
        if ((await read(page, id, 'status')).state !== 'stopped') await stop(page, id);
      }
    }
  });
}

test('failed build retries successfully through the browser', async ({ page }) => {
  const id = 'retry-build';
  await open(page, id);
  await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
  await page.getByRole('button', { name: 'Discover environment', exact: true }).click();
  await page.getByRole('combobox', { name: 'Repository definition', exact: true }).selectOption('Dockerfile');
  await page.getByRole('button', { name: 'Review environment', exact: true }).click();
  await page.getByRole('button', { name: 'Accept into draft', exact: true }).click();
  await page.getByRole('button', { name: 'Save graph', exact: true }).click();
  await page.getByRole('button', { name: 'Build', exact: true }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'E2E transient build failure' })).toBeVisible();
  expect(await read(page, id, 'executions')).toHaveLength(0);
  await page.getByRole('button', { name: 'Build again', exact: true }).click();
  await expect.poll(async () => (await read(page, id, 'environment')).state).toBe('ready');
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
  await executed(page, id);
  await stop(page, id);
});

test('idle suspension and message-driven resume preserve workspace contents', async ({ page }) => {
  const id = 'idle-resume';
  await start(page, id);
  await agent(page);
  const lifecycle = async (action: string) => (await read(page, id, 'events')).filter((e: { type: string; data?: { action: string; outcome: string } }) => e.type === 'sandbox.lifecycle' && e.data?.action === action && e.data.outcome === 'ok').length;
  await expect.poll(() => lifecycle('suspend')).toBe(1);
  await page.getByRole('tab', { name: /n0$/ }).click();
  await expect(page.getByText('sandbox suspended; workspace preserved', { exact: false })).toBeVisible();
  await page.getByRole('tab', { name: 'Files', exact: true }).click();
  await page.getByRole('button', { name: /^preserved\.txt/ }).click();
  await expect(page.locator('.file-content')).toContainText('workspace survives suspension');
  await page.getByRole('tab', { name: 'Graph', exact: true }).click();
  await agent(page);
  await page.locator('.toolbar-run-controls').getByRole('button', { name: 'Pause', exact: true }).click();
  await expect.poll(async () => (await read(page, id, 'status')).state).toBe('paused');
  await page.getByRole('textbox', { name: 'Operator message' }).fill('Resume and keep the workspace');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.toolbar-run-controls').getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
  expect(await lifecycle('resume')).toBe(0);
  expect(await read(page, id, 'executions')).toHaveLength(0);
  await page.locator('.toolbar-run-controls').getByRole('button', { name: 'Resume', exact: true }).click();
  await executed(page, id);
  await expect.poll(() => lifecycle('resume')).toBe(1);
  await expect.poll(() => lifecycle('suspend')).toBe(2);
  await page.getByRole('tab', { name: 'Files', exact: true }).click();
  await page.getByRole('button', { name: /^preserved\.txt/ }).click();
  await expect(page.locator('.file-content')).toContainText('workspace survives suspension');
  await stop(page, id);
});

test('environment build and setup wait for execution capacity, then become ready', async ({ page }) => {
  const holder = 'preparation-holder', id = 'preparation-waiter';
  try {
    await start(page, holder);
    await executed(page, holder);
    await open(page, id);
    await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
    await page.getByRole('button', { name: 'Build', exact: true }).click();
    await expect(page.getByText('Waiting for sandbox capacity', { exact: true })).toBeVisible();
    expect((await read(page, id, 'environment')).phase).toBe('waiting_capacity');
    expect(await read(page, id, 'executions')).toHaveLength(0);
    await stop(page, holder);
    await expect.poll(async () => (await read(page, id, 'environment')).state).toBe('ready');
    await open(page, id);
    await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
    await expect(page.getByText('Ready', { exact: true })).toBeVisible();
    expect((await read(page, id, 'environment')).log).toContain('Running 1 setup command');
    await page.getByRole('button', { name: 'Run all', exact: true }).click();
    await executed(page, id);
    await stop(page, id);
  } finally {
    if ((await read(page, holder, 'status')).state !== 'stopped') await stop(page, holder);
  }
});
