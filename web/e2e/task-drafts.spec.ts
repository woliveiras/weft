import {openTaskSettings} from './task-settings';
import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
test('saves and resumes a reviewed request without starting or changing the workflow',async({page})=>{
 const graph={metadata:{id:'task-draft-flow',name:'Task draft flow'},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'},loop:{mode:'once'},tools:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/tasks');
 await page.getByRole('link',{name:'New task',exact:true}).click();
 await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Goal',exact:true}).selectOption('investigate');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Investigate parser');
 await page.getByRole('textbox',{name:'Request',exact:true}).fill('Explain why the parser rejects this input.');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption('task-draft-flow');
 await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Entry agent',exact:true}).selectOption('worker');
 // Explicitly clear the suggested result to retain invalid-input coverage.
 await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Result agent',exact:true}).selectOption('');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();
 await expect(page).toHaveURL(/#\/tasks\/tsk_/);
 await expect(page.getByRole('status')).toHaveText('Draft saved. No agents have been started.');
 await page.reload();
 await expect(page.getByRole('textbox',{name:'Request',exact:true})).toHaveValue('Explain why the parser rejects this input.');
 await page.getByRole('button',{name:'Review requirements'}).click();
 await expect(page.getByRole('article',{name:'Task requirements'})).toContainText('Select an enabled result agent.');
 expect(await (await page.request.get('/api/v1/graphs/task-draft-flow/executions',{headers})).json()).toEqual([]);
 const saved=await (await page.request.get('/api/v1/graphs/task-draft-flow',{headers})).json();
 expect(saved.record.version).toBe(1);expect(saved.graph.spec.nodes[0].tools??[]).toEqual([]);
 const url=page.url();
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Updated request');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();
 await expect(page.getByRole('status')).toHaveText('Draft saved. No agents have been started.');
 await page.reload();await openTaskSettings(page);
 await expect(page.getByRole('textbox',{name:'Title',exact:true})).toHaveValue('Updated request');
 await page.getByRole('link',{name:'Back to tasks',exact:true}).click();
 await page.getByRole('link',{name:'Updated request',exact:true}).click();await expect(page).toHaveURL(url);
});

test('viewer cannot save a task through the UI or API',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-viewer-token'));
 await page.goto('/#/tasks');
 await expect(page.getByRole('button',{name:'Preview example'})).toHaveCount(0);
 await page.goto('/#/tasks/new');
 await expect(page.getByRole('button',{name:'Save draft'})).toBeDisabled();
 expect((await page.request.post('/api/v1/tasks',{headers:{Authorization:'Bearer e2e-viewer-token'},data:{}})).status()).toBe(403);
});

