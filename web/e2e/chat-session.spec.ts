import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
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

test('discovers duplicate PR numbers and publishes only reviewed comments on the pinned PR', async ({ page, request }) => {
  await selectFixtureModel(request);
  const base = 'b'.repeat(40), head = 'a'.repeat(40);
  const changedHead = 'd'.repeat(40);
  let currentHead = head;
  let writes = 0;
  let diffs = 0;
  const published: {id: number; path: string; line: number; side: string; commit_id: string; body: string}[] = [];
  const remote = createServer(async (req, res) => {
    if (req.method !== 'GET') {
      writes++;
      if (req.method !== 'POST' || req.url !== '/repos/team/repo/pulls/418/comments') { res.writeHead(405).end(); return; }
      let body = '';
      for await (const chunk of req) body += chunk;
      const comment = {...JSON.parse(body),id:990+writes} as typeof published[number];
      published.push(comment);
      if (comment.side === 'LEFT') res.destroy();
      else res.writeHead(201).end(JSON.stringify(comment));
      return;
    }
    if (req.url === '/repos/team/repo/pulls/418') {
      res.end(JSON.stringify({number: 418, title:'Update the mobile checksum', html_url: 'https://fixture.example/team/repo/pull/418', state: 'open',
        head: {ref: 'feature', sha: currentHead, repo: {full_name: 'team/repo'}},
        base: {ref: 'main', sha: base, repo: {full_name: 'team/repo'}}}));
    } else if (req.url === '/repos/team/docs/pulls/418') {
      res.end(JSON.stringify({number:418,title:'Improve the documentation',html_url:'https://fixture.example/team/docs/pull/418',state:'open',
        head:{ref:'docs',sha:'c'.repeat(40),repo:{full_name:'team/docs'}},base:{ref:'main',sha:base,repo:{full_name:'team/docs'}}}));
    } else if (req.url === '/repos/team/repo/pulls/418/comments?per_page=100&page=1') {
      res.end(JSON.stringify(published));
    } else if (req.url === `/repos/team/repo/compare/${base}...${currentHead}`) {
      diffs++;
      res.end('diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-original\n+fixture change\n');
    } else { res.writeHead(404).end(); }
  });
  await new Promise<void>(resolve => remote.listen(0, '127.0.0.1', resolve));
  try {
    const port = (remote.address() as AddressInfo).port;
    const connection = await request.post('/api/v1/scm/connections', {headers, data: {
      name: 'pr-fixture', kind: 'github', repository: 'team/repo', token: 'synthetic-browser-fixture',
      apiUrl: `http://127.0.0.1:${port}`,
    }});
    expect(connection.ok()).toBe(true);
    expect((await request.post('/api/v1/scm/connections',{headers,data:{
      name:'pr-second',kind:'github',repository:'team/docs',token:'synthetic-browser-docs-fixture',apiUrl:`http://127.0.0.1:${port}`,
    }})).ok()).toBe(true);
    const graph = await request.post('/api/v1/graphs', {headers, data: {
      metadata: {id: 'manual-review'}, spec: {
        nodes: [{id: 'reader', prompt: 'Review the change.', runtime: {kind: 'native'}, model: {provider: 'e2e', name: 'fixture'},
          loop: {mode: 'event'}, tools: [{name: 'scm.pull_request.read', repositories: ['pr-fixture','pr-second']},
            {name:'scm.pull_request.comment', repositories:['pr-fixture']}]}],
        channels: [{id: 'in', to: ['reader']}],
      },
    }});
    expect(graph.status()).toBe(201);
    expect((await request.post('/api/v1/graphs/manual-review/start', {headers})).ok()).toBe(true);
    await expect.poll(async () => {
      const graph = await (await request.get('/api/v1/graphs/manual-review',{headers})).json();
      return graph.record.state === 'running' && !!graph.record.runId;
    }).toBe(true);
    await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-operator-token'));
    await page.goto('/');
    await send(page, 'Revise o PR 418.');
    await expect(page.getByText(/Improve the documentation/)).toBeVisible();
    await expect(page.getByText(/Update the mobile checksum/)).toBeVisible();
    expect(diffs).toBe(0);
    await send(page,'o do mobile checksum');
    const review = page.getByRole('region', {name: 'Review PR #418'});
    await expect(review.getByText(head)).toBeVisible();
    await review.getByRole('button', {name: 'Inspect pinned diff'}).click();
    await expect(review.getByRole('region', {name: 'Pinned pull request diff'}).getByText(/\+fixture change/)).toBeVisible();
    expect(writes).toBe(0);
    expect(diffs).toBe(1);
    await review.getByRole('link', {name: 'View workflow'}).click();
    await expect(page).toHaveURL(/manual-review\/canvas/);
    await page.goBack();
    expect(diffs).toBe(1);
    await review.getByRole('button', {name: 'Analyze PR #418'}).click();
    await expect(review.getByText('Add a boundary test for the fixture change.')).toBeVisible();
    await review.getByRole('link', {name: 'View execution'}).click();
    await expect(page).toHaveURL(/manual-review\/runs\?execution=/);
    expect(writes).toBe(0);
    expect(diffs).toBe(2);
    await page.goBack();
    await review.getByRole('checkbox', {name: /added line 1/}).check();
    await review.getByRole('button', {name:'Review selected comments'}).click();
    const publication = page.getByRole('region', {name:'Publish comments on PR #418'});
    await expect(publication.getByText('Add a boundary test for the new value.')).toBeVisible();
    await expect(publication.getByText('Explain why the original value was removed.')).toHaveCount(0);
    await expect(publication.getByText(head)).toBeVisible();
    expect(writes).toBe(0);
    await publication.getByRole('button', {name:'Publish 1 comment',exact:true}).click();
    await expect(publication.getByText('Published · Provider comment 991')).toBeVisible();
    await page.reload();
    await expect(publication.getByText('Published · Provider comment 991')).toBeVisible();
    expect(writes).toBe(1);
    expect(published).toEqual([{id:991,path:'file',line:1,side:'RIGHT',commit_id:head,body:expect.stringContaining('Add a boundary test for the new value.')}]);

    await review.getByRole('checkbox', {name: /removed line 1/}).check();
    await review.getByRole('button', {name:'Review selected comments'}).click();
    const stale = publication.last();
    await expect(stale.getByRole('button', {name:'Publish 1 comment',exact:true})).toBeVisible();
    currentHead = changedHead;
    await stale.getByRole('button', {name:'Publish 1 comment',exact:true}).click();
    await expect(stale.getByText(/The source or grant changed/)).toBeVisible();
    expect(writes).toBe(1);

    await send(page, 'Revise o PR 418 em team/repo.');
    const fresh = review.last();
    await expect(fresh.getByText(changedHead)).toBeVisible();
    await fresh.getByRole('button', {name: 'Analyze PR #418'}).click();
    await fresh.getByRole('checkbox', {name: /removed line 1/}).check();
    await fresh.getByRole('button', {name:'Review selected comments'}).click();
    const uncertain = publication.last();
    await uncertain.getByRole('button', {name:'Publish 1 comment',exact:true}).click();
    await expect(uncertain.getByText(/Unknown · Publication receipt/)).toBeVisible();
    await page.reload();
    await uncertain.getByRole('button', {name:'Reconcile comment receipts'}).click();
    await expect(uncertain.getByText('Published · Provider comment 992')).toBeVisible();
    expect(writes).toBe(2);
    expect(published[1]).toEqual({id:992,path:'file',line:1,side:'LEFT',commit_id:changedHead,body:expect.stringContaining('Explain why the original value was removed.')});
  } finally {
    await new Promise<void>((resolve, reject) => remote.close(err => err ? reject(err) : resolve()));
  }
});

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

