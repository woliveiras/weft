import { expect, test, type Page } from '@playwright/test';
import type { GraphSummary } from '../src/api/types';

test.use({ viewport: { width: 1600, height: 1000 } });

const headers = { Authorization: 'Bearer e2e-admin-token' };
const node = (page: Page, id: string) => page.locator(`.react-flow__node[data-id="${id}"]`);
const edges = (page: Page) => page.locator('.react-flow__edge');

async function saved(page: Page, id: string): Promise<GraphSummary> {
  const response = await page.request.get(`/api/v1/graphs/${id}`, { headers });
  expect(response.ok()).toBeTruthy();
  return response.json();
}

async function save(page: Page) {
  const response = page.waitForResponse(r => r.request().method() === 'PUT' && /\/graphs\/[^/]+$/.test(r.url()));
  await page.getByRole('button', { name: 'Save graph', exact: true }).click();
  expect((await response).ok()).toBeTruthy();
  await expect(page.getByText('unsaved graph changes', { exact: true })).toHaveCount(0);
}

async function move(page: Page, id: string, x: number, y: number) {
  const card = node(page, id);
  await expect(card).toBeVisible();
  const box = await card.boundingBox();
  const canvas = await page.locator('.react-flow').boundingBox();
  expect(box).not.toBeNull();
  expect(canvas).not.toBeNull();
  await page.mouse.move(box!.x + box!.width / 2, box!.y + 25);
  await page.mouse.down();
  await page.mouse.move(canvas!.x + canvas!.width * x, canvas!.y + canvas!.height * y, { steps: 20 });
  await page.mouse.up();
}

async function connect(page: Page, source: string, target: string, count: number) {
  await page.getByRole('button', { name: 'Fit View', exact: true }).click();
  // dragTo waits for stable handles and sends real pointer events through React Flow.
  await node(page, source).locator('.react-flow__handle.source').dragTo(
    node(page, target).locator('.react-flow__handle.target'),
  );
  await expect(edges(page)).toHaveCount(count);
}

async function deleteSelection(page: Page, message: string, accept: boolean) {
  const dialogHandled = new Promise<void>((resolve, reject) => {
    page.once('dialog', async dialog => {
      try {
        expect(dialog.type()).toBe('confirm');
        expect(dialog.message()).toBe(message);
        if (accept) await dialog.accept();
        else await dialog.dismiss();
        resolve();
      } catch (error) {
        await dialog.dismiss();
        reject(error);
      }
    });
  });
  await page.keyboard.press('Delete');
  await dialogHandled;
}

