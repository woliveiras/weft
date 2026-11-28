import {expect,test} from '@playwright/test';

const headers={Authorization:'Bearer e2e-admin-token'};

test('launches a new task through visible scope review without interpretation or separate save controls',async({page})=>{
 const graphId='simple-launch-new';
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:graphId,name:'Simple launch'},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'once'}}]}}})).status()).toBe(201);
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto('/#/tasks/new');
 await page.getByRole('textbox',{name:'Request',exact:true}).fill('Explain this fixture without changing files.');
 await page.getByRole('combobox',{name:'Workflow',exact:true}).selectOption(graphId);
 const mutations:string[]=[];
 page.on('request',request=>{if(request.method()!=='GET'&&request.url().includes('/api/v1/'))mutations.push(new URL(request.url()).pathname);});
 await page.getByRole('button',{name:'Run task',exact:true}).click();
 await expect(page.getByRole('region',{name:'Launch review',exact:true})).toBeVisible();
 expect(mutations).toEqual(['/api/v1/tasks']);
 const taskId=page.url().match(/#\/tasks\/([^?]+)/)?.[1];
 expect(taskId).toBeTruthy();
 const draft=await(await page.request.get(`/api/v1/tasks/${taskId}`,{headers})).json();
 expect(draft.status).toBe('draft');expect(draft.executionGraphId).toBeUndefined();
 expect((await(await page.request.get(`/api/v1/tasks/${taskId}/authorization`,{headers})).json()).record).toBeUndefined();
 for(const width of [1440,390]){
  await page.setViewportSize({width,height:900});
  await expect(page.getByRole('region',{name:'Launch review',exact:true})).toContainText('Ask');
  await page.getByRole('region',{name:'Launch review',exact:true}).scrollIntoViewIfNeeded();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await page.screenshot({path:test.info().outputPath(`task-launch-review-${width}.png`),fullPage:true});
 }
 await page.getByRole('button',{name:'Run task',exact:true}).click();
 await expect(page.getByRole('region',{name:'Task status',exact:true})).toContainText('Task status: succeeded');
 expect(mutations.filter(path=>path.endsWith('/start'))).toHaveLength(1);
 expect(mutations.some(path=>path.endsWith('/prompt')||path.endsWith('/submit')||path.endsWith('/authorization'))).toBe(false);
 expect(await(await page.request.get(`/api/v1/graphs/${graphId}/executions`,{headers})).json()).toEqual([]);
});

test('runs an unchanged saved task with one explicit click and retains mandatory approvals',async({page})=>{
 const graphId='simple-launch-saved';
 expect((await page.request.post('/api/v1/graphs',{headers,data:{metadata:{id:graphId},spec:{nodes:[{id:'worker',runtime:{kind:'echo'},loop:{mode:'event'}}],channels:[{id:'requests',to:['worker'],approval:true,types:['task.request']}]}}})).status()).toBe(201);
 const created=await page.request.post('/api/v1/tasks',{headers,data:{title:'Ready saved task',request:'Keep the manual approval',outcome:'freeform',graphId,graphVersion:1,nodeId:'worker',resultNodeId:'worker',channelId:'requests',messageType:'task.request',payloadJson:'{}'}});
 expect(created.status()).toBe(201);const draft=await created.json();
 await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
 await page.goto(`/#/tasks/${draft.id}`);
 await expect(page.getByRole('region',{name:'Launch review',exact:true})).toContainText('Ask');
 const before=await(await page.request.get(`/api/v1/tasks/${draft.id}`,{headers})).json();
 expect(before.status).toBe('draft');expect(before.revision).toBe(draft.revision);
 let starts=0;page.on('request',request=>{if(request.method()==='POST'&&request.url().endsWith(`/tasks/${draft.id}/start`))starts++;});
 await page.getByRole('button',{name:'Run task',exact:true}).focus();await page.keyboard.press('Enter');
 const decisions=page.getByRole('region',{name:'Task decisions',exact:true});
 await expect(decisions.getByRole('button',{name:'Approve',exact:true})).toBeVisible();
 expect(starts).toBe(1);
 const waiting=await(await page.request.get(`/api/v1/tasks/${draft.id}`,{headers})).json();
 expect(waiting.status).not.toBe('succeeded');
 await decisions.getByRole('button',{name:'Approve',exact:true}).click();
 await expect(page.getByRole('region',{name:'Task status',exact:true})).toContainText('Task status: succeeded');
});
