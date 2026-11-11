import { expect, test } from '@playwright/test';

const headers = { Authorization: 'Bearer e2e-admin-token' };
const graph = (id: string) => ({ metadata: { id, name: `Activity ${id}` }, spec: { nodes: [{ id: 'worker', runtime: { kind: 'echo' }, prompt: 'Activity view fixture', loop: { mode: 'once' }, approval: { beforeTurn: true } }] } });

async function startWaiting(request: import('@playwright/test').APIRequestContext, id: string) {
  expect((await request.post('/api/v1/graphs', { headers, data: graph(id) })).status()).toBe(201);
  expect((await request.post(`/api/v1/graphs/${id}/start`, { headers })).ok()).toBe(true);
  await expect.poll(async () => (await (await request.get(`/api/v1/approvals?graph=${id}&status=pending`, { headers })).json()).length).toBe(1);
  return (await (await request.get(`/api/v1/approvals?graph=${id}&status=pending`, { headers })).json())[0].id as string;
}

test('shows what needs a person and what is running, then follows a decision live into history', async ({ page }) => {
  const id = 'activity-view-browser';
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/#/graphs');
  await page.getByRole('navigation', { name: 'Main navigation' }).getByRole('button', { name: 'Activity', exact: true }).click();
  await expect(page).toHaveURL(/#\/activity$/);
  await expect(page.getByRole('heading', { name: 'Activity', level: 1 })).toBeVisible();

  const approval = await startWaiting(page.request, id);
  const needs = page.getByRole('region', { name: 'Needs you' });
  const item = needs.getByRole('listitem').filter({ hasText: `Activity ${id}` });
  await expect(item).toContainText('Turn approval');
  await expect(item.getByRole('link', { name: 'Decide in Approvals' })).toHaveAttribute('href', '#/approvals');

  const group = page.getByRole('region', { name: 'Running now' }).getByRole('article', { name: `Activity ${id}` });
  await expect(group).toContainText('Awaiting approval');
  await expect(group.getByRole('link', { name: 'worker', exact: true })).toHaveAttribute('href', `#/g/${id}/canvas?sel=node%3Aworker`);

  // Counters filter the page and survive a reload through the route.
  await page.getByRole('button', { name: /Waiting for a person/ }).click();
  await expect(page).toHaveURL(/focus=needs/);
  await expect(page.getByRole('region', { name: 'Running now' })).toHaveCount(0);
  await page.reload();
  await expect(item).toContainText('Turn approval');
  await page.getByRole('button', { name: 'Show everything' }).click();

  // Deciding elsewhere updates this page from the event stream, without a reload.
  expect((await page.request.post(`/api/v1/approvals/${approval}/approve`, { headers })).ok()).toBe(true);
  await expect(item).toHaveCount(0);
  const history = page.getByRole('region', { name: 'Recent history' });
  await history.getByLabel('Workflow').selectOption(id);
  await expect(page).toHaveURL(new RegExp(`workflow=${id}`));
  const row = history.getByRole('row').filter({ hasText: 'worker' });
  await expect(row).toContainText('Completed');
  await row.getByRole('link').first().click();
  await expect(page).toHaveURL(new RegExp(`#/g/${id}/runs\\?execution=`));
  await expect(page.getByRole('region', { name: 'Run detail' })).toContainText('Completed');
});

test('a viewer is told which sections are hidden, and a failed load recovers on refresh', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.route('**/api/v1/activity*', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'Activity temporarily unavailable', code: 'unavailable' }) }));
  await page.goto('/#/activity');
  await expect(page.getByText('Activity temporarily unavailable', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Activity counters' })).toHaveCount(0);
  await page.unroute('**/api/v1/activity*');
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Activity counters' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Needs you' })).toContainText('Your role cannot see human reviews');
  await page.setViewportSize({ width: 580, height: 850 });
  await expect(page.getByRole('button', { name: /Agents running/ })).toBeVisible();
});

test('ranks a finished agent in Health and links to its filtered history and run details', async ({ page }) => {
  const id = 'activity-health-browser';
  const once = { metadata: { id, name: `Activity ${id}` }, spec: { nodes: [{ id: 'worker', runtime: { kind: 'echo' }, prompt: 'Health fixture', loop: { mode: 'once' } }] } };
  expect((await page.request.post('/api/v1/graphs', { headers, data: once })).status()).toBe(201);
  expect((await page.request.post(`/api/v1/graphs/${id}/start`, { headers })).ok()).toBe(true);
  await expect.poll(async () => (await (await page.request.get(`/api/v1/graphs/${id}/executions`, { headers })).json())
    .filter((e: { status: string }) => e.status === 'succeeded').length).toBe(1);

  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.goto('/#/activity');
  await page.getByRole('tab', { name: 'Health' }).click();
  await expect(page).toHaveURL(/tab=health/);
  const title = `worker · Activity ${id}`;
  await expect(page.getByRole('article', { name: 'Slowest' }).getByRole('link', { name: title })).toBeVisible();
  const row = page.getByRole('region', { name: 'Agent health' }).getByRole('row').filter({ hasText: `Activity ${id}` });
  await expect(row).toContainText('0%');
  await page.getByLabel('Window').selectOption('7d');
  await expect(page).toHaveURL(/window=7d/);
  await expect(row).toBeVisible();

  await row.getByRole('link', { name: 'History' }).click();
  await expect(page).toHaveURL(new RegExp(`#/activity\\?window=7d&workflow=${id}&agent=worker&focus=history`));
  await expect(page.getByRole('region', { name: 'Recent history' }).getByRole('row').filter({ hasText: 'worker' })).toContainText('Completed');
  await page.goBack();
  await row.getByRole('link', { name: 'Last run' }).click();
  await expect(page).toHaveURL(new RegExp(`#/g/${id}/runs\\?execution=`));
  await expect(page.getByRole('region', { name: 'Run detail' })).toContainText('Completed');
});
