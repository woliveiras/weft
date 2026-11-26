import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import type { ChatList } from '../src/api/chat';
import type { GraphSummary } from '../src/api/types';

const headers = { Authorization: 'Bearer e2e-admin-token' };

async function draftTaskWithApproval(request: APIRequestContext, graphId: string, title: string) {
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
  return await draft.json() as {id: string; revision: number};
}

async function startTaskWaitingForApproval(request: APIRequestContext, graphId: string, title: string) {
  const task = await draftTaskWithApproval(request, graphId, title);
  const started = await request.post(`/api/v1/tasks/${task.id}/start`, {
    headers: { ...headers, 'If-Match': String(task.revision) }, data: { confirm: true },
  });
  expect(started.status()).toBe(202);
  return task.id;
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

test('starts a reviewed task, preserves its approval and follows the observed result', async ({ page, request }) => {
  await selectFixtureModel(request);
  const task = await draftTaskWithApproval(request, 'chat-start-approval', 'Archive review task fixture');
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await send(page, 'Start the saved archive review task.');
  const card = page.getByRole('region', { name: 'Start task Archive review task fixture' });
  await expect(card.getByText('Ask for approval as required · Default')).toBeVisible();
  const draft = await request.get(`/api/v1/tasks/${task.id}`, { headers });
  expect(await draft.json()).toMatchObject({status: 'draft'});
  await card.getByRole('button', { name: 'Start task', exact: true }).click();
  await expect(card.getByText(/Task admission recorded/)).toBeVisible();
  await expect(page.getByText(/Decision required/).first()).toBeVisible();
  await approvePendingTaskInput(request, task.id);
  await expect(page.getByRole('link', { name: 'View execution', exact: true })).toBeVisible();
  await expect(page.getByText(`Task ${task.id}: succeeded.`)).toBeVisible();
});

test('an administrator reviews the installation budget in chat before applying it', async ({ page, request }) => {
  await selectFixtureModel(request);
  const policyPath = '/api/v1/cost/policy';
  const readPolicy = async () => {
    const response = await request.get(policyPath, { headers });
    expect(response.ok()).toBe(true);
    return response.json();
  };
  const before = await readPolicy();
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await send(page, 'Set the task budget to two dollars with an eighty percent warning.');
  const card = page.getByRole('region', { name: 'Installation configuration review' });
  await expect(card).toContainText('CostPolicy/installation');
  expect(await readPolicy()).toEqual(before);
  try {
    await card.getByRole('button', { name: 'Apply configuration' }).click();
    await expect(card).toContainText('Configuration applied.');
    await expect(card.getByRole('list', { name: 'Configuration results' })).toContainText('CostPolicy/installation');
    const saved = await readPolicy();
    expect(saved.taskRunBudgetMicros).toBe(2000000);
  } finally {
    const restored = await request.put(policyPath, {
      headers, data: {taskRunBudgetMicros:before.taskRunBudgetMicros, warningPercent:before.warningPercent},
    });
    expect(restored.ok()).toBe(true);
  }
});

test('one-time guidance records pickup on the reviewed workflow run', async ({ page, request }) => {
  await selectFixtureModel(request);
  const graph = '/api/v1/graphs/guidance-sample';
  const created = await request.post('/api/v1/graphs', {headers,data:{
    metadata:{id:'guidance-sample'},spec:{
      nodes:[{id:'reader',runtime:{kind:'echo'},loop:{mode:'event'}}],
      channels:[{id:'requests',to:['reader']}],
    },
  }});
  expect(created.status()).toBe(201);
  expect((await request.post(graph+'/start',{headers})).ok()).toBe(true);
  try {
    await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
    await page.goto('/');
    await send(page, 'Tell replica zero of the reader in the guidance sample to summarize the warnings on its next turn.');
    const card = page.getByRole('region', {name:'One-time agent guidance'});
    await expect(card).toContainText('Summarize the warnings.');
    const history = graph+'/nodes/reader/chat';
    const before = await request.get(history,{headers});
    expect(await before.json()).toHaveLength(0);
    await card.getByRole('button',{name:'Send guidance'}).click();
    await expect(card.getByRole('status')).toContainText('runtime picked up the message', {timeout:20000});
    const after = await request.get(history,{headers});
    const messages = await after.json();
    expect(messages).toHaveLength(1);
    expect(messages[0]).toMatchObject({graphId:'guidance-sample',nodeId:'reader',replica:0,text:'Summarize the warnings.'});
    expect(messages[0].consumedAt).toBeTruthy();
  } finally {
    expect((await request.post(graph+'/stop',{headers})).ok()).toBe(true);
  }
});

test('a reviewed local instruction change opens the selected node in its workflow', async ({ page, request }) => {
  await selectFixtureModel(request);
  const graphId = 'instruction-sample';
  const graph = '/api/v1/graphs/'+graphId;
  const created = await request.post('/api/v1/graphs', {headers,data:{
    metadata:{id:graphId},spec:{
      nodes:[{id:'reader',runtime:{kind:'echo'},loop:{mode:'event'},localInstructions:'Review documentation.'}],
      channels:[{id:'requests',to:['reader']}],
    },
  }});
  expect(created.status()).toBe(201);
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/');
  await send(page, 'Set local instructions for reader in instruction-sample to review test changes.');
  const review = page.getByRole('region', {name:'Workflow-local instructions'});
  await expect(review).toContainText('Review documentation.');
  await expect(review).toContainText('Review test changes.');
  await review.getByRole('button', {name:'Save workflow instructions'}).click();
  await expect(review.getByRole('status')).toContainText('Workflow version 2 saved.');
  await review.getByRole('link', {name:'View workflow'}).click();
  await expect(page).toHaveURL(/#\/g\/instruction-sample\/canvas\?sel=node%3Areader/);
  const saved = await (await request.get(graph,{headers})).json();
  expect(saved.graph.spec.nodes[0].localInstructions).toBe('Review test changes.');
  await page.goBack();
  await expect(review).toBeVisible();
});

test('a console link opens Budgets and returns to its originating chat message', async ({ page, request }) => {
  await selectFixtureModel(request);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await send(page, 'Where can I inspect the model budget?');
  const link = page.getByRole('link', { name: 'Open Budgets', exact: true });
  await link.click();
  await expect(page).toHaveURL(/#\/models\/budgets$/);
  await page.goBack();
  await expect(page.getByRole('heading', { name: 'Inspect model budget', exact: true })).toBeVisible();
  await expect(link).toBeFocused();
});

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

test('a reviewed workflow keeps the requested catalog version and opens in the manual editor', async ({ page, request }) => {
  await selectFixtureModel(request);
  const agentId = 'editorial-reader';
  const definition = {
    name: 'Editorial reader', prompt: 'Read the editorial notes.', runtime: { kind: 'native' },
    model: { provider: 'e2e', name: 'fixture' }, isolation: { mode: 'process' }, readOnly: true,
  };
  const created = await request.post('/api/v1/agents', { headers, data: { id: agentId, definition } });
  expect(created.status(), await created.text()).toBe(201);
  const newer = await request.put(`/api/v1/agents/${agentId}`, {
    headers: { ...headers, 'If-Match': '1' },
    data: { id: agentId, definition: { ...definition, prompt: 'Review editorial changes and test results.' } },
  });
  expect(newer.ok()).toBe(true);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await send(page, 'Create a workflow named Editorial check using version one of the Editorial reader.');
  const confirm = page.getByRole('button', { name: 'Create workflow', exact: true });
  await expect(confirm).toBeEnabled();
  const graphPath = '/api/v1/graphs/editorial-check';
  expect((await request.get(graphPath, { headers })).status()).toBe(404);
  await confirm.click();
  await page.getByRole('link', { name: 'View workflow', exact: true }).click();
  await expect(page).toHaveURL(/#\/g\/editorial-check(?:\/|$)/);
  const graph = await request.get(graphPath, { headers });
  expect(graph.ok()).toBe(true);
  const saved: GraphSummary = await graph.json();
  expect(saved.graph.metadata.version).toBe(1);
  expect(saved.graph.spec.nodes).toHaveLength(1);
  expect(saved.graph.spec.nodes[0].agent).toMatchObject({ id: agentId, version: 1 });
  expect(saved.record.state).toBe('stopped');
  await page.goBack();
  await expect(page.getByRole('heading', { name: 'Prepare editorial workflow', exact: true })).toBeVisible();

  await send(page, 'Create a draft task on Editorial check to summarize the style notes.');
  const draftReview = page.getByRole('region', { name: 'Task draft Summarize style notes' });
  await expect(draftReview.getByText('editorial-check · Version 1')).toBeVisible();
  await draftReview.getByRole('button', { name: 'Create task draft' }).click();
  const viewDraft = draftReview.getByRole('link', { name: 'View task draft' });
  const link = await viewDraft.getAttribute('href');
  expect(link).toMatch(/^#\/tasks\/tsk_[a-z0-9]+/);
  const id = link?.split('/')[2].split('?')[0];
  const draft = await request.get(`/api/v1/tasks/${id}`, { headers });
  expect(draft.ok()).toBe(true);
  expect(await draft.json()).toMatchObject({
    title: 'Summarize style notes', status: 'draft', graphId: 'editorial-check',
    graphVersion: 1, nodeId: 'worker', resultNodeId: 'worker', channelId: 'requests', revision: 1,
  });
  await viewDraft.click();
  await expect(page.getByRole('heading', { name: 'Summarize style notes', exact: true })).toBeVisible();
  await page.goBack();
  await expect(viewDraft).toBeVisible();
});
