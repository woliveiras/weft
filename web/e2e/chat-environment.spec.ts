import {expect,test} from '@playwright/test';

test('reviews a task environment, observes readiness and keeps agents stopped',async ({page,request})=>{
  const headers={Authorization:'Bearer e2e-admin-token'};
  const graph={metadata:{id:'chat-environment',name:'Sandbox fixture'},spec:{environment:{image:'alpine:3.22'},nodes:[{id:'reader',runtime:{kind:'echo'},isolation:{mode:'docker'},loop:{mode:'once'},tools:[]}]}};
  expect((await request.post('/api/v1/graphs',{headers,data:graph})).ok()).toBe(true);
  const created=await request.post('/api/v1/tasks',{headers,data:{title:'Release sandbox fixture',request:'Check release notes.',outcome:'review',graphId:'chat-environment',graphVersion:1,nodeId:'reader',resultNodeId:'reader',executionMode:'sandbox'}});
  expect(created.ok()).toBe(true);
  const task=await created.json();
  const settings=await (await request.get('/api/v1/chat/settings',{headers})).json();
  expect((await request.put('/api/v1/chat/settings',{headers:{...headers,'If-Match':String(settings.revision)},data:{provider:'e2e',name:'fixture'}})).ok()).toBe(true);
  await page.addInitScript(()=>localStorage.setItem('weftToken','e2e-admin-token'));
  await page.goto('/');
  await page.getByRole('textbox',{name:'Message',exact:true}).fill('Prepare the release sandbox task environment.');
  await page.getByRole('button',{name:'Send',exact:true}).click();
  const review=page.getByRole('region',{name:'Prepare task environment'});
  await expect(review).toContainText('alpine:3.22');
  await review.getByRole('button',{name:'Prepare environment',exact:true}).click();
  await expect(review).toContainText('Environment ready for the reviewed definition.');
  const progress=await (await request.get('/api/v1/tasks/'+task.id+'/progress',{headers})).json();
  expect(progress.task.status).toBe('environment_pending');
  expect(progress.executions).toHaveLength(0);
  await review.getByRole('link',{name:'Inspect task'}).click();
  await expect(page).toHaveURL(new RegExp(task.id));
});
