import {openTaskSettings,openTaskSection,openTaskStopControls} from '../e2e/task-settings';
import {test,expect,type APIRequestContext} from '@playwright/test';
import {readFileSync,mkdirSync,writeFileSync,realpathSync} from 'node:fs';
import {join,dirname} from 'node:path';
import {execFileSync} from 'node:child_process';
import {createServer} from 'node:http';
import type {AddressInfo} from 'node:net';
function createRepository(root:string, directory:string) {
 const repo=join(root,directory);
 mkdirSync(repo);
 execFileSync('git',['init','-q',repo]);
 writeFileSync(join(repo,'tracked.txt'),'original\n');
 execFileSync('git',['-C',repo,'add','tracked.txt']);
 execFileSync('git',['-C',repo,'-c','user.name=Fixture','-c','user.email=fixture@example.test','commit','-qm','initial']);
 writeFileSync(join(repo,'dirty.txt'),'preserve');
 return repo;
}

async function registerRepository(request:APIRequestContext,headers:Record<string,string>,root:string,name:string) {
 const response=await request.post('/api/v1/projects',{headers,data:{name,path:createRepository(root,name)}});
 expect(response.status()).toBe(201);
 return response.json();
}

test('native profile authenticates, registers a local repo and executes a reviewed task in a worktree',async({page})=>{
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 // Credential belongs only to the disposable native installation created by the wrapper.
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();
 const headers={Authorization:`Bearer ${token}`};
 const repo=createRepository(root,'repo');
 const initial=await(await page.request.get('/api/v1/graphs',{headers})).json();
 expect(initial.map((item:{graph:{metadata:{id:string}}})=>item.graph.metadata.id).sort()).toEqual(['software-factory','software-factory-v2']);
 for(const item of initial){
  expect(item.record.state).toBe('stopped');
  expect(item.graph.spec.nodes.every((node:{runtime:{kind:string}})=>node.runtime.kind==='native')).toBe(true);
 }
 const graph={metadata:{id:'native-synthetic',name:'Synthetic native test'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'},workspace:{mode:'worktree'},tools:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 await page.goto('/#/projects');
 await page.getByLabel('API token').fill(token);
 await page.getByRole('button',{name:'Continue',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Projects',exact:true})).toBeVisible();
 await page.getByRole('link',{name:'Add project',exact:true}).click();
 await page.getByRole('textbox',{name:'Project name',exact:true}).fill('Native repository');await page.getByRole('textbox',{name:'Repository path',exact:true}).fill(repo);await page.getByRole('checkbox').check();await page.getByRole('button',{name:'Register project',exact:true}).click();
 const row=page.getByRole('row').filter({hasText:'Native repository'});
 await row.getByText('Configure preference',{exact:true}).click();
 await row.getByRole('combobox',{name:'Preferred execution environment'}).selectOption('local');await page.getByRole('button',{name:'Save environment preference'}).click();
 await expect.poll(async()=>{const projects=await(await page.request.get('/api/v1/projects',{headers})).json();return projects.find((p:{name:string})=>p.name==='Native repository')?.executionMode;}).toBe('local');
 await page.reload();await row.getByText('Configure preference',{exact:true}).click();await expect(row.getByRole('combobox',{name:'Preferred execution environment'})).toHaveValue('local');
 await row.getByRole('link',{name:'Create task',exact:true}).click();
 await expect(page.getByRole('combobox',{name:'Workflow',exact:true})).toContainText('Software Factory');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Synthetic native worktree task');await page.getByRole('textbox',{name:'Request',exact:true}).fill('Test plumbing only; no real model or account access.');
 await openTaskSettings(page);
 await expect(page.getByRole('combobox',{name:'Repositories',exact:true})).toHaveValue('Native repository');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption('native-synthetic');await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Entry agent',exact:true}).selectOption('worker');await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Result agent',exact:true}).selectOption('worker');
 await expect(page.getByRole('combobox',{name:'Execution environment',exact:true})).toHaveValue('local');
 await page.getByRole('button',{name:'Save draft',exact:true}).click();await expect(page).toHaveURL(/#\/tasks\/tsk_/);await expect(page.getByRole('region',{name:'Run task'}).getByRole('status')).toContainText('Draft saved.');
 await page.reload();await openTaskSettings(page);
 await expect(page.getByRole('combobox',{name:'Execution environment',exact:true})).toHaveValue('local');
 await expect(page.getByRole('region',{name:'Launch review'})).toContainText('Ready to run');
 await page.getByRole('button',{name:'Run task',exact:true}).click();
 await expect(page.getByText('Task status: succeeded',{exact:true})).toBeVisible();
 await openTaskSection(page,'Changes & checks');
 await expect(page.getByText('Remote state not verified. No automatic push is performed on task completion.')).toBeVisible();
 await page.reload();await expect(page.getByText('Task status: succeeded',{exact:true})).toBeVisible();
 expect(readFileSync(join(repo,'dirty.txt'),'utf8')).toBe('preserve');
 await expect(page.getByRole('link',{name:'Open in VS Code',exact:true})).toHaveCount(0);
 await page.getByText('Open workspace in your editor',{exact:true}).click();
 await page.getByRole('checkbox',{name:'Weft and this browser are on the same computer, with VS Code installed.'}).check();
 await expect(page.getByRole('link',{name:'Open in VS Code',exact:true})).toHaveAttribute('href',/^vscode:\/\/file.*task-run-/);
 // Inspect the handoff target without launching an external editor during tests.
 const taskPath=await page.getByRole('region',{name:'Review workspace',exact:true}).locator('code').textContent();
 expect(dirname(realpathSync(taskPath!))).toBe(realpathSync(join(root,'state','workspaces')));
 writeFileSync(join(taskPath!,'tracked.txt'),'changed in disposable worktree\n');
 await page.getByRole('button',{name:'Inspect changes',exact:true}).click();
 await expect(page.getByRole('region',{name:'Git inspection',exact:true})).toContainText('M tracked.txt');
 await page.getByRole('combobox',{name:'Git view',exact:true}).selectOption('diff');
 await expect(page.getByRole('region',{name:'Git inspection',exact:true})).toContainText('+changed in disposable worktree');
 expect(readFileSync(join(repo,'tracked.txt'),'utf8')).toBe('original\n');
 await page.getByRole('combobox',{name:'Workspace to review'}).selectOption(JSON.stringify(['worker',0]));
 const agentPath=await page.getByRole('region',{name:'Review task evidence'}).getByRole('region',{name:'Review workspace',exact:true}).locator('code').textContent();
 expect(agentPath).not.toBe(taskPath);writeFileSync(join(agentPath!,'tracked.txt'),'isolated agent change\n');
 await page.getByRole('button',{name:'Inspect changes',exact:true}).click();
 await page.getByRole('combobox',{name:'Git view',exact:true}).selectOption('diff');
 await expect(page.getByRole('region',{name:'Git inspection',exact:true})).toContainText('+isolated agent change');
 expect(readFileSync(join(taskPath!,'tracked.txt'),'utf8')).toBe('changed in disposable worktree\n');
 expect(readFileSync(join(repo,'tracked.txt'),'utf8')).toBe('original\n');

 await page.getByRole('link',{name:'Workspace files',exact:true}).click();await expect(page).toHaveURL(/task-run-.*\/files/);
});

test('native first-use example needs no external account or model',async({page})=>{
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();
 await page.goto('/#/tasks');await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
 // A fresh profile has no model provider: tasks explain it, echo workflows still run.
 const noModel=page.getByRole('note',{name:'No model provider'});
 await expect(noModel).toBeVisible();await expect(noModel.getByRole('link',{name:'Models',exact:true})).toHaveAttribute('href','#/models');
 await expect(page.getByRole('link',{name:'New task',exact:true})).toBeVisible();
 const headers={Authorization:`Bearer ${token}`};
 const graphId='first-use-echo';
 const graph={metadata:{id:graphId},spec:{nodes:[{id:'echo',runtime:{kind:'echo'},loop:{mode:'once'},tools:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 await page.getByRole('link',{name:'New task',exact:true}).click();
 await page.getByRole('textbox',{name:'Request',exact:true}).fill('Echo a first request');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption(graphId);
 await page.getByRole('button',{name:'Save draft',exact:true}).click();
 await expect(page.getByRole('region',{name:'Run task'}).getByRole('status')).toContainText('Draft saved.');
 const taskId=page.url().split('/').pop()!;
 const draft=await(await page.request.get('/api/v1/tasks/'+taskId,{headers})).json();
 expect(draft.status).toBe('draft');
 expect(draft.executionGraphId).toBeFalsy();
 expect(await(await page.request.get(`/api/v1/graphs/${graphId}/executions`,{headers})).json()).toEqual([]);
 await page.reload();
 await expect(page.getByRole('region',{name:'Launch review'})).toContainText('Ready to run');
 await page.getByRole('button',{name:'Run task',exact:true}).click();
 await expect(page.getByText('Task status: succeeded',{exact:true})).toBeVisible();
});

test('preinstalled factory supplies editable task inputs without creating another workflow',async({page})=>{
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();
 const headers={Authorization:`Bearer ${token}`};
 const before=await(await page.request.get('/api/v1/graphs/software-factory',{headers})).json();
 await page.goto('/#/tasks/new');await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption('software-factory');
 await openTaskSettings(page);
 await expect(page.getByRole('combobox',{name:'Entry agent',exact:true})).toHaveValue('planner');
 await openTaskSettings(page);
 await expect(page.getByRole('combobox',{name:'Result agent',exact:true})).toHaveValue('integrator');
 await openTaskSettings(page);
 await expect(page.getByRole('combobox',{name:'Input delivery',exact:true})).toHaveValue('requests');
 await openTaskSettings(page);
 await expect(page.getByRole('textbox',{name:'Message type',exact:true})).toHaveValue('feature.request');
 await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Factory draft');await page.getByRole('textbox',{name:'Request',exact:true}).fill('Inspect a future change');
 expect(JSON.parse(await page.getByRole('textbox',{name:'Input payload (JSON)',exact:true}).inputValue())).toEqual({title:'Factory draft',description:'Inspect a future change'});
 await page.getByRole('button',{name:'Save draft',exact:true}).click();await expect(page).toHaveURL(/#\/tasks\/tsk_/);await expect(page.getByRole('region',{name:'Run task'}).getByRole('status')).toContainText('Draft saved.');await page.reload();
 await openTaskSettings(page);
 await expect(page.getByRole('combobox',{name:'Entry agent',exact:true})).toHaveValue('planner');
 const taskId=page.url().split('/').pop()!;
 await page.getByRole('button',{name:'Run task',exact:true}).click();
 await expect(page.getByRole('status').filter({hasText:/task has not started|complete task setup|before it can run|choose|model/i})).toBeVisible();
 const draft=await(await page.request.get('/api/v1/tasks/'+taskId,{headers})).json();
 expect(draft.status).toBe('draft');
 const after=await(await page.request.get('/api/v1/graphs/software-factory',{headers})).json();expect(after).toEqual(before);
 expect(await(await page.request.get('/api/v1/graphs/software-factory/executions',{headers})).json()).toEqual([]);
});

test('native MCP process is reviewed, discovered and repaired without granting agent tools',async({page})=>{
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();
 const headers={Authorization:`Bearer ${token}`};
 const script=join(root,'mcp-fixture.cjs');
 writeFileSync(script,`const readline=require('node:readline');
readline.createInterface({input:process.stdin}).on('line',line=>{const r=JSON.parse(line);if(!r.id)return;let result;
if(r.method==='initialize')result={protocolVersion:'2025-06-18',capabilities:{tools:{}}};
if(r.method==='tools/list')result={tools:[{name:'inspect',description:'Synthetic inspection fixture',inputSchema:{type:'object'}}]};
if(r.method==='tools/call')result={content:[{type:'text',text:'fixture'}]};
process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:r.id,result})+'\\n');});`);
 const graphsBefore=await(await page.request.get('/api/v1/graphs',{headers})).json();
 await page.goto('/#/connections/new');await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
 await page.getByLabel('Connection transport',{exact:true}).selectOption('stdio');
 await page.getByLabel('Connection ID',{exact:true}).fill('local-fixture');await page.getByLabel('Connection name',{exact:true}).fill('Local fixture');
 await page.getByLabel('Executable path',{exact:true}).fill(join(root,'missing-executable'));await page.getByLabel('Working directory',{exact:true}).fill(root);await page.getByLabel('Process arguments',{exact:true}).fill(script);
 await page.getByLabel('Environment values (JSON)',{exact:true}).fill('{"FIXTURE_KEY":"synthetic-native-mcp-secret"}');
 await expect(page.getByRole('button',{name:'Save MCP connection',exact:true})).toBeDisabled();
 await page.getByRole('checkbox',{name:'I reviewed this process and allow it to run on the Weft host.'}).check();await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await expect(page).toHaveURL(/connections\/c\/local-fixture/);await expect(page.getByText('Discovery failed: MCP process could not start')).toBeVisible();
 await page.getByRole('link',{name:'Settings',exact:true}).click();await page.getByRole('button',{name:'Edit Local fixture',exact:true}).click();
 await expect(page.getByLabel('Environment values (JSON)',{exact:true})).toHaveValue('');
 await page.getByLabel('Executable path',{exact:true}).fill(process.execPath);await page.getByLabel('Environment values (JSON)',{exact:true}).fill('{}');
 await page.getByRole('checkbox',{name:'I reviewed this process and allow it to run on the Weft host.'}).check();await page.getByRole('button',{name:'Save MCP connection',exact:true}).click();
 await expect(page.getByText('1 tools discovered. Grant access to agents before they can use them.')).toBeVisible();
 await page.getByRole('link',{name:'View discovered tools',exact:true}).click();await expect(page.getByText('mcp.local-fixture.inspect',{exact:true})).toBeVisible();
 await page.reload();await expect(page.getByText('mcp.local-fixture.inspect',{exact:true})).toBeVisible();
 const publicRecord=await(await page.request.get('/api/v1/mcp/connections/local-fixture',{headers})).text();expect(publicRecord).not.toContain('synthetic-native-mcp-secret');expect(publicRecord).not.toContain('processEnvironmentSealed');
 expect(await(await page.request.get('/api/v1/graphs',{headers})).json()).toEqual(graphsBefore);
 expect(await page.evaluate(()=>JSON.stringify([localStorage,sessionStorage]))).not.toContain('synthetic-native-mcp-secret');
});

test('native task preparation failure is actionable and preserves the blocking file',async({page})=>{
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();const headers={Authorization:`Bearer ${token}`};
 const graph={metadata:{id:'native-setup-failure'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'},workspace:{mode:'worktree'},tools:[]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 const response=await page.request.post('/api/v1/tasks',{headers,data:{title:'Preparation failure',request:'Synthetic setup check',outcome:'freeform',graphId:graph.metadata.id,graphVersion:1,nodeId:'worker',resultNodeId:'worker'}});expect(response.status()).toBe(201);const task=await response.json();
 const graphId=`task-run-${task.id.slice(4)}`;const obstruction=join(root,'state','workspaces','.worktrees',graphId,'worker-0');mkdirSync(dirname(obstruction),{recursive:true});writeFileSync(obstruction,'preserve');
 expect((await page.request.post(`/api/v1/tasks/${task.id}/start`,{headers:{...headers,'If-Match':String(task.revision)},data:{confirm:true}})).status()).toBe(202);
 await page.goto(`/#/tasks/${task.id}`);await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
 await expect(page.getByText('Task status: blocked',{exact:true})).toBeVisible();await expect(page.getByText(/An agent failed during preparation/)).toBeVisible();
 await page.reload();await expect(page.getByText('Task status: blocked',{exact:true})).toBeVisible();expect(readFileSync(obstruction,'utf8')).toBe('preserve');
 expect(await(await page.request.get(`/api/v1/graphs/${graphId}/executions`,{headers})).json()).toEqual([]);
});

test('stopped native task cleanup requires review and retains its branch and source',async({page})=>{
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();const headers={Authorization:`Bearer ${token}`};
 const project=await registerRepository(page.request,headers,root,'cleanup-repo');
 const graph={metadata:{id:'native-cleanup'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'event'},tools:[]}],channels:[{id:'requests',to:['worker'],types:['work.request'],approval:true}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 const response=await page.request.post('/api/v1/tasks',{headers,data:{title:'Reviewed cleanup',request:'Synthetic cleanup check',channelId:'requests',messageType:'work.request',payloadJson:'{}',outcome:'freeform',graphId:graph.metadata.id,graphVersion:1,nodeId:'worker',resultNodeId:'worker',projectId:project.id,baseCommit:project.head}});expect(response.status()).toBe(201);const task=await response.json();
 expect((await page.request.post(`/api/v1/tasks/${task.id}/start`,{headers:{...headers,'If-Match':String(task.revision)},data:{confirm:true}})).status()).toBe(202);
 await page.goto(`/#/tasks/${task.id}`);await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();await expect(page.getByRole('region',{name:'Task decisions'})).toBeVisible();
 await openTaskSection(page,'Settings & access');
 await expect(page.getByText('Review workspace cleanup',{exact:true})).toHaveCount(0);
 await openTaskStopControls(page);
 await page.getByRole('checkbox',{name:"Stop this task's execution copy and retain its data."}).check();await page.getByRole('button',{name:'Stop task',exact:true}).click();await expect(page.getByText('Task status: cancelled',{exact:true})).toBeVisible();
 await page.getByText('Review workspace cleanup',{exact:true}).click();
 const previewResult=page.waitForResponse(r=>new URL(r.url()).pathname===`/api/v1/tasks/${task.id}/cleanup`&&r.request().method()==='GET');
 await page.getByRole('button',{name:'Preview cleanup',exact:true}).click();
 const reviewed=await (await previewResult).json();
 const preview=page.getByRole('region',{name:'Workspace cleanup preview'});await expect(preview).toContainText('Working tree clean.');await expect(page.getByRole('button',{name:'Remove reviewed checkout'})).toBeDisabled();
 const path=await preview.locator('code').textContent();expect(path).toContain('task-run-');
 await page.getByRole('checkbox',{name:'I stopped external writers and authorize removal of this clean checkout, keeping its branch.'}).check();await page.getByRole('button',{name:'Remove reviewed checkout'}).click();await expect(page.getByText('Checkout removed. Its branch and task history were retained.')).toBeVisible();
 expect(()=>readFileSync(join(path!,'tracked.txt'),'utf8')).toThrow();expect(readFileSync(join(project.path,'dirty.txt'),'utf8')).toBe('preserve');
 expect(reviewed.branch).toBe(`weft/task-run-${task.id.slice(4)}`);
 expect(execFileSync('git',['--git-dir',reviewed.retainedRepository,'rev-parse','--verify',`refs/heads/${reviewed.branch}`],{encoding:'utf8'}).trim()).toBe(project.head);
 expect(execFileSync('git',['-C',project.path,'rev-parse','HEAD'],{encoding:'utf8'}).trim()).toBe(project.head);
 await page.reload();await expect(page.getByText('Task status: cancelled',{exact:true})).toBeVisible();
});

test('configures a model and runs the preinstalled factory through its human handoff gate',async({page})=>{
 test.setTimeout(120000);
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();
 const headers={Authorization:`Bearer ${token}`};
 const repo=join(root,'factory-repo');
 mkdirSync(repo);
 execFileSync('git',['init','-q',repo]);
 writeFileSync(join(repo,'tracked.txt'),'original\n');
 execFileSync('git',['-C',repo,'add','tracked.txt']);
 execFileSync('git',['-C',repo,'-c','user.name=Fixture','-c','user.email=fixture@example.test','commit','-qm','initial']);
 writeFileSync(join(repo,'dirty.txt'),'preserve');
 const registration=await page.request.post('/api/v1/projects',{headers,data:{name:'Factory repository',path:repo}});
 expect(registration.status()).toBe(201);
 const calls:Record<string,number>={};
 const upstream=createServer(async(req,res)=>{
  res.setHeader('Content-Type','application/json');
  if(req.url==='/v1/models'){res.end(JSON.stringify({data:[{id:'factory-fixture',object:'model'}]}));return;}
  if(req.url!=='/v1/chat/completions'){res.writeHead(404);res.end('{}');return;}
  let raw='';for await(const chunk of req)raw+=chunk;
  const body=JSON.parse(raw) as {messages:{role:string;content?:string}[]};
  const node=/node "([a-z]+)"/.exec(body.messages.find(m=>m.role==='system')?.content??'')?.[1];
  let message:Record<string,unknown>={role:'assistant',content:node==='integrator'?'Factory fixture handoff complete. No tests or commits were performed.':'OK'};
  let finish='stop';
  if(node&&node!=='integrator'&&!body.messages.some(m=>m.role==='tool')){
   calls[node]=(calls[node]??0)+1;
   const types:Record<string,string>={planner:calls.planner===1?'research.question':'task.ready',researcher:'research.done',developer:'code.ready',reviewer:'review.approved',qa:'qa.ready'};
   message={role:'assistant',content:null,tool_calls:[{id:`${node}-${calls[node]}`,type:'function',function:{name:'emit',arguments:JSON.stringify({type:types[node],payload:{title:'Synthetic factory validation',description:'No source mutation; inspect the retained worktree.'}})}}]};finish='tool_calls';
  }
  res.end(JSON.stringify({model:'factory-fixture',choices:[{message,finish_reason:finish}],usage:{prompt_tokens:12,completion_tokens:3}}));
 });
 await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));
 try {
  await page.goto('/#/models/plugins');await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
  const openaiPlugin=page.getByRole('listitem',{name:'Inference plugin OpenAI'});await expect(openaiPlugin).toBeVisible();
  if(await openaiPlugin.getByRole('button',{name:'Enable',exact:true}).isVisible())await openaiPlugin.getByRole('button',{name:'Enable',exact:true}).click();
  await expect(openaiPlugin.getByText('Enabled',{exact:true})).toBeVisible();
  await page.goto('/#/models/providers');
  await page.getByRole('button',{name:'Add provider',exact:true}).click();
  const form=page.getByRole('form',{name:'Provider settings'});
  await form.getByLabel(/^Name/).fill('factory-fixture');await form.getByRole('combobox',{name:/^Type/}).selectOption('openai');
  await form.getByLabel(/^Base URL/).fill(`http://127.0.0.1:${(upstream.address() as AddressInfo).port}/v1`);
  await form.getByLabel(/^Default model/).fill('factory-fixture');await form.getByLabel(/^API key/).fill('synthetic-native-factory-key');
  await form.getByRole('button',{name:'Add provider',exact:true}).click();
  const card=page.getByRole('article',{name:'Provider factory-fixture',exact:true});await expect(card).toBeVisible();
  await card.getByRole('button',{name:'Test connection',exact:true}).click();await expect(card).toContainText('Inference works with factory-fixture');
  // The first provider of a fresh profile is the default.
  await expect(card.getByText('Default',{exact:true})).toBeVisible();
  const factoryBefore=await(await page.request.get('/api/v1/graphs/software-factory',{headers})).json();
  await page.goto('/#/tasks/new');
  await openTaskSettings(page);
 await page.getByRole('textbox',{name:'Title',exact:true}).fill('Native factory acceptance');
  await page.getByRole('textbox',{name:'Request',exact:true}).fill('Review the repository using the synthetic model. Do not change files.');
  await openTaskSettings(page);
 await page.getByRole('combobox',{name:'Repositories',exact:true}).click();
 await page.getByRole('listbox',{name:'Repositories'}).getByRole('option',{name:/Factory repository/}).click();
 await page.keyboard.press('Escape');
  await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption('software-factory');
  await page.getByRole('button',{name:'Save draft',exact:true}).click();await expect(page).toHaveURL(/#\/tasks\/tsk_/);
  const taskId=page.url().split('/').pop()!;
  await expect(page.getByRole('region',{name:'Launch review'})).toContainText('Ready to run');
  await page.getByRole('button',{name:'Run task',exact:true}).click();
  const decisions=page.getByRole('region',{name:'Task decisions'});
  await expect(decisions.getByRole('button',{name:'Approve',exact:true})).toBeVisible({timeout:60000});
  const before=await(await page.request.get(`/api/v1/tasks/${taskId}/progress`,{headers})).json();
  expect(before.executions.some((e:{nodeId:string})=>e.nodeId==='integrator')).toBe(false);
  await decisions.getByRole('button',{name:'Approve',exact:true}).click();
  await expect(page.getByText('Task status: succeeded',{exact:true})).toBeVisible({timeout:30000});
  await expect(page.getByText('Factory fixture handoff complete. No tests or commits were performed.',{exact:true})).toBeVisible();
  await page.reload();await expect(page.getByText('Task status: succeeded',{exact:true})).toBeVisible();
  const after=await(await page.request.get('/api/v1/graphs/software-factory',{headers})).json();expect(after).toEqual(factoryBefore);
  expect(readFileSync(join(repo,'tracked.txt'),'utf8')).toBe('original\n');expect(readFileSync(join(repo,'dirty.txt'),'utf8')).toBe('preserve');
  expect(calls).toEqual({planner:2,researcher:1,developer:1,reviewer:1,qa:1});
 } finally {await new Promise<void>((resolve,reject)=>upstream.close(error=>error?reject(error):resolve()));}
});

test('parallel native tasks keep independent worktrees and cancellation scopes',async({page})=>{
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();const headers={Authorization:`Bearer ${token}`};
 const project=await registerRepository(page.request,headers,root,'parallel-repo');
 const graph={metadata:{id:'parallel-native',name:'Parallel native fixture'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'event'},tools:[]}],channels:[{id:'requests',to:['worker'],types:['work.request'],approval:true}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 const drafts=await Promise.all(['First','Second'].map(async title=>{
  const response=await page.request.post('/api/v1/tasks',{headers,data:{title,request:'Synthetic parallel task',channelId:'requests',messageType:'work.request',payloadJson:'{}',outcome:'freeform',graphId:'parallel-native',graphVersion:1,nodeId:'worker',resultNodeId:'worker',projectId:project.id,baseCommit:project.head,executionMode:'local'}});
  expect(response.status()).toBe(201);return response.json();
 }));
 const starts=await Promise.all(drafts.map(d=>page.request.post(`/api/v1/tasks/${d.id}/start`,{headers:{...headers,'If-Match':String(d.revision)},data:{confirm:true}})));
 for(const response of starts)expect(response.status()).toBe(202);
 for(const draft of drafts)await expect.poll(async()=> (await(await page.request.get(`/api/v1/tasks/${draft.id}/progress`,{headers})).json()).pendingApprovals?.length).toBe(1);
 const states=await Promise.all(drafts.map(async d=>(await page.request.get(`/api/v1/tasks/${d.id}/progress`,{headers})).json()));
 expect(states[0].workspacePath).not.toBe(states[1].workspacePath);
 writeFileSync(join(states[0].workspacePath,'tracked.txt'),'first task only\n');
 expect(readFileSync(join(states[1].workspacePath,'tracked.txt'),'utf8')).toBe('original\n');
 expect((await page.request.post(`/api/v1/tasks/${drafts[0].id}/cancel`,{headers,data:{confirm:true}})).status()).toBe(202);
 await expect.poll(async()=> (await(await page.request.get(`/api/v1/tasks/${drafts[0].id}/progress`,{headers})).json()).state).toBe('cancelled');
 const retained=await(await page.request.get(`/api/v1/tasks/${drafts[1].id}/progress`,{headers})).json();
 expect(retained.pendingApprovals).toEqual(states[1].pendingApprovals);
 await page.goto(`/#/tasks/${drafts[1].id}`);await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
 await page.getByRole('region',{name:'Task decisions'}).getByRole('button',{name:'Approve',exact:true}).click();
 await expect(page.getByText('Task status: succeeded',{exact:true})).toBeVisible();
 expect(readFileSync(join(states[0].workspacePath,'tracked.txt'),'utf8')).toBe('first task only\n');
 expect(readFileSync(join(project.path,'dirty.txt'),'utf8')).toBe('preserve');
});
test('native import offers explicitly selected host authentication without invoking it on selection',async({page})=>{
 const root=process.env.WEFT_NATIVE_E2E_DATA!;
 const token=readFileSync(join(root,'state','admin.token'),'utf8').trim();
 await page.goto('/#/projects');await page.getByLabel('API token').fill(token);await page.getByRole('button',{name:'Continue',exact:true}).click();
 await page.getByRole('link',{name:'Add project',exact:true}).click();
 await page.getByRole('link',{name:'Import from URL',exact:true}).click();
 const section=page.getByRole('region',{name:'Import repository'});
 await section.getByRole('textbox',{name:'Clone URL',exact:true}).fill('https://example.invalid/native-import.git');
 const consent=section.getByRole('checkbox',{name:/Authorize the selected Git authentication/});
 const submit=section.getByRole('button',{name:'Import repository',exact:true});
 await section.getByRole('combobox',{name:'Git authentication'}).selectOption('local_git');
 await expect(section).toContainText('Helpers may execute local commands');
 await expect(submit).toBeDisabled();
 await consent.check();
 await expect(submit).toBeEnabled();
 await section.getByRole('combobox',{name:'Git authentication'}).selectOption('local_gh');
 await expect(consent).not.toBeChecked();
 await expect(submit).toBeDisabled();
 const records=await(await page.request.get('/api/v1/repository-imports',{headers:{Authorization:`Bearer ${token}`}})).json();expect(records).toEqual([]);
});
