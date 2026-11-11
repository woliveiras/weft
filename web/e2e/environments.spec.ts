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
async function settings(page: Page, id: string) {
  await page.goto(`/#/g/${id}/canvas`);
  await page.getByRole('button', { name: 'Graph settings', exact: true }).click();
}
async function discover(page: Page, id: string) {
  await settings(page, id);
  await page.getByRole('button', { name: 'Discover environment', exact: true }).click();
  await page.getByRole('combobox', { name: 'Repository definition', exact: true }).selectOption({ label: 'Dockerfile' });
  await page.getByRole('button', { name: 'Review environment', exact: true }).click();
  await expect(page.getByLabel('Environment proposal', { exact: true })).toContainText('FROM alpine:3.22');
  await page.getByRole('button', { name: 'Accept into draft', exact: true }).click();
  await page.getByRole('button', { name: 'Save graph', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Build', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Build', exact: true }).click();
}
async function executed(page: Page, id: string) {
  await expect.poll(async () => (await read(page, id, 'executions')).some((e: { status: string }) => e.status === 'succeeded')).toBe(true);
}
async function propose(page: Page, id: string, resume = false) {
  await settings(page, id);
  await page.getByRole('button', { name: 'Discover environment', exact: true }).click();
  if (resume) await page.getByLabel('Start graph when the approved environment is ready').check();
  await page.getByRole('button', { name: 'Analyze repository', exact: true }).click();
}

test('discovers, reviews, builds and executes; another task reuses the compatible image', async ({ page }) => {
  await discover(page, 'discover-a');
  await expect.poll(async () => (await read(page, 'discover-a', 'environment')).state).toBe('ready');
  const first = await read(page, 'discover-a', 'environment');
  expect(first.reused ?? false).toBe(false);
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
  await executed(page, 'discover-a');
  await discover(page, 'discover-b');
  await expect.poll(async () => (await read(page, 'discover-b', 'environment')).state).toBe('ready');
  const second = await read(page, 'discover-b', 'environment');
  expect(second.reused).toBe(true);
  expect(first.sharedImage).toBeTruthy();
  expect(second.sharedImage).toBe(first.sharedImage);
  await expect(page.getByText('Reused compatible prepared image and cache snapshots.')).toBeVisible();
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
  await executed(page, 'discover-b');
  const workdirs = async (id: string): Promise<string[]> => (await read(page, id, 'events')).flatMap((e: { data?: { workdir?: string } }) => e.data?.workdir ? [e.data.workdir] : []);
  const firstDirs = await workdirs('discover-a');
  const secondDirs = await workdirs('discover-b');
  expect(firstDirs.length).toBeGreaterThan(0);
  expect(secondDirs.length).toBeGreaterThan(0);
  expect(secondDirs.some(dir => firstDirs.includes(dir))).toBe(false);
});

test('proposal waits for developer approval, then builds and resumes execution', async ({ page }) => {
  const id = 'proposal-accept';
  await propose(page, id, true);
  await expect(page.getByText('Environment proposal: awaiting_approval')).toBeVisible();
  expect((await read(page, id, 'environment')).state).toBe('absent');
  expect(await read(page, id, 'executions')).toHaveLength(0);
  const blockedStart = page.waitForResponse(r => r.url().endsWith(`/graphs/${id}/start`) && r.request().method() === 'POST');
  await page.getByRole('button', { name: 'Run all', exact: true }).click();
  expect((await blockedStart).status()).toBe(422);
  await expect(page.getByRole('alert').first()).toBeVisible();
  await page.goto('/#/approvals');
  await page.locator('article.approval').filter({ hasText: id }).getByRole('button', { name: 'Approve', exact: true }).click();
  await expect.poll(async () => (await read(page, id, 'environment')).state).toBe('ready');
  await executed(page, id);
});

test('rejecting a proposal prevents build and execution', async ({ page }) => {
  const id = 'proposal-reject';
  await propose(page, id, true);
  await expect(page.getByText('Environment proposal: awaiting_approval')).toBeVisible();
  await page.goto('/#/approvals');
  await page.locator('article.approval').filter({ hasText: id }).getByRole('button', { name: 'Reject', exact: true }).click();
  await expect.poll(async () => (await read(page, id, 'environment/proposal')).state).toBe('rejected');
  await settings(page, id);
  await page.getByRole('button', { name: 'Discover environment', exact: true }).click();
  await expect(page.getByText('Environment proposal: rejected')).toBeVisible();
  expect((await read(page, id, 'environment')).state).toBe('absent');
  expect(await read(page, id, 'executions')).toHaveLength(0);
});

test('analysis failure is presented without creating an environment', async ({ page }) => {
  await propose(page, 'analysis-failure');
  await expect(page.getByText(/E2E analysis provider unavailable/).first()).toBeVisible();
  expect((await read(page, 'analysis-failure', 'environment')).state).toBe('absent');
  expect(await read(page, 'analysis-failure', 'executions')).toHaveLength(0);
});

test('build failure is visible and offers retry without executing the graph', async ({ page }) => {
  await discover(page, 'build-failure');
  await expect.poll(async () => (await read(page, 'build-failure', 'environment')).state).toBe('failed');
  await expect(page.getByRole('button', { name: 'Build again', exact: true })).toBeVisible();
  await expect(page.getByText(/E2E controlled build failure/).first()).toBeVisible();
  expect(await read(page, 'build-failure', 'executions')).toHaveLength(0);
});
