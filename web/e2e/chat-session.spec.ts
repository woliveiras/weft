import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import type { ChatList } from '../src/api/chat';

const headers = { Authorization: 'Bearer e2e-admin-token' };

async function startTaskWaitingForApproval(request: APIRequestContext, graphId: string, title: string) {
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
      title, request: title,
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

async function selectFixtureModel(request: APIRequestContext) {
  const model = await request.get('/api/v1/chat/settings', { headers });
  expect(model.ok()).toBe(true);
  const settings = await model.json();
  const selected = await request.put('/api/v1/chat/settings', {
    headers: { ...headers, 'If-Match': String(settings.revision) },
    data: { provider: 'e2e', name: 'fixture' },
  });
  expect(selected.ok()).toBe(true);
}

async function approvePendingTaskInput(request: APIRequestContext, taskId: string) {
  const progress = await request.get(`/api/v1/tasks/${taskId}/progress`, { headers });
  expect(progress.ok()).toBe(true);
  const { pendingApprovals } = await progress.json();
  expect(pendingApprovals).toHaveLength(1);
  const decision = await request.post(`/api/v1/approvals/${pendingApprovals[0].id}/approve`, { headers });
  expect(decision.ok()).toBe(true);
}

test('a task watch reports the result after creating a reviewed agent in another chat and closing the console', async ({ page, context, request }) => {
  await selectFixtureModel(request);
  const taskId = await startTaskWaitingForApproval(request, 'chat-release-notes', 'Release notes for Chat fixture');
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

  // A second request in this session creates a new catalog agent only after review.
  await send(page, 'Create a reusable agent called Release reader to summarize release notes.');
  const confirm = page.getByRole('button', { name: 'Create agent', exact: true });
  await expect(confirm).toBeEnabled();
  await expect(page.getByText('Summarize release notes.', { exact: true })).toBeVisible();
  const agentURL = '/api/v1/agents/release-reader';
  expect((await request.get(agentURL, { headers })).status()).toBe(404);
  await confirm.click();
  const catalogLink = page.getByRole('link', { name: 'View agent' });
  await expect(catalogLink).toHaveAttribute('href', '#/agents/release-reader');
  const published = await request.get(agentURL, { headers });
  expect(published.ok()).toBe(true);
  const agent = await published.json();
  expect(agent.versions).toHaveLength(1);
  expect(agent.versions[0].definition).toMatchObject({
    name: 'Release reader', prompt: 'Summarize release notes.', readOnly: true,
  });
  expect(agent.versions[0].definition.tools ?? []).toHaveLength(0);
  await page.close();

  // An independent caller completes the actual task while no console is mounted.
  await approvePendingTaskInput(request, taskId);
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

test('an archived watch signals a new result and can be restored from the session list', async ({ page, request }) => {
  await selectFixtureModel(request);
  const taskId = await startTaskWaitingForApproval(request, 'chat-archive-check', 'Documentation check for Chat fixture');
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await send(page, 'Follow the documentation check until it finishes.');
  await expect(page.getByRole('heading', { name: 'Follow documentation check' })).toBeVisible();
  await page.getByRole('button', { name: 'Archive conversation', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Restore conversation', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  const recents = page.getByRole('region', { name: 'Recents' });
  const title = 'Follow documentation check';
  await expect(recents.getByRole('button', { name: title, exact: true })).toHaveCount(0);
  await approvePendingTaskInput(request, taskId);
  await recents.getByRole('button', { name: /Archived.*1 unread/ }).click();
  await page.getByRole('region', { name: 'Archived', exact: true }).getByRole('button', { name: title, exact: true }).click();
  await expect(page.getByRole('link', { name: 'View task result' })).toBeVisible();
  await page.getByRole('button', { name: 'Restore conversation', exact: true }).click();
  await expect(recents.getByRole('button', { name: title, exact: true })).toBeVisible();
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