test('reviews runtime controls and observes a pause limited to the named node', async ({ page, request }) => {
  await selectFixtureModel(request);
  const graphPath = '/api/v1/graphs/chat-publication';
  const graph = await request.post('/api/v1/graphs', {
    headers,
    data: {
      metadata: {id:'chat-publication', name:'Publication workflow'},
      spec:{nodes:[
        {id:'reviewer',runtime:{kind:'echo'},loop:{mode:'event'}},
        {id:'reader',runtime:{kind:'echo'},loop:{mode:'event'}},
      ]},
    },
  });
  expect(graph.status()).toBe(201);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-operator-token'));
  await page.goto('/');
  await send(page, 'Start the publication workflow.');
  const start = page.getByRole('region', {name:'Start chat-publication', exact:true});
  await expect(start.getByText('chat-publication · Version 1')).toBeVisible();
  expect(await (await request.get(graphPath,{headers})).json()).toMatchObject({record:{desiredState:'stopped'}});
  await start.getByRole('button',{name:'Start',exact:true}).click();
  await expect(start.getByText('The runtime reached the requested state.')).toBeVisible();
  await send(page, 'Pause new turns for the reviewer node in the publication workflow.');
  const pause = page.getByRole('region',{name:'Pause new turns chat-publication / reviewer'});
  await expect(pause.getByText('chat-publication / reviewer · Version 1')).toBeVisible();
  await pause.getByRole('button',{name:'Pause new turns'}).click();
  await expect(pause.getByText('The runtime reached the requested state.')).toBeVisible();
  const status = await (await request.get(`${graphPath}/status`,{headers})).json();
  expect(status).toMatchObject({desiredState:'running',nodes:[{nodeId:'reviewer',paused:true},{nodeId:'reader',paused:false}]});
  await pause.getByRole('link',{name:'Open workflow'}).click();
  await expect(page).toHaveURL(/#\/g\/chat-publication\/canvas\?sel=node%3Areviewer/);
  await page.goBack();
  await expect(pause).toBeVisible();
});

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

test('records one reviewed approval for the exact held task input', async ({ page, request }) => {
  await selectFixtureModel(request);
  const task = await startTaskWaitingForApproval(request,'chat-approval-source','Editorial gate task fixture');
  const progress = await (await request.get(`/api/v1/tasks/${task}/progress`,{headers})).json();
  expect(progress.pendingApprovals).toHaveLength(1);
  const target = progress.pendingApprovals[0];
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-operator-token'));
  await page.goto('/');
  await send(page,'Approve the pending input of the editorial gate task.');
  const decision = page.getByRole('region',{name:`Approve request ${target.id}`});
  await expect(decision.getByText(task,{exact:true})).toBeVisible();
  const before = await (await request.get(`/api/v1/tasks/${task}/progress`,{headers})).json();
  expect(before.pendingApprovals.map((approval:{id:string})=>approval.id)).toEqual([target.id]);
  await decision.getByRole('button',{name:'Approve request',exact:true}).click();
  await expect(decision.getByText('Approval recorded.')).toBeVisible();
  const list = await (await request.get(`/api/v1/approvals?graph=${target.graphId}`,{headers})).json();
  expect(list).toHaveLength(1);
  expect(list[0]).toMatchObject({id:target.id,status:'approved',decidedBy:'e2e-operator',decisionOperation:expect.any(String)});
  await decision.getByRole('link',{name:'Open task'}).click();
  await expect(page.getByRole('region',{name:'Task status',exact:true})).toContainText('Task status: succeeded');
  await page.goBack();
  await expect(decision).toBeVisible();
});

test('cancels only the reviewed task attempt and observes the final cancellation', async ({ page, request }) => {
  await selectFixtureModel(request);
  const id = await startTaskWaitingForApproval(request,'chat-cancel-source','Cancellation fixture task');
  const sourcePath = `/api/v1/tasks/${id}`;
  const before = await (await request.get(`${sourcePath}/progress`,{headers})).json();
  expect(before.pendingApprovals).toHaveLength(1);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await send(page,'Follow the cancellation test task.');
  await expect(page.getByText(/Decision required/).first()).toBeVisible();
  await send(page,'Cancel the running cancellation test task.');
  const card = page.getByRole('region',{name:'Cancel task Cancellation fixture task'});
  await expect(card.getByText(/close its pending approvals/)).toBeVisible();
  expect(await (await request.get(sourcePath,{headers})).json()).toMatchObject({status:'running'});
  await card.getByRole('button',{name:'Cancel task attempt',exact:true}).click();
  await expect(card.getByText(/Cancellation request recorded/)).toBeVisible();
  await expect(page.getByText(`Task ${id}: cancelled.`,{exact:true})).toHaveCount(1);
  const settled = await (await request.get(`${sourcePath}/progress`,{headers})).json();
  expect(settled.task.status).toBe('cancelled');
  expect(settled.pendingApprovals).toHaveLength(0);
  await card.getByRole('link',{name:'Open task',exact:true}).click();
  await expect(page.getByRole('region',{name:'Task status',exact:true})).toContainText('Task status: cancelled');
  await page.goBack();
  await expect(card).toBeVisible();
});

test('records task authorization separately and opens the native access settings', async ({ page, request }) => {
  await selectFixtureModel(request);
  const task = await draftTaskWithApproval(request,'chat-auth-source','Editorial authorization task fixture');
  const path = `/api/v1/tasks/${task.id}/authorization`;
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/');
  await send(page,'Set the editorial authorization task to workspace access.');
  const review = page.getByRole('region',{name:'Authorize task Editorial authorization task fixture'});
  await expect(review.getByText('Workspace access',{exact:true})).toBeVisible();
  expect((await(await request.get(path,{headers})).json()).record).toBeUndefined();
  await review.getByRole('button',{name:'Save task authorization',exact:true}).click();
  await expect(review.getByText('Authorization revision 1 saved.')).toBeVisible();
  const native = await (await request.get(path,{headers})).json();
  expect(native.record).toMatchObject({taskId:task.id,mode:'workspace',scope:native.scope,revision:1,operation:expect.any(String)});
  const draft = await (await request.get(`/api/v1/tasks/${task.id}`,{headers})).json();
  expect(draft.status).toBe('draft');
  expect(draft.executionGraphId).toBeUndefined();
  await review.getByRole('link',{name:'Open Settings & access',exact:true}).click();
  await expect(page).toHaveURL(new RegExp(`#/tasks/${task.id}\\?tab=settings`));
  await page.locator('summary').filter({hasText:'Access settings'}).click();
  await expect(page.getByRole('region',{name:'Task authorization'})).toContainText('Current mode: workspace.');
  await page.goBack();
  await expect(review).toBeVisible();
});

test('reviews an exact current task allowance and observes the native budget resume', async ({ page, request }) => {
  await selectFixtureModel(request);
  const policyPath = '/api/v1/cost/policy';
  const pricesPath = '/api/v1/cost/prices';
  const previousPolicy = await (await request.get(policyPath, {headers})).json();
  const previousPrices = await (await request.get(pricesPath, {headers})).json();
  const put = async (path:string, data:unknown) => {
    const response = await request.put(path, {headers,data});
    expect(response.ok(), await response.text()).toBe(true);
  };
  try {
    await put(policyPath, {taskRunBudgetMicros: 1, warningPercent: 80});
    await put(pricesPath, {currency:'USD', prices:[{provider:'e2e', model:'*', inputMicrosPerMTok:1_000_000, outputMicrosPerMTok:1_000_000}]});
    const graphId = 'chat-budget-report';
    const graph = await request.post('/api/v1/graphs', {headers,data:{metadata:{id:graphId},spec:{
      nodes:[{id:'reader',prompt:'Summarize the report.',runtime:{kind:'native'},model:{provider:'e2e'},loop:{mode:'once'}}],
    }}});
    expect(graph.status()).toBe(201);
    const draft = await request.post('/api/v1/tasks',{headers,data:{title:'Budget report task fixture',request:'Summarize the report.',
      graphId,graphVersion:1,nodeId:'reader',resultNodeId:'reader',outcome:'understand'}});
    expect(draft.status()).toBe(201);
    const task = await draft.json();
    const taskPath = `/api/v1/tasks/${task.id}`;
    const started = await request.post(`${taskPath}/start`,{headers:{...headers,'If-Match':`"${task.revision}"`},data:{confirm:true}});
    expect(started.status()).toBe(202);
    const readTask = async () => (await request.get(taskPath,{headers})).json();
    await expect.poll(async () => (await readTask()).executionGraphId).toBeTruthy();
    const {executionGraphId} = await readTask();
    const budgetPath = `/api/v1/cost/budgets/run/${executionGraphId}`;
    const readBudget = async () => (await request.get(budgetPath,{headers})).json();
    await expect.poll(readBudget).toMatchObject({limitMicros:1,paused:true,reached:true});
    await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
    await page.goto('/');
    await send(page,'Raise the current allowance for the budget report task to 2.004005 USD.');
    const review = page.getByRole('region',{name:'Increase budget for Budget report task fixture'});
    await expect(review.getByText('2.004005 USD',{exact:true})).toBeVisible();
    await expect(review.getByText('0.000001 USD',{exact:true})).toBeVisible();
    expect(await readBudget()).toMatchObject({limitMicros:1,reached:true});
    await review.getByRole('button',{name:'Increase current budget',exact:true}).click();
    await expect(review.getByText(/Limit raised to 2\.004005 USD/)).toBeVisible();
    await expect(review.getByText(/Running observed|Attempt ended after the resume command/)).toBeVisible();
    expect(await readBudget()).toMatchObject({limitMicros:2_004_005,reached:false});
    const recurring = await (await request.get(policyPath,{headers})).json();
    expect(recurring.taskRunBudgetMicros).toBe(1);
    const inspect = review.getByRole('link',{name:'Open budgets',exact:true});
    await inspect.click();
    await expect(page).toHaveURL(/#\/models\/budgets$/);
    await page.goBack();
    await expect(inspect).toBeFocused();
  } finally {
    await put(policyPath,previousPolicy);
    const {currency,unpriced,prices} = previousPrices;
    await put(pricesPath,{currency,unpriced,prices});
  }
});

test('scales a running fixed node and separately saves an autoscale ceiling', async ({ page, request }) => {
  await selectFixtureModel(request);
  for (const [id,cluster] of [
    ['chat-scale-fixed',{replicas:1}],
    ['chat-scale-auto',{mode:'autoscale',minReplicas:1,maxReplicas:3}],
  ] as const) {
    const created = await request.post('/api/v1/graphs',{headers,data:{metadata:{id},spec:{
      nodes:[{id:'reader',runtime:{kind:'echo'},loop:{mode:'event'},cluster}],
      channels:[{id:'requests',to:['reader']}],
    }}});
    expect(created.status()).toBe(201);
  }
  expect((await request.post('/api/v1/graphs/chat-scale-fixed/start',{headers})).ok()).toBe(true);
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-operator-token'));
  await page.goto('/');
  const read = async (id:string) => (await request.get(`/api/v1/graphs/${id}`,{headers})).json();
  await send(page,'Set three fixed replicas for reader in chat-scale-fixed.');
  const fixed = page.getByRole('region',{name:'Scale chat-scale-fixed / reader'});
  await expect(fixed.getByText('1 → 3')).toBeVisible();
  await fixed.getByRole('button',{name:'Save replica count',exact:true}).click();
  await expect(fixed.getByText('Observed 3 active replicas · Version 1.')).toBeVisible();
  await page.getByRole('button',{name:'New chat',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Chat',exact:true})).toBeVisible();
  await send(page,'Set the reader autoscale maximum to two in chat-scale-auto.');
  const autoscale = page.getByRole('region',{name:'Scale chat-scale-auto / reader'});
  await expect(autoscale.getByText('3 → 2')).toBeVisible();
  expect((await read('chat-scale-auto')).graph.spec.nodes[0].cluster.maxReplicas).toBe(3);
  await autoscale.getByRole('button',{name:'Save autoscale ceiling',exact:true}).click();
  await expect(autoscale.getByText('Policy saved · Version 2. The node is inactive.')).toBeVisible();
  expect((await read('chat-scale-auto')).graph.spec.nodes[0].cluster.maxReplicas).toBe(2);
  const node = autoscale.getByRole('link',{name:'View node',exact:true});
  await node.click();
  await expect(page).toHaveURL(/#\/g\/chat-scale-auto\/canvas\?sel=node%3Areader/);
  await page.goBack();
  await expect(node).toBeFocused();
});

test('pauses future issue intake and resumes the same trigger with fresh reviewed consent', async ({ page, request }) => {
  await selectFixtureModel(request);
  const workflow = 'git-inspect';
  const {graph} = await (await request.get(`/api/v1/graphs/${workflow}`,{headers})).json();
  const created = await request.post('/api/v1/triggers',{headers,data:{
    name:'Chat issue intake fixture',workflow,nodeId:graph.spec.nodes[0].id,
    source:{kind:'github',connectionId:'e2e-tracker',repository:'acme/app'},label:'chat-ready',
    authorizationMode:'ask',readinessDisabled:true,
  }});
  expect(created.status(), await created.text()).toBe(201);
  const original = await created.json();
  const read = async () => (await request.get(`/api/v1/triggers/${original.id}`,{headers})).json();
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/');
  await send(page,'Pause future issue intake for Chat issue intake fixture.');
  const pause = page.getByRole('region',{name:'Pause issue intake for Chat issue intake fixture'});
  await expect(pause).toContainText('github · e2e-tracker · acme/app');
  expect((await read()).state).toBe('active');
  await pause.getByRole('button',{name:'Pause issue intake',exact:true}).click();
  await expect(pause.getByRole('status')).toContainText('Future issue intake paused.');
  expect(await read()).toMatchObject({state:'paused',consent:original.consent});
  await send(page,'Resume future issue intake for Chat issue intake fixture.');
  const resume = page.getByRole('region',{name:'Resume issue intake for Chat issue intake fixture'});
  await expect(resume).toContainText('e2e-admin · ask');
  await resume.getByRole('button',{name:'Resume issue intake',exact:true}).click();
  await expect(resume.getByRole('status')).toContainText('Issue intake resumed with reviewed consent.');
  const reopened = await read();
  expect(reopened).toMatchObject({state:'active',consent:{actor:'e2e-admin',authorizationMode:'ask'}});
  expect(reopened.consent.recordedAt).not.toBe(original.consent.recordedAt);
  const inspect = resume.getByRole('link',{name:'Open workflow triggers'});
  await inspect.click();
  await expect(page.getByRole('article',{name:'Trigger Chat issue intake fixture'}).getByText('Active',{exact:true})).toBeVisible();
  await page.goBack();
  await expect(inspect).toBeFocused();
});

test('prepares one reviewed retry from a failed task and opens the linked draft', async ({ page, request }) => {
  await selectFixtureModel(request);
  const original = await startTaskWaitingForApproval(request,'chat-retry-source','Failed documentation task fixture');
  const sourcePath = `/api/v1/tasks/${original}`;
  const progress = await (await request.get(`${sourcePath}/progress`,{headers})).json();
  expect(progress.pendingApprovals).toHaveLength(1);
  const rejected = await request.post(`/api/v1/approvals/${progress.pendingApprovals[0].id}/reject`,{headers});
  expect(rejected.ok()).toBe(true);
  await expect.poll(async () => (await (await request.get(sourcePath,{headers})).json()).status).toBe('failed');
  const failed = await (await request.get(sourcePath,{headers})).json();
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-operator-token'));
  await page.goto('/');
  await send(page,'Prepare a retry of my failed documentation task.');
  const card = page.getByRole('region',{name:'Retry task Failed documentation task fixture'});
  await expect(card.getByText(`${original} · Revision ${failed.revision}`)).toBeVisible();
  await card.getByRole('button',{name:'Prepare retry draft',exact:true}).click();
  const result = card.getByRole('link',{name:'View retry'});
  const href = await result.getAttribute('href');
  expect(href).toMatch(/^#\/tasks\/tsk_[a-z0-9]+/);
  const childPath = `/api/v1${href?.slice(1).split('?')[0]}`;
  const retry = await (await request.get(childPath,{headers})).json();
  expect(retry).toMatchObject({
    status:'draft', retryOfTaskId:original, retryOfRevision:failed.revision,
    request:failed.request, graphId:failed.graphId, graphVersion:failed.graphVersion,
  });
  expect(retry.executionGraphId).toBeUndefined();
  const consent = await (await request.get(`${childPath}/authorization`,{headers})).json();
  expect(consent.record).toBeUndefined();
  await result.click();
  await expect(page.getByRole('link',{name:'Original failed task'})).toHaveAttribute('href',`#/tasks/${original}`);
  await page.goBack();
  await expect(result).toBeVisible();
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
  await page.getByRole('button', { name: 'Actions for Follow documentation check' }).click();
  await page.getByRole('button', { name: 'Archive', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Restore conversation', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'New chat', exact: true }).click();
  const recents = page.getByRole('region', { name: 'Recents' });
  const title = 'Follow documentation check';
  await expect(recents.getByRole('button', { name: title, exact: true })).toHaveCount(0);
  await approvePendingTaskInput(request, taskId);
  const composer = page.getByRole('textbox',{name:'Message'});
  await composer.fill('Keep this unsent draft');
  await recents.getByRole('button', { name: /Archived.*1 unread/ }).click();
  await expect(composer).toHaveValue('Keep this unsent draft');
  await page.getByRole('region', { name: 'Archived', exact: true }).getByRole('button', { name: title, exact: true }).click();
  await expect(composer).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'View task result' })).toBeVisible();
  await page.getByRole('button', { name: 'Restore conversation', exact: true }).click();
  await expect(composer).toBeEditable();
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
  await expect(page).toHaveURL(new RegExp(`#/tasks/${id}(?:[?]|$)`));
  await expect(page.getByRole('heading', { name: 'Task draft', exact: true })).toBeVisible();
  await expect(page.getByLabel('Title', {exact: true})).toHaveValue('Summarize style notes');
  await expect(page.getByRole('textbox', {name:'Request', exact:true})).toHaveValue('Summarize the style notes.');
  await page.goBack();
  await expect(viewDraft).toBeVisible();
});

test('publishes and separately adopts a reviewed catalog version on one workflow node', async ({ page, request }) => {
  await selectFixtureModel(request);
  const agentId = 'publishing-reader';
  const agentPath = `/api/v1/agents/${agentId}`;
  const created = await request.post('/api/v1/agents', {headers,data:{id:agentId,definition:{
    name:'Publishing reader',prompt:'Read the notes.',runtime:{kind:'native'},
    model:{provider:'e2e',name:'fixture'},isolation:{mode:'process'},readOnly:true,
  }}});
  expect(created.status()).toBe(201);
  const graphPath = '/api/v1/graphs/chat-pinned-reader';
  const graph = await request.post('/api/v1/graphs',{headers,data:{
    metadata:{id:'chat-pinned-reader'},
    spec:{nodes:[{id:'reader',agent:{id:agentId,version:1},localInstructions:'Observe the local constraint.'}]},
  }});
  expect(graph.status()).toBe(201);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  await page.goto('/');
  await send(page,'Publish new shared instructions for Publishing reader to check revised notes.');
  const card = page.getByRole('region',{name:'Publish shared instructions for Publishing reader'});
  await expect(card.getByText('Read the notes.')).toBeVisible();
  await expect(card.getByText('Check the revised notes.')).toBeVisible();
  expect((await(await request.get(agentPath,{headers})).json()).versions).toHaveLength(1);
  await card.getByRole('button',{name:'Publish agent version'}).click();
  await expect(card.getByText('Catalog version 2 published.')).toBeVisible();
  const catalog = await (await request.get(agentPath,{headers})).json();
  expect(catalog.versions).toHaveLength(2);
  expect(catalog.versions[1]).toMatchObject({version:2,definition:{prompt:'Check the revised notes.',readOnly:true}});
  const workflow:GraphSummary = await (await request.get(graphPath,{headers})).json();
  expect(workflow.graph.metadata.version).toBe(1);
  expect(workflow.graph.spec.nodes[0]).toMatchObject({agent:{id:agentId,version:1},localInstructions:'Observe the local constraint.'});
  await card.getByRole('link',{name:'View agent',exact:true}).click();
  await expect(page).toHaveURL(/#\/agents\/publishing-reader$/);
  await page.goBack();
  await expect(card).toBeVisible();

  await send(page,'Adopt version two for the reader node in chat-pinned-reader.');
  const adoption = page.getByRole('region',{name:'Adopt agent version for chat-pinned-reader / reader'});
  await expect(adoption.getByText('Version 1 → Version 2')).toBeVisible();
  expect((await(await request.get(graphPath,{headers})).json()).graph.spec.nodes[0].agent.version).toBe(1);
  await adoption.getByRole('button',{name:'Adopt agent version',exact:true}).click();
  await expect(adoption.getByText('Workflow version 2 saved with catalog version 2.')).toBeVisible();
  const updated:GraphSummary = await (await request.get(graphPath,{headers})).json();
  expect(updated.graph.metadata.version).toBe(2);
  expect(updated.graph.spec.nodes[0]).toMatchObject({agent:{id:agentId,version:2},localInstructions:'Observe the local constraint.'});
  await adoption.getByRole('link',{name:'Open selected node',exact:true}).click();
  await expect(page).toHaveURL(/#\/g\/chat-pinned-reader\/canvas\?sel=node%3Areader/);
  await page.goBack();
  await expect(adoption).toBeVisible();
});

test('a delayed first response never reopens that chat over a second session', async ({page,request}) => {
  await selectFixtureModel(request);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-operator-token'));
  await page.goto('/');
  let release!: () => void;
  const hold = new Promise<void>(resolve => { release = resolve; });
  let firstId!: string;
  let accepted!: () => void;
  const firstAccepted = new Promise<void>(resolve => { accepted = resolve; });
  await page.route('**/api/v1/chat/sessions', async route => {
    if (route.request().method() !== 'POST' || firstId) return route.continue();
    firstId = (route.request().postDataJSON() as {requestId:string}).requestId;
    const response = await route.fetch();
    accepted();
    await hold;
    await route.fulfill({response});
  });
  try {
    await send(page, '/inspect tasks');
    await firstAccepted;
    await page.getByRole('button',{name:'New chat', exact:true}).click();
    await send(page, '/inspect workflows');
    await expect(page).toHaveURL(/#\/chat\/[a-f0-9]{32}/);
    const second = page.url();
    expect(second).not.toContain(firstId);
    const received = page.waitForResponse(r => r.request().method() === 'POST' && r.request().postDataJSON()?.requestId === firstId);
    release();
    await received;
    await expect(page.getByText('/inspect workflows',{exact:true})).toBeVisible();
    await expect(page).toHaveURL(second);
    const original = await request.get(`/api/v1/chat/sessions/${firstId}`, {headers:{Authorization:'Bearer e2e-operator-token'}});
    expect(original.ok()).toBe(true);
    expect((await original.json()).messages[0].text).toBe('/inspect tasks');
  } finally {
    release();
  }
});

test('requests cancellation of a single delegated child from its conversation', async ({page,request}) => {
  await selectFixtureModel(request);
  const id = 'chat-stop-child';
  const graph = {metadata:{id},spec:{nodes:[{id:'parent',runtime:{kind:'native'},prompt:'delegation-fixture',model:{provider:'e2e',name:'fixture'},loop:{mode:'once'},tools:[{name:'delegation.request'}]}]}};
  expect((await request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
  expect((await request.post(`/api/v1/graphs/${id}/start`,{headers})).ok()).toBe(true);
  let child = '';
  await expect.poll(async () => {
    const pending = await (await request.get(`/api/v1/approvals?graph=${id}`,{headers})).json();
    child = pending.find((a:{kind:string;status:string}) => a.kind==='delegation' && a.status==='pending')?.executionId ?? '';
    return child;
  }).not.toBe('');
  await page.addInitScript(() => localStorage.setItem('weftToken','e2e-operator-token'));
  await page.goto('/');
  await send(page,'Cancel just the delegated execution ' + child);
  const review = page.getByRole('region',{name:'Review execution cancellation'});
  await expect(review.getByRole('heading',{name:'Cancel delegated child'})).toBeVisible();
  await review.getByRole('button',{name:'Request cancellation'}).click();
  await expect(review.getByText('The selected scope settled with an observed cancellation.')).toBeVisible();
  const tree = await (await request.get(`/api/v1/executions/${child}/tree`,{headers})).json();
  expect(tree.nodes.find((n:{execution:{id:string}})=>n.execution.id===child).execution.status).toBe('cancelled');
  expect(tree.nodes.find((n:{execution:{id:string}})=>n.execution.id===tree.rootExecutionId).cancellation).toBeUndefined();
});

test('answers a task count after inspecting the inventory in the same chat', async ({ page, request }) => {
  await selectFixtureModel(request);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.goto('/');
  await send(page, '/inspect tasks');
  const replies = page.locator('.chat-message-assistant');
  await expect(replies.first()).toContainText('tasks ·');
  await send(page, 'Quantas tarefas foram finalizadas com sucesso?');
  await expect(replies).toHaveCount(2);
  await expect(replies.last()).toContainText(/Tarefas concluídas com sucesso: \d+\./);
  await expect(replies.last().locator('strong')).toHaveText(/^\d+$/);
  await page.reload();
  await expect(replies.last().locator('strong')).toHaveText(/^\d+$/);
  await expect(replies.last()).not.toContainText('tasks ·');
  await expect(replies.last().getByRole('link', { name: 'Open Tasks' })).toBeVisible();
});

test('opens a verified task detail link from a saved conversation', async ({ page, request }) => {
  await selectFixtureModel(request);
  const task = await draftTaskWithApproval(request, 'chat-link-eval', 'Editorial checklist for the task link');
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-viewer-token'));
  await page.goto('/');
  await send(page, 'Me de o link para a task ' + task.id);
  const link = page.getByRole('link', {name:'Open task', exact:true});
  await expect(link).toHaveAttribute('href', '#/tasks/' + task.id);
  await page.reload();
  await link.click();
  await expect(page).toHaveURL(url => url.hash === '#/tasks/' + task.id);
  await expect(page.getByRole('heading', {name:'Task draft'})).toBeVisible();
  await expect(page.getByRole('textbox', {name:'Request', exact:true})).toHaveValue('Editorial checklist for the task link');
});

test('reviews a task payload in Chat before saving it to the draft', async ({ page, request }) => {
  await selectFixtureModel(request);
  const draft = await draftTaskWithApproval(request, 'chat-payload-review', 'Editorial payload fixture');
  const readDraft = async () => {
    const response = await request.get('/api/v1/tasks/' + draft.id, { headers });
    expect(response.ok()).toBe(true);
    return response.json();
  };
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-operator-token'));
  await page.goto('/');
  await send(page, 'Replace the editorial payload with a JSON object setting section to release.');
  const review = page.getByRole('region', { name: 'Task draft correction' });
  await expect(review.getByRole('heading', { name: 'Replace task input payload' })).toBeVisible();
  await expect(review.getByText('Channel requests · Message type feature.request')).toBeVisible();
  await expect(review.locator('pre')).toHaveText(['{}', '{"section":"release"}']);
  expect(await readDraft()).toMatchObject({ revision: draft.revision, payloadJson: '{}' });
  await review.getByRole('button', { name: 'Save payload replacement' }).click();
  await expect.poll(readDraft).toMatchObject({
    revision: draft.revision + 1, payloadJson: '{"section":"release"}', status: 'draft',
    title: 'Editorial payload fixture', channelId: 'requests', messageType: 'feature.request',
  });
});

test('composes a guided first turn and watches only the chosen same-name task',async({page,request})=>{
  await selectFixtureModel(request);
  const title='Guided release review';
  for(let i=0;i<7;i++) await draftTaskWithApproval(request,'guided-scroll-'+i,'Older task '+i);
  const first=await draftTaskWithApproval(request,'guided-release-one',title);
  const second=await draftTaskWithApproval(request,'guided-release-two',title);
  const viewer={Authorization:'Bearer e2e-viewer-token'};
  const index=async()=> (await (await request.get('/api/v1/chat/sessions',{headers:viewer})).json() as ChatList).items.map(item=>item.id);
  const before=await index();
  await page.setViewportSize({width:390,height:844});
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
  await page.goto('/#/chat');
  const guide=page.getByRole('region',{name:'Chat actions'});
  await guide.getByRole('button',{name:'Tasks',exact:true}).click();
  await guide.getByRole('button',{name:'Follow a task'}).press('Enter');
  const search=guide.getByRole('searchbox',{name:'Search tasks'});
  await expect(search).toBeFocused();
  await expect(guide.getByRole('status')).toContainText('Showing 8 tasks.');
  const list=guide.getByRole('list',{name:'Matching resources'});
  await page.setViewportSize({width:1440,height:900});
  const chat=await page.getByRole('region',{name:'Chat',exact:true}).boundingBox();
  const picker=await guide.boundingBox();
  const center=(picker!.y+picker!.height/2-chat!.y)/chat!.height;
  expect(center).toBeGreaterThan(0.35);
  expect(center).toBeLessThan(0.5);
  const row=list.getByRole('listitem').nth(1);
  await row.getByRole('button').hover();
  await expect(row.getByRole('button')).toHaveCSS('border-top-left-radius','0px');
  await expect(row).toHaveCSS('border-top-width','0px');
  await page.setViewportSize({width:390,height:844});
  const searchTop=(await search.boundingBox())!.y;
  await list.getByRole('button').last().focus();
  expect(await list.evaluate(el=>el.scrollTop)).toBeGreaterThan(0);
  expect((await search.boundingBox())!.y).toBe(searchTop);
  await search.fill(title);
  await expect(list.getByRole('button')).toHaveCount(2);
  expect((await search.boundingBox())!.y).toBe(searchTop);
  const field=page.getByRole('textbox',{name:'Message'});
  await field.fill('Notify me when this finishes.');
  await guide.getByRole('button',{name:new RegExp(first.id.slice(-6)+'$')}).click();
  await expect(guide.getByText(first.id,{exact:true})).toBeVisible();
  await guide.getByRole('button',{name:'Change task'}).click();
  await search.fill(second.id);
  await expect(list.getByRole('button')).toHaveCount(1);
  await list.getByRole('button',{name:new RegExp(second.id.slice(-6)+'$')}).click();
  await expect(guide.getByText(second.id,{exact:true})).toBeVisible();
  await expect(field).toHaveValue('Notify me when this finishes.');
  expect(await index()).toEqual(before);
  await expect(page.getByRole('button',{name:'Send and follow',exact:true})).toBeEnabled();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBe(390);
  await field.press('Control+Enter');
  await expect(page).toHaveURL(/#\/chat\/[a-f0-9]{32}/);
  const id=page.url().split('/').at(-1);
  const saved=await (await request.get('/api/v1/chat/sessions/'+id,{headers:viewer})).json();
  expect(saved.messages.filter((m:{role:string})=>m.role==='user')).toEqual([
    expect.objectContaining({text:'/follow '+second.id+'\nNotify me when this finishes.'}),
  ]);
  expect(saved.watches).toEqual([expect.objectContaining({task:expect.objectContaining({id:second.id})})]);
  await page.reload();
  await expect(page.getByText(/Following task:/).first()).toBeVisible();
  await expect(guide).toHaveCount(0);
});

test('returns to the same unsent guided request from each manual configuration area',async({page,request})=>{
  await selectFixtureModel(request);
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/chat');
  const field=page.getByRole('textbox',{name:'Message'});
  await field.fill('Keep this draft.');
  const guide=page.getByRole('region',{name:'Chat actions'});
  const more=guide.getByRole('button',{name:'Mais'});
  await more.click();
  await more.press('Escape');
  await expect(more).toBeFocused();
  await more.click();
  for(const [category,label,hash,heading] of [
    ['Projects','Open project registration','#/projects/new','Add project'],
    ['Connections','Open connection settings','#/connections','Connections'],
    ['Models','Open model settings','#/models','Models'],
    ['Skills & instructions','Open Skills & instructions editor','#/skills','Skills & instructions'],
  ]){
    const origin=guide.getByRole('button',{name:category,exact:true});
    await origin.click();
    const link=guide.getByRole('link',{name:label});
    await link.click();
    expect(new URL(page.url()).hash).toBe(hash);
    await expect(page.getByRole('heading',{level:1,name:heading,exact:true})).toBeVisible();
    await page.goBack();
    await expect(link).toBeFocused();
    await expect(field).toHaveValue('Keep this draft.');
    await guide.getByRole('button',{name:'Back'}).click();
    await expect(origin).toBeFocused();
  }
});

test('prepares a guided catalog agent for review before creating its first version',async({page,request})=>{
  await selectFixtureModel(request);
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/#/chat');
  const guide=page.getByRole('region',{name:'Chat actions'});
  await guide.getByRole('button',{name:'Agents'}).click();
  await guide.getByRole('button',{name:'Create a catalog agent'}).click();
  await expect(page.getByText('What should the agent be called, and what should it do?')).toBeVisible();
  await expect(page.getByRole('button',{name:'Send',exact:true})).toBeDisabled();
  await page.getByRole('textbox',{name:'Message'}).fill('Guide reader should summarize release notes.');
  const path='/api/v1/agents/guide-reader';
  expect((await request.get(path,{headers})).status()).toBe(404);
  await page.getByRole('button',{name:'Send',exact:true}).click();
  const confirm=page.getByRole('button',{name:'Create agent',exact:true});
  await expect(confirm).toBeEnabled();
  expect((await request.get(path,{headers})).status()).toBe(404);
  await confirm.click();
  await expect(page.getByRole('link',{name:'View agent'})).toHaveAttribute('href','#/agents/guide-reader');
  expect((await (await request.get(path,{headers})).json()).versions).toHaveLength(1);
});

test('recovers a guided lookup and keeps an incomplete branch out of a free-text turn',async({page,request})=>{
  await selectFixtureModel(request);
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
  await page.goto('/#/chat');
  const guide=page.getByRole('region',{name:'Chat actions'});
  await guide.getByRole('button',{name:'Mais'}).click();
  await guide.getByRole('button',{name:'Models'}).click();
  await expect(guide.getByRole('link',{name:'Open model settings'})).toHaveCount(0);
  await expect(guide.getByRole('button',{name:'View configured models'})).toBeEnabled();
  await guide.getByRole('button',{name:'Back'}).click();
  await page.route('**/api/v1/chat/candidates?*',route=>route.abort(),{times:1});
  await guide.getByRole('button',{name:'Workflows'}).click();
  await guide.getByRole('button',{name:'Find a workflow'}).click();
  await guide.getByRole('button',{name:'Retry search'}).click();
  await guide.getByRole('searchbox').fill('No-workflow-has-this-fixture-name');
  await expect(guide.getByRole('status')).toContainText('No matches');
  await page.getByRole('textbox',{name:'Message'}).fill('/inspect models');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  await expect(page).toHaveURL(/#\/chat\/[a-f0-9]{32}/);
  const id=page.url().split('/').at(-1);
  const response=await request.get('/api/v1/chat/sessions/'+id,{headers:{Authorization:'Bearer e2e-viewer-token'}});
  expect((await response.json()).messages[0].text).toBe('/inspect models');
});

test('keeps a guided draft private across an identity change in another tab',async({context,page,request})=>{
  await selectFixtureModel(request);
  await context.addInitScript(()=>{
    if(!localStorage.getItem('weftToken')) localStorage.setItem('weftToken','e2e-admin-token');
  });
  await page.goto('/#/chat');
  const guide=page.getByRole('region',{name:'Chat actions'});
  await guide.getByRole('button',{name:'Agents'}).click();
  await guide.getByRole('button',{name:'Create a catalog agent'}).click();
  const field=page.getByRole('textbox',{name:'Message'});
  await field.fill('Private unsent instructions');
  const other=await context.newPage();
  await other.goto('/#/chat');
  await other.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
  await expect(field).toHaveValue('');
  await expect(guide.getByRole('button',{name:'Mais'})).toBeVisible();
  await guide.getByRole('button',{name:'Agents'}).click();
  await expect(guide.getByRole('button',{name:'Create a catalog agent'})).toHaveCount(0);
  await expect(guide.getByRole('button',{name:'Find a catalog agent'})).toBeVisible();
});