test('creates agents, drags connections, validates, cancels and confirms Delete, then reopens persisted edits', async ({ page }, testInfo) => {
  const graphName = `browser-connections-${testInfo.repeatEachIndex}`;
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  // Seed one source agent through the real API; all creation and connection gestures use the browser.
  const created = await page.request.post('/api/v1/graphs', { headers, data: {
    metadata: { name: graphName },
    spec: { nodes: [{ id: 'source', runtime: { kind: 'echo' }, position: { x: 0, y: 0 } }] },
  } });
  expect(created.ok()).toBeTruthy();
  const initial: GraphSummary = await created.json();
  const id = initial.graph.metadata.id;
  await page.goto(`/#/g/${id}/canvas`);
  await page.getByRole('button', { name: 'Fit View', exact: true }).click();
  await move(page, 'source', 0.15, 0.25);
  for (const agent of ['agent-2', 'agent-3']) {
    await page.getByRole('button', { name: 'Add agent', exact: true }).click();
    const config = page.getByRole('region', { name: 'Agent configuration' });
    await expect(config.getByLabel('ID', { exact: true })).toHaveValue(agent);
    await config.getByRole('combobox', { name: 'Runtime', exact: true }).selectOption('echo');
    await page.getByRole('button', { name: 'Close panel', exact: true }).click();
    // Move the new card so its handles do not overlap the previous agent.
    await move(page, agent, 0.65, agent === 'agent-2' ? 0.25 : 0.7);
  }
  await connect(page, 'source', 'agent-2', 1);
  await connect(page, 'agent-2', 'agent-3', 2);
  await connect(page, 'source', 'agent-3', 3);

  await node(page, 'agent-2').click();
  const config = page.getByRole('region', { name: 'Agent configuration' });
  await config.getByRole('combobox', { name: 'Loop mode', exact: true }).selectOption('scheduled');
  await config.getByLabel('Schedule', { exact: true }).fill('invalid schedule');
  await page.getByRole('button', { name: 'Save graph', exact: true }).click();
  await expect(page.getByLabel('Graph validation', { exact: true })).toContainText('must be fixed before saving');
  await expect(node(page, 'agent-2').getByLabel(/validation errors/)).toBeVisible();
  expect((await saved(page, id)).record.version).toBe(initial.record.version);
  await config.getByRole('combobox', { name: 'Loop mode', exact: true }).selectOption('event');
  await page.getByRole('button', { name: 'Close panel', exact: true }).click();
  await save(page);
  const connected = await saved(page, id);
  expect(connected.graph.spec.nodes.map(n => n.id)).toEqual(['source', 'agent-2', 'agent-3']);
  expect(connected.graph.spec.channels).toMatchObject([
    { id: 'channel-1', from: 'source', to: ['agent-2'] },
    { id: 'channel-2', from: 'agent-2', to: ['agent-3'] },
    { id: 'channel-3', from: 'source', to: ['agent-3'] },
  ]);
  await page.reload();
  await expect(edges(page)).toHaveCount(3);
  await node(page, 'agent-2').click();
  await deleteSelection(page, 'Remove agent "Agent 2" and channels "channel-1", "channel-2"?', false);
  await expect(node(page, 'agent-2')).toBeVisible();
  await expect(edges(page)).toHaveCount(3);
  await expect(page.getByText('unsaved graph changes', { exact: true })).toHaveCount(0);
  await deleteSelection(page, 'Remove agent "Agent 2" and channels "channel-1", "channel-2"?', true);
  await expect(node(page, 'agent-2')).toHaveCount(0);
  await expect(edges(page)).toHaveCount(1);
  expect((await saved(page, id)).graph.spec).toEqual(connected.graph.spec);
  await save(page);
  await page.goto('/#/');
  await page.getByRole('button', { name: 'Select graph', exact: true }).click();
  await page.getByRole('region', { name: 'Graph selector' }).getByRole('button', { name: new RegExp(graphName) }).click();
  await expect(node(page, 'source')).toBeVisible();
  await expect(node(page, 'agent-3')).toBeVisible();
  await expect(node(page, 'agent-2')).toHaveCount(0);
  await expect(edges(page)).toHaveCount(1);
  const removed = await saved(page, id);
  expect(removed.graph.spec.nodes).toEqual(connected.graph.spec.nodes.filter(n => n.id !== 'agent-2'));
  expect(removed.graph.spec.channels).toEqual(connected.graph.spec.channels!.filter(c => c.id === 'channel-3'));

  // Delete a selected connection itself, without deleting its endpoints.
  // The label covers the midpoint; select the actual SVG edge away from it.
  const point = await edges(page).locator('.react-flow__edge-interaction').evaluate(element => {
    const path = element as SVGPathElement;
    const point = path.getPointAtLength(path.getTotalLength() / 4);
    const screen = new DOMPoint(point.x, point.y).matrixTransform(path.getScreenCTM()!);
    return { x: screen.x, y: screen.y };
  });
  await page.mouse.click(point.x, point.y);
  await deleteSelection(page, 'Remove channel "channel-3"?', false);
  await expect(edges(page)).toHaveCount(1);
  await deleteSelection(page, 'Remove channel "channel-3"?', true);
  await expect(edges(page)).toHaveCount(0);
  await save(page);
  await page.reload();
  await expect(page.locator('.react-flow__node-agent')).toHaveCount(2);
  await expect(edges(page)).toHaveCount(0);
  const disconnected = await saved(page, id);
  expect(disconnected.graph.spec.nodes).toEqual(removed.graph.spec.nodes);
  expect(disconnected.graph.spec.channels ?? []).toEqual([]);
});
