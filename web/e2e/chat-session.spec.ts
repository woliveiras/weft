import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import type { ChatList } from '../src/api/chat';

const headers = { Authorization: 'Bearer e2e-admin-token' };

async function startTaskWaitingForApproval(request: APIRequestContext) {
  const graphId = 'chat-release-notes';
  const graph = await request.post('/api/v1/graphs', {
    headers,
    data: {
      metadata: { id: graphId },
      spec: {
        nodes: [{ id: 'reviewer', runtime: { kind: 'echo' }, loop: { mode: 'event' } }],
        channels: [{ id: 'requests', to: ['reviewer'], approval: true }],
      },
    },
  });
  expect(graph.status()).toBe(201);
  const draft = await request.post('/api/v1/tasks', {
    headers,
    data: {
      title: 'Release notes for Chat fixture', request: 'Review the release notes',
      graphId, graphVersion: 1, nodeId: 'reviewer', resultNodeId: 'reviewer',
      outcome: 'freeform', channelId: 'requests', messageType: 'feature.request', payloadJson: '{}',
    },
  });
  expect(draft.status()).toBe(201);
  const task = await draft.json();
  const started = await request.post(`/api/v1/tasks/${task.id}/start`, {
    headers: { ...headers, 'If-Match': String(task.revision) }, data: { confirm: true },
  });
  expect(started.status()).toBe(202);
  return task.id as string;
}

async function send(page: Page, message: string) {
  const composer = page.getByRole('textbox', { name: 'Message', exact: true });
  await composer.fill(message);
  await expect(composer).toHaveValue(message);
  await page.getByRole('button', { name: 'Send', exact: true }).click();
}

test('a task watch reports the result after switching conversations and closing the console', async ({ page, context, request }) => {
  const model = await request.get('/api/v1/chat/settings', { headers });
  expect(model.ok()).toBe(true);
  const settings = await model.json();
  const selected = await request.put('/api/v1/chat/settings', {
    headers: { ...headers, 'If-Match': String(settings.revision) },
    data: { provider: 'e2e', name: 'fixture' },
  });
  expect(selected.ok()).toBe(true);
  const taskId = await startTaskWaitingForApproval(request);
  await context.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await send(page, 'Let me know when the release notes review is complete.');
  const recents = page.getByRole('region', { name: 'Recents' });
  await expect(recents).toContainText('Follow release notes');
  await expect(page.getByRole('heading', { name: 'Follow release notes', exact: true })).toBeVisible();
  const watchURL = page.url();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  await expect(page).toHaveURL(/#\/chat$/);
  await expect(page.getByRole('heading', { name: 'Chat', exact: true })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Message', exact: true })).toBeEmpty();
  await send(page, 'Show me the current state of the release notes review.');
  await expect(recents).toContainText('Inspect release notes');
  expect(page.url()).not.toBe(watchURL);
  await page.close();

  // An independent caller completes the actual task while no console is mounted.
  const progress = await request.get(`/api/v1/tasks/${taskId}/progress`, { headers });
  expect(progress.ok()).toBe(true);
  const { pendingApprovals } = await progress.json();
  expect(pendingApprovals).toHaveLength(1);
  const decision = await request.post(`/api/v1/approvals/${pendingApprovals[0].id}/approve`, { headers });
  expect(decision.ok()).toBe(true);
  await expect.poll(async () => (await (await request.get(`/api/v1/tasks/${taskId}`, { headers })).json()).status)
    .toBe('succeeded');
  await expect.poll(async () => {
    const reply = await request.get('/api/v1/chat/sessions', { headers });
    expect(reply.ok()).toBe(true);
    const sessions: ChatList = await reply.json();
    const watched = sessions.items.find(item => item.title === 'Follow release notes');
    return watched && { running: watched.running, unreadSuccess: watched.unreadSuccess };
  }).toEqual({ running: 0, unreadSuccess: 1 });

  const reopened = await context.newPage();
  await reopened.goto('/');
  const recent = reopened.getByRole('region', { name: 'Recents' });
  await expect(recent).toContainText('Follow release notes');
  await recent.getByText('Follow release notes', { exact: true }).click();
  await expect(reopened).toHaveURL(watchURL);
  const evidence = reopened.getByRole('link', { name: 'View task result' });
  await expect(evidence).toHaveCount(1);
  await evidence.click();
  await expect(reopened).toHaveURL(new RegExp(`#/tasks/${taskId}(?:[?]|$)`));
});

test('a private conversation clears when another tab switches the signed-in identity', async ({ page, context, request }) => {
  const privateMessage = 'Remember this private operator request.';
  const session = await request.post('/api/v1/chat/sessions', {
    headers, data: { requestId: 'ecc4f24528aa42eeb662d43cc5792147', text: privateMessage },
  });
  expect(session.ok()).toBe(true);
  const { id } = await session.json();
  await page.goto('/');
  await page.evaluate(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto(`/#/chat/${id}`);
  await page.reload();
  await expect(page.getByText(privateMessage, { exact: true })).toBeVisible();

  const otherTab = await context.newPage();
  await otherTab.goto('/#/graphs');
  await otherTab.evaluate(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await expect(page.getByText(privateMessage, { exact: true })).toHaveCount(0);
  const viewerRead = await request.get(`/api/v1/chat/sessions/${id}`, {
    headers: { Authorization: 'Bearer e2e-viewer-token' },
  });
  expect(viewerRead.status()).toBe(404);
  expect(await viewerRead.text()).not.toContain(privateMessage);
});
