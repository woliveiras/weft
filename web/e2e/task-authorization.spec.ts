import {test,expect} from '@playwright/test';
const headers={Authorization:'Bearer e2e-admin-token'};
test('reviews task scope, confirms modes and preserves consent across reload',async({page})=>{
 const graph={metadata:{id:'authorization-flow'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'event'},tools:[]}],channels:[{id:'requests',to:['worker'],approval:true}]}};
 expect((await page.request.post('/api/v1/graphs',{headers,data:graph})).status()).toBe(201);
 const res=await page.request.post('/api/v1/tasks',{headers,data:{title:'Consent test',request:'Read the fixture',outcome:'freeform',graphId:'authorization-flow',graphVersion:1,nodeId:'worker',resultNodeId:'worker',channelId:'requests',messageType:'task.request',payloadJson:'{}'}});expect(res.status()).toBe(201);const task=await res.json();
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));await page.goto(`/#/tasks/${task.id}`);
 await page.locator('summary').filter({hasText:/^Access settings/}).click();
 const section=page.getByRole('region',{name:'Task authorization'});
 for(const mode of ['workspace','ask','full']){
  await section.getByLabel('Authorization mode',{exact:true}).selectOption(mode);
  await expect(section.getByRole('button',{name:'Confirm authorization'})).toBeDisabled();
  await section.getByRole('checkbox').check();await section.getByRole('button',{name:'Confirm authorization'}).click();
  await expect(section).toContainText(`Current mode: ${mode}`);await page.reload();await page.locator('summary').filter({hasText:/^Access settings/}).click();await expect(section).toContainText(`Current mode: ${mode}`);
 }
 await expect(page.getByRole('region',{name:'Launch review',exact:true})).toContainText('Full access');
 await page.getByRole('button',{name:'Run task',exact:true}).click();
 await expect(page.getByRole('heading',{name:'Needs your decision',exact:true})).toBeVisible();
 await page.getByRole('link',{name:'Settings & access',exact:true}).click();
 await section.getByLabel('Authorization mode',{exact:true}).selectOption('ask');await section.getByRole('checkbox').check();await section.getByRole('button',{name:'Confirm authorization'}).click();await expect(section).toContainText('Current mode: ask');
 await page.reload();await expect(section).toContainText('Current mode: ask');
 await page.getByRole('region',{name:'Task decisions'}).getByRole('button',{name:'Approve',exact:true}).click();
 await expect(page.getByRole('region',{name:'Task status',exact:true})).toContainText('Task status: succeeded');
 const auth=await (await page.request.get(`/api/v1/tasks/${task.id}/authorization`,{headers})).json();
 expect((await page.request.put(`/api/v1/tasks/${task.id}/authorization`,{headers:{...headers,'If-Match':String(auth.record.revision)},data:{mode:'full',scope:auth.scope,confirm:true}})).status()).toBe(409);
});

test('explains native shell restrictions when Workspace consent is rejected', async ({page}) => {
 const graph = {
  metadata: {id: 'authorization-native-shell'},
  spec: {nodes: ['process', 'kubernetes'].map(mode => ({
   id: mode,
   runtime: {kind: 'echo'},
   loop: {mode: 'once'},
   isolation: {mode},
   tools: [{name: 'check.run'}],
  }))},
 };
 expect((await page.request.post('/api/v1/graphs', {headers, data: graph})).status()).toBe(201);
 const res = await page.request.post('/api/v1/tasks', {headers, data: {
  title: 'Native shell consent',
  request: 'Read the fixture',
  outcome: 'freeform',
  graphId: graph.metadata.id,
  graphVersion: 1,
  nodeId: 'process',
 }});
 expect(res.status()).toBe(201);
 const task = await res.json();
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto(`/#/tasks/${task.id}`);
 await page.locator('summary').filter({hasText:/^Access settings/}).click();
 const section = page.getByRole('region', {name: 'Task authorization'});
 await section.getByLabel('Authorization mode',{exact:true}).selectOption('workspace');
 await expect(section).toContainText('Process and Kubernetes isolation both use native shell');
 await section.getByRole('checkbox').check();
 await section.getByRole('button', {name: 'Confirm authorization'}).click();
 await expect(section).toContainText('use Ask for approval, Full access or Docker isolation');
 await expect(section).toContainText('Current mode: Ask for approval (default)');
});