test('reviews and starts an owned task, retains its result after reload and stops only its copy',async({page})=>{
 const graph={metadata:{id:'task-start-flow',name:'Task start flow'},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'},loop:{mode:'once'},tools:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/tasks/new');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Isolated task');
 await page.getByRole('textbox',{name:'Request',exact:true}).fill('Explain this synthetic request');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption('task-start-flow');
 await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Entry agent',exact:true}).selectOption('worker');
 await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Result agent',exact:true}).selectOption('worker');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();
 await expect(page).toHaveURL(/#\/tasks\/tsk_/);
 await page.getByRole('button',{name:'Review requirements'}).click();
 await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeDisabled();
 await page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'}).check();
 await page.getByRole('button',{name:'Start task',exact:true}).click();
 await expect(page.getByRole('status')).toHaveText('Task status: succeeded');
 await page.reload();await expect(page.getByRole('status')).toHaveText('Task status: succeeded');
 const taskId=page.url().split('/').pop()!;
 const progress=await(await page.request.get(`/api/v1/tasks/${taskId}/progress`,{headers})).json();
 await page.getByLabel('Inspect execution evidence').selectOption(progress.executions[0].id);
 await expect(page.getByRole('region',{name:'Run tool calls',exact:true})).toContainText('0 tool invocations observed');
 await page.getByText('Execution tree and interruption',{exact:true}).click();
 await expect(page.getByRole('region',{name:'Execution tree',exact:true})).toContainText('Root execution');
 await expect(page.getByRole('button',{name:'Stop entire tree',exact:true})).toBeDisabled();
 await page.getByText('Execution tree and interruption',{exact:true}).click();
 await expect(page.getByRole('link',{name:'Execution details'})).toHaveAttribute('href',/#\/g\/task-run-/);
 expect(await(await page.request.get('/api/v1/graphs/task-start-flow/executions',{headers})).json()).toEqual([]);
 await page.getByRole('checkbox',{name:"Stop this task's execution copy and retain its data."}).check();
 await page.getByRole('button',{name:'Stop task',exact:true}).click();
 await expect(page.getByRole('status')).toHaveText('Task status: cancelled');
});

test('reviews structured channel input and reports the declared result with retained output',async({page})=>{
 const graph={metadata:{id:'task-channel-flow',name:'Task channel flow'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'event'}}],channels:[{id:'requests',to:['worker'],types:['feature.*']},{id:'result',from:'worker'}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto('/#/tasks/new');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Structured task');await page.getByRole('textbox',{name:'Request',exact:true}).fill('Explain fixture');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption('task-channel-flow');
 await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Input delivery',exact:true}).selectOption('requests');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Message type',exact:true}).fill('feature.request');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Input payload (JSON)',exact:true}).fill('{"title":"Explicit input"}');
 await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Result agent',exact:true}).selectOption('worker');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();await page.getByRole('button',{name:'Review requirements'}).click();
 await page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'}).check();await page.getByRole('button',{name:'Start task',exact:true}).click();
 await expect(page.getByRole('status')).toHaveText('Task status: succeeded');
 await expect(page.getByText('echoed 1 message(s)',{exact:true})).toBeVisible();
});

