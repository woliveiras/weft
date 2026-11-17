import {test,expect} from '@playwright/test';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
const git=promisify(execFile);
const headers={Authorization:'Bearer e2e-admin-token'};
const repo='https://repository.fixture.example/source.git';
const paths=new Set<string>();
for(const mode of ['ask','workspace','full'])test(`prompt to real Git worktree and gateway with ${mode}`,async({page})=>{
 test.skip(process.env.WEFT_E2E_PROMPT_MODEL!=='1','Requires deterministic interpretation fixture');
 const graphId=mode==='ask'?'software-factory':`prompt-qualified-${mode}`;
 const graph={metadata:{id:graphId,name:mode==='ask'?'Software Factory':graphId},spec:{workspace:{git:true},nodes:[{id:'worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},prompt:'prompt-journey-fixture',loop:{mode:'once'},tools:[{name:'filesystem.write',paths:['prompt-result.txt']}]}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto('/#/tasks/new');
 await page.getByRole('textbox',{name:'Request',exact:true}).fill(`Implement the fixture change in ${repo}, without commit or push.`);
 if(mode!=='ask')await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption(graphId);
 await page.getByRole('button',{name:'Resolve request',exact:true}).click();await expect(page).toHaveURL(/#\/tasks\/tsk_/);
 const url=page.url();const taskId=url.split('/').pop()!;
 await expect(page.getByLabel('Request interpretation')).toContainText('repository');
 await page.getByRole('button',{name:'Prepare repository',exact:true}).click();
 const importer=page.getByRole('region',{name:'Import repository'});await importer.getByRole('checkbox').check();await importer.getByRole('button',{name:'Import repository',exact:true}).click();await expect(importer.getByRole('status')).toContainText('ready:');await importer.getByRole('button',{name:'Use imported project'}).click();
 await page.getByRole('button',{name:'Resolve request',exact:true}).click();await expect(page.getByLabel('Request interpretation')).toContainText('Request interpreted and draft saved');
 const auth=page.getByRole('region',{name:'Task authorization'});await auth.getByLabel('Authorization mode').selectOption(mode);
 if(mode==='full')await auth.getByLabel('Remember this mode as a project suggestion').check();
 await auth.getByLabel('I reviewed this task, its workflow permissions, execution location, file and network access, external actions and commit/push scope.').check();await auth.getByRole('button',{name:'Confirm authorization'}).click();await expect(auth).toContainText(`Current mode: ${mode}`);
 await page.reload();await expect(auth).toContainText(`Current mode: ${mode}`);
 await page.getByRole('button',{name:'Run task',exact:true}).click();
 if(mode==='ask'){
  await expect(page.getByRole('heading',{name:'Needs your decision',exact:true})).toBeVisible();await page.reload();
  await page.getByRole('region',{name:'Task decisions'}).getByRole('button',{name:'Approve',exact:true}).click();
 }
 await expect(page.getByRole('heading',{name:'Ready for review',exact:true})).toBeVisible();
 const progress=await(await page.request.get(`/api/v1/tasks/${taskId}/progress`,{headers})).json();
 expect(progress.task.graphId).toBe(graphId);expect(progress.result.summary).toContain('Fixture tool outcome:');
 const workspace=progress.workspaces[0].path;expect(paths.has(workspace)).toBe(false);paths.add(workspace);
 expect(await readFile(join(workspace,'prompt-result.txt'),'utf8')).toBe('Reviewed prompt fixture change\n');
 const projects=await(await page.request.get('/api/v1/projects',{headers})).json();const project=projects.find((p:{id:string})=>p.id===progress.task.projectId);
 expect(workspace).not.toBe(project.path);expect((await git('git',['show',`${project.head}:README.md`],{cwd:project.path,env:{PATH:process.env.PATH,GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null'}})).stdout).toBe('Public import fixture\n');
 expect((await git('git',['rev-parse','HEAD'],{cwd:project.path,env:{PATH:process.env.PATH,GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null'}})).stdout.trim()).toBe(project.head);await expect(readFile(join(project.path,'prompt-result.txt'))).rejects.toThrow();
 expect(progress.decisions.filter((a:{kind:string})=>a.kind==='tool')).toHaveLength(mode==='ask'?1:0);
 await page.getByRole('button',{name:'Inspect changes',exact:true}).click();await expect(page.getByRole('region',{name:'Review task evidence'})).toContainText('prompt-result.txt');
 const original=await(await page.request.get(`/api/v1/graphs/${graphId}`,{headers})).json();expect(original.record.version).toBe(1);expect(original.graph.spec.nodes[0].tools).toEqual(graph.spec.nodes[0].tools);
 if(mode==='full'){
  const next=await(await page.request.post('/api/v1/tasks',{headers,data:{title:'Preference review',request:'Review suggestion',outcome:'freeform',graphId,graphVersion:1,nodeId:'worker',resultNodeId:'worker',projectId:project.id,baseCommit:project.head}})).json();
  await page.goto(`/#/tasks/${next.id}`);await expect(auth).toContainText('Project suggestion: full');await expect(auth).toContainText('Ask for approval (default)');
  expect((await(await page.request.get(`/api/v1/tasks/${next.id}/authorization`,{headers})).json()).record).toBeUndefined();
 }

});

test('Full access cannot grant an unavailable tool and project preference stays advisory',async({page})=>{
 const graphId='prompt-denied';
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:graphId},spec:{nodes:[{id:'worker',runtime:{kind:'native'},model:{provider:'e2e',name:'fixture'},prompt:'prompt-journey-fixture deny-tool',loop:{mode:'once'},tools:[{name:'filesystem.write'}]}]}}})).status()).toBe(201);
 const draft=await(await page.request.post('/api/v1/tasks',{headers,data:{title:'Denied tool',request:'Attempt fixture tool',outcome:'freeform',graphId,graphVersion:1,nodeId:'worker',resultNodeId:'worker'}})).json();
 const consent=await(await page.request.get(`/api/v1/tasks/${draft.id}/authorization`,{headers})).json();
 expect((await page.request.put(`/api/v1/tasks/${draft.id}/authorization`,{headers:{...headers,'If-Match':'0'},data:{mode:'full',scope:consent.scope,confirm:true}})).status()).toBe(200);
 expect((await page.request.post(`/api/v1/tasks/${draft.id}/start`,{headers:{...headers,'If-Match':String(draft.revision)},data:{confirm:true}})).status()).toBe(202);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto(`/#/tasks/${draft.id}`);
 await expect(page.getByRole('heading',{name:'Ready for review',exact:true})).toBeVisible();await expect(page.getByRole('article',{name:'Task progress'})).toContainText('denied');
 const progress=await(await page.request.get(`/api/v1/tasks/${draft.id}/progress`,{headers})).json();expect(progress.pendingApprovals).toHaveLength(0);
 // An unrelated project suggestion is not applied to this task.
 const auth=await(await page.request.get(`/api/v1/tasks/${draft.id}/authorization`,{headers})).json();expect(auth.preference).toBeUndefined();
});
