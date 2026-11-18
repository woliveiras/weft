import { expect, test } from '@playwright/test';

test('finds a saved task by its ID and workflow and opens the matching draft', async ({ page }) => {
  const headers = { Authorization: 'Bearer e2e-admin-token' };
  const graphIds = ['task-list-review', 'task-list-triage'];
  const ids: string[] = [];
  for (const graphId of graphIds) {
    const graph = await page.request.post('/api/v1/graphs', { headers, data: {
      metadata: { id: graphId, name: graphId }, spec: { nodes: [{ id: 'worker', runtime: { kind: 'echo' } }] },
    } });
    expect(graph.status()).toBe(201);
    const task = await page.request.post('/api/v1/tasks', { headers, data: {
      title: 'Evaluate incoming change', request: 'Review the saved request.', outcome: 'freeform',
      graphId, graphVersion: 1, nodeId: 'worker',
    } });
    expect(task.status()).toBe(201);
    ids.push((await task.json()).id);
  }

  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/#/tasks');
  const table = page.getByRole('table', { name: 'Task history' });
  await expect(table.getByRole('link', { name: 'Evaluate incoming change', exact: true })).toHaveCount(2);
  await expect(table.getByRole('columnheader', { name: 'Updated' })).toHaveAttribute('aria-sort', 'descending');
  const matchingRows = table.getByRole('row').filter({ hasText: 'Evaluate incoming change' });
  await expect(matchingRows.first()).toContainText(ids[1]);
  const response = await page.request.get('/api/v1/tasks', { headers, params: { order: 'updated_desc', limit: 1 } });
  expect(response.ok()).toBeTruthy();
  const first = await response.json();
  expect(first.items.map((item: { id: string }) => item.id)).toEqual([ids[1]]);
  const next = await page.request.get('/api/v1/tasks', { headers, params: { order: 'updated_desc', limit: 1, after: first.next } });
  expect(next.ok()).toBeTruthy();
  expect((await next.json()).items.map((item: { id: string }) => item.id)).toEqual([ids[0]]);
  await page.getByRole('combobox', { name: 'Workflow', exact: true }).selectOption(graphIds[1]);
  await page.getByRole('searchbox', { name: 'Search tasks' }).fill(ids[1]);
  await page.getByRole('combobox', { name: 'Task status' }).selectOption('failed');
  await expect(page.getByText('No tasks match these filters on this page.')).toBeVisible();
  await page.getByRole('combobox', { name: 'Task status' }).selectOption('draft');
  const row = table.getByRole('row').filter({ hasText: ids[1] });
  await expect(row).toContainText('Version 1');
  await expect(row.locator('time')).toHaveCount(2);
  await expect(table.getByText(ids[0], { exact: true })).toHaveCount(0);

  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  await row.getByRole('link', { name: 'Evaluate incoming change' }).click();
  await expect(page).toHaveURL(new RegExp(`#/tasks/${ids[1]}$`));
  await expect(page.getByRole('heading', { name: 'Task draft' })).toBeVisible();
});