test('previews a synthetic example and runs it only after explicit review',async({page})=>{
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/tasks');
 await page.getByRole('button',{name:'Preview example'}).click();
 await expect(page.getByRole('button',{name:'Prepare example'})).toBeDisabled();
 await page.getByRole('checkbox',{name:'Create the synthetic workflow and task draft.'}).check();
 await page.getByRole('button',{name:'Prepare example'}).click();
 await expect(page).toHaveURL(/#\/tasks\/tsk_/);
 await expect(page.getByRole('status')).toHaveText('Draft saved. No agents have been started.');
 const id=page.url().split('/').pop()!;
 const draft=await(await page.request.get(`/api/v1/tasks/${id}`,{headers})).json();
 expect(draft.status).toBe('draft');
 expect(await(await page.request.get(`/api/v1/graphs/${draft.graphId}/executions`,{headers})).json()).toEqual([]);
 const graph=await(await page.request.get(`/api/v1/graphs/${draft.graphId}`,{headers})).json();
 expect(graph.graph.spec.nodes[0].runtime.kind).toBe('echo');expect(graph.graph.spec.nodes[0].tools??[]).toEqual([]);
 await page.getByRole('button',{name:'Review requirements'}).click();
 await page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'}).check();
 await page.getByRole('button',{name:'Start task',exact:true}).click();
 await expect(page.getByRole('status')).toHaveText('Task status: succeeded');
 await page.reload();await expect(page.getByRole('status')).toHaveText('Task status: succeeded');
});

test('keeps task approvals contextual and preserves viewer restrictions and decision outcomes',async({page})=>{
 const graph={metadata:{id:'task-decision-flow',name:'Task decision flow'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'event'}}],channels:[{id:'requests',to:['worker'],approval:true,types:['feature.request']}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 await page.addInitScript(()=>{if(!localStorage.getItem('weftToken'))localStorage.setItem('weftToken','e2e-admin-token');});
 for(const decision of ['approve','reject']) {
  const response=await page.request.post('/api/v1/tasks',{headers,data:{title:`Decision ${decision}`,request:'Explain fixture',outcome:'freeform',graphId:'task-decision-flow',graphVersion:1,nodeId:'worker',resultNodeId:'worker',channelId:'requests',messageType:'feature.request',payloadJson:'{}'}});
  expect(response.status()).toBe(201);const draft=await response.json();
  expect((await page.request.post(`/api/v1/tasks/${draft.id}/start`,{headers:{...headers,'If-Match':String(draft.revision)},data:{confirm:true}})).status()).toBe(202);
  await page.goto(`/#/tasks/${draft.id}`);const taskURL=page.url();
  const decisions=page.getByRole('region',{name:'Task decisions',exact:true});
  await expect(decisions.getByRole('heading',{name:'Your decision is needed'})).toBeVisible();
  await decisions.getByRole('button',{name:decision==='approve'?'Approve':'Reject',exact:true}).click();
  await expect(page).toHaveURL(taskURL);
  await expect(page.getByRole('status')).toHaveText(`Task status: ${decision==='approve'?'succeeded':'failed'}`);
  await expect(decisions).toContainText('No pending approvals in the latest task snapshot.');
  await page.reload();await expect(page.getByRole('status')).toHaveText(`Task status: ${decision==='approve'?'succeeded':'failed'}`);
  await page.getByText('Activity and diagnostics',{exact:true}).click();
  await expect(page.getByRole('heading',{name:'Task activity',exact:true})).toBeVisible();
  await expect(page.getByRole('region',{name:'Task events'})).toBeVisible();
 }
 const viewerResponse=await page.request.post('/api/v1/tasks',{headers,data:{title:'Viewer decision',request:'Explain fixture',outcome:'freeform',graphId:'task-decision-flow',graphVersion:1,nodeId:'worker',resultNodeId:'worker',channelId:'requests',messageType:'feature.request',payloadJson:'{}'}});
 const viewerTask=await viewerResponse.json();expect((await page.request.post(`/api/v1/tasks/${viewerTask.id}/start`,{headers:{...headers,'If-Match':String(viewerTask.revision)},data:{confirm:true}})).status()).toBe(202);
 await page.evaluate(()=>localStorage.setItem('weftToken','e2e-viewer-token'));await page.goto(`/#/tasks/${viewerTask.id}`);await page.reload();
 const decisions=page.getByRole('region',{name:'Task decisions',exact:true});await expect(decisions).toContainText('Waiting for an authorized operator to decide.');await expect(decisions.getByRole('button',{name:'Approve',exact:true})).toHaveCount(0);
});

test('repairs an MCP connection in another tab and rechecks the retained task before start',async({page})=>{
 const create=await page.request.post('/api/v1/mcp/connections',{headers,data:{id:'task-repair',name:'Task repair connection',service:'generic',endpoint:'https://mcp.fixture.invalid/mcp',enabled:true,credential:{value:'fixture-mcp-token'}}});expect(create.status()).toBe(201);
 const connection=await(await page.request.post('/api/v1/mcp/connections/task-repair/tools/refresh',{headers})).json();
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:'task-repair-flow',name:'Repair workflow'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'},tools:[{name:'mcp.task-repair.search'}]}]}}})).status()).toBe(201);
 expect((await page.request.put('/api/v1/mcp/connections/task-repair',{headers:{...headers,'If-Match':String(connection.revision)},data:{id:'task-repair',name:'Task repair connection',service:'generic',endpoint:'https://mcp.fixture.invalid/mcp',enabled:false}})).status()).toBe(200);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto('/#/tasks/new');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Retained repair draft');await page.getByRole('textbox',{name:'Request',exact:true}).fill('Keep my request during connection repair');await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption('task-repair-flow');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();await page.getByRole('button',{name:'Review requirements',exact:true}).click();await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeDisabled();
 const taskURL=page.url();await page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'}).check();
 const popupPromise=page.waitForEvent('popup');await page.getByRole('link',{name:'Review connection task-repair in another tab'}).click();const popup=await popupPromise;
 await popup.getByRole('link',{name:'Settings',exact:true}).click();await popup.getByRole('button',{name:'Edit Task repair connection',exact:true}).click();await popup.getByRole('checkbox',{name:'Connection enabled',exact:true}).check();await popup.getByRole('button',{name:'Save MCP connection',exact:true}).click();await expect(popup.getByText('2 tools discovered. Grant access to agents before they can use them.')).toBeVisible();await popup.close();
 await expect(page).toHaveURL(taskURL);await expect(page.getByRole('textbox',{name:'Request',exact:true})).toHaveValue('Keep my request during connection repair');await page.getByRole('button',{name:'Review requirements',exact:true}).click();
 await expect(page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'})).not.toBeChecked();await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeDisabled();
 await page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'}).check();await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeEnabled();
});

test('prepares a task sandbox then requires a separate reviewed start',async({page})=>{
 const graph={metadata:{id:'task-environment-flow',name:'Task environment flow'},spec:{environment:{image:'alpine:3.22'},nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'},isolation:{mode:'docker'},tools:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto('/#/tasks/new');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Prepared sandbox task');await page.getByRole('textbox',{name:'Request',exact:true}).fill('Synthetic build and execution boundary');await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption(graph.metadata.id);await page.getByRole('combobox',{name:'Execution environment',exact:true}).selectOption('sandbox');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();await expect(page).toHaveURL(/#\/tasks\/tsk_/);await page.getByRole('button',{name:'Review requirements'}).click();await expect(page.getByRole('region',{name:'Task environment definition'})).toContainText('alpine:3.22');await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeDisabled();
 await page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'}).check();await page.getByRole('button',{name:'Prepare task environment'}).click();
 await expect(page.getByRole('status')).toHaveText('Task status: environment_pending');await expect(page.getByRole('region',{name:'Task environment preparation'})).toContainText('Environment: ready');
 const id=page.url().split('/').at(-1)!;const progress=await(await page.request.get(`/api/v1/tasks/${id}/progress`,{headers})).json();
 expect(await(await page.request.get(`/api/v1/graphs/${progress.task.executionGraphId}/executions`,{headers})).json()).toEqual([]);
 await page.reload();await expect(page.getByRole('button',{name:'Start prepared task'})).toBeDisabled();await page.getByRole('button',{name:'Review prepared task requirements'}).click();await expect(page.getByText('Workflow version 1. Agent permissions remain unchanged.')).toBeVisible();
 await page.getByRole('checkbox',{name:'I reviewed this task environment and authorize the selected build or start action.'}).check();await page.getByRole('button',{name:'Start prepared task'}).click();await expect(page.getByRole('status')).toHaveText('Task status: succeeded');
 const source=await(await page.request.get(`/api/v1/graphs/${graph.metadata.id}/environment`,{headers})).json();expect(source.state).toBe('absent');
});

test('failed task environment remains inspectable and can be stopped without executing',async({page})=>{
 const graph={metadata:{id:'task-environment-failure'},spec:{environment:{dockerfile:'FROM alpine:3.22\nRUN echo E2E_BUILD_FAIL'},nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'},isolation:{mode:'docker'},tools:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 const res=await page.request.post('/api/v1/tasks',{headers,data:{title:'Failed build',request:'Synthetic failure',outcome:'freeform',graphId:graph.metadata.id,graphVersion:1,nodeId:'worker',resultNodeId:'worker',executionMode:'sandbox'}});expect(res.status()).toBe(201);const task=await res.json();
 expect((await page.request.post(`/api/v1/tasks/${task.id}/prepare-environment`,{headers:{...headers,'If-Match':String(task.revision)},data:{confirm:true}})).status()).toBe(202);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto(`/#/tasks/${task.id}`);await expect(page.getByRole('region',{name:'Task environment preparation'})).toContainText('Environment: failed');await expect(page.getByRole('button',{name:'Start prepared task'})).toBeDisabled();await expect(page.getByRole('button',{name:'Retry environment build'})).toBeDisabled();
 await page.reload();await expect(page.getByRole('region',{name:'Task environment preparation'})).toContainText('Environment: failed');
 await page.getByRole('checkbox',{name:"Stop this task's execution copy and retain its data."}).check();await page.getByRole('button',{name:'Stop task',exact:true}).click();await expect(page.getByRole('status')).toHaveText('Task status: cancelled');
 const progress=await(await page.request.get(`/api/v1/tasks/${task.id}/progress`,{headers})).json();expect(await(await page.request.get(`/api/v1/graphs/${progress.task.executionGraphId}/executions`,{headers})).json()).toEqual([]);
});

test('retains an issue source through missing-grant repair without fetching or starting',async({page})=>{
 const graph={metadata:{id:'task-issue-draft',name:'Issue draft'},spec:{nodes:[{id:'reader',runtime:{kind:'echo'},loop:{mode:'once'},tools:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 const response=await page.request.post('/api/v1/tasks',{headers,data:{title:'Investigate issue',request:'Explain the reported problem',outcome:'investigate',graphId:'task-issue-draft',graphVersion:1,nodeId:'reader',resultNodeId:'reader',source:{kind:'github',connectionId:'personal',reference:'octocat/Hello-World#42'}}});
 expect(response.status()).toBe(201);const draft=await response.json();
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto(`/#/tasks/${draft.id}`);
 await openTaskSettings(page);
 await expect(page.getByRole('combobox',{name:'Issue connection',exact:true})).toHaveValue('personal');
 await expect(page.getByRole('textbox',{name:'Issue reference',exact:true})).toHaveValue('octocat/Hello-World#42');
 await page.getByRole('button',{name:'Review requirements'}).click();
 await expect(page.getByRole('article',{name:'Task requirements'})).toContainText('mcp.personal.issue_read');
 await expect(page.getByRole('button',{name:'Start task',exact:true})).toBeDisabled();
 await page.reload();await openTaskSettings(page);await expect(page.getByRole('textbox',{name:'Issue reference',exact:true})).toHaveValue('octocat/Hello-World#42');
 expect(await(await page.request.get('/api/v1/graphs/task-issue-draft/executions',{headers})).json()).toEqual([]);
});

for(const kind of ['github','jira'] as const) test(`reads a selected ${kind} issue through the task agent gateway`,async({page})=>{
 const id=`issue-source-${kind}`;const tool=kind==='github'?'issue_read':'getJiraIssue';
 expect((await page.request.post('/api/v1/mcp/connections',{headers,data:{id,name:id,service:'generic',endpoint:'https://issues.fixture.invalid/mcp',enabled:true,credential:{value:'fixture-mcp-token'}}})).status()).toBe(201);
 expect((await page.request.post(`/api/v1/mcp/connections/${id}/tools/refresh`,{headers})).ok()).toBeTruthy();
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id,name:id},spec:{nodes:[{id:'reader',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},prompt:'Read the task issue source.',isolation:{mode:'process'},loop:{mode:'once'},tools:[{name:`mcp.${id}.${tool}`}]}]}}})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/tasks/new');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill(`Read ${kind} issue`);
 await page.getByRole('textbox',{name:'Request',exact:true}).fill('Investigate the selected issue.');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption(id);
 await page.getByRole('combobox',{name:'Issue service',exact:true}).selectOption(kind);
 await page.getByRole('combobox',{name:'Issue connection',exact:true}).selectOption(id);
 await page.getByRole('textbox',{name:'Issue reference',exact:true}).fill(kind==='github'?'https://github.com/octocat/Hello-World/issues/42':'APP-42');
 if(kind==='jira')await page.getByRole('textbox',{name:'Jira cloud ID',exact:true}).fill('11111111-2222-3333-4444-555555555555');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();await expect(page).toHaveURL(/#\/tasks\/tsk_/);
 await page.getByRole('button',{name:'Review requirements'}).click();
 await page.getByRole('checkbox',{name:'I reviewed the workflow and authorize a separate execution copy.'}).check();
 await page.getByRole('button',{name:'Start task',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Needs your decision',exact:true})).toBeVisible();
 await page.getByRole('region',{name:'Task decisions'}).getByRole('button',{name:'Approve',exact:true}).click();
 await expect(page.getByRole('status')).toHaveText('Task status: succeeded');
 await expect(page.getByText(/Issue result:.*Issue fixture read completed/)).toBeVisible();
 const taskId=page.url().split('/').pop()!;
 const progress=await(await page.request.get(`/api/v1/tasks/${taskId}/progress`,{headers})).json();
 const events=await(await page.request.get(`/api/v1/graphs/${progress.task.executionGraphId}/events`,{headers})).json();
 expect(events.some((e:{type:string;data:{tool?:string;outcome?:string}})=>e.type==='tool.completed'&&e.data.tool===`mcp.${id}.${tool}`&&e.data.outcome==='ok')).toBeTruthy();
});

test('prompt-first draft keeps settings collapsed and never starts or interprets a URL automatically',async({page})=>{
 const id='prompt-draft';
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id,name:id},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'},tools:[]}]}}})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/tasks/new');
 const summary=page.getByText('Review settings',{exact:true});
 await expect(summary.locator('..')).not.toHaveAttribute('open','');
 await expect(page.getByRole('textbox',{name:'Title',exact:true})).toHaveCount(0);
 await page.getByRole('textbox',{name:'Request',exact:true}).fill('Inspect https://example.com/repository\nDo not commit or push.');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption(id);
 await page.getByRole('button',{name:'Save draft',exact:true}).click();
 await expect(page).toHaveURL(/#\/tasks\/tsk_/);
 await page.reload();
 await expect(summary.locator('..')).not.toHaveAttribute('open','');
 await expect(page.getByRole('textbox',{name:'Request',exact:true})).toHaveValue('Inspect https://example.com/repository\nDo not commit or push.');
 const task=await(await page.request.get('/api/v1/tasks/'+page.url().split('/').pop(),{headers})).json();
 expect(task.title).toBe('Inspect https://example.com/repository');expect(task.status).toBe('draft');expect(task.projectId).toBeUndefined();
 expect(await(await page.request.get(`/api/v1/graphs/${id}/executions`,{headers})).json()).toEqual([]);
 await summary.focus();await page.keyboard.press('Enter');
 await expect(page.getByRole('textbox',{name:'Title',exact:true})).toHaveValue(task.title);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Custom title');
 await summary.click();await page.getByRole('textbox',{name:'Request',exact:true}).fill('Revised request');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();
 await expect(page.getByRole('status')).toHaveText('Draft saved. No agents have been started.');
 await page.reload();await openTaskSettings(page);
 await expect(page.getByRole('textbox',{name:'Title',exact:true})).toHaveValue('Custom title');
});

test('reveals invalid advanced fields instead of trapping validation behind collapsed settings',async({page})=>{
 const id='prompt-ambiguous';
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id,name:id},spec:{nodes:[{id:'one',runtime:{kind:'echo'}},{id:'two',runtime:{kind:'echo'}}]}}})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/tasks/new');
 await page.setViewportSize({width:390,height:844});
 await page.getByRole('textbox',{name:'Request',exact:true}).fill('Explain the project');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption(id);
 await expect(page.getByRole('combobox',{name:'Entry agent',exact:true})).toHaveCount(0);
 await page.getByRole('button',{name:'Save draft',exact:true}).click();
 await expect(page.getByRole('combobox',{name:'Entry agent',exact:true})).toBeVisible();
 await expect(page).toHaveURL(/#\/tasks\/new$/);
 await expect(page.getByRole('textbox',{name:'Request',exact:true})).toHaveValue('Explain the project');
 expect(await(await page.request.get(`/api/v1/graphs/${id}/executions`,{headers})).json()).toEqual([]);
});
